package ipni_provider

import (
	"bytes"
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
		if pInfo.SPID == 0 {
			log.Debugf("spark does not yet support pdp data")
			continue
		}
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
			return xerrors.Errorf("Failed to parse getPeer ABI: %w", err)
		}

		// Encode the function call
		callData, err := parsedABI.Pack("getPeerData", pInfo.SPID)
		if err != nil {
			return xerrors.Errorf("Failed to pack function call data: %w", err)
		}

		param := abi.CborBytes(callData)
		getParams, err := actors.SerializeParams(&param)
		if err != nil {
			return xerrors.Errorf("failed to serialize params: %w", err)
		}

		rMsg := &types.Message{
			To:         toAddr,
			From:       mInfo.Worker,
			Value:      types.NewInt(0),
			Method:     builtin.MethodsEVM.InvokeContract,
			Params:     getParams,
			GasLimit:   buildconstants.BlockGasLimit,
			GasFeeCap:  fbig.Zero(),
			GasPremium: fbig.Zero(),
		}

		res, err := p.full.StateCall(ctx, rMsg, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("state call failed: %w", err)
		}

		if res.MsgRct.ExitCode.IsError() {
			return fmt.Errorf("state call failed: %s", res.MsgRct.ExitCode.String())
		}

		var evmReturn abi.CborBytes
		err = evmReturn.UnmarshalCBOR(bytes.NewReader(res.MsgRct.Return))
		if err != nil {
			return xerrors.Errorf("failed to unmarshal evm return: %w", err)
		}

		var params []byte
		reason := "add"

		if len(evmReturn) > 0 {
			log.Debugf("res.MsgRct.Return: %x", evmReturn)

			// Define a struct that represents the tuple from the ABI
			type PeerData struct {
				PeerID    string `abi:"peerID"`
				Signature []byte `abi:"signature"`
			}

			// Define a wrapper struct that will be used for unpacking
			type WrappedPeerData struct {
				Result PeerData `abi:""`
			}

			// Create an instance of the wrapper struct
			var result WrappedPeerData

			err = parsedABI.UnpackIntoInterface(&result, "getPeerData", evmReturn)
			if err != nil {
				return xerrors.Errorf("Failed to unpack result: %w", err)
			}

			pd := result.Result

			// Check if peerID is empty
			if pd.PeerID != "" {
				// check if signature is zero bytes
				if len(pd.Signature) == 0 {
					log.Warnf("no signature found for minerID in MinerPeerIDMapping contract: %d", pInfo.SPID)
					continue
				}

				if pd.PeerID == pInfo.ID.String() {
					detail := spark.SparkMessage{
						Miner: pInfo.SPID,
						Peer:  pInfo.ID.String(),
					}

					jdetail, err := json.Marshal(detail)
					if err != nil {
						return xerrors.Errorf("failed to marshal spark message: %w", err)
					}

					ok, err := pInfo.Key.GetPublic().Verify(jdetail, pd.Signature)
					if err != nil {
						return xerrors.Errorf("failed to verify signature: %w", err)
					}
					if ok {
						log.Infof("not updating peerID for minerID in MinerPeerIDMapping contract: %d", pInfo.SPID)
						continue
					}
					reason = "update"
				}
			}
		}

		params, err = p.getSparkParams(pInfo.SPID, pInfo.ID.String(), pInfo.Key, reason)
		if err != nil {
			return xerrors.Errorf("failed to get spark params for %s: %w", pInfo.Miner.String(), err)
		}

		workerId, err := p.full.StateLookupID(ctx, mInfo.Worker, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("failed to lookup worker id: %w", err)
		}

		msg := &types.Message{
			From:   workerId,
			To:     toAddr,
			Value:  abi.NewTokenAmount(0),
			Method: builtin.MethodsEVM.InvokeContract,
			Params: params,
		}

		maxFee, err := types.ParseFIL("1 FIL")
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
	var funcName string

	if msgType == "add" {
		abiStr = spark.AddPeerAbi
		funcName = "addPeerData"
	}
	if msgType == "update" {
		abiStr = spark.UpdatePeerAbi
		funcName = "updatePeerData"
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

	data, err := parsedABI.Pack(funcName, miner, newPeer, signed)
	if err != nil {
		return nil, xerrors.Errorf("Failed to pack the `%s()` function call: %v", funcName, err)
	}

	param := abi.CborBytes(data)
	return actors.SerializeParams(&param)
}
