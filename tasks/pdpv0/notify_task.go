package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

// PDPNotifyTask only drains completed uploads left by the scratch-backed
// pipeline. New uploads cannot match its scheduling predicate because they
// publish and delete their upload intent in the transaction that marks storage
// done.
type PDPNotifyTask struct {
	db *harmonydb.DB
}

func NewPDPNotifyTask(db *harmonydb.DB) *PDPNotifyTask {
	return &PDPNotifyTask{db: db}
}

func (t *PDPNotifyTask) Do(ctx context.Context, taskID harmonytask.TaskID, _ func() bool) (bool, error) {
	committed, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var upload struct {
			id            string
			service       string
			pieceCID      sql.NullString
			pieceRef      sql.NullInt64
			pieceRawSize  sql.NullInt64
			pieceComplete sql.NullBool
		}

		err := tx.QueryRow(`
			SELECT pu.id::TEXT,
			       pu.service,
			       pu.piece_cid,
			       pu.piece_ref,
			       pp.piece_raw_size,
			       pp.complete
			FROM pdp_piece_uploads pu
			LEFT JOIN parked_piece_refs ppr ON ppr.ref_id = pu.piece_ref
			LEFT JOIN parked_pieces pp ON pp.id = ppr.piece_id
			WHERE pu.notify_task_id = $1
		`, taskID).Scan(
			&upload.id,
			&upload.service,
			&upload.pieceCID,
			&upload.pieceRef,
			&upload.pieceRawSize,
			&upload.pieceComplete,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("load legacy PDP upload for task %d: %w", taskID, err)
		}
		if !upload.pieceCID.Valid || !upload.pieceRef.Valid || !upload.pieceRawSize.Valid || !upload.pieceComplete.Valid {
			return false, fmt.Errorf("legacy PDP upload %s has no usable stored piece", upload.id)
		}
		if !upload.pieceComplete.Bool {
			return false, fmt.Errorf("legacy PDP upload %s is not in final storage", upload.id)
		}

		needsSaveCache := padreader.PaddedSize(uint64(upload.pieceRawSize.Int64)).Padded() >= abi.PaddedPieceSize(MinSizeForCache)
		n, err := tx.Exec(`
			INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
			VALUES ($1, $2, $3, NOW(), $4)
			ON CONFLICT (piece_ref) DO NOTHING
		`, upload.service, upload.pieceCID.String, upload.pieceRef.Int64, needsSaveCache)
		if err != nil {
			return false, fmt.Errorf("publish legacy PDP upload %s: %w", upload.id, err)
		}
		if n == 0 {
			var matches bool
			err = tx.QueryRow(`
				SELECT EXISTS(
					SELECT 1
					FROM pdp_piecerefs
					WHERE piece_ref = $1 AND service = $2 AND piece_cid = $3
				)
			`, upload.pieceRef.Int64, upload.service, upload.pieceCID.String).Scan(&matches)
			if err != nil {
				return false, fmt.Errorf("check published legacy PDP upload %s: %w", upload.id, err)
			}
			if !matches {
				return false, fmt.Errorf("legacy PDP upload %s conflicts with an existing PDP reference", upload.id)
			}
		}

		n, err = tx.Exec(`
			DELETE FROM pdp_piece_uploads
			WHERE id = $1 AND notify_task_id = $2 AND piece_ref = $3
		`, upload.id, taskID, upload.pieceRef.Int64)
		if err != nil {
			return false, fmt.Errorf("delete published legacy PDP upload %s: %w", upload.id, err)
		}
		if n != 1 {
			return false, fmt.Errorf("delete published legacy PDP upload %s: expected 1 row, got %d", upload.id, n)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, err
	}
	if !committed {
		return false, fmt.Errorf("legacy PDP upload task %d did not commit", taskID)
	}
	return true, nil
}

func (t *PDPNotifyTask) schedule(taskFunc harmonytask.AddTaskFunc) error {
	for {
		added := false
		var scheduleErr error
		taskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`
				WITH pending AS (
					SELECT pu.id
					FROM pdp_piece_uploads pu
					JOIN parked_piece_refs ppr ON ppr.ref_id = pu.piece_ref
					JOIN parked_pieces pp ON pp.id = ppr.piece_id
					WHERE pu.notify_task_id IS NULL
					  AND pu.piece_ref IS NOT NULL
					  AND pp.complete = TRUE
					ORDER BY pu.created_at, pu.id
					LIMIT 1
				)
				UPDATE pdp_piece_uploads pu
				SET notify_task_id = $1
				FROM pending
				WHERE pu.id = pending.id
				  AND pu.notify_task_id IS NULL
			`, taskID)
			if err != nil {
				scheduleErr = fmt.Errorf("assign legacy PDP upload task: %w", err)
				return false, scheduleErr
			}
			if n > 1 {
				scheduleErr = fmt.Errorf("assigned legacy PDP upload task to %d uploads", n)
				return false, scheduleErr
			}
			added = n == 1
			return added, nil
		})
		if scheduleErr != nil {
			return scheduleErr
		}
		if !added {
			return nil
		}
	}
}

func (t *PDPNotifyTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *PDPNotifyTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: tasknames.PDPv0_Notify,
		Max:  taskhelp.Max(4),
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 14,
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
		CanYield:    true,
		IAmBored: passcall.Every(5*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(taskFunc)
		}),
	}
}

func (t *PDPNotifyTask) Adder(harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPNotifyTask{}
var _ = harmonytask.Reg(&PDPNotifyTask{})
