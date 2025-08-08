package main

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
)

var testDebugCmd = &cli.Command{
	Name:  "debug",
	Usage: translations.T("Collection of debugging utilities"),
	Subcommands: []*cli.Command{
		testDebugIpniChunks,
		debugSNSvc,
		proofsvcClientCmd,
	},
}
