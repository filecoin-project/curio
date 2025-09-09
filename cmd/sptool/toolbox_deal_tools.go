package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-cidutil/cidenc"
	"github.com/ipfs/go-datastore"
	ds_sync "github.com/ipfs/go-datastore/sync"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	"github.com/manifoldco/promptui"
	"github.com/mitchellh/go-homedir"
	"github.com/multiformats/go-multibase"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-commp-utils/writer"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v13/datacap"
	verifreg13 "github.com/filecoin-project/go-state-types/builtin/v13/verifreg"
	markettypes "github.com/filecoin-project/go-state-types/builtin/v9/market"
	verifreg9 "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"

	"github.com/filecoin-project/curio/lib/testutils"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	datacap2 "github.com/filecoin-project/lotus/chain/actors/builtin/datacap"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/messagesigner"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/tablewriter"
	"github.com/filecoin-project/lotus/node/modules"
)

var (
	flagAssumeYes = &cli.BoolFlag{
		Name:    "assume-yes",
		Usage:   "automatic yes to prompts; assume 'yes' as answer to all prompts and run non-interactively",
		Aliases: []string{"y", "yes"},
	}
)

var marketAddCmd = &cli.Command{
	Name:        "market-add",
	Usage:       "Add funds to the Storage Market actor",
	Description: "Send signed message to add funds for the default wallet to the Storage Market actor. Uses 2x current BaseFee and a maximum fee of 1 nFIL. This is an experimental utility, do not use in production.",
	Flags: []cli.Flag{
		flagAssumeYes,
		&cli.StringFlag{
			Name:  "wallet",
			Usage: "move balance from this wallet address to its market actor",
		},
	},
	ArgsUsage: "<amount>",
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return fmt.Errorf("must pass amount to add")
		}
		f, err := types.ParseFIL(cctx.Args().First())
		if err != nil {
			return fmt.Errorf("parsing 'amount' argument: %w", err)
		}

		amt := abi.TokenAmount(f)

		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		api, closer, err := lcli.GetGatewayAPIV1(cctx)
		if err != nil {
			return fmt.Errorf("cant setup gateway connection: %w", err)
		}
		defer closer()

		walletAddr, err := n.GetProvidedOrDefaultWallet(ctx, cctx.String("wallet"))
		if err != nil {
			return err
		}

		log.Infow("selected wallet", "wallet", walletAddr)

		params, err := actors.SerializeParams(&walletAddr)
		if err != nil {
			return err
		}

		msg := &types.Message{
			To:     market.Address,
			From:   walletAddr,
			Value:  amt,
			Method: market.Methods.AddBalance,
			Params: params,
		}

		cid, sent, err := SignAndPushToMpool(cctx, ctx, api, n, nil, msg)
		if err != nil {
			return err
		}
		if !sent {
			return nil
		}

		log.Infow("submitted market-add message", "cid", cid.String())

		res, err := api.StateWaitMsg(ctx, cid, 1, 2000, true)
		if err != nil {
			return xerrors.Errorf("waiting for market-add message failed: %w", err)
		}

		if res.Receipt.ExitCode != 0 {
			return xerrors.Errorf("market-add message failed: exit %d", res.Receipt.ExitCode)
		}

		return nil
	},
}

var marketWithdrawCmd = &cli.Command{
	Name:        "market-withdraw",
	Usage:       "Withdraw funds from the Storage Market actor",
	Description: "",
	Flags: []cli.Flag{
		flagAssumeYes,
		&cli.StringFlag{
			Name:  "wallet",
			Usage: "move balance to this wallet address from its market actor",
		},
	},
	ArgsUsage: "<amount>",
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return fmt.Errorf("must pass amount to add")
		}
		f, err := types.ParseFIL(cctx.Args().First())
		if err != nil {
			return fmt.Errorf("parsing 'amount' argument: %w", err)
		}

		amt := abi.TokenAmount(f)

		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		api, closer, err := lcli.GetGatewayAPIV1(cctx)
		if err != nil {
			return fmt.Errorf("cant setup gateway connection: %w", err)
		}
		defer closer()

		walletAddr, err := n.GetProvidedOrDefaultWallet(ctx, cctx.String("wallet"))
		if err != nil {
			return err
		}

		log.Infow("selected wallet", "wallet", walletAddr)

		params, err := actors.SerializeParams(&markettypes.WithdrawBalanceParams{
			ProviderOrClientAddress: walletAddr,
			Amount:                  amt,
		})
		if err != nil {
			return err
		}

		msg := &types.Message{
			To:     market.Address,
			From:   walletAddr,
			Value:  types.NewInt(0),
			Method: market.Methods.WithdrawBalance,
			Params: params,
		}

		cid, sent, err := SignAndPushToMpool(cctx, ctx, api, n, nil, msg)
		if err != nil {
			return err
		}
		if !sent {
			return nil
		}

		log.Infow("submitted market-withdraw message", "cid", cid.String())

		return nil
	},
}

