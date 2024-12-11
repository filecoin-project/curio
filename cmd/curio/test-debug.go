package main

import (
	"github.com/urfave/cli/v2"
)

var testDebugCmd = &cli.Command{
	Name:  "debug",
	Usage: "Collection of debugging utilities",
	Subcommands: []*cli.Command{
		testDebugIpniChunks,
	},
}
