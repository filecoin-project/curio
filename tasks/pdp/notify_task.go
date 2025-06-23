package pdp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	logger "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
)

var log = logger.Logger("pdp")

type PDPNotifyTask struct {
	db *harmonydb.DB
}

func NewPDPNotifyTask(db *harmonydb.DB) *PDPNotifyTask {
	return &PDPNotifyTask{db: db}
}

func (t *PDPNotifyTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Fetch the pdp_piece_uploads entry associated with the taskID
	var upload struct {
		ID             string  `db:"id" json:"id"`
		Service        string  `db:"service" json:"service"`
		PieceCID       *string `db:"piece_cid" json:"piece_cid"`
		NotifyURL      string  `db:"notify_url" json:"notify_url"`
		PieceRef       int64   `db:"piece_ref" json:"piece_ref"`
		CheckHashCodec string  `db:"check_hash_codec" json:"check_hash_codec"`
		CheckHash      []byte  `db:"check_hash" json:"check_hash"`
	}
	err = t.db.QueryRow(ctx, `
        SELECT id, service, piece_cid, notify_url, piece_ref, check_hash_codec, check_hash 
        FROM pdp_piece_uploads 
        WHERE notify_task_id = $1`, taskID).Scan(
		&upload.ID, &upload.Service, &upload.PieceCID, &upload.NotifyURL, &upload.PieceRef, &upload.CheckHashCodec, &upload.CheckHash)
	if err != nil {
		return false, fmt.Errorf("failed to query pdp_piece_uploads for task %d: %w", taskID, err)
	}

	// Perform HTTP Post request to the notify URL
	upJson, err := json.Marshal(upload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal upload to JSON: %w", err)
	}

	log.Infow("PDP notify", "upload", upload, "task_id", taskID)

	if upload.NotifyURL != "" {

		resp, err := http.Post(upload.NotifyURL, "application/json", bytes.NewReader(upJson))
		if err != nil {
			log.Errorw("HTTP POST request to notify_url failed", "notify_url", upload.NotifyURL, "upload_id", upload.ID, "error", err)
		} else {
			defer resp.Body.Close()
			// Not reading the body as per requirement
			log.Infow("HTTP GET request to notify_url succeeded", "notify_url", upload.NotifyURL, "upload_id", upload.ID)
		}
	}

	// Move the entry from pdp_piece_uploads to pdp_piecerefs
	// Insert into pdp_piecerefs
	_, err = t.db.Exec(ctx, `
        INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at) 
        VALUES ($1, $2, $3, NOW())`,
		upload.Service, upload.PieceCID, upload.PieceRef)
	if err != nil {
		return false, fmt.Errorf("failed to insert into pdp_piecerefs: %w", err)
	}

	// Delete the entry from pdp_piece_uploads
	_, err = t.db.Exec(ctx, `DELETE FROM pdp_piece_uploads WHERE id = $1`, upload.ID)
	if err != nil {
		return false, fmt.Errorf("failed to delete upload ID %s from pdp_piece_uploads: %w", upload.ID, err)
	}

	log.Infof("Successfully processed PDP notify task %d for upload ID %s", taskID, upload.ID)

	return true, nil
}

func (t *PDPNotifyTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return nil, xerrors.Errorf("no task IDs provided")
	}
	id := ids[0]
	return &id, nil
}

func (t *PDPNotifyTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPNotify",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 128 << 20, // 128MB
		},
		MaxFailures: 14,
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
		IAmBored: passcall.Every(5*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *PDPNotifyTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // Assume we're done unless we find more tasks to schedule

			// Query for pending notifications where:
			// - piece_ref is not null
			// - The piece_ref points to a parked_piece_refs entry
			// - The parked_piece_refs entry points to a parked_pieces entry where complete = TRUE
			// - notify_task_id is NULL

			var uploads []struct {
				ID string `db:"id"`
			}

			err := tx.Select(&uploads, `
                SELECT pu.id
                FROM pdp_piece_uploads pu
                JOIN parked_piece_refs pr ON pr.ref_id = pu.piece_ref
                JOIN parked_pieces pp ON pp.id = pr.piece_id
                WHERE 
                    pu.piece_ref IS NOT NULL
                    AND pp.complete = TRUE
                    AND pu.notify_task_id IS NULL
                LIMIT 1
            `)
			if err != nil {
				return false, xerrors.Errorf("getting uploads to notify: %w", err)
			}

			if len(uploads) == 0 {
				// No uploads to process
				return false, nil
			}

			// Update the pdp_piece_uploads entry to set notify_task_id
			_, err = tx.Exec(`
                UPDATE pdp_piece_uploads 
                SET notify_task_id = $1 
                WHERE id = $2 AND notify_task_id IS NULL
            `, id, uploads[0].ID)
			if err != nil {
				return false, xerrors.Errorf("updating notify_task_id: %w", err)
			}

			stop = false     // Continue scheduling as there might be more tasks
			return true, nil // Commit the transaction
		})
	}
	return nil
}

func (t *PDPNotifyTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ = harmonytask.Reg(&PDPNotifyTask{})
var _ harmonytask.TaskInterface = &PDPNotifyTask{}
