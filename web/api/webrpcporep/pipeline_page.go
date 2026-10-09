package webrpcporep

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/go-address"
)

const POREP_PAGE_SIZE = 100
const POREP_PAGE_TIMEOUT = 45 * time.Second

type PoRepPageRequest struct {
	Offset         int
	HidePendingSDR bool
}

type PoRepPageEntry struct {
	PipelineTask
	Address string
	// The progress page is a DB snapshot, not a chain-state query. Null is
	// deliberately different from a negative on-chain result.
	AfterSeed     *bool
	ChainAlloc    *bool
	ChainSector   *bool
	ChainActive   *bool
	ChainUnproven *bool
	ChainFaulty   *bool
}

type PoRepPage struct {
	Sectors             []PoRepPageEntry
	Total               int64
	Matching            int64
	WaitingForPrecommit int64
	WaitingForCommit    int64
	ObservedAt          time.Time
	Offset              int
	Limit               int
	HidePendingSDR      bool
}

type porepPageRow struct {
	PipelineTask
	PageSpID            *int64    `db:"page_sp_id"`
	Total               int64     `db:"total"`
	Matching            int64     `db:"matching"`
	WaitingForPrecommit int64     `db:"waiting_precommit"`
	WaitingForCommit    int64     `db:"waiting_commit"`
	ObservedAt          time.Time `db:"observed_at"`
}

// Counts, filter membership and page rows share one statement snapshot. Only
// page rows get stage-task joins; proof bytes and chain enrichment are absent.
// Counts/ordering still inspect the pipeline: LIMIT is not a bound on DB scans.
const porepPageQuery = `
WITH candidates AS (
	SELECT sp.sp_id, sp.sector_number, sp.failed, sp.after_sdr,
		COALESCE(ht.owner_id > 0, FALSE) AS sdr_owned,
		(sp.after_synth AND sp.precommit_ready_at IS NOT NULL
		 AND NOT sp.after_precommit_msg AND sp.task_id_precommit_msg IS NULL) AS waiting_precommit,
		(sp.commit_ready_at IS NOT NULL AND NOT sp.after_commit_msg
		 AND sp.task_id_commit_msg IS NULL) AS waiting_commit
	FROM sectors_sdr_pipeline sp
	LEFT JOIN harmony_task ht ON ht.id = sp.task_id_sdr
), totals AS (
	SELECT COUNT(*) AS total,
		COUNT(*) FILTER (WHERE NOT $1 OR failed OR after_sdr OR sdr_owned) AS matching,
		COUNT(*) FILTER (WHERE waiting_precommit) AS waiting_precommit,
		COUNT(*) FILTER (WHERE waiting_commit) AS waiting_commit
	FROM candidates
), page AS (
	SELECT sp_id, sector_number, sdr_owned,
		CASE WHEN failed OR after_sdr OR sdr_owned THEN 0 ELSE 1 END AS priority
	FROM candidates
	WHERE NOT $1 OR failed OR after_sdr OR sdr_owned
	ORDER BY priority, sp_id, sector_number
	LIMIT $2 OFFSET $3
)
SELECT totals.*, statement_timestamp() AS observed_at, page.sp_id AS page_sp_id,
	COALESCE(sp.sp_id, 0) AS sp_id, COALESCE(sp.sector_number, 0) AS sector_number,
	COALESCE(sp.create_time, statement_timestamp()) AS create_time,
	COALESCE(sp.failed, FALSE) AS failed, COALESCE(sp.failed_reason, '') AS failed_reason,
	COALESCE(page.sdr_owned, FALSE) AS sdr_owned,
	sp.task_id_sdr, COALESCE(sp.after_sdr, FALSE) AS after_sdr,
	sp.task_id_tree_d, COALESCE(sp.after_tree_d, FALSE) AS after_tree_d,
	sp.task_id_tree_c, COALESCE(sp.after_tree_c, FALSE) AS after_tree_c,
	sp.task_id_tree_r, COALESCE(sp.after_tree_r, FALSE) AS after_tree_r,
	sp.task_id_synth, COALESCE(sp.after_synth, FALSE) AS after_synth,
	sp.precommit_ready_at, sp.task_id_precommit_msg,
	COALESCE(sp.after_precommit_msg, FALSE) AS after_precommit_msg,
	COALESCE(sp.after_precommit_msg_success, FALSE) AS after_precommit_msg_success,
	sp.seed_epoch, sp.task_id_porep, COALESCE(sp.after_porep, FALSE) AS after_porep,
	sp.task_id_finalize, COALESCE(sp.after_finalize, FALSE) AS after_finalize,
	sp.task_id_move_storage, COALESCE(sp.after_move_storage, FALSE) AS after_move_storage,
	sp.commit_ready_at, sp.task_id_commit_msg,
	COALESCE(sp.after_commit_msg, FALSE) AS after_commit_msg,
	COALESCE(sp.after_commit_msg_success, FALSE) AS after_commit_msg_success,
	COALESCE(sd.owner_id > 0, FALSE) AS started_sdr,
	COALESCE(td.owner_id > 0, FALSE) AS started_tree_d,
	COALESCE(tc.owner_id > 0, FALSE) AS started_tree_rc,
	COALESCE(sy.owner_id > 0, FALSE) AS started_synthetic,
	COALESCE(pc.owner_id > 0, FALSE) AS started_precommit_msg,
	COALESCE(pr.owner_id > 0, FALSE) AS started_porep,
	COALESCE(fi.owner_id > 0, FALSE) AS started_finalize,
	COALESCE(mo.owner_id > 0, FALSE) AS started_move_storage,
	COALESCE(cm.owner_id > 0, FALSE) AS started_commit_msg,
	ARRAY(SELECT task FROM (VALUES
		(sp.task_id_sdr, sd.id), (sp.task_id_tree_d, td.id),
		(sp.task_id_tree_c, tc.id), (sp.task_id_tree_r, tr.id),
		(sp.task_id_synth, sy.id), (sp.task_id_precommit_msg, pc.id),
		(sp.task_id_porep, pr.id), (sp.task_id_finalize, fi.id),
		(sp.task_id_move_storage, mo.id), (sp.task_id_commit_msg, cm.id)
	) AS t(task, live) WHERE task IS NOT NULL AND live IS NULL) AS missing_tasks
FROM totals
LEFT JOIN page ON TRUE
LEFT JOIN sectors_sdr_pipeline sp ON sp.sp_id = page.sp_id AND sp.sector_number = page.sector_number
LEFT JOIN harmony_task sd ON sd.id = sp.task_id_sdr
LEFT JOIN harmony_task td ON td.id = sp.task_id_tree_d
LEFT JOIN harmony_task tc ON tc.id = sp.task_id_tree_c
LEFT JOIN harmony_task tr ON tr.id = sp.task_id_tree_r
LEFT JOIN harmony_task sy ON sy.id = sp.task_id_synth
LEFT JOIN harmony_task pc ON pc.id = sp.task_id_precommit_msg
LEFT JOIN harmony_task pr ON pr.id = sp.task_id_porep
LEFT JOIN harmony_task fi ON fi.id = sp.task_id_finalize
LEFT JOIN harmony_task mo ON mo.id = sp.task_id_move_storage
LEFT JOIN harmony_task cm ON cm.id = sp.task_id_commit_msg
ORDER BY page.priority, page.sp_id, page.sector_number`

