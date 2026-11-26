package piece

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/piecestore"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

type cleanupPieceIO struct {
	piecestore.PieceIO
	removed []storiface.PieceNumber
}

func (p *cleanupPieceIO) RemovePiece(_ context.Context, id storiface.PieceNumber) error {
	p.removed = append(p.removed, id)
	return nil
}

func (p *cleanupPieceIO) LocalStorage(context.Context) ([]storiface.StoragePath, error) {
	return nil, nil
}

func cleanupTestAdder(t *testing.T, db *harmonydb.DB) harmonytask.AddTaskFunc {
	t.Helper()
	return func(extra func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {
		_, err := db.BeginTransaction(t.Context(), func(tx *harmonydb.Tx) (bool, error) {
			var id harmonytask.TaskID
			if err := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time)
				VALUES ($1, 0, CURRENT_TIMESTAMP) RETURNING id`, tasknames.HashSpaceDrop).Scan(&id); err != nil {
				return false, err
			}
			return extra(id, tx)
		}, harmonydb.OptionRetry())
		require.NoError(t, err)
	}
}

const cleanupTestTaskID harmonytask.TaskID = 123

func newCleanupPieceFixture(t *testing.T) (*CleanupPieceTask, storiface.PieceNumber, string) {
	t.Helper()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	const rawSize = 127
	v1 := zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(128).Unpadded())
	v2, err := commcid.PieceCidV2FromV1(v1, rawSize)
	require.NoError(t, err)
	var pieceID storiface.PieceNumber
	err = db.QueryRow(t.Context(), `INSERT INTO parked_pieces
		(piece_cid, piece_padded_size, piece_raw_size, complete, long_term, cleanup_task_id)
		VALUES ($1, 128, $2, TRUE, TRUE, $3) RETURNING id`,
		v1.String(), rawSize, cleanupTestTaskID).Scan(&pieceID)
	require.NoError(t, err)
	// A migrated piece has no sector_location entry; its bytes live on an
	// open-pieces disk after its last reference has been released.
	_, err = db.Exec(t.Context(), `INSERT INTO hash_space_disk (storage_id, capacity)
		VALUES ('cleanup-test-disk', 1024)`)
	require.NoError(t, err)

	return &CleanupPieceTask{
		db:        db,
		piece:     &cleanupPieceIO{},
		dropAdder: cleanupTestAdder(t, db),
	}, pieceID, v2.String()
}

func TestCleanupPieceQueuesOpenPieceDeletion(t *testing.T) {
	task, pieceID, v2 := newCleanupPieceFixture(t)
	done, err := task.Do(t.Context(), cleanupTestTaskID, func() bool {
		// The scheduler's stillOwned callback also reads through the DB pool.
		var claim harmonytask.TaskID
		err := task.db.QueryRow(t.Context(), `SELECT cleanup_task_id FROM parked_pieces WHERE id = $1`,
			pieceID).Scan(&claim)
		require.NoError(t, err)
		return claim == cleanupTestTaskID
	})
	require.NoError(t, err)
	require.True(t, done)

	var parked, queued bool
	err = task.db.QueryRow(t.Context(), `SELECT
		EXISTS (SELECT 1 FROM parked_pieces WHERE id = $1),
		EXISTS (SELECT 1 FROM hash_space_delete d
			JOIN harmony_task t ON t.id = d.task_id
			WHERE d.piece_cid = $2 AND t.name = $3 AND NOT d.started)`,
		pieceID, v2, tasknames.HashSpaceDrop).Scan(&parked, &queued)
	require.NoError(t, err)
	require.False(t, parked, "unreferenced parked metadata should be removed")
	require.True(t, queued, "open-pieces deletion must remain queued after parked metadata is removed")
	require.Equal(t, []storiface.PieceNumber{pieceID}, task.piece.(*cleanupPieceIO).removed)
}

func TestCleanupPieceCancellationBeforeDropTransaction(t *testing.T) {
	task, pieceID, _ := newCleanupPieceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	addDrop := task.dropAdder
	task.dropAdder = func(extra func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {
		cancel()
		addDrop(extra)
	}

	done, err := task.Do(ctx, cleanupTestTaskID, func() bool { return true })
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, done)
	var parked bool
	var queued, tasks int
	err = task.db.QueryRow(t.Context(), `SELECT
		EXISTS (SELECT 1 FROM parked_pieces WHERE id = $1 AND cleanup_task_id = $2),
		(SELECT count(*) FROM hash_space_delete),
		(SELECT count(*) FROM harmony_task WHERE name = $3)`,
		pieceID, cleanupTestTaskID, tasknames.HashSpaceDrop).Scan(&parked, &queued, &tasks)
	require.NoError(t, err)
	require.True(t, parked)
	require.Zero(t, queued)
	require.Zero(t, tasks)
	require.Empty(t, task.piece.(*cleanupPieceIO).removed)
}

func TestCleanupPieceRetainsReferenceAddedAfterScheduling(t *testing.T) {
	task, pieceID, _ := newCleanupPieceFixture(t)
	_, err := task.db.Exec(t.Context(), `INSERT INTO parked_piece_refs (piece_id, long_term)
		VALUES ($1, TRUE)`, pieceID)
	require.NoError(t, err)

	done, err := task.Do(t.Context(), cleanupTestTaskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	var unclaimed bool
	var refs, queued int
	err = task.db.QueryRow(t.Context(), `SELECT cleanup_task_id IS NULL, ref_count,
		(SELECT count(*) FROM hash_space_delete) FROM parked_pieces WHERE id = $1`,
		pieceID).Scan(&unclaimed, &refs, &queued)
	require.NoError(t, err)
	require.True(t, unclaimed, "a live piece must be released from cleanup")
	require.Equal(t, 1, refs)
	require.Zero(t, queued)
	require.Empty(t, task.piece.(*cleanupPieceIO).removed)
}

func TestCleanupPieceRemovesIncompleteUploadWithoutOpenPieceDeletion(t *testing.T) {
	task, pieceID, _ := newCleanupPieceFixture(t)
	_, err := task.db.Exec(t.Context(), `UPDATE parked_pieces SET complete = FALSE, skip = TRUE WHERE id = $1`, pieceID)
	require.NoError(t, err)

	done, err := task.Do(t.Context(), cleanupTestTaskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	var parked bool
	var queued int
	err = task.db.QueryRow(t.Context(), `SELECT
		EXISTS (SELECT 1 FROM parked_pieces WHERE id = $1),
		(SELECT count(*) FROM hash_space_delete)`, pieceID).Scan(&parked, &queued)
	require.NoError(t, err)
	require.False(t, parked)
	require.Zero(t, queued, "an incomplete upload has no published open-pieces file")
	require.Equal(t, []storiface.PieceNumber{pieceID}, task.piece.(*cleanupPieceIO).removed)
}

func TestCleanupPieceQueueFailureRetainsMetadataAndFile(t *testing.T) {
	task, pieceID, _ := newCleanupPieceFixture(t)
	_, err := task.db.Exec(t.Context(), `ALTER TABLE hash_space_delete
		ADD CONSTRAINT reject_cleanup_test_queue CHECK (FALSE)`)
	require.NoError(t, err)

	done, err := task.Do(t.Context(), cleanupTestTaskID, func() bool { return true })
	require.ErrorContains(t, err, "reject_cleanup_test_queue")
	require.False(t, done)
	var parked bool
	var queued, tasks int
	err = task.db.QueryRow(t.Context(), `SELECT
		EXISTS (SELECT 1 FROM parked_pieces WHERE id = $1 AND cleanup_task_id = $2),
		(SELECT count(*) FROM hash_space_delete),
		(SELECT count(*) FROM harmony_task WHERE name = $3)`,
		pieceID, cleanupTestTaskID, tasknames.HashSpaceDrop).Scan(&parked, &queued, &tasks)
	require.NoError(t, err)
	require.True(t, parked, "the only record of the file must survive a queue failure")
	require.Zero(t, queued)
	require.Zero(t, tasks, "the queue and its task must roll back together")
	require.Empty(t, task.piece.(*cleanupPieceIO).removed)
}

func TestCleanupPieceTaskCommitFailureRetainsMetadataAndFile(t *testing.T) {
	for _, tc := range []struct {
		name         string
		addReference bool
	}{
		{name: "unreferenced piece"},
		{name: "reference added before task transaction", addReference: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, pieceID, _ := newCleanupPieceFixture(t)
			task.dropAdder = func(extra func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {
				if tc.addReference {
					_, err := task.db.Exec(t.Context(), `INSERT INTO parked_piece_refs (piece_id, long_term)
						VALUES ($1, TRUE)`, pieceID)
					require.NoError(t, err)
				}
				var callbackCommitted bool
				committed, err := task.db.BeginTransaction(t.Context(), func(tx *harmonydb.Tx) (bool, error) {
					var id harmonytask.TaskID
					if err := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time)
						VALUES ($1, 0, CURRENT_TIMESTAMP) RETURNING id`, tasknames.HashSpaceDrop).Scan(&id); err != nil {
						return false, err
					}
					var err error
					callbackCommitted, err = extra(id, tx)
					// AddTaskFunc has no result. A task transaction can fail after its
					// callback succeeded, without notifying the callback's caller.
					return false, err
				})
				require.NoError(t, err)
				require.True(t, callbackCommitted)
				require.False(t, committed)
			}

			done, cleanupErr := task.Do(t.Context(), cleanupTestTaskID, func() bool { return true })
			var parked bool
			var queued, tasks int
			err := task.db.QueryRow(t.Context(), `SELECT
				EXISTS (SELECT 1 FROM parked_pieces WHERE id = $1 AND cleanup_task_id = $2),
				(SELECT count(*) FROM hash_space_delete),
				(SELECT count(*) FROM harmony_task WHERE name = $3)`,
				pieceID, cleanupTestTaskID, tasknames.HashSpaceDrop).Scan(&parked, &queued, &tasks)
			require.NoError(t, err)
			require.True(t, parked)
			require.Zero(t, queued)
			require.Zero(t, tasks)
			require.Empty(t, task.piece.(*cleanupPieceIO).removed,
				"storage removal must wait for the outer task transaction to commit")
			require.Error(t, cleanupErr)
			require.False(t, done)
		})
	}
}

