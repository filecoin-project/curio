package FWSS

import (
	"context"
	"math/big"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
)

// SettleTargetResolver builds a Filecoin Pay settlement target resolver for
// FWSS rails. It keeps FWSS-specific validator boundaries outside the common
// payment package by resolving the rail's dataset and proving state through the
// FWSS view contract.
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
		if rail.Validator != serviceAddr {
			return nil, false, nil
		}

		dataSet, err := fwssv.RailToDataSet(contract.EthCallOpts(ctx), new(big.Int).Set(railID))
		if err != nil {
			return nil, false, xerrors.Errorf("failed to get FWSS data set for rail %s: %w", railID.String(), err)
		}
		if dataSet.Sign() == 0 {
			return nil, false, xerrors.Errorf("FWSS rail %s has no associated data set", railID.String())
		}

		activationEpoch, err := fwssv.ProvingActivationEpoch(contract.EthCallOpts(ctx), new(big.Int).Set(dataSet))
		if err != nil {
			return nil, false, xerrors.Errorf("failed to get FWSS proving activation epoch for data set %s: %w", dataSet.String(), err)
		}

		target, ok := settleTarget(rail, currentEpoch, activationEpoch, maxProvingPeriod)
		return target, ok, nil
	}, nil
}

// settleTarget returns the largest epoch FWSS can safely ask Filecoin Pay
// to settle. For activated datasets, FWSS validation only accepts epochs up to
// the last proving-period deadline that is strictly before the current chain
// epoch; open periods must be left unsettled because they can still be proven.
func settleTarget(rail filecoinpayment.PaymentsRailView, currentEpoch, activationEpoch, maxProvingPeriod *big.Int) (*big.Int, bool) {
	if currentEpoch == nil || rail.SettledUpTo == nil {
		return nil, false
	}

	target := new(big.Int).Set(currentEpoch)
	if rail.EndEpoch != nil && rail.EndEpoch.Sign() > 0 && rail.EndEpoch.Cmp(target) < 0 {
		target.Set(rail.EndEpoch)
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
			return nil, false
		}
	}

	if target.Cmp(rail.SettledUpTo) <= 0 {
		return nil, false
	}

	return target, true
}