// PipelinePorepPage is additive: the legacy list/detail API retains its chain
// results and response shape. Ownership here does not establish native Do entry.
func (a *PoRep) PipelinePorepPage(ctx context.Context, req PoRepPageRequest) (*PoRepPage, error) {
	return loadPoRepPage(ctx, req, func(ctx context.Context, out interface{}, _ string, args ...interface{}) error {
		return a.Deps.DB.Select(ctx, out, porepPageQuery, args...)
	})
}

func loadPoRepPage(ctx context.Context, req PoRepPageRequest, selectRows func(context.Context, interface{}, string, ...interface{}) error) (*PoRepPage, error) {
	if req.Offset < 0 {
		return nil, fmt.Errorf("negative PoRep page offset")
	}
	ctx, cancel := context.WithTimeout(ctx, POREP_PAGE_TIMEOUT)
	defer cancel()
	var rows []porepPageRow
	if err := selectRows(ctx, &rows, porepPageQuery, req.HidePendingSDR, POREP_PAGE_SIZE, req.Offset); err != nil {
		return nil, fmt.Errorf("load PoRep page: %w", err)
	}
	if len(rows) == 0 || len(rows) > POREP_PAGE_SIZE {
		return nil, fmt.Errorf("unexpected PoRep snapshot row count: %d", len(rows))
	}
	first := rows[0]
	result := &PoRepPage{
		Sectors: make([]PoRepPageEntry, 0, len(rows)),
		Total:   first.Total, Matching: first.Matching,
		WaitingForPrecommit: first.WaitingForPrecommit, WaitingForCommit: first.WaitingForCommit,
		ObservedAt: first.ObservedAt, Offset: req.Offset, Limit: POREP_PAGE_SIZE,
		HidePendingSDR: req.HidePendingSDR,
	}
	for _, row := range rows {
		if row.PageSpID == nil {
			continue // Empty/out-of-range page still returns the totals row.
		}
		addr, err := address.NewIDAddress(uint64(row.SpID))
		if err != nil {
			return nil, fmt.Errorf("PoRep page address: %w", err)
		}
		result.Sectors = append(result.Sectors, PoRepPageEntry{PipelineTask: row.PipelineTask, Address: addr.String()})
	}
	return result, nil
}
