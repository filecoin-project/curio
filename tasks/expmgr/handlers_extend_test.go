package expmgr

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
)

func TestSplitParamsPreservesSectors(t *testing.T) {
	params := &miner.ExtendSectorExpiration2Params{
		Extensions: []miner.ExpirationExtension2{{
			Deadline:      3,
			Partition:     2,
			Sectors:       sectorNumsToBitfield([]abi.SectorNumber{1, 3, 8, 13, 21}),
			NewExpiration: 10000,
		}},
	}

	splits, err := splitParams(params)
	require.NoError(t, err)
	require.Len(t, splits, 2)
	seen := make(map[uint64]bool)
	for _, split := range splits {
		require.Len(t, split.Extensions, 1)
		ext := split.Extensions[0]
		require.Equal(t, params.Extensions[0].Deadline, ext.Deadline)
		require.Equal(t, params.Extensions[0].Partition, ext.Partition)
		require.Equal(t, params.Extensions[0].NewExpiration, ext.NewExpiration)
		require.Empty(t, ext.SectorsWithClaims)
		count, err := ext.Sectors.Count()
		require.NoError(t, err)
		require.Greater(t, count, uint64(0))
		require.Less(t, count, uint64(5))
		require.NoError(t, ext.Sectors.ForEach(func(sn uint64) error {
			require.False(t, seen[sn], "sector %d occurs in multiple messages", sn)
			seen[sn] = true
			return nil
		}))
	}
	require.Equal(t, map[uint64]bool{1: true, 3: true, 8: true, 13: true, 21: true}, seen)
}

func TestValidateSectorAgainstChainLegacyDealWeights(t *testing.T) {
	for _, tc := range []struct {
		name     string
		dealIDs  []abi.DealID
		regular  int64
		verified int64
		isCC     bool
	}{
		{name: "committed capacity", isCC: true},
		{name: "legacy market deal", dealIDs: []abi.DealID{5}},
		{name: "regular data", regular: 1},
		{name: "legacy verified data", verified: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &miner.SectorOnChainInfo{
				Expiration:         10000,
				DeprecatedDealIDs:  tc.dealIDs,
				DealWeight:         big.NewInt(tc.regular),
				VerifiedDealWeight: big.NewInt(tc.verified),
			}
			metadata := SectorMeta{SectorNum: 1, Expiration: 10000, IsCC: tc.isCC}
			require.NoError(t, validateSectorAgainstChain(1, metadata, info))

			metadata.Expiration++
			require.ErrorContains(t, validateSectorAgainstChain(1, metadata, info), "expiration mismatch")
			metadata.Expiration--
			metadata.IsCC = !metadata.IsCC
			require.ErrorContains(t, validateSectorAgainstChain(1, metadata, info), "CC status mismatch")
		})
	}
}
