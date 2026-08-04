package piece

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

type CleanupPieceTask struct {
	max int
	db  *harmonydb.DB
	sc  *ffi.SealCalls

	TF promise.Promise[harmonytask.AddTaskFunc]

	wake chan struct{}
}

func NewCleanupPieceTask(db *harmonydb.DB, sc *ffi.SealCalls, max int) *CleanupPieceTask {
	pt := &CleanupPieceTask{
		db:   db,
		sc:   sc,
		max:  max,
		wake: make(chan struct{}, 1),
	}
	go pt.pollCleanupTasks(context.Background())
	return pt
}

// WakePoll nudges pollCleanupTasks to run soon.
func (c *CleanupPieceTask) WakePoll() {
	if c == nil || c.wake == nil {
		return
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// cleanupCandidateBatch caps how many candidate pieces the poller pulls per
// iteration. The bounded SELECT plus the cleanup index on
// (ref_count, cleanup_task_id, id) keeps each poll cheap regardless of total
// parked_pieces size.
const cleanupCandidateBatch = 256

func (c *CleanupPieceTask) pollCleanupTasks(ctx context.Context) {
	ticker := time.NewTicker(PieceParkPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-ticker.C:
		}

		err := c.schedule(ctx)
		if err != nil {
			log.Errorf("failed to schedule cleanup piece task: %s", err)
		}
	}
}

func (c *CleanupPieceTask) schedule(ctx context.Context) error {
	// ref_count is maintained by triggers on parked_piece_refs, so this
	// query is served by idx_parked_pieces_cleanup_eligible without
	// scanning parked_piece_refs at all.
	var pieceIDs []storiface.PieceNumber

	err := c.db.Select(ctx, &pieceIDs, `SELECT id
			FROM parked_pieces
			WHERE cleanup_task_id IS NULL
			  AND ref_count = 0
			ORDER BY id
			LIMIT $1`, cleanupCandidateBatch)
	if err != nil {
		return xerrors.Errorf("failed to get parked pieces: %w", err)
	}

	for _, pieceID := range pieceIDs {

		// create a task for each piece
		c.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
			// Atomically claim the piece. ref_count = 0 must still hold;
			// if a ref was added since the SELECT, the trigger bumped
			// ref_count and this UPDATE becomes a no-op.
			n, err := tx.Exec(`UPDATE parked_pieces
						SET cleanup_task_id = $1
						WHERE cleanup_task_id IS NULL
						  AND id = $2
						  AND ref_count = 0`, id, pieceID)
			if err != nil {
				return false, xerrors.Errorf("updating parked piece: %w", err)
			}

			if n > 0 {
				log.Debugf("piece id %d scheduled for cleanup", pieceID)
			}

			// commit only if we updated the piece
			return n > 0, nil
		})
	}

	return nil
}

func (c *CleanupPieceTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	// select by cleanup_task_id
	var pieceID int64

	err = c.db.QueryRow(ctx, "SELECT id FROM parked_pieces WHERE cleanup_task_id = $1", taskID).Scan(&pieceID)
	if err != nil {
		return false, xerrors.Errorf("query parked_piece: %w", err)
	}

	// Delete from the database first so new refs cannot attach to this piece.
	// Storage removal below is best effort after the DB row is gone.
	n, err := c.db.Exec(ctx, `DELETE FROM parked_pieces pp
		WHERE pp.id = $1
		  AND NOT EXISTS (
			  SELECT 1
			  FROM parked_piece_refs ppr
			  WHERE ppr.piece_id = pp.id
		  )`, pieceID)
	if err != nil {
		return false, xerrors.Errorf("delete parked_piece: %w", err)
	}

	if n == 0 {
		_, err = c.db.Exec(ctx, `UPDATE parked_pieces SET cleanup_task_id = NULL WHERE id = $1`, pieceID)
		if err != nil {
			return false, xerrors.Errorf("marking piece as complete: %w", err)
		}

		return true, nil
	}

	// remove from storage
	err = c.sc.RemovePiece(ctx, storiface.PieceNumber(pieceID))
	if err != nil {
		log.Errorw("remove piece", "piece_id", pieceID, "error", err)
	}

	return true, nil
}

func (c *CleanupPieceTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if storiface.FTPiece != 32 {
		panic("storiface.FTPiece != 32")
	}

	ctx := context.Background()

	ls, err := c.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}
	if len(ls) == 0 {
		return nil, nil
	}

	storageIDs := make([]string, 0, len(ls))
	for _, l := range ls {
		storageIDs = append(storageIDs, string(l.ID))
	}

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	var acceptedIDs []harmonytask.TaskID
	err = c.db.QueryRow(ctx, `SELECT COALESCE(array_agg(cleanup_task_id), '{}')::bigint[] AS cleanup_task_ids FROM 
										(
										    SELECT pp.cleanup_task_id FROM parked_pieces pp
											INNER JOIN sector_location l ON l.miner_id = 0 AND l.sector_num = pp.id AND l.sector_filetype = 32
											WHERE cleanup_task_id = ANY ($1) 
											  AND l.storage_id = ANY ($2)
											  LIMIT 100
										) s`, indIDs, storageIDs).Scan(&acceptedIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting tasks from DB: %w", err)
	}

	return acceptedIDs, nil
}

func (c *CleanupPieceTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:       taskhelp.Max(c.max),
		Name:      tasknames.DropPiece,
		MayFollow: []string{tasknames.MoveStorage, tasknames.UpdateStore},
		Cost: resources.Resources{
			Cpu:     0,
			Gpu:     0,
			Ram:     64 << 20,
			Storage: nil,
		},
		MaxFailures: 10,
	}
}

func (c *CleanupPieceTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	c.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &CleanupPieceTask{}
var _ = harmonytask.Reg(&CleanupPieceTask{})
