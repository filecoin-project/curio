package proofshare

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
)

var SubmitScheduleInterval = 10 * time.Second

type TaskSubmit struct {
	db *harmonydb.DB
}

func NewTaskSubmit(db *harmonydb.DB) *TaskSubmit {
	return &TaskSubmit{db: db}
}

func (t *TaskSubmit) Adder(addTask harmonytask.AddTaskFunc) {
	ticker := time.NewTicker(SubmitScheduleInterval)
	defer ticker.Stop()

	for range ticker.C {
		t.schedule(context.Background(), addTask)
	}
}

func (t *TaskSubmit) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) {
	var stop bool

	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true

			// 1) Find any rows that are ready to be scheduled for submission.
			var rows []struct {
				ServiceID int64 `db:"service_id"`
			}
			err := tx.Select(&rows, `
				SELECT service_id 
				FROM proofshare_queue
				WHERE compute_done  = TRUE
				  AND submit_done   = FALSE
				  AND submit_task_id IS NULL
				LIMIT 1
			`)
			if err != nil {
				return false, xerrors.Errorf("querying unsubmitted proofs: %w", err)
			}

			// 2) If no rows are found, we’re done scheduling.
			if len(rows) == 0 {
				return false, nil
			}

			// 3) Here we pick the row we want to schedule;
			//    the example picks randomly, but we’ll pick the first for simplicity.
			taskRow := rows[0]

			// 4) Mark this row with the new task ID.
			_, err = tx.Exec(`
				UPDATE proofshare_queue
				SET submit_task_id = $1
				WHERE service_id   = $2
				  AND compute_done = TRUE
				  AND submit_done  = FALSE
				  AND submit_task_id IS NULL
			`, id, taskRow.ServiceID)
			if err != nil {
				return false, xerrors.Errorf("failed to update row with submit_task_id: %w", err)
			}

			stop = false // keep going in our outer loop; maybe we can schedule more
			return true, nil
		})
	}
}

func (t *TaskSubmit) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (t *TaskSubmit) Do(taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	ctx := context.Background()

	// 1) Look up the row assigned to this submit_task_id
	var row struct {
		ServiceID    int64           `db:"service_id"`
		RequestData  json.RawMessage `db:"request_data"`
		ResponseData []byte          `db:"response_data"`
	}
	err := t.db.QueryRow(ctx, `
		SELECT service_id, request_data, response_data
		FROM proofshare_queue
		WHERE submit_task_id = $1
	`, taskID).Scan(&row.ServiceID, &row.RequestData, &row.ResponseData)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Errorf("no row found for submit_task_id=%d, ignoring", taskID)
			return true, nil
		}
		return false, xerrors.Errorf("failed to query proofshare_queue: %w", err)
	}

	// 2) Fetch the remote 'wallet' address from proofshare_meta
	var wallet string
	err = t.db.QueryRow(ctx, `
		SELECT wallet
		FROM proofshare_meta
		WHERE singleton = TRUE
	`).Scan(&wallet)
	if err != nil {
		return false, xerrors.Errorf("failed to read wallet from proofshare_meta: %w", err)
	}
	if wallet == "" {
		return false, xerrors.Errorf("no wallet address configured in proofshare_meta")
	}

	// 3) Prepare the request object for RespondWork
	wreq := common.WorkRequest{
		ID: row.ServiceID,
	}
	proofResp := common.ProofResponse{
		ID:    strconv.FormatInt(row.ServiceID, 10),
		Proof: row.ResponseData,
	}

	// 4) Submit the proof to the remote service
	if err := proofsvc.RespondWork(wallet, wreq, proofResp); err != nil {
		return false, xerrors.Errorf("failed to respond to work: %w", err)
	}

	// 5) Mark the row as submitted in the DB
	_, err = t.db.Exec(ctx, `
		UPDATE proofshare_queue
		SET submit_done = TRUE, submit_task_id = NULL
		WHERE service_id = $1
	`, row.ServiceID)
	if err != nil {
		return false, xerrors.Errorf("failed to update row after submit: %w", err)
	}

	log.Infof("successfully submitted proof for service_id=%d", row.ServiceID)
	return true, nil
}

func (t *TaskSubmit) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PShareSubmit",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 5,
		RetryWait: func(retries int) time.Duration {
			return time.Second * 10 * time.Duration(retries)
		},
	}
}

// Register with the harmonytask engine
var _ = harmonytask.Reg(&TaskSubmit{})
