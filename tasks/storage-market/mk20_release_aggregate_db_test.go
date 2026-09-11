package storage_market

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-commp-utils/v2/zerocomm"
	"github.com/filecoin-project/go-data-segment/datasegment"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/market/mk20release"
)

// Construct commitments only: no CAR bytes, HTTP requests, or chain calls.
func mk20ReleaseAggregateFixture(t *testing.T) *mk20.Deal {
	t.Helper()
	var pieces []mk20.DataSource
	var infos []abi.PieceInfo
	for _, size := range []abi.PaddedPieceSize{128, 256} {
		v1 := zerocomm.ZeroPieceCommitment(size.Unpadded())
		v2, err := commcid.PieceCidV2FromV1(v1, uint64(size.Unpadded()))
		require.NoError(t, err)
		infos = append(infos, abi.PieceInfo{PieceCID: v1, Size: size})
		pieces = append(pieces, mk20.DataSource{
			PieceCID:   v2,
			Format:     mk20.PieceDataFormat{Raw: &mk20.FormatBytes{}},
			SourceHTTP: &mk20.DataSourceHTTP{URLs: []mk20.HttpUrl{{URL: "https://example.invalid/piece"}}},
		})
	}
	const size = abi.PaddedPieceSize(2048)
	aggregate, err := datasegment.NewAggregate(size, infos)
	require.NoError(t, err)
	v1, err := aggregate.PieceCID()
	require.NoError(t, err)
	v2, err := commcid.PieceCidV2FromV1(v1, uint64(size.Unpadded()))
	require.NoError(t, err)
	provider, err := address.NewIDAddress(1000)
	require.NoError(t, err)
	start := abi.ChainEpoch(100_000)
	return &mk20.Deal{
		Identifier: ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
		Client:     "aggregate-fixture",
		Data: &mk20.DataSource{
			PieceCID:        v2,
			Format:          mk20.PieceDataFormat{Aggregate: &mk20.FormatAggregate{Type: mk20.AggregateTypeV1}},
			SourceAggregate: &mk20.DataSourceAggregate{Pieces: pieces},
		},
		Products: mk20.Products{
			DDOV1:       &mk20.DDOV1{Provider: provider, Duration: 1_000_000, StartEpoch: &start},
			RetrievalV1: &mk20.RetrievalV1{},
		},
	}
}

func TestMK20ReleaseAggregateFixtureRowCost(t *testing.T) {
	deal := mk20ReleaseAggregateFixture(t)
	rows, err := mk20PipelineRowCost(deal)
	require.NoError(t, err)
	require.EqualValues(t, 2, rows)
	info, err := deal.PieceInfo()
	require.NoError(t, err)
	require.EqualValues(t, 2048, info.Size)
	require.NotEqual(t, deal.Data.SourceAggregate.Pieces[0].PieceCID, deal.Data.SourceAggregate.Pieces[1].PieceCID)
}

func seedMK20ReleaseAggregate(t *testing.T, ctx context.Context, db *harmonydb.DB) *mk20.Deal {
	t.Helper()
	deal := mk20ReleaseAggregateFixture(t)
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if err := deal.SaveToDB(tx); err != nil {
			return false, err
		}
		n, err := tx.Exec(`INSERT INTO market_mk20_pipeline_waiting (id) VALUES ($1)`, deal.Identifier.String())
		return n == 1 && err == nil, err
	})
	require.NoError(t, err)
	require.True(t, committed)
	return deal
}

func assertMK20ReleaseAggregateRows(t *testing.T, ctx context.Context, db *harmonydb.DB, deal *mk20.Deal, inserted bool) {
	t.Helper()
	var pipeline, downloads, refs, parked, waiting int
	err := db.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM market_mk20_pipeline WHERE id = $1),
		(SELECT COUNT(*) FROM market_mk20_download_pipeline WHERE id = $1),
		(SELECT COUNT(*) FROM parked_piece_refs),
		(SELECT COUNT(*) FROM parked_pieces),
		(SELECT COUNT(*) FROM market_mk20_pipeline_waiting WHERE id = $1)`, deal.Identifier.String()).
		Scan(&pipeline, &downloads, &refs, &parked, &waiting)
	require.NoError(t, err)
	want, wantWaiting := 0, 1
	if inserted {
		want, wantWaiting = 2, 0
	}
	require.Equal(t, []int{want, want, want, want, wantWaiting}, []int{pipeline, downloads, refs, parked, waiting})
	if !inserted {
		return
	}
	var matched int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_pipeline p
		JOIN market_mk20_download_pipeline d ON d.id = p.id AND d.piece_cid_v2 = p.piece_cid_v2
		JOIN parked_piece_refs r ON r.ref_id = ANY(d.ref_ids)
		JOIN parked_pieces pp ON pp.id = r.piece_id
		WHERE p.id = $1 AND p.sp_id = 1000 AND p.deal_aggregation = $2
		AND p.duration = $3 AND p.complete = FALSE AND p.started = TRUE AND p.offline = FALSE
		AND d.product = $4 AND cardinality(d.ref_ids) = 1 AND r.long_term = FALSE
		AND pp.piece_cid = p.piece_cid AND pp.piece_padded_size = p.piece_size
		AND ((p.aggr_index = 0 AND p.piece_cid_v2 = $5) OR (p.aggr_index = 1 AND p.piece_cid_v2 = $6))`,
		deal.Identifier.String(), mk20.AggregateTypeV1, deal.Products.DDOV1.Duration, mk20.ProductNameDDOV1,
		deal.Data.SourceAggregate.Pieces[0].PieceCID.String(), deal.Data.SourceAggregate.Pieces[1].PieceCID.String()).Scan(&matched)
	require.NoError(t, err)
	require.Equal(t, 2, matched)
}

