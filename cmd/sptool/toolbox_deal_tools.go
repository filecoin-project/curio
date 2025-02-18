package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-cidutil/cidenc"
	"github.com/ipfs/go-datastore"
	ds_sync "github.com/ipfs/go-datastore/sync"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	"github.com/manifoldco/promptui"
	"github.com/multiformats/go-multibase"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-commp-utils/writer"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	markettypes "github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/lib/testutils"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/messagesigner"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
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

		api, closer, err := lcli.GetGatewayAPI(cctx)
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

		api, closer, err := lcli.GetGatewayAPI(cctx)
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
		defer rdr.Close() //nolint:errcheck

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
		&cli.IntFlag{
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
		size := cctx.Int("size")
		cs := cctx.Int64("chunksize")
		ml := cctx.Int("maxlinks")

		rf, err := testutils.CreateRandomFile(outPath, int(time.Now().Unix()), size)
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
