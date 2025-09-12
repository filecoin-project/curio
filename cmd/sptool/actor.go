package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker/go-units"
	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/createminer"
	"github.com/filecoin-project/curio/lib/reqcontext"

	builtin2 "github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/cli/spcli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/lib/tablewriter"
)

var actorCmd = &cli.Command{
	Name:  "actor",
	Usage: "Manage Filecoin Miner Actor Metadata",
	Subcommands: []*cli.Command{
		spcli.ActorSetAddrsCmd(SPTActorGetter),
		spcli.ActorWithdrawCmd(SPTActorGetter),
		spcli.ActorRepayDebtCmd(SPTActorGetter),
		spcli.ActorSetPeeridCmd(SPTActorGetter),
		spcli.ActorSetOwnerCmd(SPTActorGetter),
		spcli.ActorControlCmd(SPTActorGetter, actorControlListCmd(SPTActorGetter)),
		spcli.ActorProposeChangeWorkerCmd(SPTActorGetter),
		spcli.ActorConfirmChangeWorkerCmd(SPTActorGetter),
		spcli.ActorCompactAllocatedCmd(SPTActorGetter),
		spcli.ActorProposeChangeBeneficiaryCmd(SPTActorGetter),
		spcli.ActorConfirmChangeBeneficiaryCmd(SPTActorGetter),
		ActorNewMinerCmd,
	},
}

func actorControlListCmd(getActor spcli.ActorAddressGetter) *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "Get currently set control addresses. Note: This excludes most roles as they are not known to the immediate chain state.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "verbose",
			},
		},
		Action: func(cctx *cli.Context) error {
			full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
			if err != nil {
				return xerrors.Errorf("connecting to full node: %w", err)
			}
			defer closer()

			ctx := reqcontext.ReqContext(cctx)

			maddr, err := getActor(cctx)
			if err != nil {
				return err
			}

			mi, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
			if err != nil {
				return err
			}

			tw := tablewriter.New(
				tablewriter.Col("name"),
				tablewriter.Col("ID"),
				tablewriter.Col("key"),
				tablewriter.Col("use"),
				tablewriter.Col("balance"),
			)

			post := map[address.Address]struct{}{}

			for _, ca := range mi.ControlAddresses {
				post[ca] = struct{}{}
			}

			printKey := func(name string, a address.Address) {
				var actor *types.Actor
				if actor, err = full.StateGetActor(ctx, a, types.EmptyTSK); err != nil {
					fmt.Printf("%s\t%s: error getting actor: %s\n", name, a, err)
					return
				}
				b := actor.Balance

				var k = a
				// 'a' maybe a 'robust', in that case, 'StateAccountKey' returns an error.
				if builtin2.IsAccountActor(actor.Code) {
					if k, err = full.StateAccountKey(ctx, a, types.EmptyTSK); err != nil {
						fmt.Printf("%s\t%s: error getting account key: %s\n", name, a, err)
						return
					}
				}
				kstr := k.String()
				if !cctx.Bool("verbose") {
					if len(kstr) > 9 {
						kstr = kstr[:6] + "..."
					}
				}

				bstr := types.FIL(b).String()
				switch {
				case b.LessThan(types.FromFil(10)):
					bstr = color.RedString(bstr)
				case b.LessThan(types.FromFil(50)):
					bstr = color.YellowString(bstr)
				default:
					bstr = color.GreenString(bstr)
				}

				var uses []string
				if a == mi.Worker {
					uses = append(uses, color.YellowString("other"))
				}
				if _, ok := post[a]; ok {
					uses = append(uses, color.GreenString("post"))
				}

				tw.Write(map[string]interface{}{
					"name":    name,
					"ID":      a,
					"key":     kstr,
					"use":     strings.Join(uses, " "),
					"balance": bstr,
				})
			}

			printKey("owner", mi.Owner)
			printKey("worker", mi.Worker)
			printKey("beneficiary", mi.Beneficiary)
			for i, ca := range mi.ControlAddresses {
				printKey(fmt.Sprintf("control-%d", i), ca)
			}

			return tw.Flush(os.Stdout)
		},
	}
}

var ActorNewMinerCmd = &cli.Command{
	Name:  "new-miner",
	Usage: "Initializes a new miner actor",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "worker",
			Aliases: []string{"w"},
			Usage:   "worker key to use for new miner initialisation",
		},
		&cli.StringFlag{
			Name:    "owner",
			Aliases: []string{"o"},
			Usage:   "owner key to use for new miner initialisation",
		},
		&cli.StringFlag{
			Name:    "from",
			Aliases: []string{"f"},
			Usage:   "address to send actor(miner) creation message from",
		},
		&cli.StringFlag{
			Name:  "sector-size",
			Usage: "specify sector size to use for new miner initialisation",
		},
		&cli.IntFlag{
			Name:  "confidence",
			Usage: "number of block confirmations to wait for",
			Value: 5,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context

		full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
		if err != nil {
			return xerrors.Errorf("connecting to full node: %w", err)
		}
		defer closer()

		var owner address.Address
		if cctx.String("owner") == "" {
			return xerrors.Errorf("must provide a owner address")
		}
		owner, err = address.NewFromString(cctx.String("owner"))

		if err != nil {
			return err
		}

		worker := owner
		if cctx.String("worker") != "" {
			worker, err = address.NewFromString(cctx.String("worker"))
			if err != nil {
				return xerrors.Errorf("could not parse worker address: %w", err)
			}
		}

		sender := owner
		if fromstr := cctx.String("from"); fromstr != "" {
			faddr, err := address.NewFromString(fromstr)
			if err != nil {
				return xerrors.Errorf("could not parse from address: %w", err)
			}
			sender = faddr
		}

		if !cctx.IsSet("sector-size") {
			return xerrors.Errorf("must define sector size")
		}

		sectorSizeInt, err := units.RAMInBytes(cctx.String("sector-size"))
		if err != nil {
			return err
		}
		ssize := abi.SectorSize(sectorSizeInt)

		_, err = createminer.CreateStorageMiner(ctx, full, owner, worker, sender, ssize, cctx.Uint64("confidence"))
		if err != nil {
			return err
		}
		return nil
	},
}