var commpCmd = &cli.Command{
	Name:      "commp",
	Usage:     "",
	ArgsUsage: "<inputPath>",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 1 {
			return fmt.Errorf("usage: commP <inputPath>")
		}

		inPath := cctx.Args().Get(0)

		rdr, err := os.Open(inPath)
		if err != nil {
			return err
		}
		defer func() {
			_ = rdr.Close()
		}()

		w := &writer.Writer{}
		_, err = io.CopyBuffer(w, rdr, make([]byte, writer.CommPBuf))
		if err != nil {
			return fmt.Errorf("copy into commp writer: %w", err)
		}

		commp, err := w.Sum()
		if err != nil {
			return fmt.Errorf("computing commP failed: %w", err)
		}

		encoder := cidenc.Encoder{Base: multibase.MustNewEncoder(multibase.Base32)}

		stat, err := os.Stat(inPath)
		if err != nil {
			return err
		}

		fmt.Println("CommP CID: ", encoder.Encode(commp.PieceCID))
		fmt.Println("Piece size: ", types.NewInt(uint64(commp.PieceSize.Unpadded().Padded())))
		fmt.Println("Car file size: ", stat.Size())
		return nil
	},
}

var generateRandCar = &cli.Command{
	Name:      "generate-rand-car",
	Usage:     "creates a randomly generated dense car",
	ArgsUsage: "<outputPath>",
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name:    "size",
			Aliases: []string{"s"},
			Usage:   "The size of the data to turn into a car",
			Value:   8000000,
		},
		&cli.IntFlag{
			Name:    "chunksize",
			Aliases: []string{"c"},
			Value:   512,
			Usage:   "Size of chunking that should occur",
		},
		&cli.IntFlag{
			Name:    "maxlinks",
			Aliases: []string{"l"},
			Value:   8,
			Usage:   "Max number of leaves per level",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() < 1 {
			return fmt.Errorf("usage: generate-car <outputPath> -size -chunksize -maxleaves")
		}

		outPath := cctx.Args().Get(0)
		size := cctx.Int64("size")
		cs := cctx.Int64("chunksize")
		ml := cctx.Int("maxlinks")

		rf, err := testutils.CreateRandomTmpFile(outPath, size)
		if err != nil {
			return err
		}

		// carv1
		caropts := []carv2.Option{
			blockstore.WriteAsCarV1(true),
		}

		root, cn, err := testutils.CreateDenseCARWith(outPath, rf, cs, ml, caropts)
		if err != nil {
			return err
		}

		err = os.Remove(rf)
		if err != nil {
			return err
		}

		encoder := cidenc.Encoder{Base: multibase.MustNewEncoder(multibase.Base32)}
		rn := encoder.Encode(root)
		base := path.Dir(cn)
		np := path.Join(base, rn+".car")

		err = os.Rename(cn, np)
		if err != nil {
			return err
		}

		fmt.Printf("Payload CID: %s, written to: %s\n", rn, np)

		return nil
	},
}

