package indexstore

import (
	"context"
	"math"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/gocql"
	"golang.org/x/xerrors"
)

// ---------------------------------------------------------------------------
// CID migration
// ---------------------------------------------------------------------------

// UpdatePieceCidV1ToV2 re-keys every row in both tables from oldCid (v1) to
// newCid (v2). Logged batches are used so that each page of mutations is
// applied atomically.
func (i *IndexStore) UpdatePieceCidV1ToV2(ctx context.Context, oldCid cid.Cid, newCid cid.Cid) error {
	old := oldCid.Bytes()
	new_ := newCid.Bytes()

	// Derive a page size that keeps each logged batch under the configured limit.
	batchLimit := i.settings.InsertBatchSize
	if batchLimit <= 0 {
		batchLimit = defaultBatchSize
	}
	// Each row produces two entries (insert + delete), so halve the limit.
	pageSize := int(math.Floor(float64(batchLimit) / 2))

	// flush executes a batch only when it has entries.
	flush := func(batch *gocql.Batch) error {
		if len(batch.Entries) == 0 {
			return nil
		}
		if err := i.executeBatchWithRetry(ctx, batch, newCid); err != nil {
			return xerrors.Errorf("migrating piece %s → %s: %w", oldCid, newCid, err)
		}
		return nil
	}

	// Pass 1: PayloadToPieces – insert under newCid, delete under oldCid.
	{
		iter := i.session.Query(
			`SELECT PayloadMultihash, BlockSize FROM PayloadToPieces WHERE PieceCid = ?`, old,
		).WithContext(ctx).PageSize(pageSize).Iter()

		batch := i.session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		var mh []byte
		var bs int64
		for iter.Scan(&mh, &bs) {
			// Copy multihash – driver reuses the underlying slice.
			mhCopy := make([]byte, len(mh))
			copy(mhCopy, mh)

			// Insert the row under the new (v2) CID.
			batch.Entries = append(batch.Entries, gocql.BatchEntry{
				Stmt: `INSERT INTO PayloadToPieces (PayloadMultihash, PieceCid, BlockSize) VALUES (?, ?, ?)`,
				Args: []any{mhCopy, new_, bs}, Idempotent: true,
			})
			// Delete the row under the old (v1) CID.
			batch.Entries = append(batch.Entries, gocql.BatchEntry{
				Stmt: `DELETE FROM PayloadToPieces WHERE PayloadMultihash = ? AND PieceCid = ?`,
				Args: []any{mhCopy, old}, Idempotent: true,
			})

			if len(batch.Entries) >= batchLimit {
				if err := flush(batch); err != nil {
					_ = iter.Close()
					return err
				}
				batch = i.session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
			}
		}
		if err := iter.Close(); err != nil {
			return xerrors.Errorf("scan PayloadToPieces for piece %s: %w", oldCid, err)
		}
		if err := flush(batch); err != nil {
			return err
		}
	}

	// Pass 2: PieceBlockOffsetSize – insert under newCid, delete under oldCid.
	{
		iter := i.session.Query(
			`SELECT PayloadMultihash, BlockOffset FROM PieceBlockOffsetSize WHERE PieceCid = ?`, old,
		).WithContext(ctx).PageSize(pageSize).Iter()

		batch := i.session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		var mh []byte
		var off int64
		for iter.Scan(&mh, &off) {
			// Copy multihash – driver reuses the underlying slice.
			mhCopy := make([]byte, len(mh))
			copy(mhCopy, mh)

			// Insert the row under the new (v2) CID.
			batch.Entries = append(batch.Entries, gocql.BatchEntry{
				Stmt: `INSERT INTO PieceBlockOffsetSize (PieceCid, PayloadMultihash, BlockOffset) VALUES (?, ?, ?)`,
				Args: []any{new_, mhCopy, off}, Idempotent: true,
			})
			// Delete the row under the old (v1) CID.
			batch.Entries = append(batch.Entries, gocql.BatchEntry{
				Stmt: `DELETE FROM PieceBlockOffsetSize WHERE PieceCid = ? AND PayloadMultihash = ?`,
				Args: []any{old, mhCopy}, Idempotent: true,
			})

			if len(batch.Entries) >= batchLimit {
				if err := flush(batch); err != nil {
					_ = iter.Close()
					return err
				}
				batch = i.session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
			}
		}
		if err := iter.Close(); err != nil {
			return xerrors.Errorf("scan PieceBlockOffsetSize for piece %s: %w", oldCid, err)
		}
		if err := flush(batch); err != nil {
			return err
		}
	}

	return nil
}
