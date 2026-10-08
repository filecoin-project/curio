package openpieces

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	// PLACE_WAIT_MAX bounds how long a client-facing completion waits for
	// placement, and how long a queued placement keeps its piece from being
	// reported ready. A piece whose placement takes longer is reported ready
	// anyway; it is served once placement finishes.
	PLACE_WAIT_MAX = 5 * time.Minute

	PLACE_POLL_MIN = 200 * time.Millisecond
	PLACE_POLL_MAX = 2 * time.Second
)

// WaitPlaced returns once none of the parked_piece_refs pieceRefs has a
// queued placement, or after PLACE_WAIT_MAX. Placement may be handed from one
// task to another, so it waits on the hash_space_place row, which
// PlaceTask.finish deletes. Without open-pieces disks no row is ever queued
// and it returns at once. Call it after the transaction that queued the
// placement has committed.
func WaitPlaced(ctx context.Context, db *harmonydb.DB, pieceRefs ...int64) error {
	return WaitPlacedFor(ctx, db, PLACE_WAIT_MAX, pieceRefs...)
}

// WaitPlacedFor is WaitPlaced with an explicit bound. Reaching the bound is
// logged and is not an error.
func WaitPlacedFor(ctx context.Context, db *harmonydb.DB, bound time.Duration, pieceRefs ...int64) error {
	if len(pieceRefs) == 0 {
		return nil
	}
	return waitUntilZero(ctx, bound, pieceRefs, func(ctx context.Context) (int, error) {
		var queued int
		err := db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_place WHERE piece_ref = ANY($1)`, pieceRefs).Scan(&queued)
		return queued, err
	})
}

// waitUntilZero polls queued until it returns 0, bound passes or ctx ends.
func waitUntilZero(ctx context.Context, bound time.Duration, pieceRefs []int64, queuedFn func(context.Context) (int, error)) error {
	deadline := time.Now().Add(bound)
	delay := PLACE_POLL_MIN
	for {
		queued, err := queuedFn(ctx)
		if err != nil {
			return xerrors.Errorf("checking open-pieces placement: %w", err)
		}
		if queued == 0 {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			log.Warnw("open-pieces placement still queued, not waiting any longer", "pieceRefs", pieceRefs, "waited", bound)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(delay, remaining)):
		}
		delay = min(delay*2, PLACE_POLL_MAX)
	}
}
