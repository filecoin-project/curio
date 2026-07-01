package main

import (
	_ "net/http/pprof"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/gracehttpsvc"
	"github.com/filecoin-project/curio/lib/reqcontext"
)

var restartCmd = &cli.Command{
	Name:  "restart",
	Usage: translations.T("Gracefully restart a running Curio process"),
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "local",
			Usage: translations.T("restart via local pid file instead of the RPC API"),
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Bool("local") {
			repoPath, err := homedir.Expand(cctx.String(deps.FlagRepoPath))
			if err != nil {
				return err
			}
			return gracehttpsvc.RestartFromPIDFile(filepath.Join(repoPath, "curio.pid"))
		}

		api, closer, err := rpc.GetCurioAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		return api.Restart(reqcontext.ReqContext(cctx))
	},
}
