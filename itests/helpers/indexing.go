package helpers

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/tasks/indexing"
)

// AddIndexFromCAR indexes a CAR fixture into the test index store for the
// supplied piece CID.
func AddIndexFromCAR(ctx context.Context, idx *indexstore.IndexStore, pieceCID cid.Cid, carBytes []byte) error {
	recs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})

	var eg errgroup.Group
	eg.Go(func() error {
		return idx.AddIndex(ctx, pieceCID, recs)
	})

	_, _, interrupted, idxErr := indexing.IndexCAR(bytes.NewReader(carBytes), 4<<20, recs, addFail)
	close(recs)

	addErr := eg.Wait()
	if idxErr != nil {
		return idxErr
	}
	if addErr != nil {
		return addErr
	}
	if interrupted {
		return fmt.Errorf("indexing was interrupted while adding piece %s", pieceCID)
	}
	return nil
}

// AddAggregateIndexFromPiece indexes an aggregate fixture and stores both its
// payload block index and aggregate child-piece mappings in the test index
// store.
func AddAggregateIndexFromPiece(t *testing.T, ctx context.Context, idx *indexstore.IndexStore, aggregate PieceFixture, subPieces []mk20.DataSource) error {
	recs := make(chan indexstore.Record, 64)
	aggRecs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})

	var eg errgroup.Group
	eg.Go(func() error {
		return idx.AddIndex(ctx, aggregate.PieceCIDV2, recs)
	})
	eg.Go(func() error {
		return idx.InsertAggregateIndex(ctx, aggregate.PieceCIDV2, aggRecs)
	})

	blocks, interrupted, idxErr := indexing.IndexAggregate(
		aggregate.PieceCIDV2,
		bytes.NewReader(aggregate.CarBytes),
		aggregate.PieceSize,
		subPieces,
		recs,
		aggRecs,
		addFail,
	)
	close(recs)
	close(aggRecs)

	addErr := eg.Wait()
	if idxErr != nil {
		return idxErr
	}
	if addErr != nil {
		return addErr
	}
	if interrupted {
		return fmt.Errorf("aggregate indexing was interrupted for piece %s", aggregate.PieceCIDV2)
	}
	if blocks <= 0 {
		return fmt.Errorf("aggregate piece %s produced no indexed blocks", aggregate.PieceCIDV2)
	}

	for _, subPiece := range subPieces {
		pieces, err := idx.FindPieceInAggregate(ctx, subPiece.PieceCID)
		require.NoError(t, err)
		require.Len(t, pieces, 1)
		require.True(t, aggregate.PieceCIDV2.Equals(pieces[0].Cid))
	}

	return nil
}

// AddPDPv0IndexFromPiece indexes a PDPv0 parked piece for retrieval tests.
func AddPDPv0IndexFromPiece(t *testing.T, ctx context.Context, idx *indexstore.IndexStore, piece PieceFixture) error {
	recs := make(chan indexstore.Record, 64)
	aggRecs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})

	var eg errgroup.Group
	eg.Go(func() error {
		return idx.AddIndex(ctx, piece.PieceCIDV2, recs)
	})
	eg.Go(func() error {
		return idx.InsertAggregateIndex(ctx, piece.PieceCIDV2, aggRecs)
	})

	blocks, interrupted, idxErr := indexing.IndexPDPv0(
		piece.PieceCIDV2,
		bytes.NewReader(piece.CarBytes),
		piece.PieceSize,
		recs,
		aggRecs,
		addFail,
	)
	close(recs)
	close(aggRecs)

	addErr := eg.Wait()

	if idxErr != nil {
		return idxErr
	}
	if addErr != nil {
		return addErr
	}
	if interrupted {
		return fmt.Errorf("PDPv0 indexing was interrupted for piece %s", piece.PieceCIDV2)
	}
	if blocks <= 0 {
		return fmt.Errorf("PDPv0 piece %s produced no indexed blocks", piece.PieceCIDV2)
	}

	return nil
}

// LogIPNIStatus writes the current IPNI rows to the test log for debugging
// retrieval/indexing test failures.
func LogIPNIStatus(t *testing.T, ctx context.Context, db *harmonydb.DB) {
	var ipnirows []struct {
		AdCID      string         `db:"ad_cid"`
		AsRm       bool           `db:"is_rm"`
		Previous   sql.NullString `db:"previous"`
		PieceCidv2 string         `db:"piece_cid_v2"`
	}
	err := db.Select(ctx, &ipnirows, `SELECT ad_cid, is_rm, previous, piece_cid_v2 FROM ipni`)
	require.NoError(t, err)

	for _, row := range ipnirows {
		prev := ""
		if row.Previous.Valid {
			prev = row.Previous.String
		}
		t.Logf("IPNI: Ad: %s, rm: %v, previous: %s, piece cid v2: %s", row.AdCID, row.AsRm, prev, row.PieceCidv2)
	}
}
