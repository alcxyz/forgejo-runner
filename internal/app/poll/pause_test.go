// Copyright 2026 alcxyz.
// SPDX-License-Identifier: GPL-3.0-or-later

package poll

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"connectrpc.com/connect"

	runnerv1 "code.forgejo.org/forgejo/actions-proto/runner/v1"
	"code.forgejo.org/forgejo/runner/v13/internal/app/run"
	mock_runner "code.forgejo.org/forgejo/runner/v13/internal/app/run/mocks"
	"code.forgejo.org/forgejo/runner/v13/internal/pkg/client"
	"code.forgejo.org/forgejo/runner/v13/internal/pkg/client/mocks"
	"code.forgejo.org/forgejo/runner/v13/internal/pkg/config"

	gouuid "github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPauseStateReportsChanges(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paused")
	var sent []string
	state := pauseState{file: file, notify: func(status string) error {
		sent = append(sent, status)
		return nil
	}}

	assert.False(t, state.update(1))
	assert.Empty(t, sent, "an unpaused runner reports nothing")

	require.NoError(t, os.WriteFile(file, nil, 0o644))
	assert.True(t, state.update(1))
	assert.True(t, state.update(1))
	assert.True(t, state.update(0))
	assert.Equal(t, []string{"paused tasks=1", "paused tasks=0"}, sent)

	require.NoError(t, os.Remove(file))
	assert.False(t, state.update(0))
	assert.Equal(t, []string{"paused tasks=1", "paused tasks=0", ""}, sent)
}

func TestPauseStateRetriesAFailedReport(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paused")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	calls := 0
	state := pauseState{file: file, notify: func(string) error {
		calls++
		if calls == 1 {
			return errors.New("no socket")
		}
		return nil
	}}
	assert.True(t, state.update(0))
	assert.True(t, state.update(0))
	assert.True(t, state.update(0))
	assert.Equal(t, 2, calls)
}

func TestPauseStateWithoutAFile(t *testing.T) {
	var state pauseState
	assert.False(t, state.update(3))
}

func TestSdNotifyStatus(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socket, Net: "unixgram"})
	require.NoError(t, err)
	defer conn.Close()

	t.Setenv("NOTIFY_SOCKET", socket)
	require.NoError(t, sdNotifyStatus("paused tasks=2"))
	buffer := make([]byte, 64)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	n, err := conn.Read(buffer)
	require.NoError(t, err)
	assert.Equal(t, "STATUS=paused tasks=2", string(buffer[:n]))

	t.Setenv("NOTIFY_SOCKET", "")
	assert.NoError(t, sdNotifyStatus("ignored"))
}

// A paused poller fetches nothing, lets its running task finish and reports
// that, and fetches again once the pause ends.
func TestPollerPause(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paused")

	synctest.Test(t, func(t *testing.T) {
		pollingCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		release := make(chan struct{})
		statuses := make(chan string, 8)

		mockClient := mocks.NewMockClient(t)
		mockClient.On("FetchInterval").Return(1 * time.Second)
		mockClient.On("Address").Return("https://client")
		mockClient.On("SetRequestKey", mock.Anything).Return(func() {})
		mockClient.On("FetchTask", mock.Anything, mock.Anything).Return(connect.NewResponse(&runnerv1.FetchTaskResponse{
			Task:         &runnerv1.Task{},
			TasksVersion: int64(1),
		}), nil).Once()
		mockClient.On("FetchTask", mock.Anything, mock.Anything).Return(connect.NewResponse(&runnerv1.FetchTaskResponse{
			TasksVersion: int64(1),
		}), nil)

		mockRunner := mock_runner.NewMockRunner(t)
		mockRunner.On("Run", mock.Anything, mock.Anything).Run(func(mock.Arguments) {
			<-release
		}).Once()

		// Two slots: the running task leaves one that a pause must keep empty.
		p := New(pollingCtx, &config.Config{
			Runner: config.Runner{Capacity: 2},
		}, []client.Client{mockClient}, []run.RunnerInterface{mockRunner}).(*poller)
		p.pause = pauseState{file: file, notify: func(status string) error {
			statuses <- status
			return nil
		}}
		go p.Poll()

		time.Sleep(1500 * time.Millisecond)
		mockClient.AssertNumberOfCalls(t, "FetchTask", 2)

		require.NoError(t, os.WriteFile(file, nil, 0o644))
		time.Sleep(1 * time.Second)
		assert.Equal(t, "paused tasks=1", <-statuses)

		time.Sleep(5 * time.Second)
		mockClient.AssertNumberOfCalls(t, "FetchTask", 2)

		close(release)
		time.Sleep(1 * time.Second)
		assert.Equal(t, "paused tasks=0", <-statuses)
		mockClient.AssertNumberOfCalls(t, "FetchTask", 2)

		require.NoError(t, os.Remove(file))
		time.Sleep(1 * time.Second)
		assert.Empty(t, <-statuses)
		mockClient.AssertNumberOfCalls(t, "FetchTask", 3)

		require.NoError(t, p.Shutdown(t.Context()))
	})
}

