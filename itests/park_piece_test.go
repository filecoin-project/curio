package itests

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/parkpiece"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/piece"
)

// TestParkPieceCanAccept_SliceBounds is a regression test for a panic in
// ParkPieceTask.CanAccept where capacity (maxInPark - count - running) could
// exceed len(ids), causing "slice bounds out of range [:N] with capacity M".
//
// The production crash was: maxInPark=15, count=0, running=0, len(ids)=2
// → ids[:15] panicked on a 2-element slice.
func TestParkPieceCanAccept_SliceBounds(t *testing.T) {
	// Slow down the background piece poller so it doesn't interfere.
	oldInterval := piece.PieceParkPollInterval
	piece.PieceParkPollInterval = time.Hour
	t.Cleanup(func() { piece.PieceParkPollInterval = oldInterval })

	ctx, db, sc, remote, storageID := setupPieceParkTestEnv(t)

	// Create ParkPieceTask with maxInPark=15, matching the production crash.
	const maxInPark = 15
	ppt, err := piece.NewParkPieceTask(db, sc, remote, 10, maxInPark, nil, 10)
	require.NoError(t, err)

	// Create a real TaskEngine so RunningCount works.
	engine, err := harmonytask.New(db, []harmonytask.TaskInterface{ppt}, "testhost:1234")
	require.NoError(t, err)
	t.Cleanup(func() { engine.GracefullyTerminate() })

	// --- Regression case ---
	// 2 IDs, maxInPark=15, 0 pieces in storage, 0 running.
	// Before the fix this panicked: ids[:15] on a 2-element slice.
	ids := []harmonytask.TaskID{1, 2}
	require.NotPanics(t, func() {
		accepted, err := ppt.CanAccept(ids, engine)
		require.NoError(t, err)
		require.Equal(t, ids, accepted, "should accept all available IDs when capacity exceeds len(ids)")
	})

	// --- Partial capacity ---
	// Fill storage to maxInPark-1 pieces so only 1 slot remains.
	for i := range maxInPark - 1 {
		_, err := db.Exec(ctx, `INSERT INTO sector_location
			(miner_id, sector_num, sector_filetype, storage_id, is_primary)
			VALUES ($1, $2, $3, $4, $5)`,
			0, i, int(storiface.FTPiece), string(storageID), true)
		require.NoError(t, err)
	}

	accepted, err := ppt.CanAccept(ids, engine)
	require.NoError(t, err)
	require.Len(t, accepted, 1, "should cap to remaining capacity")

	// --- At capacity ---
	// Add one more piece → count == maxInPark → reject all.
	_, err = db.Exec(ctx, `INSERT INTO sector_location
		(miner_id, sector_num, sector_filetype, storage_id, is_primary)
		VALUES ($1, $2, $3, $4, $5)`,
		0, maxInPark, int(storiface.FTPiece), string(storageID), true)
	require.NoError(t, err)

	accepted, err = ppt.CanAccept(ids, engine)
	require.NoError(t, err)
	require.Nil(t, accepted, "should reject all when at capacity")
}

