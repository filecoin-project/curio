package main

import (
	_ "net/http/pprof"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/cmd/curio/rpc"
	"github.com/filecoin-project/curio/lib/reqcontext"
)

var stopCmd = &cli.Command{
	Name:  "stop",
	Usage: "Stop a running Curio process",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		api, closer, err := rpc.GetCurioAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		err = api.Shutdown(reqcontext.ReqContext(cctx))
		if err != nil {
			return err
		}

		return nil
	},
}
