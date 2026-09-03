package FWSS

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
)

// DataSetPayer resolves a data set's on-chain payer via the FWSS state view. Shared by the PDP pull
// validator (pdp.EthCallValidator) and the retrieval gate (market/retrieval/gate.Resolver) so the
// FWSS-view round-trip lives in one place.
func DataSetPayer(ctx context.Context, ethClient ethchain.EthClient, dataSetId uint64) (common.Address, error) {
	if dataSetId == 0 {
		return common.Address{}, xerrors.New("dataSetId must be greater than 0")
	}
	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, ethClient)
	if err != nil {
		return common.Address{}, xerrors.Errorf("resolve FWSS view address: %w", err)
	}
	view, err := NewFilecoinWarmStorageServiceStateView(viewAddr, ethClient)
	if err != nil {
		return common.Address{}, xerrors.Errorf("bind FWSS state view: %w", err)
	}
	ds, err := view.GetDataSet(contract.EthCallOpts(ctx), new(big.Int).SetUint64(dataSetId))
	if err != nil {
		return common.Address{}, xerrors.Errorf("get FWSS data set %d: %w", dataSetId, err)
	}
	if ds.Payer == (common.Address{}) {
		return common.Address{}, xerrors.Errorf("data set %d payer is zero address", dataSetId)
	}
	return ds.Payer, nil
}