// TestParkedPiecesActivePieceKey asserts that
// parked_pieces_active_piece_key enforces at most one row per
// (piece_cid, piece_padded_size, long_term) where cleanup_task_id IS NULL,
// and that parkpiece.Upsert / UpsertSkip respect that invariant.
//
// Subtests: NEGATIVE (must fail or preserve invariant), POSITIVE (outside
// the predicate, must succeed), HELPER (Upsert / UpsertSkip behaviour).
func TestParkedPiecesActivePieceKey(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	require.True(t, parkedPieceIndexExists(t, ctx, db),
		"parked_pieces_active_piece_key must be present and well-formed for these tests to mean anything")

	rawInsert := func(cid string, paddedSize, rawSize int64, longTerm bool) error {
		_, err := db.Exec(ctx, `
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			VALUES ($1, $2, $3, $4)
		`, cid, paddedSize, rawSize, longTerm)
		return err
	}
	// insertCleanupRow inserts a row whose cleanup_task_id IS NOT NULL, so it
	// sits OUTSIDE the partial unique index. Used to set up coexistence cases.
	insertCleanupRow := func(cid string, paddedSize, rawSize, cleanupTaskID int64, longTerm bool) int64 {
		var id int64
		require.NoError(t, db.QueryRow(ctx, `
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, cleanup_task_id)
			VALUES ($1, $2, $3, $4, $5) RETURNING id
		`, cid, paddedSize, rawSize, longTerm, cleanupTaskID).Scan(&id))
		return id
	}
	countByCID := func(cid string) int {
		var n int
		require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM parked_pieces WHERE piece_cid = $1`, cid).Scan(&n))
		return n
	}
	activeCountByCID := func(cid string) int {
		var n int
		require.NoError(t, db.QueryRow(ctx,
			`SELECT COUNT(*) FROM parked_pieces WHERE piece_cid = $1 AND cleanup_task_id IS NULL`,
			cid).Scan(&n))
		return n
	}
	// assertNoActiveDuplicates is the global invariant: across the entire
	// parked_pieces table, no (piece_cid, piece_padded_size, long_term)
	// triple may have more than one row with cleanup_task_id IS NULL.
	assertNoActiveDuplicates := func(t *testing.T) {
		t.Helper()
		var dupes int
		require.NoError(t, db.QueryRow(ctx, `
			SELECT COUNT(*) FROM (
				SELECT piece_cid, piece_padded_size, long_term
				FROM parked_pieces
				WHERE cleanup_task_id IS NULL
				GROUP BY 1, 2, 3
				HAVING COUNT(*) > 1
			) AS d`).Scan(&dupes))
		require.Equal(t, 0, dupes, "table-wide invariant violated: active duplicates exist")
	}

	// NEGATIVE: must fail or preserve the invariant.

	t.Run("DirectDuplicateInsertFails", func(t *testing.T) {
		const cid = "test-direct-duplicate"
		require.NoError(t, rawInsert(cid, 32, 16, true))
		err := rawInsert(cid, 32, 16, true)
		require.Error(t, err)
		require.True(t, harmonydb.IsErrUniqueContraint(err), "expected unique-violation, got %v", err)
		require.Equal(t, 1, countByCID(cid))
	})

	t.Run("DifferentRawSizeStillFails", func(t *testing.T) {
		// piece_raw_size is NOT in the index key; same (cid, padded, long_term) must still conflict.
		const cid = "test-different-rawsize"
		require.NoError(t, rawInsert(cid, 32, 10, true))
		require.Error(t, rawInsert(cid, 32, 20, true))
		require.Equal(t, 1, countByCID(cid))
	})

	t.Run("DifferentSkipStillFails", func(t *testing.T) {
		const cid = "test-different-skip"
		_, err := db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
			VALUES ($1, 32, 16, TRUE, FALSE)`, cid)
		require.NoError(t, err)
		_, err = db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
			VALUES ($1, 32, 16, TRUE, TRUE)`, cid)
		require.Error(t, err)
		require.Equal(t, 1, countByCID(cid))
	})

	t.Run("DifferentCompleteStillFails", func(t *testing.T) {
		const cid = "test-different-complete"
		_, err := db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, complete)
			VALUES ($1, 32, 16, TRUE, FALSE)`, cid)
		require.NoError(t, err)
		_, err = db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, complete)
			VALUES ($1, 32, 16, TRUE, TRUE)`, cid)
		require.Error(t, err)
		require.Equal(t, 1, countByCID(cid))
	})

	t.Run("MultiRowInsertWithInternalDuplicateFails", func(t *testing.T) {
		// Two duplicates inside a single INSERT statement. Atomic - neither row should land.
		const cid = "test-multirow-duplicate"
		_, err := db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			VALUES ($1, 32, 16, TRUE), ($1, 32, 16, TRUE)`, cid)
		require.Error(t, err)
		require.Equal(t, 0, countByCID(cid))
	})

	t.Run("InsertFromSelectFails", func(t *testing.T) {
		// INSERT ... SELECT does not get to bypass the index.
		const cid = "test-insert-from-select"
		require.NoError(t, rawInsert(cid, 32, 16, true))
		_, err := db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			SELECT piece_cid, piece_padded_size, piece_raw_size, long_term FROM parked_pieces WHERE piece_cid = $1`, cid)
		require.Error(t, err)
		require.Equal(t, 1, countByCID(cid))
	})

	t.Run("UpdateToReintroduceConflictFails", func(t *testing.T) {
		// A live row plus a cleanup-marked sibling can coexist. Clearing the
		// cleanup_task_id on the sibling would create two live rows; the
		// index must reject that UPDATE.
		const cid = "test-update-reintroduce"
		require.NoError(t, rawInsert(cid, 32, 16, true))
		cleanupID := insertCleanupRow(cid, 32, 16, 8888, true)
		_, err := db.Exec(ctx, `UPDATE parked_pieces SET cleanup_task_id = NULL WHERE id = $1`, cleanupID)
		require.Error(t, err, "UPDATE that produces a duplicate live row must fail")
	})

	t.Run("DifferentTaskIDStillFails", func(t *testing.T) {
		// task_id (the work-task pointer) is NOT cleanup_task_id. Two rows
		// with different task_id but cleanup_task_id NULL on both must still
		// collide on the partial index.
		const cid = "test-different-taskid"
		_, err := db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, task_id)
			VALUES ($1, 32, 16, TRUE, 100)`, cid)
		require.NoError(t, err)
		_, err = db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, task_id)
			VALUES ($1, 32, 16, TRUE, 200)`, cid)
		require.Error(t, err)
		require.Equal(t, 1, activeCountByCID(cid))
	})

	t.Run("ExplicitNullCleanupTaskIDStillFails", func(t *testing.T) {
		// Spelling cleanup_task_id = NULL explicitly must behave the same as the default.
		const cid = "test-explicit-null-cleanup"
		_, err := db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, cleanup_task_id)
			VALUES ($1, 32, 16, TRUE, NULL)`, cid)
		require.NoError(t, err)
		_, err = db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, cleanup_task_id)
			VALUES ($1, 32, 16, TRUE, NULL)`, cid)
		require.Error(t, err)
		require.Equal(t, 1, activeCountByCID(cid))
	})

	t.Run("UntargetedDoNothingPreservesSingleActiveRow", func(t *testing.T) {
		// Untargeted ON CONFLICT DO NOTHING does not error, but it also
		// must not produce a second active row. The partial unique index
		// still catches the conflict; DO NOTHING just swallows it.
		const cid = "test-untargeted-do-nothing"
		_, err := db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			VALUES ($1, 32, 16, TRUE)`, cid)
		require.NoError(t, err)
		_, err = db.Exec(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			VALUES ($1, 32, 16, TRUE) ON CONFLICT DO NOTHING`, cid)
		require.NoError(t, err, "untargeted DO NOTHING must succeed silently")
		require.Equal(t, 1, activeCountByCID(cid), "no second active row may exist")
	})

	t.Run("ConcurrentRawInsertsAllButOneFail", func(t *testing.T) {
		// N goroutines race to INSERT the same key. Exactly one must win;
		// the rest must fail with 23505.
		const (
			cid = "test-concurrent-raw"
			n   = 20
		)
		var (
			wg      sync.WaitGroup
			barrier = make(chan struct{})
			errs    = make([]error, n)
		)
		for i := range n {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-barrier
				errs[idx] = rawInsert(cid, 32, 16, true)
			}(i)
		}
		close(barrier)
		wg.Wait()

		successes := 0
		for _, err := range errs {
			if err == nil {
				successes++
				continue
			}
			require.True(t, harmonydb.IsErrUniqueContraint(err), "unexpected error: %v", err)
		}
		require.Equal(t, 1, successes, "exactly one direct insert must win")
		require.Equal(t, 1, countByCID(cid))
	})

	// POSITIVE: outside the index predicate or differ on a key column.

	t.Run("DifferentLongTermSucceeds", func(t *testing.T) {
		// long_term IS part of the index key.
		const cid = "test-different-longterm"
		require.NoError(t, rawInsert(cid, 32, 16, true))
		require.NoError(t, rawInsert(cid, 32, 16, false))
		require.Equal(t, 2, countByCID(cid))
	})

	t.Run("DifferentPaddedSizeSucceeds", func(t *testing.T) {
		const cid = "test-different-padded"
		require.NoError(t, rawInsert(cid, 32, 16, true))
		require.NoError(t, rawInsert(cid, 64, 32, true))
		require.Equal(t, 2, countByCID(cid))
	})

	t.Run("CleanupSoftDeletedAndLiveCoexist", func(t *testing.T) {
		// cleanup_task_id IS NOT NULL puts the row outside the partial index.
		const cid = "test-cleanup-coexist"
		cleanupID := insertCleanupRow(cid, 32, 16, 7777, true)
		require.NoError(t, rawInsert(cid, 32, 16, true))
		var liveID int64
		require.NoError(t, db.QueryRow(ctx,
			`SELECT id FROM parked_pieces WHERE piece_cid = $1 AND cleanup_task_id IS NULL`,
			cid).Scan(&liveID))
		require.NotEqual(t, cleanupID, liveID)
		require.Equal(t, 2, countByCID(cid))
	})

	t.Run("MultipleCleanupRowsCoexist", func(t *testing.T) {
		// Multiple rows with non-null cleanup_task_id all sit outside the partial index.
		const cid = "test-multi-cleanup"
		id1 := insertCleanupRow(cid, 32, 16, 11, true)
		id2 := insertCleanupRow(cid, 32, 16, 22, true)
		require.NotEqual(t, id1, id2)
		require.Equal(t, 2, countByCID(cid))
	})

	// HELPER: parkpiece.Upsert / UpsertSkip behaviour.

	upsert := func(t *testing.T, cid string, paddedSize, rawSize int64, longTerm bool) int64 {
		t.Helper()
		var id int64
		_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			pid, perr := parkpiece.Upsert(tx, cid, paddedSize, rawSize, longTerm)
			if perr != nil {
				return false, perr
			}
			id = pid
			return true, nil
		}, harmonydb.OptionRetry())
		require.NoError(t, err)
		return id
	}
	upsertSkip := func(t *testing.T, cid string, paddedSize, rawSize int64, longTerm, skip bool) int64 {
		t.Helper()
		var id int64
		_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			pid, perr := parkpiece.UpsertSkip(tx, cid, paddedSize, rawSize, longTerm, skip)
			if perr != nil {
				return false, perr
			}
			id = pid
			return true, nil
		}, harmonydb.OptionRetry())
		require.NoError(t, err)
		return id
	}

	t.Run("UpsertIdempotent", func(t *testing.T) {
		const cid = "test-upsert-idempotent"
		id1 := upsert(t, cid, 32, 16, true)
		id2 := upsert(t, cid, 32, 16, true)
		require.Equal(t, id1, id2, "second Upsert must return the existing row id")
		require.Equal(t, 1, countByCID(cid))
	})

	t.Run("UpsertAfterRawInsertReturnsExistingID", func(t *testing.T) {
		// Mixed: a row created by direct INSERT must be discoverable by Upsert.
		const cid = "test-upsert-after-raw"
		rawID := insertParkedPiece(t, ctx, db, cid, 32, 16, false, true, nil)
		upsertID := upsert(t, cid, 32, 16, true)
		require.Equal(t, rawID, upsertID)
		require.Equal(t, 1, countByCID(cid))
	})

	t.Run("UpsertWithDifferentRawSizeKeepsSingleRow", func(t *testing.T) {
		// Calling Upsert twice with different raw_size must converge on
		// one row. DO UPDATE writes EXCLUDED.piece_raw_size on the
		// conflict path, so the row's raw_size reflects the latest call.
		const cid = "test-upsert-different-rawsize"
		id1 := upsert(t, cid, 32, 10, true)
		id2 := upsert(t, cid, 32, 20, true)
		require.Equal(t, id1, id2)
		require.Equal(t, 1, activeCountByCID(cid))
		var rawSize int64
		require.NoError(t, db.QueryRow(ctx, `SELECT piece_raw_size FROM parked_pieces WHERE id = $1`, id1).Scan(&rawSize))
		require.EqualValues(t, 20, rawSize, "DO UPDATE must apply the new raw_size on conflict")
	})

	t.Run("UpsertSkipPreservesExistingSkip", func(t *testing.T) {
		// DO UPDATE only touches piece_raw_size, so the existing skip value
		// is preserved across subsequent UpsertSkip calls.
		const cid = "test-upsert-skip-preserve"
		id1 := upsertSkip(t, cid, 32, 16, true, true)
		id2 := upsertSkip(t, cid, 32, 16, true, false)
		require.Equal(t, id1, id2)
		var skip bool
		require.NoError(t, db.QueryRow(ctx, `SELECT skip FROM parked_pieces WHERE id = $1`, id1).Scan(&skip))
		require.True(t, skip, "DO UPDATE must not overwrite skip on the conflict path")
	})

	t.Run("UpsertConcurrentSamePieceConverges", func(t *testing.T) {
		// N goroutines concurrently call Upsert with the same key. All must
		// succeed, all must return the same id, and exactly one row must exist.
		const (
			cid = "test-upsert-concurrent"
			n   = 32
		)
		var (
			wg      sync.WaitGroup
			barrier = make(chan struct{})
			ids     = make([]int64, n)
			errs    = make([]error, n)
		)
		for i := range n {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-barrier
				_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
					pid, perr := parkpiece.Upsert(tx, cid, 32, 16, true)
					if perr != nil {
						return false, perr
					}
					ids[idx] = pid
					return true, nil
				}, harmonydb.OptionRetry())
				errs[idx] = err
			}(i)
		}
		close(barrier)
		wg.Wait()

		for i, e := range errs {
			require.NoErrorf(t, e, "goroutine %d failed", i)
		}
		for i := 1; i < n; i++ {
			require.Equal(t, ids[0], ids[i], "all goroutines must return the same id")
		}
		require.Equal(t, 1, activeCountByCID(cid))
	})

	t.Run("UpsertMixedConcurrentConverges", func(t *testing.T) {
		// Mix Upsert and UpsertSkip on the same key concurrently. Both
		// helpers target the same partial unique index; all writers must
		// converge on a single row regardless of which helper they used.
		const (
			cid = "test-upsert-mixed-concurrent"
			n   = 32
		)
		var (
			wg      sync.WaitGroup
			barrier = make(chan struct{})
			ids     = make([]int64, n)
			errs    = make([]error, n)
		)
		for i := range n {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-barrier
				_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
					var (
						pid  int64
						perr error
					)
					if idx%2 == 0 {
						pid, perr = parkpiece.Upsert(tx, cid, 32, 16, true)
					} else {
						pid, perr = parkpiece.UpsertSkip(tx, cid, 32, 16, true, true)
					}
					if perr != nil {
						return false, perr
					}
					ids[idx] = pid
					return true, nil
				}, harmonydb.OptionRetry())
				errs[idx] = err
			}(i)
		}
		close(barrier)
		wg.Wait()

		for i, e := range errs {
			require.NoErrorf(t, e, "goroutine %d (idx%%2=%d) failed", i, i%2)
		}
		for i := 1; i < n; i++ {
			require.Equal(t, ids[0], ids[i], "all goroutines must return the same id regardless of helper")
		}
		require.Equal(t, 1, activeCountByCID(cid))
	})

	t.Run("UpsertFallsBackWhenIndexMissing", func(t *testing.T) {
		// Without the index, ON CONFLICT inference raises 42P10; the helper
		// must fall back to check-then-insert.
		const cid = "test-upsert-fallback"
		dropParkedPieceIndex(t, ctx, db)
		t.Cleanup(func() {
			_, err := db.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS parked_pieces_active_piece_key
				ON parked_pieces (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL`)
			require.NoError(t, err)
		})

		id1 := upsert(t, cid, 32, 16, true)
		id2 := upsert(t, cid, 32, 16, true)
		require.Equal(t, id1, id2)
		require.Equal(t, 1, activeCountByCID(cid))

		const skipCid = "test-upsert-skip-fallback"
		idA := upsertSkip(t, skipCid, 32, 16, true, true)
		idB := upsertSkip(t, skipCid, 32, 16, true, false)
		require.Equal(t, idA, idB)
		require.Equal(t, 1, activeCountByCID(skipCid))
	})

	assertNoActiveDuplicates(t)
}

