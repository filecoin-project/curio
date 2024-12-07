package webrpc

import (
	"context"

	"golang.org/x/xerrors"
)

type PipelineStage struct {
	Name    string
	Pending int64
	Running int64
}

type PipelineStats struct {
	// Total pipeline count
	Total int64

	Stages []PipelineStage
}

func (a *WebRPC) PipelineStatsMarket(ctx context.Context) (*PipelineStats, error) {
	var out PipelineStats

	// Count total active pipelines
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk12_deal_pipeline WHERE complete = false`).Scan(&out.Total)
	if err != nil {
		return nil, xerrors.Errorf("failed to count pipelines: %w", err)
	}

	// We'll use a CTE to gather all necessary data in one query.
	// We assume `after_psd` and `after_find_deal` columns exist as indicated by the schema comments.
	// Adjust field names if different in actual schema.
	const query = `
WITH pipeline_data AS (
    SELECT dp.uuid,
           dp.complete,
           dp.commp_task_id,
           dp.psd_task_id,
           dp.find_deal_task_id,
           dp.indexing_task_id,
           dp.sector,
           dp.after_commp,
           dp.after_psd,
           dp.after_find_deal,
           pp.task_id AS downloading_task_id
    FROM market_mk12_deal_pipeline dp
             LEFT JOIN parked_pieces pp ON pp.piece_cid = dp.piece_cid
    WHERE dp.complete = false
),
     joined AS (
         SELECT p.*,
                dt.owner_id AS downloading_owner,
                ct.owner_id AS commp_owner,
                pt.owner_id AS psd_owner,
                ft.owner_id AS find_deal_owner,
                it.owner_id AS index_owner
         FROM pipeline_data p
                  LEFT JOIN harmony_task dt ON dt.id = p.downloading_task_id
                  LEFT JOIN harmony_task ct ON ct.id = p.commp_task_id
                  LEFT JOIN harmony_task pt ON pt.id = p.psd_task_id
                  LEFT JOIN harmony_task ft ON ft.id = p.find_deal_task_id
                  LEFT JOIN harmony_task it ON it.id = p.indexing_task_id
     )
SELECT
    -- Downloading stage
    COUNT(*) FILTER (WHERE downloading_task_id IS NOT NULL AND downloading_owner IS NULL) AS downloading_pending,
    COUNT(*) FILTER (WHERE downloading_task_id IS NOT NULL AND downloading_owner IS NOT NULL) AS downloading_running,

    -- Verify stage (commp)
    COUNT(*) FILTER (WHERE commp_task_id IS NOT NULL AND commp_owner IS NULL) AS verify_pending,
    COUNT(*) FILTER (WHERE commp_task_id IS NOT NULL AND commp_owner IS NOT NULL) AS verify_running,

    -- Publish stage
    -- pending if PSD required and not done
    COUNT(*) FILTER (
        WHERE after_find_deal = false
            AND psd_task_id IS NOT NULL
            AND after_psd = false
        ) AS publish_pending,
    -- running for all other cases in publish stage
    COUNT(*) FILTER (
        WHERE after_find_deal = false AND after_commp = true
            AND NOT (psd_task_id IS NOT NULL AND after_psd = false)
        ) AS publish_running,

    -- Seal stage
    -- Only after find_deal is done we consider seal stage
    COUNT(*) FILTER (
        WHERE after_find_deal = true
            AND sector IS NULL
        ) AS seal_pending,
    COUNT(*) FILTER (
        WHERE after_find_deal = true
            AND sector IS NOT NULL
        ) AS seal_running,

    -- Index stage
    COUNT(*) FILTER (WHERE indexing_task_id IS NOT NULL AND index_owner IS NULL) AS index_pending,
    COUNT(*) FILTER (WHERE indexing_task_id IS NOT NULL AND index_owner IS NOT NULL) AS index_running
FROM joined
`

	var cts []struct {
		DownloadingPending int64 `db:"downloading_pending"`
		DownloadingRunning int64 `db:"downloading_running"`
		VerifyPending      int64 `db:"verify_pending"`
		VerifyRunning      int64 `db:"verify_running"`
		PublishPending     int64 `db:"publish_pending"`
		PublishRunning     int64 `db:"publish_running"`
		SealPending        int64 `db:"seal_pending"`
		SealRunning        int64 `db:"seal_running"`
		IndexPending       int64 `db:"index_pending"`
		IndexRunning       int64 `db:"index_running"`
	}

	err = a.deps.DB.Select(ctx, &cts, query)
	if err != nil {
		return nil, xerrors.Errorf("failed to run pipeline stage query: %w", err)
	}

	counts := cts[0]

	out.Stages = []PipelineStage{
		{Name: "Downloading", Pending: counts.DownloadingPending, Running: counts.DownloadingRunning},
		{Name: "Verify", Pending: counts.VerifyPending, Running: counts.VerifyRunning},
		{Name: "Publish", Pending: counts.PublishPending, Running: counts.PublishRunning},
		{Name: "Seal", Pending: counts.SealPending, Running: counts.SealRunning},
		{Name: "Index", Pending: counts.IndexPending, Running: counts.IndexRunning},
	}

	return &out, nil
}

func (a *WebRPC) PipelineStatsSnap(ctx context.Context) (*PipelineStats, error) {
	var out PipelineStats

	const query = `
WITH pipeline_data AS (
    SELECT sp.*,
           et.owner_id AS encode_owner, 
           pt.owner_id AS prove_owner,
           st.owner_id AS submit_owner,
           mt.owner_id AS move_storage_owner
    FROM sectors_snap_pipeline sp
    LEFT JOIN harmony_task et ON et.id = sp.task_id_encode
    LEFT JOIN harmony_task pt ON pt.id = sp.task_id_prove
    LEFT JOIN harmony_task st ON st.id = sp.task_id_submit
    LEFT JOIN harmony_task mt ON mt.id = sp.task_id_move_storage
    WHERE after_move_storage = false
)
SELECT
    COUNT(*) AS total,

    -- Encode stage
    COUNT(*) FILTER (WHERE after_encode = false AND task_id_encode IS NOT NULL AND encode_owner IS NULL) AS encode_pending,
    COUNT(*) FILTER (WHERE after_encode = false AND task_id_encode IS NOT NULL AND encode_owner IS NOT NULL) AS encode_running,

    -- Prove stage
    COUNT(*) FILTER (WHERE after_encode = true AND after_prove = false AND task_id_prove IS NOT NULL AND prove_owner IS NULL) AS prove_pending,
    COUNT(*) FILTER (WHERE after_encode = true AND after_prove = false AND task_id_prove IS NOT NULL AND prove_owner IS NOT NULL) AS prove_running,

    -- Submit stage
    COUNT(*) FILTER (WHERE after_prove = true AND after_submit = false AND task_id_submit IS NOT NULL AND submit_owner IS NULL) AS submit_pending,
    COUNT(*) FILTER (WHERE after_prove = true AND after_submit = false AND task_id_submit IS NOT NULL AND submit_owner IS NOT NULL) AS submit_running,

    -- Move Storage stage
    COUNT(*) FILTER (WHERE after_submit = true AND after_move_storage = false AND task_id_move_storage IS NOT NULL AND move_storage_owner IS NULL) AS move_storage_pending,
    COUNT(*) FILTER (WHERE after_submit = true AND after_move_storage = false AND task_id_move_storage IS NOT NULL AND move_storage_owner IS NOT NULL) AS move_storage_running
FROM pipeline_data
`

	var cts []struct {
		Total int64 `db:"total"`

		EncodePending int64 `db:"encode_pending"`
		EncodeRunning int64 `db:"encode_running"`

		ProvePending int64 `db:"prove_pending"`
		ProveRunning int64 `db:"prove_running"`

		SubmitPending int64 `db:"submit_pending"`
		SubmitRunning int64 `db:"submit_running"`

		MoveStoragePending int64 `db:"move_storage_pending"`
		MoveStorageRunning int64 `db:"move_storage_running"`
	}

	err := a.deps.DB.Select(ctx, &cts, query)
	if err != nil {
		return nil, xerrors.Errorf("failed to run snap pipeline stage query: %w", err)
	}

	if len(cts) == 0 {
		// No active pipelines
		return &PipelineStats{
			Total: 0,
			Stages: []PipelineStage{
				{Name: "Encode", Pending: 0, Running: 0},
				{Name: "Prove", Pending: 0, Running: 0},
				{Name: "Submit", Pending: 0, Running: 0},
				{Name: "MoveStorage", Pending: 0, Running: 0},
			},
		}, nil
	}

	counts := cts[0]

	out.Total = counts.Total
	out.Stages = []PipelineStage{
		{Name: "Encode", Pending: counts.EncodePending, Running: counts.EncodeRunning},
		{Name: "Prove", Pending: counts.ProvePending, Running: counts.ProveRunning},
		{Name: "Submit", Pending: counts.SubmitPending, Running: counts.SubmitRunning},
		{Name: "MoveStorage", Pending: counts.MoveStoragePending, Running: counts.MoveStorageRunning},
	}

	return &out, nil
}

func (a *WebRPC) PipelineStatsSDR(ctx context.Context) (*PipelineStats, error) {
	var out PipelineStats

	// Get chain head for seed comparison
	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain head: %w", err)
	}

	const query = `
WITH pipeline_data AS (
    SELECT
        sp.*,
        sdrt.owner_id AS sdr_owner,
        tdt.owner_id AS tree_d_owner,
        tct.owner_id AS tree_c_owner,
        trt.owner_id AS tree_r_owner,
        pmt.owner_id AS precommit_msg_owner,
        pot.owner_id AS porep_owner,
        cmt.owner_id AS commit_msg_owner
    FROM sectors_sdr_pipeline sp
    LEFT JOIN harmony_task sdrt ON sdrt.id = sp.task_id_sdr
    LEFT JOIN harmony_task tdt ON tdt.id = sp.task_id_tree_d
    LEFT JOIN harmony_task tct ON tct.id = sp.task_id_tree_c
    LEFT JOIN harmony_task trt ON trt.id = sp.task_id_tree_r
    LEFT JOIN harmony_task pmt ON pmt.id = sp.task_id_precommit_msg
    LEFT JOIN harmony_task pot ON pot.id = sp.task_id_porep
    LEFT JOIN harmony_task cmt ON cmt.id = sp.task_id_commit_msg
),
stages AS (
    SELECT
        *,
        -- Determine stage membership booleans
        (after_sdr = false) AS at_sdr,
        (
          after_sdr = true
          AND (
            after_tree_d = false
            OR after_tree_c = false
            OR after_tree_r = false
          )
        ) AS at_trees,
        (after_tree_r = true AND after_precommit_msg = false) AS at_precommit_msg,
        (after_precommit_msg_success = true AND seed_epoch > $1) AS at_wait_seed,
        (after_porep = false AND after_precommit_msg_success = true AND seed_epoch < $1) AS at_porep,
        (after_commit_msg_success = false AND after_porep = true) AS at_commit_msg,
        (after_commit_msg_success = true) AS at_done,
        (failed = true) AS at_failed
    FROM pipeline_data
)
SELECT
    -- Total active pipelines: those not done and not failed
    COUNT(*) FILTER (WHERE NOT at_done AND NOT at_failed) AS total,

    -- SDR stage pending/running
    COUNT(*) FILTER (WHERE at_sdr AND task_id_sdr IS NOT NULL AND sdr_owner IS NULL) AS sdr_pending,
    COUNT(*) FILTER (WHERE at_sdr AND task_id_sdr IS NOT NULL AND sdr_owner IS NOT NULL) AS sdr_running,

    -- Trees stage pending/running
    -- A pipeline at the trees stage may have up to three tasks.
    -- Pending if ANY tree task that is not completed is present with no owner
    -- Running if ANY tree task that is not completed is present with owner
    COUNT(*) FILTER (
        WHERE at_trees
          AND (
              (task_id_tree_d IS NOT NULL AND tree_d_owner IS NULL AND after_tree_d = false)
           OR (task_id_tree_c IS NOT NULL AND tree_c_owner IS NULL AND after_tree_c = false)
           OR (task_id_tree_r IS NOT NULL AND tree_r_owner IS NULL AND after_tree_r = false)
          )
    ) AS trees_pending,
    COUNT(*) FILTER (
        WHERE at_trees
          AND (
              (task_id_tree_d IS NOT NULL AND tree_d_owner IS NOT NULL AND after_tree_d = false)
           OR (task_id_tree_c IS NOT NULL AND tree_c_owner IS NOT NULL AND after_tree_c = false)
           OR (task_id_tree_r IS NOT NULL AND tree_r_owner IS NOT NULL AND after_tree_r = false)
          )
    ) AS trees_running,

    -- PrecommitMsg stage
    COUNT(*) FILTER (WHERE at_precommit_msg AND task_id_precommit_msg IS NOT NULL AND precommit_msg_owner IS NULL) AS precommit_msg_pending,
    COUNT(*) FILTER (WHERE at_precommit_msg AND task_id_precommit_msg IS NOT NULL AND precommit_msg_owner IS NOT NULL) AS precommit_msg_running,

    -- WaitSeed stage (no tasks)
    0 AS wait_seed_pending,
    0 AS wait_seed_running,

    -- PoRep stage
    COUNT(*) FILTER (WHERE at_porep AND task_id_porep IS NOT NULL AND porep_owner IS NULL) AS porep_pending,
    COUNT(*) FILTER (WHERE at_porep AND task_id_porep IS NOT NULL AND porep_owner IS NOT NULL) AS porep_running,

    -- CommitMsg stage
    COUNT(*) FILTER (WHERE at_commit_msg AND task_id_commit_msg IS NOT NULL AND commit_msg_owner IS NULL) AS commit_msg_pending,
    COUNT(*) FILTER (WHERE at_commit_msg AND task_id_commit_msg IS NOT NULL AND commit_msg_owner IS NOT NULL) AS commit_msg_running

FROM stages
`

	var cts []struct {
		Total int64 `db:"total"`

		SDRPending          int64 `db:"sdr_pending"`
		SDRRunning          int64 `db:"sdr_running"`
		TreesPending        int64 `db:"trees_pending"`
		TreesRunning        int64 `db:"trees_running"`
		PrecommitMsgPending int64 `db:"precommit_msg_pending"`
		PrecommitMsgRunning int64 `db:"precommit_msg_running"`
		WaitSeedPending     int64 `db:"wait_seed_pending"`
		WaitSeedRunning     int64 `db:"wait_seed_running"`
		PoRepPending        int64 `db:"porep_pending"`
		PoRepRunning        int64 `db:"porep_running"`
		CommitMsgPending    int64 `db:"commit_msg_pending"`
		CommitMsgRunning    int64 `db:"commit_msg_running"`
	}

	err = a.deps.DB.Select(ctx, &cts, query, head.Height())
	if err != nil {
		return nil, xerrors.Errorf("failed to run sdr pipeline stage query: %w", err)
	}

	if len(cts) == 0 {
		// No active pipelines
		return &PipelineStats{
			Total: 0,
			Stages: []PipelineStage{
				{Name: "SDR", Pending: 0, Running: 0},
				{Name: "Trees", Pending: 0, Running: 0},
				{Name: "PrecommitMsg", Pending: 0, Running: 0},
				{Name: "WaitSeed", Pending: 0, Running: 0},
				{Name: "PoRep", Pending: 0, Running: 0},
				{Name: "CommitMsg", Pending: 0, Running: 0},
				{Name: "Done", Pending: 0, Running: 0},
				{Name: "Failed", Pending: 0, Running: 0},
			},
		}, nil
	}

	counts := cts[0]

	out.Total = counts.Total
	out.Stages = []PipelineStage{
		{Name: "SDR", Pending: counts.SDRPending, Running: counts.SDRRunning},
		{Name: "Trees", Pending: counts.TreesPending, Running: counts.TreesRunning},
		{Name: "PrecommitMsg", Pending: counts.PrecommitMsgPending, Running: counts.PrecommitMsgRunning},
		{Name: "WaitSeed", Pending: counts.WaitSeedPending, Running: counts.WaitSeedRunning},
		{Name: "PoRep", Pending: counts.PoRepPending, Running: counts.PoRepRunning},
		{Name: "CommitMsg", Pending: counts.CommitMsgPending, Running: counts.CommitMsgRunning},
	}

	return &out, nil
}
