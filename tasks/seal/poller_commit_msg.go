package seal

import (
	"context"
	"math"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	actorstypes "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/exitcode"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"

	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

func (s *SealPoller) pollerAddStartEpoch(ctx context.Context, task pollTask) error {
	if task.AfterPrecommitMsgSuccess && !task.StartEpoch.Valid {
		ts, err := s.api.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("failed to get chain head: %w", err)
		}

		nv, err := s.api.StateNetworkVersion(ctx, ts.Key())
		if err != nil {
			return xerrors.Errorf("failed to get network version: %w", err)
		}

		av, err := actorstypes.VersionForNetwork(nv)
		if err != nil {
			return xerrors.Errorf("unsupported network version: %w", err)
		}

		maddr, err := address.NewIDAddress(uint64(task.SpID))
		if err != nil {
			return xerrors.Errorf("failed to create miner address: %w", err)
		}
		pci, err := s.api.StateSectorPreCommitInfo(ctx, maddr, abi.SectorNumber(task.SectorNumber), ts.Key())
		if err != nil {
			return xerrors.Errorf("failed to get precommit info: %w", err)
		}
		if pci == nil {
			return xerrors.Errorf("precommit info not found for sp %s and sector %d", maddr.String(), task.SectorNumber)
		}
		mpcd, err := policy.GetMaxProveCommitDuration(av, task.RegisteredSealProof)
		if err != nil {
			return xerrors.Errorf("failed to get max prove commit duration: %w", err)
		}
		startEpoch := pci.PreCommitEpoch + mpcd
		_, err = s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline p
										SET start_epoch = COALESCE(
											(SELECT MIN(LEAST(s.f05_deal_start_epoch, s.direct_start_epoch))
											 FROM sectors_sdr_initial_pieces s
											 WHERE s.sp_id = $2 
											   AND s.sector_number = $3
											), 
											$1
										)
										WHERE p.sp_id = $2
										  AND p.sector_number = $3
										  AND p.after_porep = TRUE 
										  AND p.after_commit_msg = FALSE 
										  AND p.start_epoch IS NULL`, startEpoch, task.SpID, task.SectorNumber)
		if err != nil {
			return xerrors.Errorf("failed to update start epoch: %w", err)
		}
		log.Debugw("updated start epoch", "sp", task.SpID, "sector", task.SectorNumber, "start_epoch", startEpoch)
	}

	return nil
}

func (s *SealPoller) pollStartBatchCommitMsg(ctx context.Context) {
	if !s.pollers[pollerCommitMsg].IsSet() {
		return
	}

	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		log.Errorf("error getting chain head: %s", err)
		return
	}

	slackEpoch := int64(math.Ceil(s.cfg.commit.Slack.Seconds() / float64(build.BlockDelaySecs)))
	feeOk := false
	if ts.MinTicketBlock().ParentBaseFee.LessThan(s.cfg.commit.BaseFeeThreshold) {
		feeOk = true
	}

	s.pollers[pollerCommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		var updatedCount int64
		var reason string

		log.Debugw("Trying to assign a commit batch",
			"slack_epoch", slackEpoch,
			"current_height", ts.Height(),
			"max_batch", s.cfg.commit.MaxCommitBatch,
			"new_task_id", id,
			"basefee_ok", feeOk,
			"timeout_secs", s.cfg.commit.Timeout.Seconds())

		err = tx.QueryRow(`SELECT updated_count, reason FROM poll_start_batch_commit_msg($1, $2, $3, $4, $5, $6)`,
			slackEpoch,                          // p_slack_epoch
			ts.Height(),                         // p_current_height
			s.cfg.commit.MaxCommitBatch,         // p_max_batch
			id,                                  // p_new_task_id
			feeOk,                               // p_basefee_ok
			int(s.cfg.commit.Timeout.Seconds()), // p_timeout_secs
		).Scan(&updatedCount, &reason)
		if err != nil {
			return false, err
		}

		if updatedCount > 0 {
			log.Debugf("Assigned %d sectors to commit batch with taskID %d with reason %s", updatedCount, id, reason)
			return true, nil
		} else {
			log.Debugf("No commit batch assigned for ID %d", id)
		}
		return false, nil
	})
}

func (s *SealPoller) pollCommitMsgLanded(ctx context.Context, task pollTask) error {
	if task.AfterCommitMsg && !task.AfterCommitMsgSuccess && s.pollers[pollerCommitMsg].IsSet() {

		var execResult []dbExecResult

		comm, err := s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			err = tx.Select(&execResult, `SELECT spipeline.precommit_msg_cid, spipeline.commit_msg_cid, executed_tsk_cid, executed_tsk_epoch, executed_msg_cid, executed_rcpt_exitcode, executed_rcpt_gas_used
					FROM sectors_sdr_pipeline spipeline
					JOIN message_waits ON spipeline.commit_msg_cid = message_waits.signed_message_cid
					WHERE sp_id = $1 AND sector_number = $2 AND executed_tsk_epoch IS NOT NULL`, task.SpID, task.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("failed to query message_waits: %w", err)
			}

			if len(execResult) > 0 {
				maddr, err := address.NewIDAddress(uint64(task.SpID))
				if err != nil {
					return false, xerrors.Errorf("failed to create miner address: %w", err)
				}

				if exitcode.ExitCode(execResult[0].ExecutedRcptExitCode) != exitcode.Ok {
					if err := s.pollCommitMsgFail(ctx, maddr, task, execResult[0]); err != nil {
						return false, xerrors.Errorf("failed to handle commit message failure: %w", err)
					}
				}

				si, err := s.api.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(task.SectorNumber), types.EmptyTSK)
				if err != nil {
					return false, xerrors.Errorf("get sector info: %w", err)
				}

				if si == nil {
					return false, xerrors.Errorf("todo handle missing sector info (not found after cron), sp %d, sector %d, exec_epoch %d, exec_tskcid %s, msg_cid %s", task.SpID, task.SectorNumber, execResult[0].ExecutedTskEpoch, execResult[0].ExecutedTskCID, execResult[0].ExecutedMsgCID)
				} else {
					// yay!

					_, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET
						after_commit_msg_success = TRUE, commit_msg_tsk = $1
						WHERE sp_id = $2 AND sector_number = $3 AND after_commit_msg_success = FALSE`,
						execResult[0].ExecutedTskCID, task.SpID, task.SectorNumber)
					if err != nil {
						return false, xerrors.Errorf("update sectors_sdr_pipeline: %w", err)
					}

					n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET sealed = TRUE WHERE sp_id = $1 AND sector = $2 AND sealed = FALSE`, task.SpID, task.SectorNumber)
					if err != nil {
						return false, xerrors.Errorf("update market_mk12_deal_pipeline: %w", err)
					}
					log.Debugw("marked deals as sealed", "sp", task.SpID, "sector", task.SectorNumber, "count", n)
					return true, nil
				}
			}
			return false, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return xerrors.Errorf("failed to commit transaction: %w", err)
		}
		if len(execResult) > 0 {
			if !comm {
				return xerrors.Errorf("failed to commit transaction")
			}
		}
	}

	return nil
}

func (s *SealPoller) pollCommitMsgFail(ctx context.Context, maddr address.Address, task pollTask, execResult dbExecResult) error {
	switch exitcode.ExitCode(execResult.ExecutedRcptExitCode) {
	case exitcode.SysErrInsufficientFunds, exitcode.ErrInsufficientFunds:
		fallthrough
	case exitcode.SysErrOutOfGas:
		// just retry
		return s.pollRetryCommitMsgSend(ctx, task, execResult)
	case exitcode.ErrNotFound:
		// message not found, but maybe it's fine?

		si, err := s.api.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(task.SectorNumber), types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("get sector info: %w", err)
		}
		if si != nil {
			return nil
		}

		return xerrors.Errorf("sector not found after, commit message can't be found either")
	default:
		return xerrors.Errorf("commit message (s %d:%d m:%s) failed with exit code %s", task.SpID, task.SectorNumber, execResult.CommitMsgCID.String, exitcode.ExitCode(execResult.ExecutedRcptExitCode))
	}
}

func (s *SealPoller) pollRetryCommitMsgSend(ctx context.Context, task pollTask, execResult dbExecResult) error {
	if !execResult.CommitMsgCID.Valid {
		return xerrors.Errorf("commit msg cid was nil")
	}

	// make the pipeline entry seem like precommit send didn't happen, next poll loop will retry

	_, err := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET
                                commit_msg_cid = NULL, task_id_commit_msg = NULL, after_commit_msg = FALSE
                            	WHERE commit_msg_cid = $1 AND sp_id = $2 AND sector_number = $3 AND after_commit_msg_success = FALSE`,
		execResult.CommitMsgCID.String, task.SpID, task.SectorNumber)
	if err != nil {
		return xerrors.Errorf("update sectors_sdr_pipeline to retry precommit msg send: %w", err)
	}

	return nil
}
