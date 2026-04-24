package itests

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffi"
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
		name                    string
		setup                   func(t *testing.T, ctx context.Context, db *harmonydb.DB, sc *ffi.SealCalls) []refExpectation
		expectIndexAfterCleanup bool
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
			expectIndexAfterCleanup: true,
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
			expectIndexAfterCleanup: true,
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
			expectIndexAfterCleanup: false,
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
			expectIndexAfterCleanup: true,
		},
		{
			name: "does not move refs when no readable complete winner exists",
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
					{refID: staleRef, expectedPieceID: staleLoserID},
				}
			},
			expectIndexAfterCleanup: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, sc, _, _ := setupPieceParkTestEnv(t)
			dropParkedPieceIndex(t, ctx, db)

			expectedRefs := tc.setup(t, ctx, db, sc)

			fixTask := piece.NewFixParkPieceTask(db, sc)
			done, err := fixTask.Do(0, func() bool { return true })
			require.False(t, done)
			require.ErrorContains(t, err, "creating index")

			for _, expected := range expectedRefs {
				require.Equal(t, expected.expectedPieceID, parkedPieceIDForRef(t, ctx, db, expected.refID))
			}

			cleanupTaskIDs := markOrphanPiecesForCleanup(t, ctx, db)
			if len(cleanupTaskIDs) > 0 {
				cleanupTask := piece.NewCleanupPieceTask(db, sc, 0)
				for _, cleanupTaskID := range cleanupTaskIDs {
					done, err = cleanupTask.Do(cleanupTaskID, func() bool { return true })
					require.True(t, done)
					require.NoError(t, err)
				}
			}

			done, err = fixTask.Do(0, func() bool { return true })
			if tc.expectIndexAfterCleanup {
				require.True(t, done)
				require.NoError(t, err)
				require.True(t, parkedPieceIndexExists(t, ctx, db))
			} else {
				require.False(t, done)
				require.ErrorContains(t, err, "creating index")
				require.False(t, parkedPieceIndexExists(t, ctx, db))
			}

			for _, expected := range expectedRefs {
				require.Equal(t, expected.expectedPieceID, parkedPieceIDForRef(t, ctx, db, expected.refID))
			}
		})
	}
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

func markOrphanPiecesForCleanup(t *testing.T, ctx context.Context, db *harmonydb.DB) []harmonytask.TaskID {
	t.Helper()

	var orphanPieceIDs []int64
	err := db.Select(ctx, &orphanPieceIDs, `
		SELECT pp.id
		FROM parked_pieces pp
		WHERE pp.cleanup_task_id IS NULL
		  AND NOT EXISTS (
			  SELECT 1
			  FROM parked_piece_refs ppr
			  WHERE ppr.piece_id = pp.id
		  )
		ORDER BY pp.id
	`)
	require.NoError(t, err)

	taskIDs := make([]harmonytask.TaskID, 0, len(orphanPieceIDs))
	for i, pieceID := range orphanPieceIDs {
		taskID := harmonytask.TaskID(10_000 + i + int(pieceID))
		_, err := db.Exec(ctx, `UPDATE parked_pieces SET cleanup_task_id = $1 WHERE id = $2`, taskID, pieceID)
		require.NoError(t, err)
		taskIDs = append(taskIDs, taskID)
	}

	return taskIDs
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
