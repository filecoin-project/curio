package pdpv0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/promise"
)

var log = logging.Logger("pdpv0")

// NotifyPollInterval is how often to poll for uploads ready to finalize.
var NotifyPollInterval = 2 * time.Second

// PDPNotifyTask finalizes completed piece uploads.
//
// When piece data finishes uploading (parked_pieces.complete = TRUE), this task:
//  1. Sends an optional HTTP POST callback to notify_url if configured
//  2. Creates a permanent reference in pdp_piecerefs linking piece_cid to piece_ref
//  3. Removes the temporary upload record from pdp_piece_uploads
//
// The poll goroutine watches for uploads where the underlying piece is complete
// but no finalization task has been assigned yet.
type PDPNotifyTask struct {
	db     *harmonydb.DB
	TF     promise.Promise[harmonytask.AddTaskFunc]
	client *http.Client
}

func NewPDPNotifyTask(ctx context.Context, db *harmonydb.DB) *PDPNotifyTask {
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}
	n := &PDPNotifyTask{db: db, client: client}
	go n.poll(ctx)
	return n
}

func (t *PDPNotifyTask) poll(ctx context.Context) {
	ticker := time.NewTicker(NotifyPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		var uploads []struct {
			ID string `db:"id"`
		}

		err := t.db.Select(ctx, &uploads, `
                SELECT pu.id
                FROM pdp_piece_uploads pu
                JOIN parked_piece_refs pr ON pr.ref_id = pu.piece_ref
                JOIN parked_pieces pp ON pp.id = pr.piece_id
                WHERE
                    pu.piece_ref IS NOT NULL
                    AND pp.complete = TRUE
                    AND pu.notify_task_id IS NULL LIMIT 10`)
		if err != nil {
			log.Errorf("getting uploads to notify: %s", err)
			continue
		}

		if len(uploads) == 0 {
			continue
		}

		for _, upload := range uploads {
			failed := false

			t.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				n, err := tx.Exec(`
					UPDATE pdp_piece_uploads
					SET notify_task_id = $1
					WHERE id = $2 AND notify_task_id IS NULL`, id, upload.ID)
				if err != nil {
					failed = true
					return false, xerrors.Errorf("updating notify_task_id: %w", err)
				}
				return n > 0, nil
			})
			if failed {
				break
			}
		}
	}
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
		PieceRawSize   uint64  `db:"piece_raw_size" json:"piece_raw_size"`
	}
	err = t.db.QueryRow(ctx, `
        SELECT pu.id, pu.service, pu.piece_cid, pu.notify_url, pu.piece_ref, pu.check_hash_codec, pu.check_hash,
               pp.piece_raw_size
        FROM pdp_piece_uploads pu
        JOIN parked_piece_refs ppr ON ppr.ref_id = pu.piece_ref
        JOIN parked_pieces pp ON pp.id = ppr.piece_id
        WHERE pu.notify_task_id = $1`, taskID).Scan(
		&upload.ID, &upload.Service, &upload.PieceCID, &upload.NotifyURL, &upload.PieceRef, &upload.CheckHashCodec, &upload.CheckHash, &upload.PieceRawSize)
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

		resp, err := t.client.Post(upload.NotifyURL, "application/json", bytes.NewReader(upJson))
		if err != nil {
			log.Errorw("HTTP POST request to notify_url failed", "notify_url", upload.NotifyURL, "upload_id", upload.ID, "error", err)
		} else {
			defer func() {
				_ = resp.Body.Close()
			}()
			// Not reading the body as per requirement
			log.Infow("HTTP GET request to notify_url succeeded", "notify_url", upload.NotifyURL, "upload_id", upload.ID)
		}
	}

	comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Move the entry from pdp_piece_uploads to pdp_piecerefs
		// Insert into pdp_piecerefs
		// Set needs_save_cache=TRUE for large pieces to enable proactive caching
		needsSaveCache := upload.PieceRawSize >= MinSizeForCache
		_, err = t.db.Exec(ctx, `
        INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
        VALUES ($1, $2, $3, NOW(), $4)`,
			upload.Service, upload.PieceCID, upload.PieceRef, needsSaveCache)
		if err != nil {
			return false, fmt.Errorf("failed to insert into pdp_piecerefs: %w", err)
		}

		_, err = tx.Exec(`DELETE FROM pdp_piece_uploads WHERE id = $1`, upload.ID)
		if err != nil {
			return false, fmt.Errorf("failed to delete upload ID %s from pdp_piece_uploads: %w", upload.ID, err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, fmt.Errorf("failed to move upload to piecerefs: %w", err)
	}
	if !comm {
		return false, fmt.Errorf("transaction to move upload to piecerefs was not committed")
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
		Name: "PDPv0_Notify",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 128 << 20, // 128MB
		},
		MaxFailures: 14,
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
	}
}

func (t *PDPNotifyTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var _ = harmonytask.Reg(&PDPNotifyTask{})
var _ harmonytask.TaskInterface = &PDPNotifyTask{}
