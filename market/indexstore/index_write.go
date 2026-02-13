package indexstore

import (
	"context"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/gocql"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"
)

// ---------------------------------------------------------------------------
// Index write operations
// ---------------------------------------------------------------------------

// AddIndex inserts multihash → pieceCid mappings for every record received
// on recordsChan. Callers must close the channel when all records have been
// sent. The pieceCid may be either v1 or v2.
//
// Internally the work is split across InsertConcurrency goroutines, each
// accumulating records into unlogged batches of InsertBatchSize rows.
func (i *IndexStore) AddIndex(ctx context.Context, pieceCid cid.Cid, recordsChan chan Record) error {
	const (
		// CQL statement that maps (pieceCid, multihash) → byte offset.
		insertOffsetStmt = `INSERT INTO PieceBlockOffsetSize (PieceCid, PayloadMultihash, BlockOffset) VALUES (?, ?, ?)`
		// CQL statement that maps multihash → (pieceCid, block size).
		insertPayloadStmt = `INSERT INTO PayloadToPieces (PayloadMultihash, PieceCid, BlockSize) VALUES (?, ?, ?)`
	)
	// Pre-serialise the piece CID once for all workers.
	pieceCidBytes := pieceCid.Bytes()

	// Fan out record consumption across InsertConcurrency workers.
	var eg errgroup.Group
	for worker := 0; worker < i.settings.InsertConcurrency; worker++ {
		eg.Go(func() error {
			var batchOffset, batchPayload *gocql.Batch
			for {
				// Lazily initialise offset batch (PieceBlockOffsetSize table).
				if batchOffset == nil {
					batchOffset = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batchOffset.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}
				// Lazily initialise payload batch (PayloadToPieces table).
				if batchPayload == nil {
					batchPayload = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batchPayload.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}

				// Block until next record arrives or channel is closed.
				rec, ok := <-recordsChan
				if !ok {
					// Channel closed – flush any remaining entries in both batches.
					if err := i.flushBatch(ctx, batchOffset, pieceCid); err != nil {
						return err
					}
					if err := i.flushBatch(ctx, batchPayload, pieceCid); err != nil {
						return err
					}
					return nil
				}

				// Extract the raw multihash bytes from the record's CID.
				mhBytes := []byte(rec.Cid.Hash())

				// Append an offset row: maps (pieceCid, multihash) → byte offset.
				batchOffset.Entries = append(batchOffset.Entries, gocql.BatchEntry{
					Stmt: insertOffsetStmt, Args: []interface{}{pieceCidBytes, mhBytes, rec.Offset}, Idempotent: true,
				})
				// Append a payload row: maps multihash → (pieceCid, block size).
				batchPayload.Entries = append(batchPayload.Entries, gocql.BatchEntry{
					Stmt: insertPayloadStmt, Args: []interface{}{mhBytes, pieceCidBytes, rec.Size}, Idempotent: true,
				})

				// Flush offset batch when it reaches the configured size limit.
				if len(batchOffset.Entries) == i.settings.InsertBatchSize {
					if err := i.executeBatchWithRetry(ctx, batchOffset, pieceCid); err != nil {
						return err
					}
					batchOffset = nil // will be re-created on next iteration
				}
				// Flush payload batch when it reaches the configured size limit.
				if len(batchPayload.Entries) == i.settings.InsertBatchSize {
					if err := i.executeBatchWithRetry(ctx, batchPayload, pieceCid); err != nil {
						return err
					}
					batchPayload = nil // will be re-created on next iteration
				}
			}
		})
	}

	// Wait for all workers to drain the channel and flush.
	if err := eg.Wait(); err != nil {
		return xerrors.Errorf("addindex wait: %w", err)
	}
	return nil
}

// RemoveIndexes deletes every multihash mapping and offset row associated
// with the given piece CID from both tables.
func (i *IndexStore) RemoveIndexes(ctx context.Context, pieceCid cid.Cid) error {
	pieceCidBytes := pieceCid.Bytes()

	// Collect all multihashes stored under this piece.
	multihashes, err := i.collectMultihashes(ctx, pieceCidBytes)
	if err != nil {
		return xerrors.Errorf("scanning PayloadMultihash for piece %s: %w", pieceCid, err)
	}

	// Batch-delete from PayloadToPieces.
	if err := i.batchDeletePayloadToPieces(ctx, pieceCid, pieceCidBytes, multihashes); err != nil {
		return err
	}

	// Single partition delete from PieceBlockOffsetSize.
	qry := `DELETE FROM PieceBlockOffsetSize WHERE PieceCid = ?`
	if err := i.session.Query(qry, pieceCidBytes).WithContext(ctx).Exec(); err != nil {
		return xerrors.Errorf("deleting PieceBlockOffsetSize for piece %s: %w", pieceCid, err)
	}

	return nil
}

// collectMultihashes returns every PayloadMultihash stored in
// PieceBlockOffsetSize for the given piece.
func (i *IndexStore) collectMultihashes(ctx context.Context, pieceCidBytes []byte) ([][]byte, error) {
	qry := `SELECT PayloadMultihash FROM PieceBlockOffsetSize WHERE PieceCid = ?`
	iter := i.session.Query(qry, pieceCidBytes).WithContext(ctx).Iter()

	var raw []byte
	var out [][]byte
	for iter.Scan(&raw) {
		// Copy the bytes – the driver reuses the underlying slice.
		cp := make([]byte, len(raw))
		copy(cp, raw)
		out = append(out, cp)
	}
	return out, iter.Close()
}

// batchDeletePayloadToPieces removes rows from PayloadToPieces for the given
// piece CID in batches of InsertBatchSize.
func (i *IndexStore) batchDeletePayloadToPieces(ctx context.Context, pieceCid cid.Cid, pieceCidBytes []byte, multihashes [][]byte) error {
	const delQryStmt = `DELETE FROM PayloadToPieces WHERE PayloadMultihash = ? AND PieceCid = ?`
	batch := i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)

	for idx, mh := range multihashes {
		batch.Entries = append(batch.Entries, gocql.BatchEntry{
			Stmt: delQryStmt, Args: []interface{}{mh, pieceCidBytes}, Idempotent: true,
		})

		// Flush when the batch is full or we've reached the last multihash.
		if len(batch.Entries) >= i.settings.InsertBatchSize || idx == len(multihashes)-1 {
			if err := i.executeBatchWithRetry(ctx, batch, pieceCid); err != nil {
				return xerrors.Errorf("batch delete PayloadToPieces for piece %s: %w", pieceCid, err)
			}
			batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
		}
	}

	// Flush any remaining entries (possible empty batch – harmless).
	if len(batch.Entries) > 0 {
		if err := i.executeBatchWithRetry(ctx, batch, pieceCid); err != nil {
			return xerrors.Errorf("batch delete PayloadToPieces for piece %s: %w", pieceCid, err)
		}
	}
	return nil
}
