package FWSS

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
)

// SettleTargetResolver builds a Filecoin Pay settlement target resolver for
// FWSS rails. FWSS uses multiple rail types: PDP validator rails must respect
// proving-period boundaries, while non-validator rails can use the normal
// Filecoin Pay settlement target.
func SettleTargetResolver(ctx context.Context, ethClient ethchain.EthClient) (filecoinpayment.SettleTargetResolver, error) {
	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService

	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("failed to get FWSS view address: %w", err)
	}

	fwssv, err := NewFilecoinWarmStorageServiceStateView(viewAddr, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("failed to create FWSS view: %w", err)
	}

	config, err := fwssv.GetPDPConfig(contract.EthCallOpts(ctx))
	if err != nil {
		return nil, xerrors.Errorf("failed to get FWSS PDP config: %w", err)
	}
	maxProvingPeriod := new(big.Int).SetUint64(config.MaxProvingPeriod)
	if maxProvingPeriod.Sign() <= 0 {
		return nil, xerrors.Errorf("FWSS max proving period is zero")
	}

	return func(ctx context.Context, railID *big.Int, rail filecoinpayment.PaymentsRailView, currentEpoch *big.Int) (*big.Int, bool, error) {
		if rail.Validator == (common.Address{}) {
			// CDN/cache rails use immediate one-time payments and no FWSS
			// validator, but terminated rails still need settlement so
			// Filecoin Pay can finalize and release fixed lockup accounting.
			target, ok := standardSettleTarget(rail, currentEpoch)
			return target, ok, nil
		}
		if rail.Validator != serviceAddr {
			return nil, false, xerrors.Errorf("FWSS rail %s uses unexpected validator %s", railID.String(), rail.Validator.Hex())
		}

		dataSet, err := fwssv.RailToDataSet(contract.EthCallOpts(ctx), new(big.Int).Set(railID))
		if err != nil {
			return nil, false, xerrors.Errorf("failed to get FWSS data set for rail %s: %w", railID.String(), err)
		}
		if dataSet.Sign() == 0 {
			return nil, false, xerrors.Errorf("FWSS rail %s has no associated data set", railID.String())
		}

		dataSetInfo, err := fwssv.GetDataSet(contract.EthCallOpts(ctx), new(big.Int).Set(dataSet))
		if err != nil {
			return nil, false, xerrors.Errorf("failed to get FWSS data set %s for rail %s: %w", dataSet.String(), railID.String(), err)
		}
		if dataSetInfo.PdpRailId == nil || dataSetInfo.PdpRailId.Cmp(railID) != 0 {
			return nil, false, xerrors.Errorf("FWSS validator rail %s is not the PDP rail for data set %s", railID.String(), dataSet.String())
		}

		activationEpoch, err := fwssv.ProvingActivationEpoch(contract.EthCallOpts(ctx), new(big.Int).Set(dataSet))
		if err != nil {
			return nil, false, xerrors.Errorf("failed to get FWSS proving activation epoch for data set %s: %w", dataSet.String(), err)
		}

		target, ok := pdpSettleTarget(rail, currentEpoch, activationEpoch, maxProvingPeriod)
		return target, ok, nil
	}, nil
}

// standardSettleTarget returns the normal Filecoin Pay target for FWSS rails
// that are not validated by FWSS proving state. This is used for CDN/cache
// rails where settlement is about Pay lockup/finalization, not PDP proofs.
func standardSettleTarget(rail filecoinpayment.PaymentsRailView, currentEpoch *big.Int) (*big.Int, bool) {
	if currentEpoch == nil || rail.SettledUpTo == nil {
		return nil, false
	}

	target := new(big.Int).Set(currentEpoch)
	if rail.EndEpoch != nil && rail.EndEpoch.Sign() > 0 && rail.EndEpoch.Cmp(target) < 0 {
		target.Set(rail.EndEpoch)
	}
	if target.Cmp(rail.SettledUpTo) <= 0 {
		return nil, false
	}

	return target, true
}

// pdpSettleTarget returns the largest epoch FWSS can safely ask Filecoin Pay
// to settle. For activated datasets, FWSS validation only accepts epochs up to
// the last proving-period deadline that is strictly before the current chain
// epoch; open periods must be left unsettled because they can still be proven.
func pdpSettleTarget(rail filecoinpayment.PaymentsRailView, currentEpoch, activationEpoch, maxProvingPeriod *big.Int) (*big.Int, bool) {
	target, ok := standardSettleTarget(rail, currentEpoch)
	if !ok {
		return nil, false
	}

	if shouldSkipFWSSActivationTerminationBoundary(rail, activationEpoch) {
		return nil, false
	}

	if activationEpoch != nil && activationEpoch.Sign() > 0 {
		if maxProvingPeriod == nil || maxProvingPeriod.Sign() <= 0 {
			return nil, false
		}
		if currentEpoch.Cmp(activationEpoch) < 0 {
			return nil, false
		}

		closedTarget := new(big.Int).Set(activationEpoch)
		if currentEpoch.Cmp(activationEpoch) > 0 {
			delta := new(big.Int).Sub(new(big.Int).Set(currentEpoch), activationEpoch)
			delta.Sub(delta, big.NewInt(1))
			closedPeriods := new(big.Int).Div(delta, maxProvingPeriod)
			closedOffset := new(big.Int).Mul(closedPeriods, maxProvingPeriod)
			closedTarget.Add(closedTarget, closedOffset)
		}

		if target.Cmp(closedTarget) > 0 {
			target.Set(closedTarget)
		}
		if target.Cmp(activationEpoch) < 0 {
			// A terminated PDP rail can have endEpoch before proving activation.
			// FWSS cannot validate that range today, but skipping it would hide a
			// rail that still needs final settlement/finalization from lockup.
			if rail.EndEpoch != nil && rail.EndEpoch.Sign() > 0 {
				return target, true
			}
			return nil, false
		}
	}

	if target.Cmp(rail.SettledUpTo) <= 0 {
		return nil, false
	}

	return target, true
}

// shouldSkipFWSSActivationTerminationBoundary is a temporary FWSS contract
// workaround. When a terminated PDP rail ends exactly at proving activation,
// FWSS accepts toEpoch == activationEpoch in its outer range check but then
// treats the same epoch as an invalid proving period internally. Lower targets
// are also invalid for that rail, so Curio has no settlement epoch to submit
// until the contract edge case is fixed.
func shouldSkipFWSSActivationTerminationBoundary(rail filecoinpayment.PaymentsRailView, activationEpoch *big.Int) bool {
	if activationEpoch == nil || activationEpoch.Sign() <= 0 {
		return false
	}
	if rail.SettledUpTo == nil || rail.EndEpoch == nil || rail.EndEpoch.Sign() <= 0 {
		return false
	}
	if rail.EndEpoch.Cmp(activationEpoch) != 0 {
		return false
	}

	return rail.SettledUpTo.Cmp(rail.EndEpoch) < 0
}
