package seal

import (
	"context"
	"sort"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	actorstypes "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/exitcode"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

func (s *SealPoller) pollStartCommitMsg(ctx context.Context, task pollTask) {
	if task.afterPoRep() && len(task.PoRepProof) > 0 && task.TaskCommitMsg == nil && !task.AfterCommitMsg && s.pollers[pollerCommitMsg].IsSet() {
		s.pollers[pollerCommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_commit_msg = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_commit_msg IS NULL`, id, task.SpID, task.SectorNumber)
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

func (s *SealPoller) pollStartBatchCommitMsg(ctx context.Context, tasks []pollTask) {
	// Make batches based on Proof types
	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		log.Errorf("error getting chain head: %s", err)
		return
	}

	nv, err := s.api.StateNetworkVersion(ctx, ts.Key())
	if err != nil {
		log.Errorf("getting network version: %s", err)
		return
	}

	av, err := actorstypes.VersionForNetwork(nv)
	if err != nil {
		log.Errorf("unsupported network version: %s", err)
		return
	}

	var tsks []pollTask

	for i := range tasks {
		if tasks[i].afterPoRep() && len(tasks[i].PoRepProof) > 0 && tasks[i].TaskCommitMsg == nil && !tasks[i].AfterCommitMsg && s.pollers[pollerCommitMsg].IsSet() {
			// If CC sector set StartEpoch/CutOff
			if tasks[i].StartEpoch == 0 {
				maddr, err := address.NewIDAddress(uint64(tasks[i].SpID))
				if err != nil {
					log.Errorf("error creating miner address: %s", err)
					return
				}

				pci, err := s.api.StateSectorPreCommitInfo(ctx, maddr, abi.SectorNumber(tasks[i].SectorNumber), ts.Key())
				if err != nil {
					log.Errorf("getting precommit info: %s", err)
					return
				}

				if pci == nil {
					log.Errorf("precommit info not found for sp %s and sector %d", maddr.String(), tasks[i].SectorNumber)
					return
				}

				mpcd, err := policy.GetMaxProveCommitDuration(av, tasks[i].RegisteredSealProof)
				if err != nil {
					log.Errorf("getting max prove commit duration: %s", err)
					return
				}

				tasks[i].StartEpoch = pci.PreCommitEpoch + mpcd
			}
			tsks = append(tsks, tasks[i])
		}
	}

	sort.Slice(tsks, func(i, j int) bool {
		return tsks[i].StartEpoch < tsks[j].StartEpoch
	})

	batchMap := make(map[int64]map[abi.RegisteredSealProof][]pollTask)
	for i := range tsks {
		v, ok := batchMap[tsks[i].SpID]
		if !ok {
			v = make(map[abi.RegisteredSealProof][]pollTask)
			batchMap[tsks[i].SpID] = v
		} else {
			v[tsks[i].RegisteredSealProof] = append(v[tsks[i].RegisteredSealProof], tsks[i])
			batchMap[tsks[i].SpID] = v
		}
	}

	// Send batches per MinerID and per Proof type based on the following logic:
	// 1. Check if MaxWait for any sector is reaching, if yes then send full batch (might as well as message will cost us)
	// 2. If minimum batch size is not reached wait for next loop
	// 3. If max batch size reached then check if baseFee below set threshold. If yes then send
	// 4. Retry on next loop

	for spid, miners := range batchMap {
		for _, pts := range miners {
			// Break into batches
			var batches []sectorBatch
			for i := 0; i < len(pts); i += s.cfg.commit.MaxCommitBatch {
				// Create a batch of size `maxBatchSize` or smaller for the last batch
				end := i + s.cfg.commit.MaxCommitBatch
				if end > len(pts) {
					end = len(pts)
				}
				var batch []int64
				var cutoff abi.ChainEpoch
				for _, pt := range pts[i:end] {

					if cutoff == 0 || pt.StartEpoch < cutoff {
						cutoff = pt.StartEpoch
					}

					batch = append(batch, pt.SectorNumber)
				}

				batches = append(batches, sectorBatch{
					cutoff:  cutoff,
					sectors: batch,
				})
			}

			// Process batch if cutoff has reached
			for i := range batches {
				if (time.Duration(batches[i].cutoff-ts.Height()) * time.Duration(build.BlockDelaySecs) * time.Second) < s.cfg.commit.CommitBatchSlack {
					// If not enough for a batch i.e. <  miner.MinAggregatedSectors (4)
					if len(batches[i].sectors) < miner.MinAggregatedSectors {
						for _, sector := range batches[i].sectors {
							s.pollers[pollerCommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
								n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_commit_msg = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_commit_msg IS NULL`, id, spid, sector)
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
					s.pollers[pollerCommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
						n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_commit_msg = $1 WHERE sp_id = $2 AND sector_number = ANY($3) AND task_id_commit_msg IS NULL`, id, spid, batches[i])
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
			if len(pts) >= s.cfg.commit.MaxCommitBatch {
				if !s.cfg.commit.BaseFeeThreshold.Equals(abi.NewTokenAmount(0)) && ts.MinTicketBlock().ParentBaseFee.LessThan(s.cfg.commit.BaseFeeThreshold) {
					for i := range batches {
						s.pollers[pollerCommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
							n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_commit_msg = $1 WHERE sp_id = $2 AND sector_number = ANY($3) AND task_id_commit_msg IS NULL`, id, spid, batches[i])
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

func (s *SealPoller) pollCommitMsgLanded(ctx context.Context, task pollTask) error {
	if task.AfterCommitMsg && !task.AfterCommitMsgSuccess && s.pollers[pollerCommitMsg].IsSet() {
		var execResult []dbExecResult

		err := s.db.Select(ctx, &execResult, `SELECT spipeline.precommit_msg_cid, spipeline.commit_msg_cid, executed_tsk_cid, executed_tsk_epoch, executed_msg_cid, executed_rcpt_exitcode, executed_rcpt_gas_used
					FROM sectors_sdr_pipeline spipeline
					JOIN message_waits ON spipeline.commit_msg_cid = message_waits.signed_message_cid
					WHERE sp_id = $1 AND sector_number = $2 AND executed_tsk_epoch IS NOT NULL`, task.SpID, task.SectorNumber)
		if err != nil {
			log.Errorw("failed to query message_waits", "error", err)
		}

		if len(execResult) > 0 {
			maddr, err := address.NewIDAddress(uint64(task.SpID))
			if err != nil {
				return err
			}

			if exitcode.ExitCode(execResult[0].ExecutedRcptExitCode) != exitcode.Ok {
				if err := s.pollCommitMsgFail(ctx, maddr, task, execResult[0]); err != nil {
					return err
				}
			}

			si, err := s.api.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(task.SectorNumber), types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("get sector info: %w", err)
			}

			if si == nil {
				log.Errorw("todo handle missing sector info (not found after cron)", "sp", task.SpID, "sector", task.SectorNumber, "exec_epoch", execResult[0].ExecutedTskEpoch, "exec_tskcid", execResult[0].ExecutedTskCID, "msg_cid", execResult[0].ExecutedMsgCID)
				// todo handdle missing sector info (not found after cron)
			} else {
				// yay!

				_, err := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET
						after_commit_msg_success = TRUE, commit_msg_tsk = $1
						WHERE sp_id = $2 AND sector_number = $3 AND after_commit_msg_success = FALSE`,
					execResult[0].ExecutedTskCID, task.SpID, task.SectorNumber)
				if err != nil {
					return xerrors.Errorf("update sectors_sdr_pipeline: %w", err)
				}
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
		return xerrors.Errorf("commit message failed with exit code %s", exitcode.ExitCode(execResult.ExecutedRcptExitCode))
	}
}

func (s *SealPoller) pollRetryCommitMsgSend(ctx context.Context, task pollTask, execResult dbExecResult) error {
	if execResult.CommitMsgCID == nil {
		return xerrors.Errorf("commit msg cid was nil")
	}

	// make the pipeline entry seem like precommit send didn't happen, next poll loop will retry

	_, err := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET
                                commit_msg_cid = NULL, task_id_commit_msg = NULL, after_commit_msg = FALSE
                            	WHERE commit_msg_cid = $1 AND sp_id = $2 AND sector_number = $3 AND after_commit_msg_success = FALSE`,
		*execResult.CommitMsgCID, task.SpID, task.SectorNumber)
	if err != nil {
		return xerrors.Errorf("update sectors_sdr_pipeline to retry precommit msg send: %w", err)
	}

	return nil
}