func SignAndPushToMpool(cctx *cli.Context, ctx context.Context, api lapi.Gateway, n *Node, ds *ds_sync.MutexDatastore, msg *types.Message) (cid cid.Cid, sent bool, err error) {
	if ds == nil {
		ds = ds_sync.MutexWrap(datastore.NewMapDatastore())
	}
	vmessagesigner := messagesigner.NewMessageSigner(n.Wallet, &modules.MpoolNonceAPI{ChainModule: api, StateModule: api}, ds)

	head, err := api.ChainHead(ctx)
	if err != nil {
		return
	}
	basefee := head.Blocks()[0].ParentBaseFee

	spec := &lapi.MessageSendSpec{
		MaxFee: abi.NewTokenAmount(1000000000), // 1 nFIL
	}
	msg, err = api.GasEstimateMessageGas(ctx, msg, spec, types.EmptyTSK)
	if err != nil {
		err = fmt.Errorf("GasEstimateMessageGas error: %w", err)
		return
	}

	// use basefee + 20%
	newGasFeeCap := big.Mul(big.Int(basefee), big.NewInt(6))
	newGasFeeCap = big.Div(newGasFeeCap, big.NewInt(5))

	if big.Cmp(msg.GasFeeCap, newGasFeeCap) < 0 {
		msg.GasFeeCap = newGasFeeCap
	}

	smsg, err := vmessagesigner.SignMessage(ctx, msg, nil, func(*types.SignedMessage) error { return nil })
	if err != nil {
		return
	}

	fmt.Println("about to send message with the following gas costs")
	maxFee := big.Mul(smsg.Message.GasFeeCap, big.NewInt(smsg.Message.GasLimit))
	fmt.Println("max fee:     ", types.FIL(maxFee), "(absolute maximum amount you are willing to pay to get your transaction confirmed)")
	fmt.Println("gas fee cap: ", types.FIL(smsg.Message.GasFeeCap))
	fmt.Println("gas limit:   ", smsg.Message.GasLimit)
	fmt.Println("gas premium: ", types.FIL(smsg.Message.GasPremium))
	fmt.Println("basefee:     ", types.FIL(basefee))
	fmt.Println("nonce:       ", smsg.Message.Nonce)
	fmt.Println()
	if !cctx.Bool("assume-yes") {
		validate := func(input string) error {
			if strings.EqualFold(input, "y") || strings.EqualFold(input, "yes") {
				return nil
			}
			if strings.EqualFold(input, "n") || strings.EqualFold(input, "no") {
				return nil
			}
			return errors.New("incorrect input")
		}

		templates := &promptui.PromptTemplates{
			Prompt:  "{{ . }} ",
			Valid:   "{{ . | green }} ",
			Invalid: "{{ . | red }} ",
			Success: "{{ . | cyan | bold }} ",
		}

		prompt := promptui.Prompt{
			Label:     "Proceed? Yes [Y/y] / No [N/n], Ctrl+C (^C) to exit",
			Templates: templates,
			Validate:  validate,
		}

		var input string

		input, err = prompt.Run()
		if err != nil {
			return
		}
		if strings.Contains(strings.ToLower(input), "n") {
			fmt.Println("Message not sent")
			return
		}
	}

	cid, err = api.MpoolPush(ctx, smsg)
	if err != nil {
		err = fmt.Errorf("mpool push: failed to push message: %w", err)
		return
	}
	fmt.Println("sent message: ", cid)
	sent = true
	return
}

