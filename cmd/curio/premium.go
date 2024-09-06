package main

import (
	"fmt"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/premium"
	"github.com/urfave/cli/v2"
)

var premiumCmd = &cli.Command{
	Name:  "premium",
	Usage: "View premium information",
	Subcommands: []*cli.Command{
		{
			Name:   "id",
			Usage:  "Get the current premium ID. Ensure all miner addresses are in the base layer.",
			Action: getID,
			Flags: []cli.Flag{
				&cli.StringSliceFlag{
					Name:    "layers",
					Usage:   "list of layers to be interpreted (atop defaults). Includes: base",
					EnvVars: []string{"CURIO_LAYERS"},
					Aliases: []string{"l", "layer"},
				},
			},
		},
	},
}

func getID(cctx *cli.Context) error {
	db, err := deps.MakeDB(cctx)
	if err != nil {
		return err
	}

	layers := append([]string{"base"}, cctx.StringSlice("layers")...)
	cfg, err := deps.GetConfig(cctx.Context, layers, db)
	if err != nil {
		return err
	}
	fmt.Println(premium.GetCurrentID(cfg))
	return nil
}
