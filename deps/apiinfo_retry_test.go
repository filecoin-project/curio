package deps

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/api"
)

func TestErrorIsIn(t *testing.T) {
	types := []error{&jsonrpc.RPCConnectionError{}, &jsonrpc.ErrClient{}}

	require.True(t, ErrorIsIn(&jsonrpc.RPCConnectionError{}, types))
	require.True(t, ErrorIsIn(&api.ChainError{Err: &jsonrpc.RPCConnectionError{}}, types))
	require.True(t, ErrorIsIn(&jsonrpc.ErrClient{}, types))

	require.False(t, ErrorIsIn(context.DeadlineExceeded, types))
	require.False(t, ErrorIsIn(&api.ChainError{Err: context.DeadlineExceeded}, types))
	require.False(t, ErrorIsIn(fmt.Errorf("i/o timeout"), types))
	require.False(t, ErrorIsIn(errors.New("execution reverted"), types))
	require.False(t, ErrorIsIn(nil, types))
}

func TestShouldRetryRPCError(t *testing.T) {
	connErr := &jsonrpc.RPCConnectionError{}

	require.True(t, shouldRetryRPCError(connErr, 1))
	require.True(t, shouldRetryRPCError(connErr, 2))

	require.False(t, shouldRetryRPCError(context.DeadlineExceeded, 1))
	require.True(t, shouldRetryRPCError(context.DeadlineExceeded, 2))
	require.False(t, shouldRetryRPCError(fmt.Errorf("i/o timeout"), 1))
	require.True(t, shouldRetryRPCError(fmt.Errorf("i/o timeout"), 2))
	require.True(t, shouldRetryRPCError(&net.OpError{Op: "read", Err: fmt.Errorf("i/o timeout")}, 2))
	require.False(t, shouldRetryRPCError(errors.New("execution reverted"), 2))
}

func TestRetryDoesNotRetryNonMatchingErrors(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	_, err := Retry(ctx, 5, time.Millisecond, func(error) bool { return false }, func(isRetry bool) (int, error) {
		attempts++
		return 0, context.DeadlineExceeded
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, attempts)
}

func TestRetryFailsOverOnMatchingErrors(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	val, err := Retry(ctx, 5, time.Millisecond, func(error) bool { return true }, func(isRetry bool) (int, error) {
		attempts++
		if attempts < 3 {
			return 0, &jsonrpc.RPCConnectionError{}
		}
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, val)
	require.Equal(t, 3, attempts)
}

func TestPerProviderTryTimeout(t *testing.T) {
	ctx := context.Background()
	require.Equal(t, perProviderRPCTimeout, perProviderTryTimeout(ctx))

	deadlineCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := perProviderTryTimeout(deadlineCtx)
	require.Greater(t, got, time.Duration(0))
	require.LessOrEqual(t, got, 5*time.Second)
}