var allocateCmd = &cli.Command{
	Name:        "allocate",
	Usage:       "Create new allocation[s] for verified deals",
	Description: "The command can accept a CSV formatted file in the format 'pieceCid,pieceSize,miner,tmin,tmax,expiration'",
	Flags: []cli.Flag{
		&cli.StringSliceFlag{
			Name:    "miner",
			Usage:   "storage provider address[es]",
			Aliases: []string{"m", "provider", "p"},
		},
		&cli.StringFlag{
			Name:    "piece-cid",
			Usage:   "data piece-cid to create the allocation",
			Aliases: []string{"piece"},
		},
		&cli.Int64Flag{
			Name:    "piece-size",
			Usage:   "piece size to create the allocation",
			Aliases: []string{"size"},
		},
		&cli.StringFlag{
			Name:  "wallet",
			Usage: "the wallet address that will used create the allocation",
		},
		&cli.BoolFlag{
			Name:  "quiet",
			Usage: "do not print the allocation list",
			Value: false,
		},
		&cli.Int64Flag{
			Name: "term-min",
			Usage: "The minimum duration which the provider must commit to storing the piece to avoid early-termination penalties (epochs).\n" +
				"Default is 180 days.",
			Aliases: []string{"tmin"},
			Value:   verifreg13.MinimumVerifiedAllocationTerm,
		},
		&cli.Int64Flag{
			Name: "term-max",
			Usage: "The maximum period for which a provider can earn quality-adjusted power for the piece (epochs).\n" +
				"Default is 5 years.",
			Aliases: []string{"tmax"},
			Value:   verifreg13.MaximumVerifiedAllocationTerm,
		},
		&cli.Int64Flag{
			Name: "expiration",
			Usage: "The latest epoch by which a provider must commit data before the allocation expires (epochs).\n" +
				"Default is 60 days.",
			Value: verifreg13.MaximumVerifiedAllocationExpiration,
		},
		&cli.StringFlag{
			Name:    "piece-file",
			Usage:   "file containing piece information to create the allocation. Each line in the file should be in the format 'pieceCid,pieceSize,miner,tmin,tmax,expiration'",
			Aliases: []string{"pf"},
		},
		&cli.IntFlag{
			Name:  "batch-size",
			Usage: "number of extend requests per batch. If set incorrectly, this will lead to out of gas error",
			Value: 500,
		},
		&cli.IntFlag{
			Name:  "confidence",
			Usage: "number of block confirmations to wait for",
			Value: int(build.MessageConfidence),
		},
		&cli.BoolFlag{
			Name:    "assume-yes",
			Usage:   "automatic yes to prompts; assume 'yes' as answer to all prompts and run non-interactively",
			Aliases: []string{"y", "yes"},
		},
		&cli.StringFlag{
			Name:  "evm-client-contract",
			Usage: "f4 address of EVM contract to spend DataCap from",
		},
		&cli.BoolFlag{
			Name:    "json",
			Usage:   "print output in JSON format",
			Aliases: []string{"j"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context

		pieceFile := cctx.String("piece-file")
		miners := cctx.StringSlice("miner")
		pcids := cctx.String("piece-cid")

		if pieceFile == "" && pcids == "" {
			return fmt.Errorf("must provide at least one --piece-cid or use --piece-file")
		}

		if pieceFile == "" && len(miners) < 1 {
			return fmt.Errorf("must provide at least one miner address or use --piece-file")
		}

		if pieceFile != "" && pcids != "" {
			return fmt.Errorf("cannot use both --piece-cid and --piece-file flags at once")
		}

		var pieceInfos []PieceInfos

		if pieceFile != "" {
			// Read file line by line
			loc, err := homedir.Expand(pieceFile)
			if err != nil {
				return err
			}
			file, err := os.Open(loc)
			if err != nil {
				return err
			}
			defer func() {
				_ = file.Close()
			}()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()
				// Extract pieceCid, pieceSize and MinerAddr from line
				parts := strings.Split(line, ",")
				if len(parts) != 6 {
					return fmt.Errorf("invalid line format. Expected pieceCid, pieceSize, MinerAddr, TMin, TMax, Exp at %s", line)
				}
				if parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" || parts[4] == "" || parts[5] == "" {
					return fmt.Errorf("empty column value in the input file at %s", line)
				}

				pieceCid, err := cid.Parse(parts[0])
				if err != nil {
					return fmt.Errorf("failed to parse CID: %w", err)
				}
				pieceSize, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse size %w", err)
				}
				maddr, err := address.NewFromString(parts[2])
				if err != nil {
					return fmt.Errorf("failed to parse miner address %w", err)
				}

				mid, err := address.IDFromAddress(maddr)
				if err != nil {
					return fmt.Errorf("failed to convert miner address %w", err)
				}

				tmin, err := strconv.ParseUint(parts[3], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to tmin %w", err)
				}

				tmax, err := strconv.ParseUint(parts[4], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to tmax %w", err)
				}

				exp, err := strconv.ParseUint(parts[5], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to expiration %w", err)
				}

				if tmax < tmin {
					return fmt.Errorf("maximum duration %d cannot be smaller than minimum duration %d", tmax, tmin)
				}

				pieceInfos = append(pieceInfos, PieceInfos{
					Cid:       pieceCid,
					Size:      pieceSize,
					Miner:     abi.ActorID(mid),
					MinerAddr: maddr,
					Tmin:      abi.ChainEpoch(tmin),
					Tmax:      abi.ChainEpoch(tmax),
					Exp:       abi.ChainEpoch(exp),
				})
				if err := scanner.Err(); err != nil {
					return err
				}
			}
		} else {
			for _, miner := range miners {
				maddr, err := address.NewFromString(miner)
				if err != nil {
					return fmt.Errorf("failed to parse miner address %w", err)
				}

				mid, err := address.IDFromAddress(maddr)
				if err != nil {
					return fmt.Errorf("failed to convert miner address %w", err)
				}
				pcid, err := cid.Parse(cctx.String("piece-cid"))
				if err != nil {
					return fmt.Errorf("failed to parse pieceCid %w", err)
				}
				size := cctx.Int64("piece-size")

				tmin := abi.ChainEpoch(cctx.Int64("term-min"))

				tmax := abi.ChainEpoch(cctx.Int64("term-max"))

				exp := abi.ChainEpoch(cctx.Int64("expiration"))
				if exp == verifreg13.MaximumVerifiedAllocationExpiration {
					exp -= 5
				}

				pieceInfos = append(pieceInfos, PieceInfos{
					Cid:       pcid,
					Size:      size,
					Miner:     abi.ActorID(mid),
					MinerAddr: maddr,
					Tmin:      tmin,
					Tmax:      tmax,
					Exp:       exp,
				})
			}
		}

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		gapi, closer, err := lcli.GetGatewayAPIV1(cctx)
		if err != nil {
			return fmt.Errorf("can't setup gateway connection: %w", err)
		}
		defer closer()

		// Get wallet address from input
		walletAddr, err := n.GetProvidedOrDefaultWallet(ctx, cctx.String("wallet"))
		if err != nil {
			return err
		}

		log.Debugw("selected wallet", "wallet", walletAddr)

		var msgs []*types.Message
		var allocationsAddr address.Address
		if cctx.IsSet("evm-client-contract") {
			evmContract := cctx.String("evm-client-contract")
			if evmContract == "" {
				return fmt.Errorf("evm-client-contract can't be empty")
			}
			evmContractAddr, err := address.NewFromString(evmContract)
			if err != nil {
				return err
			}
			allocationsAddr = evmContractAddr
			msgs, err = CreateAllocationViaEVMMsg(ctx, gapi, pieceInfos, walletAddr, evmContractAddr, cctx.Int("batch-size"))
			if err != nil {
				return err
			}
		} else {
			allocationsAddr = walletAddr
			msgs, err = CreateAllocationMsg(ctx, gapi, pieceInfos, walletAddr, cctx.Int("batch-size"))
			if err != nil {
				return err
			}
		}

		oldallocations, err := gapi.StateGetAllocations(ctx, allocationsAddr, types.EmptyTSK)
		if err != nil {
			return fmt.Errorf("failed to get allocations: %w", err)
		}

		var mcids []cid.Cid

		ds := ds_sync.MutexWrap(datastore.NewMapDatastore())
		for _, msg := range msgs {
			mcid, sent, err := SignAndPushToMpool(cctx, ctx, gapi, n, ds, msg)
			if err != nil {
				return err
			}
			if !sent {
				fmt.Printf("message %s with method %s not sent\n", msg.Cid(), msg.Method.String())
				continue
			}
			mcids = append(mcids, mcid)
		}

		var mcidStr []string
		for _, c := range mcids {
			mcidStr = append(mcidStr, c.String())
		}

		log.Infow("submitted data cap allocation message[s]", "CID", mcidStr)
		log.Info("waiting for message to be included in a block")

		// wait for msgs to get mined into a block
		eg := errgroup.Group{}
		eg.SetLimit(10)
		for _, msg := range mcids {
			m := msg
			eg.Go(func() error {
				wait, err := gapi.StateWaitMsg(ctx, m, uint64(cctx.Int("confidence")), 2000, true)
				if err != nil {
					return fmt.Errorf("timeout waiting for message to land on chain %s", m.String())

				}

				if wait.Receipt.ExitCode.IsError() {
					return fmt.Errorf("failed to execute message %s: %w", m.String(), wait.Receipt.ExitCode)
				}
				return nil
			})
		}
		err = eg.Wait()
		if err != nil {
			return err
		}

		// Return early of quiet flag is set
		if cctx.Bool("quiet") {
			return nil
		}

		newallocations, err := gapi.StateGetAllocations(ctx, allocationsAddr, types.EmptyTSK)
		if err != nil {
			return fmt.Errorf("failed to get allocations: %w", err)
		}

		// Generate a diff to find new allocations
		for i := range newallocations {
			_, ok := oldallocations[i]
			if ok {
				delete(newallocations, i)
			}
		}

		return printAllocation(newallocations, cctx.Bool("json"))
	},
}

