package webrpc

import (
	"context"
	"database/sql"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/snadrus/must"
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
	ServiceID     int64     `db:"service_id"     json:"service_id"`
	ObtainedAt    time.Time `db:"obtained_at"    json:"obtained_at"`
	ComputeTaskID *int64    `db:"compute_task_id" json:"compute_task_id"`
	ComputeDone   bool      `db:"compute_done"   json:"compute_done"`
	SubmitTaskID  *int64    `db:"submit_task_id" json:"submit_task_id"`
	SubmitDone    bool      `db:"submit_done"    json:"submit_done"`
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
	items := []ProofShareQueueItem{}

	err := a.deps.DB.Select(ctx, &items, `
        SELECT service_id,
               obtained_at,
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

///////
// CLIENT

// ProofShareClientSettings model
// Matches proofshare_client_settings table columns
type ProofShareClientSettings struct {
	SpID               int64   `db:"sp_id"                 json:"sp_id"`
	Enabled            bool    `db:"enabled"               json:"enabled"`
	Wallet             *string `db:"wallet"                json:"wallet"`
	MinimumPendingSecs int64   `db:"minimum_pending_seconds" json:"minimum_pending_seconds"`
	DoPoRep            bool    `db:"do_porep"              json:"do_porep"`
	DoSnap             bool    `db:"do_snap"               json:"do_snap"`

	Address string `db:"-" json:"address"`
}

// ProofShareClientRequest model
// Matches proofshare_client_requests table columns
type ProofShareClientRequest struct {
	TaskID    int64        `db:"task_id"      json:"task_id"`
	SpID      int64        `db:"sp_id"        json:"sp_id"`
	SectorNum int64        `db:"sector_num"   json:"sector_num"`
	ServiceID int64        `db:"service_id"   json:"service_id"`
	Done      bool         `db:"done"         json:"done"`
	CreatedAt time.Time    `db:"created_at"   json:"created_at"`
	DoneAt    sql.NullTime `db:"done_at"      json:"done_at,omitempty"`
}
// PSClientGet fetches all proofshare_client_settings rows.
func (a *WebRPC) PSClientGet(ctx context.Context) ([]ProofShareClientSettings, error) {
	var out []ProofShareClientSettings
	err := a.deps.DB.Select(ctx, &out, `
        SELECT sp_id, enabled, wallet, minimum_pending_seconds, do_porep, do_snap
        FROM proofshare_client_settings
        ORDER BY sp_id ASC
    `)
	if err != nil {
		return nil, xerrors.Errorf("PSClientGet: query error: %w", err)
	}


	for i := range out {
		out[i].Address = must.One(address.NewIDAddress(uint64(out[i].SpID))).String()
	}

	return out, nil
}

// PSClientSet updates or inserts a row in proofshare_client_settings.
// If a row for sp_id doesn’t exist, do an INSERT; otherwise do an UPDATE.
func (a *WebRPC) PSClientSet(ctx context.Context, s ProofShareClientSettings) error {
	maddr, err := address.NewFromString(s.Address)
	if err != nil {
		return xerrors.Errorf("PSClientSet: invalid address: %w", err)
	}

	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return xerrors.Errorf("PSClientSet: invalid address: %w", err)
	}

	s.SpID = int64(mid)

	_, err = a.deps.DB.Exec(ctx, `
        INSERT INTO proofshare_client_settings (sp_id, enabled, wallet, minimum_pending_seconds, do_porep, do_snap)
        VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (sp_id) DO UPDATE
          SET enabled = EXCLUDED.enabled,
              wallet  = EXCLUDED.wallet,
              minimum_pending_seconds = EXCLUDED.minimum_pending_seconds,
              do_porep = EXCLUDED.do_porep,
              do_snap = EXCLUDED.do_snap
    `,
		s.SpID,
		s.Enabled,
		s.Wallet,
		s.MinimumPendingSecs,
		s.DoPoRep,
		s.DoSnap,
	)
	if err != nil {
		return xerrors.Errorf("PSClientSet: upsert error: %w", err)
	}
	return nil
}

// PSClientRequests returns the list of proofshare_client_requests for a given sp_id
// (or all if sp_id=0 and that’s your convention).
func (a *WebRPC) PSClientRequests(ctx context.Context, spId int64) ([]ProofShareClientRequest, error) {
	var rows []ProofShareClientRequest

	// If you want spId=0 to mean "all," you can do logic in WHERE
	// e.g.: WHERE (sp_id = $1 OR $1=0)
	err := a.deps.DB.Select(ctx, &rows, `
        SELECT task_id, sp_id, sector_num, service_id, done, created_at, done_at
        FROM proofshare_client_requests
        WHERE (sp_id = $1 OR $1=0)
        ORDER BY created_at DESC
    `, spId)
	if err != nil {
		return nil, xerrors.Errorf("PSClientRequests: query error: %w", err)
	}
	return rows, nil
}

// PSClientRemove removes a row from proofshare_client_settings if sp_id != 0.
func (a *WebRPC) PSClientRemove(ctx context.Context, spId int64) error {
    if spId == 0 {
        return xerrors.Errorf("cannot remove default sp_id=0 row")
    }
    _, err := a.deps.DB.Exec(ctx, `
        DELETE FROM proofshare_client_settings
        WHERE sp_id = $1
    `, spId)
    if err != nil {
        return xerrors.Errorf("PSClientRemove: delete error: %w", err)
    }
    return nil
}
