package ffi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/ffiselect"
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

type localC2Error struct{ err error }

func (s *localC2Error) SealCommitPhase2([]byte, abi.SectorNumber, abi.ActorID) ([]byte, error) {
	return nil, s.err
}

func TestLocalPoRepBackendRPCClassification(t *testing.T) {
	for _, message := range []string{"No CUDA devices available", "remote source: No CUDA devices available"} {
		t.Run(message, func(t *testing.T) {
			// Substitute only the native result; use the same server/client codec
			// and FFI method signature as the local C2 subprocess boundary.
			server := jsonrpc.NewServer()
			server.Register("FFI", &localC2Error{err: errors.New(message)})
			client := ffiselect.FFISelect
			closeClient, err := jsonrpc.NewCustomClient("FFI", []any{&client}, func(ctx context.Context, body []byte) (io.ReadCloser, error) {
				var output bytes.Buffer
				server.HandleRequest(ctx, bytes.NewReader(body), &output)
				return io.NopCloser(&output), nil
			})
			require.NoError(t, err)
			defer closeClient()

			_, err = client.SealCommitPhase2(context.Background(), nil, 1, 1000)
			var rpc *jsonrpc.JSONRPCError
			require.ErrorAs(t, err, &rpc)
			require.Equal(t, message, rpc.Message)
			var backend *LocalPoRepBackendUnavailable
			classified := classifyLocalPoRepC2Error(context.Background(), err)
			if message == "No CUDA devices available" {
				require.ErrorAs(t, classified, &backend)
				require.Same(t, err, backend.Cause)
			} else {
				require.Same(t, err, classified)
				require.False(t, errors.As(classified, &backend))
			}
		})
	}
}
