package curiochain

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
)

func TestSectorQAPower(t *testing.T) {
	tests := []struct {
		name           string
		verifiedWeight int64
		flags          miner.SectorOnChainInfoFlags
		wantPower      int64
	}{
		{name: "legacy unverified sector", wantPower: 2048},
		{name: "legacy partially verified sector", verifiedWeight: 307200, wantPower: 11264},
		{name: "legacy fully verified sector", verifiedWeight: 614400, wantPower: 20480},
		{name: "full QA without verified weight", flags: miner.FULL_QA_POWER, wantPower: 20480},
		{name: "full QA overrides partial verified weight", flags: miner.FULL_QA_POWER, verifiedWeight: 307200, wantPower: 20480},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &miner.SectorOnChainInfo{
				SealProof:          abi.RegisteredSealProof_StackedDrg2KiBV1_1,
				Activation:         50,
				PowerBaseEpoch:     200,
				Expiration:         500,
				VerifiedDealWeight: big.NewInt(tt.verifiedWeight),
				Flags:              tt.flags,
			}

			power, err := SectorQAPower(info)
			require.NoError(t, err)
			require.Equal(t, big.NewInt(tt.wantPower), power)
		})
	}
}

func TestSectorQAPowerInvalidSealProof(t *testing.T) {
	power, err := SectorQAPower(&miner.SectorOnChainInfo{SealProof: abi.RegisteredSealProof(-1)})
	require.Error(t, err)
	require.Equal(t, big.Zero(), power)
}
