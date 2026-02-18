package webrpc

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

type FailedSectorDetail struct {
	SpID         int64    `db:"sp_id" json:"SpID"`
	SectorNumber int64    `db:"sector_number" json:"SectorNumber"`
	CreateTime   time.Time `db:"create_time" json:"CreateTime"`

	// Terminal failure info (failed = true)
	Failed          bool     `db:"failed" json:"Failed"`
	FailedAt        NullTime `db:"failed_at" json:"FailedAt"`
	FailedReason    string   `db:"failed_reason" json:"FailedReason"`
	FailedReasonMsg string   `db:"failed_reason_msg" json:"FailedReasonMsg"`

	// Pipeline stage info - which stage was reached
	AfterSDR                 bool `db:"after_sdr" json:"AfterSDR"`
	AfterTreeD               bool `db:"after_tree_d" json:"AfterTreeD"`
	AfterTreeC               bool `db:"after_tree_c" json:"AfterTreeC"`
	AfterTreeR               bool `db:"after_tree_r" json:"AfterTreeR"`
	AfterSynth               bool `db:"after_synth" json:"AfterSynth"`
	AfterPrecommitMsg        bool `db:"after_precommit_msg" json:"AfterPrecommitMsg"`
	AfterPrecommitMsgSuccess bool `db:"after_precommit_msg_success" json:"AfterPrecommitMsgSuccess"`
	AfterPorep               bool `db:"after_porep" json:"AfterPorep"`
	AfterFinalize            bool `db:"after_finalize" json:"AfterFinalize"`
	AfterMoveStorage         bool `db:"after_move_storage" json:"AfterMoveStorage"`
	AfterCommitMsg           bool `db:"after_commit_msg" json:"AfterCommitMsg"`
	AfterCommitMsgSuccess    bool `db:"after_commit_msg_success" json:"AfterCommitMsgSuccess"`

	// Missing/stuck task info
	MissingTasks []int64 `db:"missing_tasks" json:"MissingTasks"`
}

func (a *WebRPC) PipelineFailedSectors(ctx context.Context) ([]FailedSectorDetail, error) {
	var result []FailedSectorDetail

	err := a.deps.DB.Select(ctx, &result, `
		SELECT
			sp.sp_id,
			sp.sector_number,
			sp.create_time,
			sp.failed,
			sp.failed_at,
			sp.failed_reason,
			sp.failed_reason_msg,
			sp.after_sdr,
			sp.after_tree_d,
			sp.after_tree_c,
			sp.after_tree_r,
			sp.after_synth,
			sp.after_precommit_msg,
			sp.after_precommit_msg_success,
			sp.after_porep,
			sp.after_finalize,
			sp.after_move_storage,
			sp.after_commit_msg,
			sp.after_commit_msg_success,
			(
				SELECT COALESCE(array_agg(task_id), ARRAY[]::bigint[])
				FROM (
					SELECT task_id
					FROM UNNEST(ARRAY[
						sp.task_id_sdr,
						sp.task_id_tree_d,
						sp.task_id_tree_c,
						sp.task_id_tree_r,
						sp.task_id_synth,
						sp.task_id_precommit_msg,
						sp.task_id_porep,
						sp.task_id_finalize,
						sp.task_id_move_storage,
						sp.task_id_commit_msg
					]) AS task_id
					LEFT JOIN harmony_task ht ON ht.id = task_id
					WHERE task_id IS NOT NULL AND ht.id IS NULL
				) AS missing
			) AS missing_tasks
		FROM sectors_sdr_pipeline sp
		WHERE
			-- Terminal failure
			sp.failed = true
			-- OR has task references pointing to tasks that no longer exist (stuck/crashed)
			OR EXISTS (
				SELECT 1
				FROM UNNEST(ARRAY[
					sp.task_id_sdr,
					sp.task_id_tree_d,
					sp.task_id_tree_c,
					sp.task_id_tree_r,
					sp.task_id_synth,
					sp.task_id_precommit_msg,
					sp.task_id_porep,
					sp.task_id_finalize,
					sp.task_id_move_storage,
					sp.task_id_commit_msg
				]) AS task_id
				LEFT JOIN harmony_task ht ON ht.id = task_id
				WHERE task_id IS NOT NULL AND ht.id IS NULL
			)
		ORDER BY
			sp.failed DESC,
			sp.failed_at DESC NULLS LAST,
			sp.sp_id,
			sp.sector_number
		LIMIT 100`)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch failed sectors: %w", err)
	}

	return result, nil
}
