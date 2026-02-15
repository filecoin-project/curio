package webrpc

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

type FailedSectorDetail struct {
	SpID            int64     `db:"sp_id" json:"SpID"`
	SectorNumber    int64     `db:"sector_number" json:"SectorNumber"`
	CreateTime      time.Time `db:"create_time" json:"CreateTime"`
	FailedAt        NullTime  `db:"failed_at" json:"FailedAt"`
	FailedReason    string    `db:"failed_reason" json:"FailedReason"`
	FailedReasonMsg string    `db:"failed_reason_msg" json:"FailedReasonMsg"`

	// Pipeline stage info - which stage was reached
	AfterSDR                 bool `db:"after_sdr" json:"AfterSDR"`
	AfterTreeD               bool `db:"after_tree_d" json:"AfterTreeD"`
	AfterTreeC               bool `db:"after_tree_c" json:"AfterTreeC"`
	AfterTreeR               bool `db:"after_tree_r" json:"AfterTreeR"`
	AfterPrecommitMsg        bool `db:"after_precommit_msg" json:"AfterPrecommitMsg"`
	AfterPrecommitMsgSuccess bool `db:"after_precommit_msg_success" json:"AfterPrecommitMsgSuccess"`
	AfterPorep               bool `db:"after_porep" json:"AfterPorep"`
	AfterFinalize            bool `db:"after_finalize" json:"AfterFinalize"`
	AfterMoveStorage         bool `db:"after_move_storage" json:"AfterMoveStorage"`
	AfterCommitMsg           bool `db:"after_commit_msg" json:"AfterCommitMsg"`
	AfterCommitMsgSuccess    bool `db:"after_commit_msg_success" json:"AfterCommitMsgSuccess"`
}

func (a *WebRPC) PipelineFailedSectors(ctx context.Context) ([]FailedSectorDetail, error) {
	var result []FailedSectorDetail

	err := a.deps.DB.Select(ctx, &result, `
		SELECT sp_id, sector_number, create_time, failed_at, failed_reason, failed_reason_msg,
		       after_sdr, after_tree_d, after_tree_c, after_tree_r,
		       after_precommit_msg, after_precommit_msg_success,
		       after_porep, after_finalize, after_move_storage,
		       after_commit_msg, after_commit_msg_success
		FROM sectors_sdr_pipeline
		WHERE failed = true
		ORDER BY failed_at DESC NULLS LAST
		LIMIT 100`)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch failed sectors: %w", err)
	}

	return result, nil
}
