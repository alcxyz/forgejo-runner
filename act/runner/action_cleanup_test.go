package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRemoveDockerActionContainerAfterCancellation(t *testing.T) {
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "action-value"))
	cancel()
	require.ErrorIs(t, ctx.Err(), context.Canceled)

	container := &containerMock{}
	called := 0
	container.On("Remove").Return(func(cleanupCtx context.Context) error {
		called++
		require.NoError(t, cleanupCtx.Err())
		require.Equal(t, "action-value", cleanupCtx.Value(contextKey{}))
		deadline, ok := cleanupCtx.Deadline()
		require.True(t, ok, "cleanup must have a deadline")
		remaining := time.Until(deadline)
		require.Greater(t, remaining, time.Duration(0))
		require.LessOrEqual(t, remaining, time.Minute)
		return nil
	}).Once()

	require.NoError(t, removeDockerActionContainer(container, false)(ctx))
	require.Equal(t, 1, called)
	container.AssertExpectations(t)
}

func TestRemoveDockerActionContainerRespectsReuse(t *testing.T) {
	container := &containerMock{}
	require.NoError(t, removeDockerActionContainer(container, true)(t.Context()))
	container.AssertNotCalled(t, "Remove")
}

func TestRemoveDockerActionContainerReturnsRemovalError(t *testing.T) {
	container := &containerMock{}
	want := errors.New("remove failed")
	container.On("Remove").Return(func(ctx context.Context) error {
		return want
	}).Once()
	require.ErrorIs(t, removeDockerActionContainer(container, false)(t.Context()), want)
	container.AssertExpectations(t)
}