var listAllocationsCmd = &cli.Command{
	Name:  "list-allocations",
	Usage: "Lists all allocations for a client address(wallet)",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "miner",
			Usage:   "Storage provider address. If provided, only allocations against this minerID will be printed",
			Aliases: []string{"m", "provider", "p"},
		},
		&cli.StringFlag{
			Name:  "wallet",
			Usage: "the wallet address that will used create the allocation",
		},
		&cli.BoolFlag{
			Name:    "json",
			Usage:   "print output in JSON format",
			Aliases: []string{"j"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		gapi, closer, err := lcli.GetGatewayAPIV1(cctx)
		if err != nil {
			return fmt.Errorf("cant setup gateway connection: %w", err)
		}
		defer closer()

		// Get wallet address from input
		walletAddr, err := n.GetProvidedOrDefaultWallet(ctx, cctx.String("wallet"))
		if err != nil {
			return err
		}

		log.Debugw("selected wallet", "wallet", walletAddr)

		allocations, err := gapi.StateGetAllocations(ctx, walletAddr, types.EmptyTSK)
		if err != nil {
			return fmt.Errorf("failed to get allocations: %w", err)
		}

		if cctx.String("miner") != "" {
			// Get all minerIDs from input
			minerId := cctx.String("miner")
			maddr, err := address.NewFromString(minerId)
			if err != nil {
				return err
			}

			// Verify that minerID exists
			_, err = gapi.StateMinerInfo(ctx, maddr, types.EmptyTSK)
			if err != nil {
				return err
			}

			mid, err := address.IDFromAddress(maddr)
			if err != nil {
				return err
			}

			for i, v := range allocations {
				if v.Provider != abi.ActorID(mid) {
					delete(allocations, i)
				}
			}
		}

		return printAllocation(allocations, cctx.Bool("json"))
	},
}

