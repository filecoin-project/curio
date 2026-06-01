package itests

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// TestParkedPieceRefCount_TriggersMaintainCount verifies that the
// AFTER INSERT / DELETE / UPDATE triggers on parked_piece_refs keep
// parked_pieces.ref_count in sync with the actual number of refs.
func TestParkedPieceRefCount_TriggersMaintainCount(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	pieceID := insertParkedPieceForRefcount(t, ctx, db, "refcount-trigger-piece", 32, 16)
	require.Equal(t, 0, refCount(t, ctx, db, pieceID))

	ref1 := insertParkedPieceRefcountRef(t, ctx, db, pieceID)
	require.Equal(t, 1, refCount(t, ctx, db, pieceID))

	ref2 := insertParkedPieceRefcountRef(t, ctx, db, pieceID)
	require.Equal(t, 2, refCount(t, ctx, db, pieceID))

	deleteParkedPieceRef(t, ctx, db, ref1)
	require.Equal(t, 1, refCount(t, ctx, db, pieceID))

	deleteParkedPieceRef(t, ctx, db, ref2)
	require.Equal(t, 0, refCount(t, ctx, db, pieceID))
}

// TestParkedPieceRefCount_UpdateTriggerMovesCount asserts that reassigning a
// parked_piece_refs row to a different piece (used during duplicate
// consolidation in FixParkPieceTask and during piece reassignment in MK20
// upload error recovery) moves the ref count from the old piece to the new.
func TestParkedPieceRefCount_UpdateTriggerMovesCount(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	srcID := insertParkedPieceForRefcount(t, ctx, db, "refcount-update-src", 32, 16)
	dstID := insertParkedPieceForRefcount(t, ctx, db, "refcount-update-dst", 32, 16)

	refA := insertParkedPieceRefcountRef(t, ctx, db, srcID)
	refB := insertParkedPieceRefcountRef(t, ctx, db, srcID)
	require.Equal(t, 2, refCount(t, ctx, db, srcID))
	require.Equal(t, 0, refCount(t, ctx, db, dstID))

	_, err = db.Exec(ctx, `UPDATE parked_piece_refs SET piece_id = $1 WHERE ref_id = ANY($2::bigint[])`, dstID, []int64{refA, refB})
	require.NoError(t, err)

	require.Equal(t, 0, refCount(t, ctx, db, srcID))
	require.Equal(t, 2, refCount(t, ctx, db, dstID))

	// Updating to the same piece_id must not change anything.
	_, err = db.Exec(ctx, `UPDATE parked_piece_refs SET piece_id = piece_id WHERE ref_id = $1`, refA)
	require.NoError(t, err)
	require.Equal(t, 2, refCount(t, ctx, db, dstID))
}

// TestParkedPieceRefCount_CleanupCandidateVisibility asserts that ref_count
// changes flip pieces in and out of the cleanup-candidate set the same way
// the original anti-join did. This is the behaviour the DropPiece poller
// relies on after the switch from NOT EXISTS to ref_count = 0.
func TestParkedPieceRefCount_CleanupCandidateVisibility(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	pieceID := insertParkedPieceForRefcount(t, ctx, db, "refcount-candidate", 32, 16)
	require.True(t, isCleanupCandidate(t, ctx, db, pieceID), "new piece without refs must be a cleanup candidate")

	refID := insertParkedPieceRefcountRef(t, ctx, db, pieceID)
	require.False(t, isCleanupCandidate(t, ctx, db, pieceID), "piece with a ref must not be a cleanup candidate")

	deleteParkedPieceRef(t, ctx, db, refID)
	require.True(t, isCleanupCandidate(t, ctx, db, pieceID), "piece must return to cleanup-eligible state when last ref is removed")

	_, err = db.Exec(ctx, `UPDATE parked_pieces SET cleanup_task_id = $1 WHERE id = $2`, 9999, pieceID)
	require.NoError(t, err)
	require.False(t, isCleanupCandidate(t, ctx, db, pieceID), "piece already claimed by a cleanup task must not be re-picked")
}