func TestCleanupPieceRetryKeepsOneDurableOpenPieceDeletion(t *testing.T) {
	task, pieceID, v2 := newCleanupPieceFixture(t)
	for range 2 {
		done, err := task.Do(t.Context(), cleanupTestTaskID, func() bool { return true })
		require.NoError(t, err)
		require.True(t, done)
	}
	var queued, tasks int
	err := task.db.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM hash_space_delete WHERE piece_cid = $1),
		(SELECT count(*) FROM harmony_task WHERE name = $2)`,
		v2, tasknames.HashSpaceDrop).Scan(&queued, &tasks)
	require.NoError(t, err)
	require.Equal(t, 1, queued)
	require.Equal(t, 1, tasks)
	require.Equal(t, []storiface.PieceNumber{pieceID}, task.piece.(*cleanupPieceIO).removed)
}

func TestCleanupPieceRemovesLegacyFileWithoutHashSpaceDisks(t *testing.T) {
	task, pieceID, _ := newCleanupPieceFixture(t)
	_, err := task.db.Exec(t.Context(), `DELETE FROM hash_space_disk`)
	require.NoError(t, err)
	_, err = task.db.Exec(t.Context(), `INSERT INTO sector_location
		(miner_id, sector_num, sector_filetype, storage_id, is_primary)
		VALUES (0, $1, 32, 'legacy-test-disk', TRUE)`, pieceID)
	require.NoError(t, err)
	task.dropAdder = nil
	t.Cleanup(harmonytask.SetEngineForTest(nil))

	done, err := task.Do(t.Context(), cleanupTestTaskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	var parked bool
	var queued, tasks int
	err = task.db.QueryRow(t.Context(), `SELECT
		EXISTS (SELECT 1 FROM parked_pieces WHERE id = $1),
		(SELECT count(*) FROM hash_space_delete),
		(SELECT count(*) FROM harmony_task WHERE name = $2)`,
		pieceID, tasknames.HashSpaceDrop).Scan(&parked, &queued, &tasks)
	require.NoError(t, err)
	require.False(t, parked)
	require.Zero(t, queued)
	require.Zero(t, tasks)
	require.Equal(t, []storiface.PieceNumber{pieceID}, task.piece.(*cleanupPieceIO).removed)
}

func TestCleanupPieceWithoutLocalStorageAcceptsOnlyFilelessPieces(t *testing.T) {
	task, migratedPieceID, _ := newCleanupPieceFixture(t)
	const remoteCleanupTaskID harmonytask.TaskID = 456
	var remotePieceID storiface.PieceNumber
	err := task.db.QueryRow(t.Context(), `INSERT INTO parked_pieces
		(piece_cid, piece_padded_size, piece_raw_size, complete, long_term, cleanup_task_id)
		SELECT piece_cid, piece_padded_size, piece_raw_size, TRUE, long_term, $1
		FROM parked_pieces WHERE id = $2 RETURNING id`,
		remoteCleanupTaskID, migratedPieceID).Scan(&remotePieceID)
	require.NoError(t, err)
	_, err = task.db.Exec(t.Context(), `INSERT INTO sector_location
		(miner_id, sector_num, sector_filetype, storage_id, is_primary)
		VALUES (0, $1, 32, 'remote-test-disk', TRUE)`, remotePieceID)
	require.NoError(t, err)

	accepted, err := task.CanAccept([]harmonytask.TaskID{cleanupTestTaskID, remoteCleanupTaskID}, nil)
	require.NoError(t, err)
	require.Equal(t, []harmonytask.TaskID{cleanupTestTaskID}, accepted,
		"a worker without local storage can clean migrated metadata but cannot remove a remote legacy file")
}