func TestFixParkPieceTask_RepairsDuplicateRefs(t *testing.T) {
	// Slow down the background piece poller so it doesn't interfere.
	oldInterval := piece.PieceParkPollInterval
	piece.PieceParkPollInterval = time.Hour
	t.Cleanup(func() { piece.PieceParkPollInterval = oldInterval })

	type refExpectation struct {
		refID           int64
		expectedPieceID int64
	}

	testCases := []struct {
		name             string
		setup            func(t *testing.T, ctx context.Context, db *harmonydb.DB, sc *ffi.SealCalls) []refExpectation
		expectIndexAfter bool
	}{
		{
			name: "moves refs from incomplete row without task",
			setup: func(t *testing.T, ctx context.Context, db *harmonydb.DB, sc *ffi.SealCalls) []refExpectation {
				const (
					pieceCID   = "fix-piece-no-task"
					paddedSize = int64(32)
					rawSize    = int64(16)
				)

				winnerID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, true, true, nil)
				makePieceReadable(t, ctx, sc, winnerID, rawSize, true)
				winnerRef := insertParkedPieceRef(t, ctx, db, winnerID, true)

				loserID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, nil)
				loserRef := insertParkedPieceRef(t, ctx, db, loserID, true)

				return []refExpectation{
					{refID: winnerRef, expectedPieceID: winnerID},
					{refID: loserRef, expectedPieceID: winnerID},
				}
			},
			expectIndexAfter: true,
		},
		{
			name: "moves refs from incomplete row with stale task",
			setup: func(t *testing.T, ctx context.Context, db *harmonydb.DB, sc *ffi.SealCalls) []refExpectation {
				const (
					pieceCID   = "fix-piece-stale-task"
					paddedSize = int64(32)
					rawSize    = int64(16)
				)

				winnerID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, true, true, nil)
				makePieceReadable(t, ctx, sc, winnerID, rawSize, true)
				winnerRef := insertParkedPieceRef(t, ctx, db, winnerID, true)

				staleTaskID := int64(999_001)
				loserID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, &staleTaskID)
				loserRef := insertParkedPieceRef(t, ctx, db, loserID, true)

				return []refExpectation{
					{refID: winnerRef, expectedPieceID: winnerID},
					{refID: loserRef, expectedPieceID: winnerID},
				}
			},
			expectIndexAfter: true,
		},
		{
			name: "skips active incomplete row but moves stale sibling",
			setup: func(t *testing.T, ctx context.Context, db *harmonydb.DB, sc *ffi.SealCalls) []refExpectation {
				const (
					pieceCID   = "fix-piece-active-task"
					paddedSize = int64(32)
					rawSize    = int64(16)
				)

				winnerID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, true, true, nil)
				makePieceReadable(t, ctx, sc, winnerID, rawSize, true)
				winnerRef := insertParkedPieceRef(t, ctx, db, winnerID, true)

				activeTaskID := insertHarmonyTask(t, ctx, db, "ParkPiece")
				activeLoserID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, &activeTaskID)
				activeRef := insertParkedPieceRef(t, ctx, db, activeLoserID, true)

				staleTaskID := int64(999_002)
				staleLoserID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, &staleTaskID)
				staleRef := insertParkedPieceRef(t, ctx, db, staleLoserID, true)

				return []refExpectation{
					{refID: winnerRef, expectedPieceID: winnerID},
					{refID: activeRef, expectedPieceID: activeLoserID},
					{refID: staleRef, expectedPieceID: winnerID},
				}
			},
			expectIndexAfter: false,
		},
		{
			name: "chooses readable winner among multiple complete rows",
			setup: func(t *testing.T, ctx context.Context, db *harmonydb.DB, sc *ffi.SealCalls) []refExpectation {
				const (
					pieceCID   = "fix-piece-multiple-complete"
					paddedSize = int64(32)
					rawSize    = int64(16)
				)

				unreadableCompleteID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, true, true, nil)
				unreadableRef := insertParkedPieceRef(t, ctx, db, unreadableCompleteID, true)

				readableWinnerID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, true, true, nil)
				makePieceReadable(t, ctx, sc, readableWinnerID, rawSize, true)
				winnerRef := insertParkedPieceRef(t, ctx, db, readableWinnerID, true)

				staleTaskID := int64(999_003)
				staleLoserID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, &staleTaskID)
				staleRef := insertParkedPieceRef(t, ctx, db, staleLoserID, true)

				return []refExpectation{
					{refID: unreadableRef, expectedPieceID: readableWinnerID},
					{refID: winnerRef, expectedPieceID: readableWinnerID},
					{refID: staleRef, expectedPieceID: readableWinnerID},
				}
			},
			expectIndexAfter: true,
		},
		{
			name: "falls back to lowest-id non-active when no readable winner exists",
			setup: func(t *testing.T, ctx context.Context, db *harmonydb.DB, sc *ffi.SealCalls) []refExpectation {
				const (
					pieceCID   = "fix-piece-no-readable-winner"
					paddedSize = int64(32)
					rawSize    = int64(16)
				)

				unreadableCompleteID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, true, true, nil)
				unreadableRef := insertParkedPieceRef(t, ctx, db, unreadableCompleteID, true)

				staleTaskID := int64(999_004)
				staleLoserID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, &staleTaskID)
				staleRef := insertParkedPieceRef(t, ctx, db, staleLoserID, true)

				return []refExpectation{
					{refID: unreadableRef, expectedPieceID: unreadableCompleteID},
					{refID: staleRef, expectedPieceID: unreadableCompleteID},
				}
			},
			expectIndexAfter: true,
		},
		{
			name: "consolidates all-incomplete group via lowest-id fallback",
			setup: func(t *testing.T, ctx context.Context, db *harmonydb.DB, sc *ffi.SealCalls) []refExpectation {
				const (
					pieceCID   = "fix-piece-all-incomplete"
					paddedSize = int64(32)
					rawSize    = int64(16)
				)

				winnerID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, nil)
				winnerRef := insertParkedPieceRef(t, ctx, db, winnerID, true)

				loser1ID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, nil)
				loser1Ref := insertParkedPieceRef(t, ctx, db, loser1ID, true)

				loser2ID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, nil)
				loser2Ref := insertParkedPieceRef(t, ctx, db, loser2ID, true)

				return []refExpectation{
					{refID: winnerRef, expectedPieceID: winnerID},
					{refID: loser1Ref, expectedPieceID: winnerID},
					{refID: loser2Ref, expectedPieceID: winnerID},
				}
			},
			expectIndexAfter: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, sc, _, _ := setupPieceParkTestEnv(t)
			dropParkedPieceIndex(t, ctx, db)

			expectedRefs := tc.setup(t, ctx, db, sc)

			// Active rows in a group block index creation; the task returns
			// success and leaves the index absent for the next pass.
			fixTask := piece.NewFixParkPieceTask(db, sc)
			done, err := fixTask.Do(0, func() bool { return true })
			require.True(t, done)
			require.NoError(t, err)

			for _, expected := range expectedRefs {
				require.Equal(t, expected.expectedPieceID, parkedPieceIDForRef(t, ctx, db, expected.refID))
			}
			require.Equal(t, tc.expectIndexAfter, parkedPieceIndexExists(t, ctx, db))
		})
	}
}

