package indexstore

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/gocql"
	"golang.org/x/xerrors"
)

// ---------------------------------------------------------------------------
// Internal batch helpers
// ---------------------------------------------------------------------------

// flushBatch is a convenience wrapper: it calls executeBatchWithRetry only
// when the batch is non-nil and has entries.
func (i *IndexStore) flushBatch(ctx context.Context, batch *gocql.Batch, pieceCid cid.Cid) error {
	if batch == nil || len(batch.Entries) == 0 {
		return nil
	}
	return i.executeBatchWithRetry(ctx, batch, pieceCid)
}

// executeBatchWithRetry executes a CQL batch, with retries
func (i *IndexStore) executeBatchWithRetry(ctx context.Context, batch *gocql.Batch, pieceCid cid.Cid) error {
	backoff := initialBackoff

	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		start := time.Now()
		err = i.session.ExecuteBatch(batch)
		elapsed := time.Since(start)

		// Log slow batches as warnings, normal ones as debug.
		if elapsed > slowBatchThreshold {
			log.Warnw("Batch Insert", "took", elapsed, "entries", len(batch.Entries))
		} else {
			log.Debugw("Batch Insert", "took", elapsed, "entries", len(batch.Entries))
		}

		// Success – return immediately.
		if err == nil {
			return nil
		}
		// Context cancelled – no point retrying.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Warnf("Batch attempt %d failed for piece %s: %v", attempt+1, pieceCid, err)

		// Exhausted all retries.
		if attempt == maxRetries {
			return xerrors.Errorf("batch insert for piece %s after %d retries: %w", pieceCid, maxRetries, err)
		}

		// Wait with exponential backoff, respecting context cancellation.
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	return nil
}
