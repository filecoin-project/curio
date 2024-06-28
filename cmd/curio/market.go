package main

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/market"
	"github.com/filecoin-project/curio/market/lmrpc"

	lcli "github.com/filecoin-project/lotus/cli"
)

var marketCmd = &cli.Command{
	Name: "market",
	Subcommands: []*cli.Command{
		marketRPCInfoCmd,
		marketSealCmd,
	},
}

var marketRPCInfoCmd = &cli.Command{
	Flags: []cli.Flag{
		&cli.StringSliceFlag{
			Name:  "layers",
			Usage: "list of layers to be interpreted (atop defaults). Default: base",
		},
	},
	Action: func(cctx *cli.Context) error {
		db, err := deps.MakeDB(cctx)
		if err != nil {
			return err
		}

		layers := cctx.StringSlice("layers")

		cfg, err := deps.GetConfig(cctx.Context, layers, db)
		if err != nil {
			return xerrors.Errorf("get config: %w", err)
		}

		ts, err := lmrpc.MakeTokens(cfg)
		if err != nil {
			return xerrors.Errorf("make tokens: %w", err)
		}

		var addrTokens []struct {
			Address string
			Token   string
		}

		for address, s := range ts {
			addrTokens = append(addrTokens, struct {
				Address string
				Token   string
			}{
				Address: address.String(),
				Token:   s,
			})
		}

		sort.Slice(addrTokens, func(i, j int) bool {
			return addrTokens[i].Address < addrTokens[j].Address
		})

		for _, at := range addrTokens {
			fmt.Printf("[lotus-miner/boost compatible] %s %s\n", at.Address, at.Token)
		}

		return nil
	},
	Name: "rpc-info",
}

var marketSealCmd = &cli.Command{
	Name:  "seal",
	Usage: "start sealing a deal sector early",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "actor",
			Usage:    "Specify actor address to start sealing sectors for",
			Required: true,
		},
		&cli.BoolFlag{
			Name:  "synthetic",
			Usage: "Use synthetic PoRep",
			Value: false, // todo implement synthetic
		},
	},
	ArgsUsage: "<sector>",
	Action: func(cctx *cli.Context) error {
		act, err := address.NewFromString(cctx.String("actor"))
		if err != nil {
			return xerrors.Errorf("parsing --actor: %w", err)
		}

		if cctx.Args().Len() > 1 {
			return xerrors.Errorf("specify only one sector")
		}

		sec := cctx.Args().First()

		sector, err := strconv.ParseUint(sec, 10, 64)
		if err != nil {
			return xerrors.Errorf("failed to parse the sector number: %w", err)
		}

		ctx := lcli.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		return market.SealNow(ctx, dep.Full, dep.DB, act, abi.SectorNumber(sector), cctx.Bool("synthetic"))
	},
}