// TestFixParkPieceTask_RecoversInvalidIndex asserts that an INVALID
// parked_pieces_active_piece_key is dropped and recreated, not skipped via
// CREATE UNIQUE INDEX IF NOT EXISTS.
func TestFixParkPieceTask_RecoversInvalidIndex(t *testing.T) {
	oldInterval := piece.PieceParkPollInterval
	piece.PieceParkPollInterval = time.Hour
	t.Cleanup(func() { piece.PieceParkPollInterval = oldInterval })

	ctx, db, sc, _, _ := setupPieceParkTestEnv(t)
	dropParkedPieceIndex(t, ctx, db)

	const (
		pieceCID   = "fix-piece-invalid-index"
		paddedSize = int64(32)
		rawSize    = int64(16)
	)
	winnerID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, nil)
	winnerRef := insertParkedPieceRef(t, ctx, db, winnerID, true)
	loserID := insertParkedPiece(t, ctx, db, pieceCID, paddedSize, rawSize, false, true, nil)
	loserRef := insertParkedPieceRef(t, ctx, db, loserID, true)

	// CONCURRENTLY against pre-existing duplicates fails and leaves the
	// index INVALID, matching the on-disk state from the racy upload era.
	_, err := db.Exec(ctx, `CREATE UNIQUE INDEX CONCURRENTLY parked_pieces_active_piece_key
		ON parked_pieces (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL`)
	require.Error(t, err)

	var indisvalid bool
	require.NoError(t, db.QueryRow(ctx, `
		SELECT indisvalid FROM pg_index
		WHERE indexrelid = (SELECT oid FROM pg_class WHERE relname = 'parked_pieces_active_piece_key')`).Scan(&indisvalid))
	require.False(t, indisvalid)
	require.False(t, parkedPieceIndexExists(t, ctx, db))

	fixTask := piece.NewFixParkPieceTask(db, sc)
	done, err := fixTask.Do(0, func() bool { return true })
	require.True(t, done)
	require.NoError(t, err)

	require.True(t, parkedPieceIndexExists(t, ctx, db))
	require.Equal(t, winnerID, parkedPieceIDForRef(t, ctx, db, winnerRef))
	require.Equal(t, winnerID, parkedPieceIDForRef(t, ctx, db, loserRef))
}

