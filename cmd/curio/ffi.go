package main

import (
	"fmt"
	"os"
	"reflect"

	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/ffiselect/ffidirect"
	"github.com/filecoin-project/go-jsonrpc"
  
	"github.com/ipfs/go-cid"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
	"github.com/snadrus/must"
	"golang.org/x/net/context"
)

var ffiCmd = &cli.Command{
	Name:   "ffi",
	Hidden: true,
	Flags: []cli.Flag{
		layersFlag,
	},
	Action: func(cctx *cli.Context) (err error) {
		output := os.NewFile(uintptr(3), "out")

		srv := jsonrpc.NewServer()
		srv.Register("FFI", &ffidirect.FFI{})
		srv.HandleRequest(cctx.Context, os.Stdin, output)
		return nil
	},
}

func ffiSelfTest() {
	val1, val2 := 12345678, must.One(cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"))
	ret2, err := ffiselect.FFISelect.SelfTest(ffiselect.WithLogCtx(context.Background(), "test", "val"), val1, val2)
	if err != nil {
		panic("ffi self test failed:" + err.Error())
	}
	if !val2.Equals(ret2) {
		panic(fmt.Sprint("ffi self test failed: values do not match: ", val1, val2, ret2))
	}
}