// A fetch that failed may hold a task on the server. A pause must retry it with
// the same request key and count it as work, or a restart would abandon the task.
func TestPollerPauseResolvesAFailedFetch(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paused")

	synctest.Test(t, func(t *testing.T) {
		pollingCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		release := make(chan struct{})
		statuses := make(chan string, 8)
		var keys []gouuid.UUID

		mockClient := mocks.NewMockClient(t)
		mockClient.On("FetchInterval").Return(1 * time.Second)
		mockClient.On("Address").Return("https://client")
		mockClient.On("SetRequestKey", mock.Anything).Run(func(args mock.Arguments) {
			keys = append(keys, args.Get(0).(gouuid.UUID))
		}).Return(func() {})
		// The pause arrives during the failed fetch, whose response is lost twice.
		mockClient.On("FetchTask", mock.Anything, mock.Anything).Run(func(mock.Arguments) {
			require.NoError(t, os.WriteFile(file, nil, 0o644))
		}).Return(nil, errors.New("connection lost")).Once()
		mockClient.On("FetchTask", mock.Anything, mock.Anything).Return(nil, errors.New("connection lost")).Once()
		mockClient.On("FetchTask", mock.Anything, mock.Anything).Return(connect.NewResponse(&runnerv1.FetchTaskResponse{
			Task:         &runnerv1.Task{},
			TasksVersion: int64(1),
		}), nil).Once()

		mockRunner := mock_runner.NewMockRunner(t)
		mockRunner.On("Run", mock.Anything, mock.Anything).Run(func(mock.Arguments) {
			<-release
		}).Once()

		p := New(pollingCtx, &config.Config{
			Runner: config.Runner{Capacity: 2},
		}, []client.Client{mockClient}, []run.RunnerInterface{mockRunner}).(*poller)
		p.pause = pauseState{file: file, notify: func(status string) error {
			statuses <- status
			return nil
		}}
		go p.Poll()

		time.Sleep(2500 * time.Millisecond)
		assert.Equal(t, "paused tasks=1", <-statuses, "the unresolved fetch counts as work")
		mockClient.AssertNumberOfCalls(t, "FetchTask", 3)
		assert.Equal(t, keys[0], keys[1])
		assert.Equal(t, keys[0], keys[2])

		// The task it held runs, and nothing more is fetched while paused.
		time.Sleep(5 * time.Second)
		mockClient.AssertNumberOfCalls(t, "FetchTask", 3)
		assert.Empty(t, statuses, "the count stays at one")

		close(release)
		time.Sleep(1 * time.Second)
		assert.Equal(t, "paused tasks=0", <-statuses)

		require.NoError(t, p.Shutdown(t.Context()))
	})
}

func TestPauseStatePausesOnLookupError(t *testing.T) {
	for _, previouslyPaused := range []bool{false, true} {
		t.Run(fmt.Sprintf("previouslyPaused=%t", previouslyPaused), func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "control")
			require.NoError(t, os.Mkdir(parent, 0o755))
			file := filepath.Join(parent, "paused")
			var sent []string
			state := pauseState{file: file, notify: func(status string) error {
				sent = append(sent, status)
				return nil
			}}
			if previouslyPaused {
				require.NoError(t, os.WriteFile(file, nil, 0o644))
				assert.True(t, state.update(1))
				require.NoError(t, os.Remove(file))
			}
			require.NoError(t, os.Remove(parent))
			require.NoError(t, os.WriteFile(parent, nil, 0o644))
			_, err := os.Lstat(file)
			require.Error(t, err)
			require.False(t, errors.Is(err, os.ErrNotExist))
			assert.True(t, state.update(1))
			assert.Equal(t, []string{"paused tasks=1"}, sent)

			require.NoError(t, os.Remove(parent))
			require.NoError(t, os.Mkdir(parent, 0o755))
			assert.False(t, state.update(0))
			assert.Equal(t, []string{"paused tasks=1", ""}, sent)
		})
	}
}
