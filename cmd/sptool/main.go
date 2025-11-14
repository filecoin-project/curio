package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/build"

	"github.com/filecoin-project/lotus/cli/spcli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

var log = logging.Logger("sptool")

func main() {
	local := []*cli.Command{
		actorCmd,
		spcli.InfoCmd(SPTActorGetter),
		sectorsCmd,
		provingCmd,
		toolboxCmd,
	}

	app := &cli.App{
		Name:                 "sptool",
		Usage:                "Manage Filecoin Miner Actor",
		Version:              build.UserVersion(),
		EnableBashCompletion: true,
		Commands:             local,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repo",
				EnvVars: []string{"LOTUS_PATH"},
				Hidden:  true,
				Value:   "~/.lotus", // TODO: Consider XDG_DATA_HOME
			},
			&cli.StringFlag{
				Name:  "log-level",
				Value: "info",
			},
			&cli.StringFlag{
				Name:     "actor",
				Required: os.Getenv("LOTUS_DOCS_GENERATION") != "1",
				Usage:    "miner actor to manage",
				EnvVars:  []string{"SP_ADDRESS"},
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Usage:   "enable verbose logging",
				Aliases: []string{"vv"},
			},
		},
		Before: func(cctx *cli.Context) error {
			if cctx.IsSet("verbose") {
				cliutil.IsVeryVerbose = true
				return logging.SetLogLevel("sptool", "DEBUG")
			}
			return logging.SetLogLevel("sptool", "INFO")
		},
	}

	// terminate early on ctrl+c
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-c
		cancel()
		fmt.Println("Received interrupt, shutting down... Press CTRL+C again to force shutdown")
		<-c
		fmt.Println("Forcing stop")
		os.Exit(1)
	}()

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Errorf("%+v", err)
		os.Exit(1)
		return
	}

}

func SPTActorGetter(cctx *cli.Context) (address.Address, error) {
	addr, err := address.NewFromString(cctx.String("actor"))
	if err != nil {
		return address.Undef, fmt.Errorf("parsing address: %w", err)
	}
	return addr, nil
}

var toolboxCmd = &cli.Command{
	Name:  "toolbox",
	Usage: "some tools to fix some problems",
	Subcommands: []*cli.Command{
		sparkCmd,
		mk12Clientcmd,
		mk20Clientcmd,
	},
}
