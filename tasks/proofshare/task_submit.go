package proofshare

import (
	"context"
	"database/sql"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/proofsvc"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

var SubmitScheduleInterval = 10 * time.Second

type TaskSubmit struct {
	db    *harmonydb.DB
	chain api.FullNode
}

func NewTaskSubmit(db *harmonydb.DB, chain api.FullNode) *TaskSubmit {
	return &TaskSubmit{db: db, chain: chain}
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

			// 2) If no rows are found, we're done scheduling.
			if len(rows) == 0 {
				return false, nil
			}

			// 3) Here we pick the row we want to schedule;
			//    the example picks randomly, but we'll pick the first for simplicity.
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

func (t *TaskSubmit) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *TaskSubmit) Do(taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	ctx := context.Background()

	// 1) Look up the row assigned to this submit_task_id
	var row struct {
		ServiceID    int64  `db:"service_id"`
		RequestCid   string `db:"request_cid"`
		ResponseData []byte `db:"response_data"`
	}
	err := t.db.QueryRow(ctx, `
		SELECT service_id, request_cid, response_data
		FROM proofshare_queue
		WHERE submit_task_id = $1
	`, taskID).Scan(&row.ServiceID, &row.RequestCid, &row.ResponseData)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Errorf("no row found for submit_task_id=%d, ignoring", taskID)
			return true, nil
		}

		log.Errorf("failed to query proofshare_queue: %w", err)
		// orphan, can't do much
		return true, nil
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

	// wallet to id addr
	addr, err := address.NewFromString(wallet)
	if err != nil {
		return false, xerrors.Errorf("failed to parse wallet address: %w", err)
	}

	actid, err := t.chain.StateLookupID(ctx, addr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("failed to lookup address: %w", err)
	}

	walletId, err := address.IDFromAddress(actid)
	if err != nil {
		return false, xerrors.Errorf("failed to get wallet id: %w", err)
	}

	// 3) Submit the proof to the remote service
	// Create address resolver
	resolver, err := proofsvc.NewAddressResolver(t.chain)
	if err != nil {
		return false, xerrors.Errorf("failed to create address resolver: %w", err)
	}

	reward, gone, err := proofsvc.RespondWork(ctx, resolver, addr, row.RequestCid, row.ResponseData)
	if err != nil && !gone {
		return false, xerrors.Errorf("failed to respond to work (gone=%t): %w", gone, err)
	}

	if gone {
		log.Infof("work request gone (reassigned to another provider) for service_id=%d", row.ServiceID)

		// delete the row
		_, err = t.db.Exec(ctx, `
			DELETE FROM proofshare_queue
			WHERE service_id = $1
		`, row.ServiceID)
		if err != nil {
			return false, xerrors.Errorf("failed to delete row after work request gone: %w", err)
		}
		return true, nil
	}

	// 4) Mark the row as submitted in the DB
	_, err = t.db.Exec(ctx, `
		UPDATE proofshare_queue
		SET submit_done = TRUE, submit_task_id = NULL, was_pow = $2
		WHERE service_id = $1
	`, row.ServiceID, reward.PoW)
	if err != nil {
		return false, xerrors.Errorf("failed to update row after submit: %w", err)
	}

	// 5) Insert the payment into the DB
	if !reward.PoW {
		_, err = t.db.Exec(ctx, `
			INSERT INTO proofshare_provider_payments (provider_id, request_cid, payment_nonce, payment_cumulative_amount, payment_signature)
			VALUES ($1, $2, $3, $4, $5)
		`, walletId, row.RequestCid, reward.Nonce, reward.CumulativeAmount, reward.Signature)
		if err != nil {
			return false, xerrors.Errorf("failed to insert payment into DB: %w", err)
		}
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
		RetryWait: func(retries int) time.Duration {
			return time.Second * 10 * time.Duration(retries)
		},
	}
}

// Register with the harmonytask engine
var _ = harmonytask.Reg(&TaskSubmit{})
