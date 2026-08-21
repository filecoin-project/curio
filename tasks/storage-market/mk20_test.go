package storage_market

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestMK20StartEpochTooSoon(t *testing.T) {
	head := abi.ChainEpoch(100)
	duration := abi.ChainEpoch(20)
	atBoundary := abi.ChainEpoch(120)
	tooSoon := abi.ChainEpoch(119)

	if mk20StartEpochTooSoon(nil, head, duration) {
		t.Fatal("nil start epoch must use the dynamic default")
	}
	if mk20StartEpochTooSoon(&atBoundary, head, duration) {
		t.Fatal("start epoch at the expected-duration boundary must be accepted")
	}
	if !mk20StartEpochTooSoon(&tooSoon, head, duration) {
		t.Fatal("start epoch inside the expected seal duration must be rejected")
	}
}

func TestIntegration_FailMK20DealBeforeSector(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	market := &CurioStorageDealMarket{db: db}

	tests := []struct {
		name         string
		sector       any
		wantFailed   bool
		wantPipeline int
	}{
		{name: "unassigned deal is failed", wantFailed: true},
		{name: "assigned deal is unchanged", sector: int64(123), wantPipeline: 1},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := fmt.Sprintf("mk20-failure-test-%d", i)
			insertMK20FailureTestDeal(t, ctx, db, id, test.sector)

			reason := "deal cannot be sealed before its start epoch"
			failed, err := market.failMK20DealBeforeSector(ctx, id, reason)
			require.NoError(t, err)
			require.Equal(t, test.wantFailed, failed)

			var dealError sql.NullString
			var marker string
			err = db.QueryRow(ctx, `SELECT ddo_v1->>'error', ddo_v1->>'marker' FROM market_mk20_deal WHERE id = $1`, id).Scan(&dealError, &marker)
			require.NoError(t, err)
			require.Equal(t, "kept", marker)

			var pipelineRows int
			var pipelineSector sql.NullInt64
			err = db.QueryRow(ctx, `SELECT COUNT(*), MAX(sector) FROM market_mk20_pipeline WHERE id = $1`, id).Scan(&pipelineRows, &pipelineSector)
			require.NoError(t, err)
			require.Equal(t, test.wantPipeline, pipelineRows)

			if test.wantFailed {
				require.Equal(t, sql.NullString{String: reason, Valid: true}, dealError)
				require.False(t, pipelineSector.Valid)
			} else {
				require.Equal(t, sql.NullString{String: "", Valid: true}, dealError)
				require.Equal(t, sql.NullInt64{Int64: 123, Valid: true}, pipelineSector)
			}
		})
	}
}

func insertMK20FailureTestDeal(t *testing.T, ctx context.Context, db *harmonydb.DB, id string, sector any) {
	t.Helper()

	_, err := db.Exec(ctx, `INSERT INTO market_mk20_deal (id, client, ddo_v1)
		VALUES ($1, $2, $3)`, id, "client", `{"ddo": {}, "deal_id": 0, "complete": false, "error": "", "marker": "kept"}`)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `INSERT INTO market_mk20_pipeline (
		id, sp_id, contract, client, piece_cid_v2, piece_cid, piece_size,
		raw_size, offline, url, indexing, announce, duration, aggregated, sector
	) VALUES ($1, 1000, '', 'client', $2, $3, 128, 127, FALSE, 'pieceref:1', FALSE, FALSE, 1000, TRUE, $4)`,
		id, "piece-v2-"+id, "piece-"+id, sector)
	require.NoError(t, err)
}
