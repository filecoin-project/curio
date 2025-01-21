package ipni_provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/libp2p/go-libp2p/core/crypto"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	fbig "github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/market/ipni/spark"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build/buildconstants"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

func (p *Provider) updateSparkContract(ctx context.Context) error {
	for _, pInfo := range p.keys {
		pInfo := pInfo
		mInfo, err := p.full.StateMinerInfo(ctx, pInfo.Miner, types.EmptyTSK)
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
		callData, err := parsedABI.Pack("getPeerData", pInfo.SPID)
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

		res, err := p.full.StateCall(ctx, rMsg, types.EmptyTSK)
		if err != nil {
			return fmt.Errorf("state call failed: %w", err)
		}

		if res.MsgRct.ExitCode.IsError() {
			return fmt.Errorf("state call failed: %s", res.MsgRct.ExitCode.String())
		}

		var params []byte

		if len(res.MsgRct.Return) == 0 {
			params, err = p.getSparkParams(pInfo.SPID, pInfo.ID.String(), pInfo.Key, "add")
			if err != nil {
				return xerrors.Errorf("failed to get spark params for %s: %w", pInfo.Miner.String(), err)
			}
		} else {
			pd := spark.SparkMessage{}

			err = parsedABI.UnpackIntoInterface(&pd, "getPeerData", res.MsgRct.Return)
			if err != nil {
				log.Fatalf("Failed to unpack result: %v", err)
			}

			if pd.Peer == pInfo.ID.String() {
				continue
			}

			params, err = p.getSparkParams(pInfo.SPID, pInfo.ID.String(), pInfo.Key, "update")
			if err != nil {
				return xerrors.Errorf("failed to get spark params for %s: %w", pInfo.Miner.String(), err)
			}
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

		msg, err = p.full.GasEstimateMessageGas(ctx, msg, mspec, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("failed to estimate gas: %w", err)
		}

		sm, err := p.full.MpoolPushMessage(ctx, msg, mspec)
		if err != nil {
			return xerrors.Errorf("failed to push message to mempool: %w", err)
		}

		wait, err := p.full.StateWaitMsg(ctx, sm.Cid(), 1, 2000, true)
		if err != nil {
			return xerrors.Errorf("failed to wait for message: %w", err)
		}

		if wait.Receipt.ExitCode != 0 {
			return xerrors.Errorf("message execution failed (exit code %d)", wait.Receipt.ExitCode)
		}

	}

	return nil
}

func (p *Provider) getSparkParams(miner abi.ActorID, newPeer string, key crypto.PrivKey, msgType string) ([]byte, error) {
	var abiStr string

	if msgType == "add" {
		abiStr = spark.AddPeerAbi
	}
	if msgType == "update" {
		abiStr = spark.UpdatePeerAbi
	}

	parsedABI, err := eabi.JSON(strings.NewReader(abiStr))
	if err != nil {
		return nil, xerrors.Errorf("Failed to parse contract ABI: %w", err)
	}

	detail := spark.SparkMessage{
		Miner: miner,
		Peer:  newPeer,
	}

	jdetail, err := json.Marshal(detail)
	if err != nil {
		return nil, xerrors.Errorf("failed to marshal spark message: %w", err)
	}

	signed, err := key.Sign(jdetail)
	if err != nil {
		return nil, xerrors.Errorf("failed to sign spark message: %w", err)
	}

	data, err := parsedABI.Pack("updatePeerData", miner, newPeer, signed)
	if err != nil {
		return nil, xerrors.Errorf("Failed to pack the `updatePeerData()` function call: %v", err)
	}

	param := abi.CborBytes(data)
	return actors.SerializeParams(&param)
}