func setupPieceParkTestEnv(t *testing.T) (context.Context, *harmonydb.DB, *ffi.SealCalls, *paths.Remote, storiface.ID) {
	t.Helper()

	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	root := t.TempDir()
	storageDir := filepath.Join(root, "storage")
	require.NoError(t, os.MkdirAll(storageDir, 0755))

	storageID := storiface.ID(uuid.New().String())
	meta := &storiface.LocalStorageMeta{
		ID:       storageID,
		Weight:   10,
		CanSeal:  true,
		CanStore: true,
	}
	mb, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storageDir, "sectorstore.json"), mb, 0644))

	bls := &paths.BasicLocalStorage{PathToJSON: filepath.Join(root, "storage.json")}
	index := paths.NewDBIndex(nil, db)
	ls, err := paths.NewLocal(ctx, bls, index, "")
	require.NoError(t, err)
	require.NoError(t, ls.OpenPath(ctx, storageDir))

	remote, err := paths.NewRemote(ls, index, nil, 20, &paths.DefaultPartialFileHandler{})
	require.NoError(t, err)

	sc := ffi.NewSealCalls(remote, ls, index)
	return ctx, db, sc, remote, storageID
}

func dropParkedPieceIndex(t *testing.T, ctx context.Context, db *harmonydb.DB) {
	t.Helper()

	_, err := db.Exec(ctx, `DROP INDEX IF EXISTS parked_pieces_active_piece_key`)
	require.NoError(t, err)
}