// TestParkedPieceRefCount_CascadeDeleteLeavesNoOrphans verifies that deleting
// a parked_pieces row that still has refs (which the cleanup path explicitly
// avoids, but which can happen in test/admin paths) cascade-deletes the refs
// without leaving stale rows or breaking the ref_count of other pieces.
func TestParkedPieceRefCount_CascadeDeleteLeavesNoOrphans(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	doomedID := insertParkedPieceForRefcount(t, ctx, db, "refcount-cascade-doomed", 32, 16)
	survivorID := insertParkedPieceForRefcount(t, ctx, db, "refcount-cascade-survivor", 32, 16)

	insertParkedPieceRefcountRef(t, ctx, db, doomedID)
	insertParkedPieceRefcountRef(t, ctx, db, doomedID)
	survivorRef := insertParkedPieceRefcountRef(t, ctx, db, survivorID)

	require.Equal(t, 2, refCount(t, ctx, db, doomedID))
	require.Equal(t, 1, refCount(t, ctx, db, survivorID))

	_, err = db.Exec(ctx, `DELETE FROM parked_pieces WHERE id = $1`, doomedID)
	require.NoError(t, err)

	// Cascade should have removed all refs for the doomed piece.
	var doomedRefs int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM parked_piece_refs WHERE piece_id = $1`, doomedID).Scan(&doomedRefs))
	require.Equal(t, 0, doomedRefs)

	// Survivor's ref_count must remain accurate.
	require.Equal(t, 1, refCount(t, ctx, db, survivorID))
	require.Equal(t, survivorID, parkedPieceIDForRef(t, ctx, db, survivorRef))
}

// TestParkedPieceRefCount_ConcurrentInsertsConverge stresses the INSERT
// trigger with many parallel writers attaching refs to the same piece. The
// final ref_count must match the number of inserts.
func TestParkedPieceRefCount_ConcurrentInsertsConverge(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	pieceID := insertParkedPieceForRefcount(t, ctx, db, "refcount-concurrent", 32, 16)

	const n = 32
	var wg sync.WaitGroup
	barrier := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			_, err := db.Exec(ctx, `INSERT INTO parked_piece_refs (piece_id) VALUES ($1)`, pieceID)
			require.NoError(t, err)
		}()
	}
	close(barrier)
	wg.Wait()

	require.Equal(t, n, refCount(t, ctx, db, pieceID))
}

// Helpers ----------------------------------------------------------------

func insertParkedPieceForRefcount(t *testing.T, ctx context.Context, db *harmonydb.DB, pieceCID string, paddedSize, rawSize int64) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
		VALUES ($1, $2, $3, FALSE)
		RETURNING id`, pieceCID, paddedSize, rawSize).Scan(&id))
	return id
}

func insertParkedPieceRefcountRef(t *testing.T, ctx context.Context, db *harmonydb.DB, pieceID int64) int64 {
	t.Helper()
	var refID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id) VALUES ($1) RETURNING ref_id`, pieceID).Scan(&refID))
	return refID
}

func deleteParkedPieceRef(t *testing.T, ctx context.Context, db *harmonydb.DB, refID int64) {
	t.Helper()
	_, err := db.Exec(ctx, `DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
	require.NoError(t, err)
}

func refCount(t *testing.T, ctx context.Context, db *harmonydb.DB, pieceID int64) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(ctx, `SELECT ref_count FROM parked_pieces WHERE id = $1`, pieceID).Scan(&n))
	return n
}

func isCleanupCandidate(t *testing.T, ctx context.Context, db *harmonydb.DB, pieceID int64) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM parked_pieces
			WHERE id = $1 AND cleanup_task_id IS NULL AND ref_count = 0
		)`, pieceID).Scan(&exists))
	return exists
}