func printAllocation(allocations map[verifreg.AllocationId]verifreg.Allocation, json bool) error {
	// Map Keys. Corresponds to the standard tablewriter output
	allocationID := "AllocationID"
	client := "Client"
	provider := "Miner"
	pieceCid := "PieceCid"
	pieceSize := "PieceSize"
	tMin := "TermMin"
	tMax := "TermMax"
	expr := "Expiration"

	var allocs []map[string]interface{}

	for key, val := range allocations {
		alloc := map[string]interface{}{
			allocationID: key,
			client:       val.Client,
			provider:     val.Provider,
			pieceCid:     val.Data,
			pieceSize:    val.Size,
			tMin:         val.TermMin,
			tMax:         val.TermMax,
			expr:         val.Expiration,
		}
		allocs = append(allocs, alloc)
	}

	if json {
		type jalloc struct {
			Client     abi.ActorID         `json:"client"`
			Provider   abi.ActorID         `json:"provider"`
			Data       cid.Cid             `json:"data"`
			Size       abi.PaddedPieceSize `json:"size"`
			TermMin    abi.ChainEpoch      `json:"term_min"`
			TermMax    abi.ChainEpoch      `json:"term_max"`
			Expiration abi.ChainEpoch      `json:"expiration"`
		}
		allocMap := make(map[verifreg13.AllocationId]jalloc, len(allocations))
		for id, allocation := range allocations {
			allocMap[verifreg13.AllocationId(id)] = jalloc{
				Provider:   allocation.Provider,
				Client:     allocation.Client,
				Data:       allocation.Data,
				Size:       allocation.Size,
				TermMin:    allocation.TermMin,
				TermMax:    allocation.TermMax,
				Expiration: allocation.Expiration,
			}
		}
		return PrintJson(map[string]any{"allocations": allocations})
	} else {
		// Init the tablewriter's columns
		tw := tablewriter.New(
			tablewriter.Col(allocationID),
			tablewriter.Col(client),
			tablewriter.Col(provider),
			tablewriter.Col(pieceCid),
			tablewriter.Col(pieceSize),
			tablewriter.Col(tMin),
			tablewriter.Col(tMax),
			tablewriter.NewLineCol(expr))
		// populate it with content
		for _, alloc := range allocs {
			tw.Write(alloc)
		}
		// return the corresponding string
		return tw.Flush(os.Stdout)
	}
}

