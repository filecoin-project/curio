package contract

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
)

// Minimal ABIs for payee/worker rotation. Full bindings are regenerated after the
// ServiceProviderRegistry and FWSS upgrades land; until then these fragments are enough
// to pack calldata and estimate gas.

const registryRoleRotationABI = `[
	{"type":"function","name":"changeServiceProvider","inputs":[{"name":"providerId","type":"uint256"},{"name":"newServiceProvider","type":"address"}],"outputs":[],"stateMutability":"nonpayable"},
	{"type":"function","name":"providerActionNonce","inputs":[{"name":"providerId","type":"uint256"}],"outputs":[{"name":"nonce","type":"uint256"}],"stateMutability":"view"}
]`

const fwssRetargetABI = `[
	{"type":"function","name":"retargetDataSetPayee","inputs":[{"name":"dataSetId","type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"}
]`

func registryRoleABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(registryRoleRotationABI))
}

func fwssRetargetParsedABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(fwssRetargetABI))
}

// PackChangeServiceProvider builds calldata for a payee-or-owner changeServiceProvider call.
func PackChangeServiceProvider(providerID *big.Int, newWorker common.Address) ([]byte, error) {
	parsed, err := registryRoleABI()
	if err != nil {
		return nil, err
	}
	return parsed.Pack("changeServiceProvider", providerID, newWorker)
}

// PackRetargetDataSetPayee builds calldata for FWSS.retargetDataSetPayee.
func PackRetargetDataSetPayee(dataSetID *big.Int) ([]byte, error) {
	parsed, err := fwssRetargetParsedABI()
	if err != nil {
		return nil, err
	}
	return parsed.Pack("retargetDataSetPayee", dataSetID)
}

// ProviderActionNonce reads the payee-action nonce from the registry.
func ProviderActionNonce(ctx context.Context, ethClient ethchain.EthClient, providerID *big.Int) (*big.Int, error) {
	parsed, err := registryRoleABI()
	if err != nil {
		return nil, err
	}
	registryAddr, err := ServiceRegistryAddress()
	if err != nil {
		return nil, err
	}
	data, err := parsed.Pack("providerActionNonce", providerID)
	if err != nil {
		return nil, err
	}
	out, err := ethClient.CallContract(ctx, ethereum.CallMsg{To: &registryAddr, Data: data}, nil)
	if err != nil {
		return nil, xerrors.Errorf("providerActionNonce: %w", err)
	}
	vals, err := parsed.Unpack("providerActionNonce", out)
	if err != nil {
		return nil, err
	}
	nonce, ok := vals[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected providerActionNonce type")
	}
	return nonce, nil
}

// EstimateRetargetGas estimates gas for one retargetDataSetPayee call from the worker.
func EstimateRetargetGas(ctx context.Context, ethClient ethchain.EthClient, worker common.Address, dataSetID *big.Int) (uint64, error) {
	calldata, err := PackRetargetDataSetPayee(dataSetID)
	if err != nil {
		return 0, err
	}
	addrs := ContractAddresses()
	fwss := addrs.AllowedPublicRecordKeepers.FWSService
	if fwss == (common.Address{}) {
		return 0, fmt.Errorf("FWSS address not configured")
	}
	return ethClient.EstimateGas(ctx, ethereum.CallMsg{
		From: worker,
		To:   &fwss,
		Data: calldata,
	})
}

// SubmitWorkerTransaction signs and broadcasts a zero-value call from the PDP worker key.
func SubmitWorkerTransaction(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, to common.Address, calldata []byte) (string, error) {
	sender, _, privateKey, err := getSender(ctx, db)
	if err != nil {
		return "", err
	}
	signedTx, err := createSignedTransaction(ctx, ethClient, privateKey, sender, to, big.NewInt(0), calldata)
	if err != nil {
		return "", xerrors.Errorf("creating signed transaction: %w", err)
	}
	if err := ethClient.SendTransaction(ctx, signedTx); err != nil {
		return "", xerrors.Errorf("sending transaction: %w", err)
	}
	log.Infof("Sent worker transaction %s at %s", signedTx.Hash().String(), time.Now().Format(time.RFC3339Nano))
	return signedTx.Hash().Hex(), nil
}
