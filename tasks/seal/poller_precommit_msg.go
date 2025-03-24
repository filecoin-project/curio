package seal

import (
	"context"
	"database/sql"
	"math"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/exitcode"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"

	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

func (s *SealPoller) pollStartBatchPrecommitMsg(ctx context.Context) {
	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		log.Errorf("error getting chain head: %s", err)
		return
	}

	slackEpoch := int64(math.Ceil(s.cfg.preCommit.Slack.Seconds() / float64(build.BlockDelaySecs)))
	feeOk := false
	if ts.MinTicketBlock().ParentBaseFee.LessThan(s.cfg.preCommit.BaseFeeThreshold) {
		feeOk = true
	}

	s.pollers[pollerPrecommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		var updatedCount int64
		var reason string

		log.Infow("Trying to assign a precommit batch",
			"slack_epoch", slackEpoch,
			"current_height", ts.Height(),
			"max_batch", s.cfg.preCommit.MaxPreCommitBatch,
			"new_task_id", id,
			"baseFee", ts.MinTicketBlock().ParentBaseFee.String(),
			"basefee_ok", feeOk,
			"timeout_secs", s.cfg.preCommit.Timeout.Seconds(),
			"timeout_at", time.Now().Add(s.cfg.preCommit.Timeout).UTC().Format(time.RFC3339),
			"randomness_lookback", policy.MaxPreCommitRandomnessLookback,
		)

		err = tx.QueryRow(`SELECT updated_count, reason FROM poll_start_batch_precommit_msg($1, $2, $3, $4, $5, $6, $7)`,
			policy.MaxPreCommitRandomnessLookback,  // p_randomnessLookBack BIGINT,  -- policy.MaxPreCommitRandomnessLookback
			slackEpoch,                             // p_slack_epoch        BIGINT,  -- "Slack" epoch to compare against a sector's start_epoch
			ts.Height(),                            // p_current_height     BIGINT,  -- Current on-chain height
			s.cfg.preCommit.MaxPreCommitBatch,      // p_max_batch          INT,     -- Max number of sectors per batch
			id,                                     // p_new_task_id        BIGINT,  -- Task ID to assign if a batch is chosen
			feeOk,                                  // p_basefee_ok 		   BOOLEAN, -- If TRUE, fee-based condition is considered met
			int(s.cfg.preCommit.Timeout.Seconds()), // p_timeout_secs  	   INT      -- Timeout in seconds for earliest_ready_at check
		).Scan(&updatedCount, &reason)
		if err != nil {
			return false, err
		}

		if updatedCount > 0 {
			log.Debugf("Assigned %d sectors to precommit batch with taskID %d with reason %s", updatedCount, id, reason)
			return true, nil
		} else {
			log.Debugf("No precommit batch assigned")
		}
		return false, nil
	})
}

type dbExecResult struct {
	PrecommitMsgCID sql.NullString `db:"precommit_msg_cid"`
	CommitMsgCID    sql.NullString `db:"commit_msg_cid"`

	ExecutedTskCID   string `db:"executed_tsk_cid"`
	ExecutedTskEpoch int64  `db:"executed_tsk_epoch"`
	ExecutedMsgCID   string `db:"executed_msg_cid"`

	ExecutedRcptExitCode int64 `db:"executed_rcpt_exitcode"`
	ExecutedRcptGasUsed  int64 `db:"executed_rcpt_gas_used"`
}

func (s *SealPoller) pollPrecommitMsgLanded(ctx context.Context, task pollTask) error {
	if task.AfterPrecommitMsg && !task.AfterPrecommitMsgSuccess {
		var execResult []dbExecResult

		err := s.db.Select(ctx, &execResult, `SELECT spipeline.precommit_msg_cid, spipeline.commit_msg_cid, executed_tsk_cid, executed_tsk_epoch, executed_msg_cid, executed_rcpt_exitcode, executed_rcpt_gas_used
					FROM sectors_sdr_pipeline spipeline
					JOIN message_waits ON spipeline.precommit_msg_cid = message_waits.signed_message_cid
					WHERE sp_id = $1 AND sector_number = $2 AND executed_tsk_epoch IS NOT NULL`, task.SpID, task.SectorNumber)
		if err != nil {
			log.Errorw("failed to query message_waits", "error", err)
		}

		if len(execResult) > 0 {
			if exitcode.ExitCode(execResult[0].ExecutedRcptExitCode) != exitcode.Ok {
				return s.pollPrecommitMsgFail(ctx, task, execResult[0])
			}

			maddr, err := address.NewIDAddress(uint64(task.SpID))
			if err != nil {
				return err
			}

			pci, err := s.api.StateSectorPreCommitInfo(ctx, maddr, abi.SectorNumber(task.SectorNumber), types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("get precommit info: %w", err)
			}

			if pci != nil {
				randHeight := pci.PreCommitEpoch + policy.GetPreCommitChallengeDelay()

				_, err := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET 
                                seed_epoch = $1, precommit_msg_tsk = $2, after_precommit_msg_success = TRUE 
                            WHERE sp_id = $3 AND sector_number = $4 AND seed_epoch IS NULL`,
					randHeight, execResult[0].ExecutedTskCID, task.SpID, task.SectorNumber)
				if err != nil {
					return xerrors.Errorf("update sectors_sdr_pipeline: %w", err)
				}
			} // todo handle missing precommit info (eg expired precommit)

		}
	}

	return nil
}

func (s *SealPoller) pollPrecommitMsgFail(ctx context.Context, task pollTask, execResult dbExecResult) error {
	switch exitcode.ExitCode(execResult.ExecutedRcptExitCode) {
	case exitcode.SysErrInsufficientFunds, exitcode.ErrInsufficientFunds:
		fallthrough
	case exitcode.SysErrOutOfGas:
		// just retry
		return s.pollRetryPrecommitMsgSend(ctx, task, execResult)
	default:
		return xerrors.Errorf("precommit message (s %d:%d m:%s) failed with exit code %s", task.SpID, task.SectorNumber, execResult.PrecommitMsgCID.String, exitcode.ExitCode(execResult.ExecutedRcptExitCode))
	}
}

func (s *SealPoller) pollRetryPrecommitMsgSend(ctx context.Context, task pollTask, execResult dbExecResult) error {
	if !execResult.PrecommitMsgCID.Valid {
		return xerrors.Errorf("precommit msg cid was nil")
	}

	// make the pipeline entry seem like precommit send didn't happen, next poll loop will retry

	_, err := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET
                                precommit_msg_cid = NULL, task_id_precommit_msg = NULL, after_precommit_msg = FALSE
                            	WHERE precommit_msg_cid = $1 AND sp_id = $2 AND sector_number = $3 AND after_precommit_msg_success = FALSE`,
		execResult.PrecommitMsgCID.String, task.SpID, task.SectorNumber)
	if err != nil {
		return xerrors.Errorf("update sectors_sdr_pipeline to retry precommit msg send: %w", err)
	}

	return nil
}
