package ffi

import (
	"context"
	"errors"

	"github.com/filecoin-project/go-jsonrpc"
)

// LocalPoRepBackendUnavailable identifies only a known C2 worker failure.
// Vanilla transport errors, invalid proofs and generic child failures are not
// evidence that this worker's execution backend is unavailable.
type LocalPoRepBackendUnavailable struct{ Cause error }

func (e *LocalPoRepBackendUnavailable) Error() string { return e.Cause.Error() }
func (e *LocalPoRepBackendUnavailable) Unwrap() error { return e.Cause }

func classifyLocalPoRepC2Error(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	message := err.Error()
	var rpc *jsonrpc.JSONRPCError
	if errors.As(err, &rpc) {
		message = rpc.Message
	}
	if message == "No CUDA devices available" {
		return &LocalPoRepBackendUnavailable{Cause: err}
	}
	return err
}
