//go:build !skiff

package deps

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps/stats"
)

func initProofTypes(deps *Deps) error {
	if deps.ProofTypes == nil {
		deps.ProofTypes = map[abi.RegisteredSealProof]bool{}
	}
	if len(deps.ProofTypes) == 0 {
		for maddr := range deps.Maddrs.Get() {
			spt, err := sealProofType(maddr, deps.Chain)
			if err != nil {
				return err
			}
			deps.ProofTypes[spt] = true
		}
	}
	if deps.Cfg.Subsystems.EnableProofShare {
		deps.ProofTypes[abi.RegisteredSealProof_StackedDrg32GiBV1_1] = true
	}
	return nil
}

func startWalletExporterIfEnabled(ctx context.Context, deps *Deps) {
	if !deps.Cfg.Subsystems.EnableWalletExporter {
		return
	}
	spIDs := []address.Address{}
	for maddr := range deps.Maddrs.Get() {
		spIDs = append(spIDs, address.Address(maddr))
	}
	stats.StartWalletExporter(ctx, deps.DB, deps.Chain, spIDs)
}

func sealProofType(maddr dtypes.MinerAddress, fnapi api.Chain) (abi.RegisteredSealProof, error) {
	mi, err := fnapi.StateMinerInfo(context.TODO(), address.Address(maddr), types.EmptyTSK)
	if err != nil {
		return 0, err
	}
	networkVersion, err := fnapi.StateNetworkVersion(context.TODO(), types.EmptyTSK)
	if err != nil {
		return 0, err
	}
	return miner.PreferredSealProofTypeFromWindowPoStType(networkVersion, mi.WindowPoStProofType, false)
}
