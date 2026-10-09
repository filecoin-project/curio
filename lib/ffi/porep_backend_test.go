package ffi

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-jsonrpc"
)

func TestLocalPoRepBackendClassification(t *testing.T) {
	for _, err := range []error{errors.New("No CUDA devices available"), &jsonrpc.JSONRPCError{Code: -32000, Message: "No CUDA devices available"}} {
		var backend *LocalPoRepBackendUnavailable
		require.ErrorAs(t, classifyLocalPoRepC2Error(context.Background(), err), &backend)
	}
	for _, err := range []error{nil, io.ErrUnexpectedEOF, context.Canceled, errors.New("invalid proof"), errors.New("exit status 1"), errors.New("remote source: No CUDA devices available")} {
		var backend *LocalPoRepBackendUnavailable
		require.False(t, errors.As(classifyLocalPoRepC2Error(context.Background(), err), &backend))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cause := errors.New("No CUDA devices available")
	require.Same(t, cause, classifyLocalPoRepC2Error(ctx, cause))
}
