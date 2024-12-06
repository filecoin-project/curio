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
