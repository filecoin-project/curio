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

func AddIndexFromCAR(ctx context.Context, idx *indexstore.IndexStore, pieceCID cid.Cid, carBytes []byte) error {
	recs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})

	var eg errgroup.Group
	eg.Go(func() error {
		return idx.AddIndex(ctx, pieceCID, recs)
	})

	_, interrupted, idxErr := indexing.IndexCAR(bytes.NewReader(carBytes), 4<<20, recs, addFail)
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

func AddAggregateIndexFromPiece(t *testing.T, ctx context.Context, idx *indexstore.IndexStore, aggregate PieceFixture, subPieces []mk20.DataSource) error {
	recs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})

	var eg errgroup.Group
	eg.Go(func() error {
		return idx.AddIndex(ctx, aggregate.PieceCIDV2, recs)
	})

	blocks, aggidx, interrupted, idxErr := indexing.IndexAggregate(
		aggregate.PieceCIDV2,
		bytes.NewReader(aggregate.CarBytes),
		aggregate.PieceSize,
		subPieces,
		recs,
		addFail,
	)
	close(recs)

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

	for k, v := range aggidx {
		if err := idx.InsertAggregateIndex(ctx, k, v); err != nil {
			return fmt.Errorf("inserting aggregate index for %s: %w", k, err)
		}
		for i := range v {
			pieces, err := idx.FindPieceInAggregate(ctx, v[i].Cid)
			require.NoError(t, err)
			require.Len(t, pieces, 1)
			require.True(t, aggregate.PieceCIDV2.Equals(pieces[0].Cid))
		}
	}

	return nil
}

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
