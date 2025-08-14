package main

import (
	"golang.org/x/xerrors"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/lib/supraffi"
)

var testSupraCmd = &cli.Command{
	Name:  "supra",
	Usage: translations.T("Supra consensus testing utilities"),
	Subcommands: []*cli.Command{
		testSupraTreeRFileCmd,
	},
}

var testSupraTreeRFileCmd = &cli.Command{
	Name:  "tree-r-file",
	Usage: "Test tree-r-file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "last-layer-filename",
			Usage: "Last layer filename",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "data-filename",
			Usage: "Data filename",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "output-dir",
			Usage: "Output directory",
			Required: true,
		},
		&cli.Uint64Flag{
			Name:  "sector-size",
			Usage: "Sector size",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		res := supraffi.TreeRFile(cctx.String("last-layer-filename"), cctx.String("data-filename"), cctx.String("output-dir"), cctx.Uint64("sector-size"))
		if res != 0 {
			return xerrors.Errorf("tree-r-file failed: %d", res)
		}
		return nil
	},
}
