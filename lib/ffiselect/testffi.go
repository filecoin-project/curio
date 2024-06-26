package ffiselect

import (
	"bytes"
	"context"
	"io"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/lib/ffiselect/ffidirect"
)

func callTest(ctx context.Context, body []byte) (io.ReadCloser, error) {
	var output bytes.Buffer

	srv := jsonrpc.NewServer()
	srv.Register("FFI", &ffidirect.FFI{})
	srv.HandleRequest(ctx, bytes.NewReader(body), &output)

	return io.NopCloser(&output), nil
}
