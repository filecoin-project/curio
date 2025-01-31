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
	var priv []byte
	var onChainPeerID string

	err := p.db.QueryRow(ctx, `select priv_key, peer_id from libp2p LIMIT 1`).Scan(&priv, &onChainPeerID)
	if err != nil {
		return xerrors.Errorf("querying libp2p peer: %w", err)
	}

	pKey, err := crypto.UnmarshalPrivateKey(priv)
	if err != nil {
		return xerrors.Errorf("unmarshaling private key: %w", err)
	}

	for _, pInfo := range p.keys {
		pInfo := pInfo
		mInfo, err := p.full.StateMinerInfo(ctx, pInfo.Miner, types.EmptyTSK)
		if err != nil {
			return err
		}

		if mInfo.PeerId == nil {
			return xerrors.Errorf("peer id not found for miner: %s", pInfo.Miner)
		}

		if mInfo.PeerId.String() != onChainPeerID {
			return xerrors.Errorf("peer id mismatch for miner: %s: onChain: %s and DB: %s", pInfo.Miner, mInfo.PeerId.String(), onChainPeerID)
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
			params, err = p.getSparkParams(pInfo.SPID, pInfo.ID.String(), pKey, "add")
			if err != nil {
				return xerrors.Errorf("failed to get spark params for %s: %w", pInfo.Miner.String(), err)
			}
		} else {
			pd := struct {
				PeerID        string
				SignedMessage []byte
			}{}

			log.Debugf("res.MsgRct.Return: %v", res.MsgRct.Return)
			log.Debugf("res.MsgRct.Return: %s", string(res.MsgRct.Return))

			err = parsedABI.UnpackIntoInterface(&pd, "getPeerData", res.MsgRct.Return)
			if err != nil {
				return xerrors.Errorf("Failed to unpack result: %w", err)
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

				ok, err := pKey.GetPublic().Verify(jdetail, pd.SignedMessage)
				if err != nil {
					return xerrors.Errorf("failed to verify signed message: %w", err)
				}
				if ok {
					continue
				}
			}

			params, err = p.getSparkParams(pInfo.SPID, pInfo.ID.String(), pKey, "update")
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
