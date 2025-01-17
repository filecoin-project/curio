package webrpc

import (
	"context"
	"encoding/json"
	"time"

	"golang.org/x/xerrors"
)

// ProofShareMeta holds the data from the proofshare_meta table.
type ProofShareMeta struct {
	Enabled       bool    `db:"enabled" json:"enabled"`
	Wallet        *string `db:"wallet" json:"wallet"`
	RequestTaskID *int64  `db:"request_task_id" json:"request_task_id"`
}

// ProofShareQueueItem represents each row in proofshare_queue.
type ProofShareQueueItem struct {
	ServiceID     int64           `db:"service_id"     json:"service_id"`
	ObtainedAt    time.Time       `db:"obtained_at"    json:"obtained_at"`
	RequestData   json.RawMessage `db:"request_data"   json:"request_data"`
	ResponseData  json.RawMessage `db:"response_data"  json:"response_data"`
	ComputeTaskID *int64          `db:"compute_task_id" json:"compute_task_id"`
	ComputeDone   bool            `db:"compute_done"   json:"compute_done"`
	SubmitTaskID  *int64          `db:"submit_task_id" json:"submit_task_id"`
	SubmitDone    bool            `db:"submit_done"    json:"submit_done"`
}

// PSGetMeta returns the current meta row from proofshare_meta (always a single row).
func (a *WebRPC) PSGetMeta(ctx context.Context) (ProofShareMeta, error) {
	var meta ProofShareMeta

	err := a.deps.DB.QueryRow(ctx, `
        SELECT enabled, wallet, request_task_id
        FROM proofshare_meta
        WHERE singleton = TRUE
    `).Scan(&meta.Enabled, &meta.Wallet, &meta.RequestTaskID)
	if err != nil {
		return meta, xerrors.Errorf("PSGetMeta: failed to query proofshare_meta: %w", err)
	}

	return meta, nil
}

// PSSetMeta updates proofshare_meta with new "enabled" flag and "wallet" address.
// If you want to allow a NULL wallet, you could accept a pointer or do conditional logic.
func (a *WebRPC) PSSetMeta(ctx context.Context, enabled bool, wallet string) error {
	_, err := a.deps.DB.Exec(ctx, `
        UPDATE proofshare_meta
        SET enabled = $1, wallet = $2
        WHERE singleton = TRUE
    `, enabled, wallet)
	if err != nil {
		return xerrors.Errorf("PSSetMeta: failed to update proofshare_meta: %w", err)
	}
	return nil
}

// PSListQueue returns all records from the proofshare_queue table, ordered by the newest first.
func (a *WebRPC) PSListQueue(ctx context.Context) ([]ProofShareQueueItem, error) {
	var items []ProofShareQueueItem

	err := a.deps.DB.Select(ctx, &items, `
        SELECT service_id,
               obtained_at,
               request_data,
               response_data,
               compute_task_id,
               compute_done,
               submit_task_id,
               submit_done
        FROM proofshare_queue
        ORDER BY obtained_at DESC
    `)
	if err != nil {
		return nil, xerrors.Errorf("PSListQueue: failed to query proofshare_queue: %w", err)
	}

	return items, nil
}
