package pdp

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

var sessionKeyRegistryABI = mustParseSessionKeyRegistryABI(`[
	{"type":"function","name":"authorizationExpiry","inputs":[
		{"name":"owner","type":"address"},
		{"name":"sessionKey","type":"address"},
		{"name":"permission","type":"bytes32"}
	],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"}
]`)

func mustParseSessionKeyRegistryABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return parsed
}

func authorizationExpiry(ctx context.Context, ethClient ethchain.EthClient, payer, sessionKey common.Address, permission common.Hash) (time.Time, error) {
	fwssAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	fwss, err := FWSS.NewFilecoinWarmStorageService(fwssAddr, ethClient)
	if err != nil {
		return time.Time{}, fmt.Errorf("bind FWSS: %w", err)
	}

	registryAddr, err := fwss.SessionKeyRegistry(contract.EthCallOpts(ctx))
	if err != nil {
		return time.Time{}, fmt.Errorf("read session key registry: %w", err)
	}

	data, err := sessionKeyRegistryABI.Pack("authorizationExpiry", payer, sessionKey, permission)
	if err != nil {
		return time.Time{}, fmt.Errorf("pack authorizationExpiry: %w", err)
	}

	out, err := ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &registryAddr,
		Data: data,
	}, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("authorizationExpiry call: %w", err)
	}

	values, err := sessionKeyRegistryABI.Unpack("authorizationExpiry", out)
	if err != nil {
		return time.Time{}, fmt.Errorf("unpack authorizationExpiry: %w", err)
	}
	if len(values) != 1 {
		return time.Time{}, fmt.Errorf("unexpected authorizationExpiry result length %d", len(values))
	}

	expiry, ok := values[0].(*big.Int)
	if !ok {
		return time.Time{}, fmt.Errorf("authorizationExpiry result is not *big.Int")
	}
	if expiry.Sign() == 0 {
		return time.Time{}, nil
	}

	return time.Unix(expiry.Int64(), 0), nil
}