func insertParkedPiece(t *testing.T, ctx context.Context, db *harmonydb.DB, pieceCID string, paddedSize, rawSize int64, complete, longTerm bool, taskID *int64) int64 {
	t.Helper()

	var pieceID int64
	err := db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term, task_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, pieceCID, paddedSize, rawSize, complete, longTerm, taskID).Scan(&pieceID)
	require.NoError(t, err)
	return pieceID
}

func insertParkedPieceRef(t *testing.T, ctx context.Context, db *harmonydb.DB, pieceID int64, longTerm bool) int64 {
	t.Helper()

	var refID int64
	err := db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, long_term)
		VALUES ($1, $2)
		RETURNING ref_id
	`, pieceID, longTerm).Scan(&refID)
	require.NoError(t, err)
	return refID
}

func insertHarmonyTask(t *testing.T, ctx context.Context, db *harmonydb.DB, name string) int64 {
	t.Helper()

	var taskID int64
	err := db.QueryRow(ctx, `
		INSERT INTO harmony_task (name, added_by, posted_time)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		RETURNING id
	`, name, 1).Scan(&taskID)
	require.NoError(t, err)
	return taskID
}

func makePieceReadable(t *testing.T, ctx context.Context, sc *ffi.SealCalls, pieceID, rawSize int64, longTerm bool) {
	t.Helper()

	payload := bytes.Repeat([]byte{byte(pieceID % 251)}, int(rawSize))
	storageType := storiface.PathSealing
	if longTerm {
		storageType = storiface.PathStorage
	}

	err := sc.WritePiece(ctx, nil, storiface.PieceNumber(pieceID), rawSize, bytes.NewReader(payload), storageType)
	require.NoError(t, err)
}

func parkedPieceIDForRef(t *testing.T, ctx context.Context, db *harmonydb.DB, refID int64) int64 {
	t.Helper()

	var pieceID int64
	err := db.QueryRow(ctx, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refID).Scan(&pieceID)
	require.NoError(t, err)
	return pieceID
}

func parkedPieceIndexExists(t *testing.T, ctx context.Context, db *harmonydb.DB) bool {
	t.Helper()

	var exists bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_index ix
			JOIN pg_catalog.pg_class idx ON idx.oid = ix.indexrelid
			JOIN pg_catalog.pg_class tbl ON tbl.oid = ix.indrelid
			JOIN pg_catalog.pg_namespace ns ON ns.oid = tbl.relnamespace
			JOIN pg_catalog.pg_am am ON am.oid = idx.relam
			WHERE ns.nspname = current_schema()
			  AND idx.relnamespace = ns.oid
			  AND tbl.relname = 'parked_pieces'
			  AND idx.relname = 'parked_pieces_active_piece_key'
			  AND idx.relkind = 'i'
			  AND am.amname = 'btree'
			  AND ix.indisunique
			  AND ix.indisvalid
			  AND ix.indisready
			  AND ix.indislive
			  AND ix.indnkeyatts = 3
			  AND ix.indnatts = 3
			  AND ix.indexprs IS NULL
			  AND ARRAY(
				  SELECT pg_catalog.pg_get_indexdef(ix.indexrelid, n, true)
				  FROM generate_series(1, ix.indnkeyatts) AS n
				  ORDER BY n
			  ) = ARRAY['piece_cid', 'piece_padded_size', 'long_term']
			  AND pg_catalog.pg_get_expr(ix.indpred, ix.indrelid, false) = '(cleanup_task_id IS NULL)'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	return exists
}
