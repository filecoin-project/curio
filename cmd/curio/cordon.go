package main

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
)

var cordonCmd = &cli.Command{
	Name:  "cordon",
	Usage: translations.T("Cordon a machine, set it to maintenance mode"),
	Action: func(cctx *cli.Context) error {
		api, closer, err := rpc.GetCurioAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		return api.Cordon(cctx.Context)
	},
}

var uncordonCmd = &cli.Command{
	Name:  "uncordon",
	Usage: translations.T("Uncordon a machine, resume scheduling"),
	Action: func(cctx *cli.Context) error {
		api, closer, err := rpc.GetCurioAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		return api.Uncordon(cctx.Context)
	},
}