func TestMK20ReleaseDBAggregateCapacity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active int
		cap    int64
		want   mk20release.Outcome
	}{
		{name: "exact fit", active: 1, cap: 3, want: mk20release.Released},
		{name: "insufficient remaining slots", active: 1, cap: 2, want: mk20release.DoesNotFit},
		{name: "larger than entire cap", active: 0, cap: 1, want: mk20release.TooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbs := newMK20ReleaseITestDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			deal := seedMK20ReleaseAggregate(t, ctx, dbs.primary)
			if tc.active > 0 {
				seedActiveMK20PipelineRow(t, ctx, dbs.primary, "unrelated", 2000, false)
			}
			// Includes DealFromTX, the production row planner, actual batched
			// insertion, gate postconditions, and waiting deletion in one tx.
			outcome, err := releaseMK20WaitingDeal(ctx, dbs.secondary, deal.Identifier.String(), tc.cap)
			require.NoError(t, err)
			require.Equal(t, tc.want, outcome)
			inserted := outcome == mk20release.Released
			assertMK20ReleaseAggregateRows(t, ctx, dbs.primary, deal, inserted)
			active, waiting := tc.active, 1
			if inserted {
				active, waiting = active+2, 0
			}
			assertMK20ReleaseCounts(t, ctx, dbs.primary, active, waiting)
			_, err = dbs.primary.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				stored, err := mk20.DealFromTX(tx, deal.Identifier)
				if err == nil {
					require.Equal(t, deal.Products.DDOV1, stored.Products.DDOV1)
				}
				return false, err
			})
			require.NoError(t, err)
		})
	}
}

func TestMK20ReleaseDBAggregateInsertFailureRollsBack(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	deal := seedMK20ReleaseAggregate(t, ctx, dbs.primary)
	// Fail only after both download/ref writes and the first pipeline row are
	// visible in this transaction. This trigger belongs only to the fixture.
	_, err := dbs.primary.Exec(ctx, `CREATE FUNCTION reject_second_aggregate_row() RETURNS TRIGGER AS $$
	BEGIN
		IF NEW.aggr_index = 1 THEN
			RAISE EXCEPTION 'injected aggregate failure: pipeline=%, downloads=%, refs=%, parked=%',
				(SELECT COUNT(*) FROM market_mk20_pipeline WHERE id = NEW.id),
				(SELECT COUNT(*) FROM market_mk20_download_pipeline WHERE id = NEW.id),
				(SELECT COUNT(*) FROM parked_piece_refs), (SELECT COUNT(*) FROM parked_pieces);
		END IF;
		RETURN NEW;
	END; $$ LANGUAGE plpgsql;
	CREATE TRIGGER reject_second_aggregate_row BEFORE INSERT ON market_mk20_pipeline
	FOR EACH ROW EXECUTE FUNCTION reject_second_aggregate_row();`)
	require.NoError(t, err)
	outcome, err := releaseMK20WaitingDeal(ctx, dbs.secondary, deal.Identifier.String(), 2)
	require.ErrorContains(t, err, "injected aggregate failure: pipeline=1, downloads=2, refs=2, parked=2")
	require.NotEqual(t, mk20release.Released, outcome)
	assertMK20ReleaseAggregateRows(t, ctx, dbs.primary, deal, false)
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 0, 1)

	_, err = dbs.primary.Exec(ctx, `DROP TRIGGER reject_second_aggregate_row ON market_mk20_pipeline`)
	require.NoError(t, err)
	outcome, err = releaseMK20WaitingDeal(ctx, dbs.secondary, deal.Identifier.String(), 2)
	require.NoError(t, err)
	require.Equal(t, mk20release.Released, outcome)
	assertMK20ReleaseAggregateRows(t, ctx, dbs.primary, deal, true)
}
