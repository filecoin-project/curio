package pay

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

// getProviderPayee retrieves the payee address from the on-chain service registry
// for the PDP operator configured in the database.
func getProviderPayee(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) (common.Address, error) {
	var sender string
	err := db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' ORDER BY address ASC`).Scan(&sender)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to retrieve PDP operator address: %w", err)
	}

	if sender == "" {
		return common.Address{}, fmt.Errorf("no PDP operator addresses found")
	}

	opAddr := common.HexToAddress(sender)

	registryAddr, err := contract.ServiceRegistryAddress()
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to get service registry address: %w", err)
	}

	registry, err := contract.NewServiceProviderRegistry(registryAddr, ethClient)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to create service registry: %w", err)
	}

	registered, err := registry.IsRegisteredProvider(&bind.CallOpts{Context: ctx}, opAddr)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to check if provider is registered: %w", err)
	}

	if !registered {
		return common.Address{}, xerrors.Errorf("provider is not registered")
	}

	provider, err := registry.GetProviderByAddress(&bind.CallOpts{Context: ctx}, opAddr)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to get provider: %w", err)
	}

	return provider.Info.Payee, nil
}
