package webrpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/lazy"
	"github.com/filecoin-project/curio/pdp/paymentstatus"
)

func TestCountAtRiskByStatus(t *testing.T) {
	entries := []atRiskEntry{
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusGrace}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusTerminating}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusTerminating}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusPendingDelete}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusTerminated}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusOK}},
	}
	got := countAtRiskByStatus(entries)
	if got.Total != 6 || got.Grace != 1 || got.Terminated != 1 || got.Terminating != 2 || got.PendingDelete != 1 {
		t.Fatalf("counts = %+v", got)
	}
}

func TestMergeAtRiskEntries(t *testing.T) {
	entry := func(id int64, payer string) atRiskEntry {
		return atRiskEntry{
			stats: pdpDataSetStatsRow{ID: id},
			snap:  paymentstatus.Snapshot{DataSetID: id, Payer: payer},
		}
	}

	got := mergeAtRiskEntries(
		[]atRiskEntry{entry(1, ""), entry(2, "")},
		[]atRiskEntry{entry(2, "0xpayer"), entry(3, "0xother")},
	)
	require.Equal(t, []atRiskEntry{entry(1, ""), entry(2, "0xpayer"), entry(3, "0xother")}, got)
}

func TestIntegration_PDPDataSetAtRiskCount_RecordedDataSetsWithoutChain(t *testing.T) {
	const recorded = int64(921_000_000_001)

	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	rpc := &WebRPC{Deps: &deps.Deps{
		DB: db,
		EthClient: lazy.MakeLazy(func() (ethchain.EthClient, error) {
			return nil, errors.New("eth client unavailable")
		}),
	}}

	_, err = rpc.PDPDataSetAtRiskCount(ctx)
	require.ErrorContains(t, err, "eth client unavailable", "nothing is known without the chain or a record")

	insertRequestDeletionDataSet(t, ctx, db, recorded)
	_, err = db.Exec(ctx, `INSERT INTO pdp_data_set_review (data_set, service_end_epoch) VALUES ($1, 5000)`, recorded)
	require.NoError(t, err)

	// The data set holds no pieces, which is below the size the chain scan covers.
	entries, err := rpc.awaitingDecisionEntries(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, recorded, entries[0].stats.ID)
	require.Zero(t, entries[0].stats.SizeBytes)
	require.Equal(t, paymentstatus.StatusTerminated, entries[0].snap.Status)

	counts, err := rpc.PDPDataSetAtRiskCount(ctx)
	require.NoError(t, err)
	require.Equal(t, PDPDataSetAtRiskCounts{Total: 1, Terminated: 1}, counts)
}