type PieceInfos struct {
	Cid       cid.Cid
	Size      int64
	MinerAddr address.Address
	Miner     abi.ActorID
	Tmin      abi.ChainEpoch
	Tmax      abi.ChainEpoch
	Exp       abi.ChainEpoch
}

func CreateAllocationMsg(ctx context.Context, api lapi.Gateway, infos []PieceInfos, wallet address.Address, batchSize int) ([]*types.Message, error) {
	// Create allocation requests
	rDataCap, allocationRequests, err := CreateAllocationRequests(ctx, api, infos)
	if err != nil {
		return nil, err
	}

	// Get datacap balance
	aDataCap, err := api.StateVerifiedClientStatus(ctx, wallet, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	if aDataCap == nil {
		return nil, fmt.Errorf("wallet %s does not have any datacap", wallet)
	}

	// Check that we have enough data cap to make the allocation
	if rDataCap.GreaterThan(big.NewInt(aDataCap.Int64())) {
		return nil, fmt.Errorf("requested datacap %s is greater then the available datacap %s", rDataCap, aDataCap)
	}

	// Batch allocationRequests to create message
	var messages []*types.Message
	for i := 0; i < len(allocationRequests); i += batchSize {
		end := i + batchSize
		if end > len(allocationRequests) {
			end = len(allocationRequests)
		}
		batch := allocationRequests[i:end]
		arequest := &verifreg9.AllocationRequests{
			Allocations: batch,
		}
		bDataCap := big.NewInt(0)
		for _, bd := range batch {
			bDataCap.Add(big.NewInt(int64(bd.Size)).Int, bDataCap.Int)
		}

		receiverParams, err := actors.SerializeParams(arequest)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize the parameters: %w", err)
		}

		transferParams, err := actors.SerializeParams(&datacap.TransferParams{
			To:           builtin.VerifiedRegistryActorAddr,
			Amount:       big.Mul(bDataCap, builtin.TokenPrecision),
			OperatorData: receiverParams,
		})

		if err != nil {
			return nil, fmt.Errorf("failed to serialize transfer parameters: %w", err)
		}
		msg := &types.Message{
			To:     builtin.DatacapActorAddr,
			From:   wallet,
			Method: datacap2.Methods.TransferExported,
			Params: transferParams,
			Value:  big.Zero(),
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func CreateAllocationRequests(ctx context.Context, api lapi.Gateway, infos []PieceInfos) (*big.Int, []verifreg9.AllocationRequest, error) {
	var allocationRequests []verifreg9.AllocationRequest
	rDataCap := big.NewInt(0)
	head, err := api.ChainHead(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, info := range infos {
		minfo, err := api.StateMinerInfo(ctx, info.MinerAddr, types.EmptyTSK)
		if err != nil {
			return nil, nil, err
		}
		if uint64(minfo.SectorSize) < uint64(info.Size) {
			return nil, nil, fmt.Errorf("specified piece size %d is bigger than miner's sector size %s", info.Size, minfo.SectorSize.String())
		}
		allocationRequests = append(allocationRequests, verifreg9.AllocationRequest{
			Provider:   info.Miner,
			Data:       info.Cid,
			Size:       abi.PaddedPieceSize(info.Size),
			TermMin:    info.Tmin,
			TermMax:    info.Tmax,
			Expiration: head.Height() + info.Exp,
		})
		rDataCap.Add(big.NewInt(info.Size).Int, rDataCap.Int)
	}
	return &rDataCap, allocationRequests, nil
}
