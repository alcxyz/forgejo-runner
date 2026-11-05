// Copyright 2026 alcxyz.
// SPDX-License-Identifier: GPL-3.0-or-later

package poll

import (
	"errors"
	"fmt"
	"net"
	"os"

	log "github.com/sirupsen/logrus"
)

// pauseFileEnv names a file that pauses the daemon poller while it exists: the runner
// fetches no new tasks and lets the ones it runs finish, with no time limit.
const pauseFileEnv = "FORGEJO_RUNNER_PAUSE_FILE"

// pauseState reports a pause through the service manager's status text, as
// "paused tasks=<running>", and clears the text when the pause ends. Its
// methods run while fetchMutex is held, so no fetch is in flight, and the count
// includes failed fetches that may still hold a task on the server, so zero
// means that the runner has nothing to do.
type pauseState struct {
	file   string
	notify func(status string) error
	last   string
}

func newPauseState() pauseState {
	return pauseState{file: os.Getenv(pauseFileEnv), notify: sdNotifyStatus}
}

// update reports whether the runner is paused, and tells the service manager
// when the running count or the pause itself changes.
func (s *pauseState) update(running int64) bool {
	if s.file == "" {
		return false
	}
	_, err := os.Lstat(s.file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.WithError(err).Warn("[poller] could not check the pause file")
	}
	paused := !errors.Is(err, os.ErrNotExist)
	status := ""
	if paused {
		status = fmt.Sprintf("paused tasks=%d", running)
	}
	if status != s.last {
		log.Infof("[poller] pause status: %q", status)
		if err := s.notify(status); err != nil {
			log.WithError(err).Warn("[poller] could not report the pause status")
		} else {
			s.last = status
		}
	}
	return paused
}

// sdNotifyStatus sends the status text to NOTIFY_SOCKET, if there is one.
func sdNotifyStatus(status string) error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return nil
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte("STATUS=" + status))
	return err
}
