package seal

import (
	"context"
	"sort"
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

func (s *SealPoller) pollStartPrecommitMsg(ctx context.Context, task pollTask) {
	if task.TaskPrecommitMsg == nil && !task.AfterPrecommitMsg && task.afterSynth() && s.pollers[pollerPrecommitMsg].IsSet() {
		s.pollers[pollerPrecommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_precommit_msg = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_precommit_msg IS NULL AND after_synth = TRUE`, id, task.SpID, task.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("update sectors_sdr_pipeline: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected to update 1 row, updated %d", n)
			}

			return true, nil
		})
	}
}

type sectorBatch struct {
	cutoff  abi.ChainEpoch
	sectors []int64
}

func (s *SealPoller) pollStartBatchPrecommitMsg(ctx context.Context, tasks []pollTask) {
	// Make batches based on Proof types
	var tsks []pollTask
	for i := range tasks {
		if tasks[i].TaskPrecommitMsg == nil && !tasks[i].AfterPrecommitMsg && tasks[i].afterSynth() && s.pollers[pollerPrecommitMsg].IsSet() {
			// If CC sector set StartEpoch/CutOff
			if tasks[i].StartEpoch == 0 {
				tasks[i].StartEpoch = abi.ChainEpoch(tasks[i].TicketEpoch) + policy.MaxPreCommitRandomnessLookback
			}
			tsks = append(tsks, tasks[i])
		}
	}

	// Sort in ascending order to allow maximum time for sectors to wait for base free drop
	sort.Slice(tsks, func(i, j int) bool {
		return tsks[i].StartEpoch < tsks[j].StartEpoch
	})

	batchMap := make(map[int64]map[abi.RegisteredSealProof][]pollTask)
	for i := range tsks {
		{
			v, ok := batchMap[tsks[i].SpID]
			if !ok {
				v = make(map[abi.RegisteredSealProof][]pollTask)
				v[tsks[i].RegisteredSealProof] = append(v[tsks[i].RegisteredSealProof], tsks[i])
			} else {
				v[tsks[i].RegisteredSealProof] = append(v[tsks[i].RegisteredSealProof], tsks[i])
				batchMap[tsks[i].SpID] = v
			}
		}
	}

	// Send batches per MinerID and per Proof type based on the following logic:
	// 1. Check if MaxWait for any sector is reaching, if yes then send full batch (might as well as message will cost us)
	// 2. If minimum batch size is not reached wait for next loop
	// 3. If max batch size reached then check if baseFee below set threshold. If yes then send
	// 4. Retry on next loop

	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		log.Errorf("error getting chain head: %s", err)
		return
	}

	for spid, miners := range batchMap {
		for _, pts := range miners {
			// Break into batches
			var batches []sectorBatch
			for i := 0; i < len(pts); i += s.cfg.preCommit.MaxPreCommitBatch {
				// Create a batch of size `maxBatchSize` or smaller for the last batch
				end := i + s.cfg.preCommit.MaxPreCommitBatch
				if end > len(pts) {
					end = len(pts)
				}
				var batch []int64
				var cutoff abi.ChainEpoch
				for _, pt := range pts[i:end] {
					batch = append(batch, pt.SectorNumber)
					if cutoff == 0 || pt.StartEpoch < cutoff {
						cutoff = pt.StartEpoch
					}
				}

				batches = append(batches, sectorBatch{
					cutoff:  cutoff,
					sectors: batch,
				})
			}

			// Process batch if cutoff has reached
			for i := range batches {
				if (time.Duration(batches[i].cutoff-ts.Height()) * time.Duration(build.BlockDelaySecs) * time.Second) < s.cfg.preCommit.PreCommitBatchSlack {
					s.pollers[pollerPrecommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
						n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_precommit_msg = $1 WHERE sp_id = $2 AND sector_number = ANY($3) AND task_id_precommit_msg IS NULL AND after_synth = TRUE`, id, spid, batches[i])
						if err != nil {
							return false, xerrors.Errorf("update sectors_sdr_pipeline: %w", err)
						}
						if n != 1 {
							return false, xerrors.Errorf("expected to update 1 row, updated %d", n)
						}

						return true, nil
					})
				}
			}

			// If we have enough sectors then check if base fee is low enough for us to send batched
			if len(pts) >= s.cfg.preCommit.MaxPreCommitBatch {
				if !s.cfg.commit.BaseFeeThreshold.Equals(abi.NewTokenAmount(0)) && ts.MinTicketBlock().ParentBaseFee.LessThan(s.cfg.preCommit.BaseFeeThreshold) {
					batches := batches[:len(batches)-1]
					for i := range batches {
						s.pollers[pollerPrecommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
							n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_precommit_msg = $1 WHERE sp_id = $2 AND sector_number = ANY($3) AND task_id_precommit_msg IS NULL AND after_synth = TRUE`, id, spid, batches[i])
							if err != nil {
								return false, xerrors.Errorf("update sectors_sdr_pipeline: %w", err)
							}
							if n != 1 {
								return false, xerrors.Errorf("expected to update 1 row, updated %d", n)
							}

							return true, nil
						})
					}
				}
			}
		}
	}
}

type dbExecResult struct {
	PrecommitMsgCID *string `db:"precommit_msg_cid"`
	CommitMsgCID    *string `db:"commit_msg_cid"`

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
		return xerrors.Errorf("precommit message failed with exit code %s", exitcode.ExitCode(execResult.ExecutedRcptExitCode))
	}
}

func (s *SealPoller) pollRetryPrecommitMsgSend(ctx context.Context, task pollTask, execResult dbExecResult) error {
	if execResult.PrecommitMsgCID == nil {
		return xerrors.Errorf("precommit msg cid was nil")
	}

	// make the pipeline entry seem like precommit send didn't happen, next poll loop will retry

	_, err := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET
                                precommit_msg_cid = NULL, task_id_precommit_msg = NULL, after_precommit_msg = FALSE
                            	WHERE precommit_msg_cid = $1 AND sp_id = $2 AND sector_number = $3 AND after_precommit_msg_success = FALSE`,
		*execResult.PrecommitMsgCID, task.SpID, task.SectorNumber)
	if err != nil {
		return xerrors.Errorf("update sectors_sdr_pipeline to retry precommit msg send: %w", err)
	}

	return nil
}
