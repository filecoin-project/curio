package main

import (
	"fmt"
	"strings"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	fbig "github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/market/ipni/spark"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build/buildconstants"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

var sparkCmd = &cli.Command{
	Name:  "spark",
	Usage: "Manage Smart Contract PeerID used by Spark",
	Subcommands: []*cli.Command{
		deletePeer,
	},
}

var deletePeer = &cli.Command{
	Name:      "delete-peer",
	Usage:     "Delete PeerID from Spark Smart Contract",
	ArgsUsage: "<Miner ID>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "Send the message to the smart contract",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 1 {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}

		minerID := cctx.Args().Get(0)

		full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
		if err != nil {
			return xerrors.Errorf("connecting to full node: %w", err)
		}
		defer closer()

		maddr, err := address.NewFromString(minerID)
		if err != nil {
			return xerrors.Errorf("parsing miner address: %w", err)
		}

		actorId, err := address.IDFromAddress(maddr)
		if err != nil {
			return err
		}

		ctx := cctx.Context

		mInfo, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		contractID, err := spark.GetContractAddress()
		if err != nil {
			return err
		}

		to, err := ethtypes.ParseEthAddress(contractID)
		if err != nil {
			return xerrors.Errorf("failed to parse contract address: %w", err)
		}

		toAddr, err := to.ToFilecoinAddress()
		if err != nil {
			return xerrors.Errorf("failed to convert Eth address to Filecoin address: %w", err)
		}

		// Parse the contract ABI
		parsedABI, err := eabi.JSON(strings.NewReader(spark.GetPeerAbi))
		if err != nil {
			log.Fatalf("Failed to parse getPeer ABI: %v", err)
		}

		// Encode the function call
		callData, err := parsedABI.Pack("getPeerData", int64(actorId))
		if err != nil {
			log.Fatalf("Failed to pack function call data: %v", err)
		}

		rMsg := &types.Message{
			To:         toAddr,
			From:       mInfo.Worker,
			Value:      types.NewInt(0),
			Method:     builtin.MethodsEVM.InvokeContract,
			Params:     callData,
			GasLimit:   buildconstants.BlockGasLimit,
			GasFeeCap:  fbig.Zero(),
			GasPremium: fbig.Zero(),
		}

		res, err := full.StateCall(ctx, rMsg, types.EmptyTSK)
		if err != nil {
			return fmt.Errorf("state call failed: %w", err)
		}

		if res.MsgRct.ExitCode.IsError() {
			return fmt.Errorf("state call failed: %s", res.MsgRct.ExitCode.String())
		}

		var params []byte

		if len(res.MsgRct.Return) == 0 {
			return xerrors.Errorf("no peer returned for miner %s", maddr.String())
		}

		// Parse the contract's ABI
		parsedABI, err = eabi.JSON(strings.NewReader(spark.DeletePeerAbi))
		if err != nil {
			return xerrors.Errorf("Failed to parse contract ABI: %w", err)
		}

		//Encode the method call to the contract (using the pay method with UUID)
		data, err := parsedABI.Pack("deletePeerData", int64(actorId))
		if err != nil {
			return xerrors.Errorf("Failed to pack the `deletePeerData()` function call: %v", err)
		}

		param := abi.CborBytes(data)
		params, err = actors.SerializeParams(&param)
		if err != nil {
			return fmt.Errorf("failed to serialize params: %w", err)
		}

		msg := &types.Message{
			From:       mInfo.Worker,
			To:         toAddr,
			Value:      abi.NewTokenAmount(0),
			Method:     builtin.MethodsEVM.InvokeContract,
			Params:     params,
			GasLimit:   buildconstants.BlockGasLimit,
			GasFeeCap:  fbig.Zero(),
			GasPremium: fbig.Zero(),
		}

		maxFee, err := types.ParseFIL("5 FIL")
		if err != nil {
			return xerrors.Errorf("failed to parse max fee: %w", err)
		}

		mspec := &api.MessageSendSpec{
			MaxFee: abi.TokenAmount(maxFee),
		}

		msg, err = full.GasEstimateMessageGas(ctx, msg, mspec, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("failed to estimate gas: %w", err)
		}

		if !cctx.Bool("really-do-it") {
			fmt.Println("Not sending the message... Use '--really-do-it flag to send the message'")
			return nil
		}

		sm, err := full.MpoolPushMessage(ctx, msg, mspec)
		if err != nil {
			return xerrors.Errorf("failed to push message to mempool: %w", err)
		}

		fmt.Printf("Sent the delete request in message %s\n", sm.Cid().String())

		wait, err := full.StateWaitMsg(ctx, sm.Cid(), 5, 2000, true)
		if err != nil {
			return xerrors.Errorf("failed to wait for message: %w", err)
		}

		if wait.Receipt.ExitCode != 0 {
			return xerrors.Errorf("message execution failed (exit code %d)", wait.Receipt.ExitCode)
		}

		fmt.Printf("PeerID binding deleted successfully with %s!\n", wait.Message.String())

		return nil
	},
}
