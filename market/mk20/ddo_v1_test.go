package mk20

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"
)

func TestMarketDealViewPreservesHistoricalAllocation(t *testing.T) {
	provider, err := address.NewIDAddress(1000)
	require.NoError(t, err)
	client, err := address.NewIDAddress(2000)
	require.NoError(t, err)
	allocationID := verifreg.AllocationId(4321)
	marketDealID := uint64(1234)
	start := abi.ChainEpoch(100)

	for _, test := range []struct {
		name       string
		allocation *verifreg.AllocationId
		duration   abi.ChainEpoch
	}{
		{name: "new deal", duration: 518400},
		{name: "historical allocation deal", allocation: &allocationID, duration: 1000},
	} {
		t.Run(test.name, func(t *testing.T) {
			deal := Deal{
				Client: client.String(),
				Data: &DataSource{
					PieceCID: mustCID(t, "bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pq"),
				},
				Products: Products{DDOV1: &DDOV1{
					Provider:     provider,
					StartEpoch:   &start,
					Duration:     test.duration,
					AllocationId: test.allocation,
					MarketDealID: &marketDealID,
				}},
			}
			stored, err := json.Marshal(deal)
			require.NoError(t, err)

			var loaded Deal
			require.NoError(t, json.Unmarshal(stored, &loaded))
			view, err := loaded.Products.DDOV1.marketDealView(&loaded)
			require.NoError(t, err)
			require.Zero(t, view.AllocationId.Sign())
			require.Equal(t, marketDealID, view.DealId.Uint64())
			require.Equal(t, uint64(1000), view.ProviderActorId.Uint64())
			require.Equal(t, client.Bytes(), view.ClientId)
			require.Equal(t, deal.Data.PieceCID.Bytes(), view.PieceCidV2)
			require.Equal(t, int64(start), view.StartEpoch.Int64())
			require.Equal(t, int64(test.duration), view.Duration.Int64())
			require.Zero(t, view.FinalizedEpoch.Sign())

			after, err := json.Marshal(loaded)
			require.NoError(t, err)
			require.JSONEq(t, string(stored), string(after))
		})
	}
}
