package webrpc

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

// FailedSectorGroup is a summary row for the grouped view
type FailedSectorGroup struct {
	Status       string `db:"status" json:"Status"`             // "terminal" or "stuck"
	LastStage    string `db:"last_stage" json:"LastStage"`       // e.g. "MoveStorage", "CommitMsg"
	FailedReason string `db:"failed_reason" json:"FailedReason"` // e.g. "precommit-check" or "" for stuck
	Count        int64  `db:"count" json:"Count"`
}

// FailedSectorDetail is an individual sector row within a group
type FailedSectorDetail struct {
	SpID            int64    `db:"sp_id" json:"SpID"`
	SectorNumber    int64    `db:"sector_number" json:"SectorNumber"`
	CreateTime      time.Time `db:"create_time" json:"CreateTime"`
	FailedAt        NullTime `db:"failed_at" json:"FailedAt"`
	FailedReasonMsg string   `db:"failed_reason_msg" json:"FailedReasonMsg"`
	MissingTasks    []int64  `db:"missing_tasks" json:"MissingTasks"`
}

// stageExpr is the SQL CASE expression for computing the last completed pipeline stage.
// Shared between summary and detail queries.
const stageExpr = `
CASE
	WHEN after_commit_msg_success THEN 'CommitMsgSuccess'
	WHEN after_commit_msg THEN 'CommitMsg'
	WHEN after_move_storage THEN 'MoveStorage'
	WHEN after_finalize THEN 'Finalize'
	WHEN after_porep THEN 'PoRep'
	WHEN after_precommit_msg_success THEN 'PrecommitMsgSuccess'
	WHEN after_precommit_msg THEN 'PrecommitMsg'
	WHEN after_synth THEN 'Synth'
	WHEN after_tree_r THEN 'TreeR'
	WHEN after_tree_c THEN 'TreeC'
	WHEN after_tree_d THEN 'TreeD'
	WHEN after_sdr THEN 'SDR'
	ELSE 'New'
END`

// hasMissingTaskExpr checks if a pipeline row has any dangling task references
const hasMissingTaskExpr = `
EXISTS (
	SELECT 1
	FROM UNNEST(ARRAY[
		sp.task_id_sdr, sp.task_id_tree_d, sp.task_id_tree_c,
		sp.task_id_tree_r, sp.task_id_synth, sp.task_id_precommit_msg,
		sp.task_id_porep, sp.task_id_finalize, sp.task_id_move_storage,
		sp.task_id_commit_msg
	]) AS task_id
	LEFT JOIN harmony_task ht ON ht.id = task_id
	WHERE task_id IS NOT NULL AND ht.id IS NULL
)`

// missingTasksSubquery returns the array of dangling task IDs for a pipeline row
const missingTasksSubquery = `
(
	SELECT COALESCE(array_agg(task_id), ARRAY[]::bigint[])
	FROM (
		SELECT task_id
		FROM UNNEST(ARRAY[
			sp.task_id_sdr, sp.task_id_tree_d, sp.task_id_tree_c,
			sp.task_id_tree_r, sp.task_id_synth, sp.task_id_precommit_msg,
			sp.task_id_porep, sp.task_id_finalize, sp.task_id_move_storage,
			sp.task_id_commit_msg
		]) AS task_id
		LEFT JOIN harmony_task ht ON ht.id = task_id
		WHERE task_id IS NOT NULL AND ht.id IS NULL
	) AS missing
)`

// PipelineFailedSectors returns grouped summary of failed and stuck sectors
func (a *WebRPC) PipelineFailedSectors(ctx context.Context) ([]FailedSectorGroup, error) {
	var result []FailedSectorGroup

	err := a.deps.DB.Select(ctx, &result, `
		WITH problem_sectors AS (
			SELECT
				sp.sp_id,
				sp.sector_number,
				sp.failed,
				sp.failed_reason,
				`+stageExpr+` AS last_stage
			FROM sectors_sdr_pipeline sp
			WHERE sp.failed = true OR `+hasMissingTaskExpr+`
		)
		SELECT
			CASE WHEN failed THEN 'terminal' ELSE 'stuck' END AS status,
			last_stage,
			COALESCE(failed_reason, '') AS failed_reason,
			COUNT(*) AS count
		FROM problem_sectors
		GROUP BY status, last_stage, failed_reason
		ORDER BY
			CASE WHEN failed THEN 0 ELSE 1 END,
			count DESC`)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch failed sector groups: %w", err)
	}

	return result, nil
}

// PipelineFailedSectorDetails returns individual sectors for a specific group
func (a *WebRPC) PipelineFailedSectorDetails(ctx context.Context, status string, lastStage string, failedReason string, offset int, limit int) ([]FailedSectorDetail, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	isTerminal := status == "terminal"

	var result []FailedSectorDetail

	err := a.deps.DB.Select(ctx, &result, `
		SELECT
			sp.sp_id,
			sp.sector_number,
			sp.create_time,
			sp.failed_at,
			sp.failed_reason_msg,
			`+missingTasksSubquery+` AS missing_tasks
		FROM sectors_sdr_pipeline sp
		WHERE
			sp.failed = $1
			AND (`+stageExpr+`) = $2
			AND COALESCE(sp.failed_reason, '') = $3
			AND (sp.failed = true OR `+hasMissingTaskExpr+`)
		ORDER BY sp.sp_id, sp.sector_number
		LIMIT $4 OFFSET $5`,
		isTerminal, lastStage, failedReason, limit, offset)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch failed sector details: %w", err)
	}

	return result, nil
}
