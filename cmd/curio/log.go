package main

import (
	"fmt"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
	"github.com/filecoin-project/curio/lib/reqcontext"
)

var logCmd = &cli.Command{
	Name:  "log",
	Usage: translations.T("Manage logging"),
	Subcommands: []*cli.Command{
		LogList,
		LogSetLevel,
	},
}

var LogList = &cli.Command{
	Name:  "list",
	Usage: translations.T("List log systems"),
	Action: func(cctx *cli.Context) error {
		minerApi, closer, err := rpc.GetCurioAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := reqcontext.ReqContext(cctx)

		systems, err := minerApi.LogList(ctx)
		if err != nil {
			return err
		}

		for _, system := range systems {
			fmt.Println(system)
		}

		return nil
	},
}

var LogSetLevel = &cli.Command{
	Name:      "set-level",
	Usage:     translations.T("Set log level"),
	ArgsUsage: translations.T("[level]"),
	Description: translations.T(`Set the log level for logging systems:

   The system flag can be specified multiple times.

   eg) log set-level --system chain --system chainxchg debug

   Available Levels:
   debug
   info
   warn
   error

   Environment Variables:
   GOLOG_LOG_LEVEL - Default log level for all log systems
   GOLOG_LOG_FMT   - Change output log format (json, nocolor)
   GOLOG_FILE      - Write logs to file
   GOLOG_OUTPUT    - Specify whether to output to file, stderr, stdout or a combination, i.e. file+stderr
`),
	Flags: []cli.Flag{
		&cli.StringSliceFlag{
			Name:  "system",
			Usage: translations.T("limit to log system"),
			Value: &cli.StringSlice{},
		},
	},
	Action: func(cctx *cli.Context) error {
		minerApi, closer, err := rpc.GetCurioAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := reqcontext.ReqContext(cctx)

		if !cctx.Args().Present() {
			return fmt.Errorf("level is required")
		}

		systems := cctx.StringSlice("system")
		if len(systems) == 0 {
			var err error
			systems, err = minerApi.LogList(ctx)
			if err != nil {
				return err
			}
		}

		for _, system := range systems {
			if err := minerApi.LogSetLevel(ctx, system, cctx.Args().First()); err != nil {
				return xerrors.Errorf("setting log level on %s: %v", system, err)
			}
		}

		return nil
	},
}
