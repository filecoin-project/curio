package seal

import (
	"context"
	"database/sql"
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
	if !s.pollers[pollerPrecommitMsg].IsSet() {
		return
	}

	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		log.Errorf("error getting chain head: %s", err)
		return
	}

	slackEpochs := SlackEpochs(s.cfg.preCommit.Slack, build.BlockDelaySecs)
	timeout := s.cfg.preCommit.Timeout
	maxBatch := s.cfg.preCommit.MaxPreCommitBatch
	currentHeight := int64(ts.Height())

	s.pollers[pollerPrecommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		var rows []BatchRow
		err := tx.Select(&rows, `
			WITH initial AS (
				SELECT
					p.sp_id,
					p.sector_number,
					COALESCE(
						(SELECT MIN(LEAST(s.f05_deal_start_epoch, s.direct_start_epoch))
						 FROM sectors_sdr_initial_pieces s
						 WHERE s.sp_id = p.sp_id AND s.sector_number = p.sector_number),
						p.ticket_epoch + $1
					) AS start_epoch,
					COALESCE(p.precommit_ready_at, '1970-01-01 00:00:00+00'::TIMESTAMPTZ) AS ready_at,
					p.reg_seal_proof
				FROM sectors_sdr_pipeline p
				WHERE p.after_synth = TRUE
					AND p.task_id_precommit_msg IS NULL
					AND p.after_precommit_msg = FALSE
			),
			numbered AS (
				SELECT l.*,
					ROW_NUMBER() OVER (
						PARTITION BY l.sp_id, l.reg_seal_proof
						ORDER BY l.start_epoch
					) AS rn
				FROM initial l
			)
			SELECT
				sp_id,
				sector_number,
				start_epoch,
				ready_at,
				reg_seal_proof,
				FLOOR((rn - 1)::NUMERIC / $2)::BIGINT AS batch_index
			FROM numbered
			ORDER BY sp_id, reg_seal_proof, batch_index, start_epoch`,
			policy.MaxPreCommitRandomnessLookback,
			s.cfg.preCommit.MaxPreCommitBatch)
		if err != nil {
			return false, xerrors.Errorf("getting precommit batch candidates: %w", err)
		}

		if len(rows) == 0 {
			return false, nil
		}

		now := time.Now()
		for _, batch := range GroupBatchRows(rows) {
			result := EvalBatchTimeout(batch.EarliestReady, timeout, batch.MinStartEpoch, slackEpochs, currentHeight, now, len(batch.SectorNums), maxBatch)
			if !result.ShouldFire {
				continue
			}

			n, err := tx.Exec(`
				UPDATE sectors_sdr_pipeline
				SET task_id_precommit_msg = $1
				WHERE sp_id = $2
					AND reg_seal_proof = $3
					AND sector_number = ANY($4::bigint[])
					AND after_synth = TRUE
					AND task_id_precommit_msg IS NULL
					AND after_precommit_msg = FALSE`,
				id, batch.SpID, batch.RegSealProof, batch.SectorNums)
			if err != nil {
				return false, xerrors.Errorf("assigning precommit batch: %w", err)
			}

			if n > 0 {
				log.Infow("Assigned precommit batch", "task_id", id, "sectors", n, "reason", result.Reason)
				return true, nil
			}
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
