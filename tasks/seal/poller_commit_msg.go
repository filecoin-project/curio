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

	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

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
		// Check if SpID exists in batchMap
		v, ok := batchMap[tsks[i].SpID]
		if !ok {
			// If not, initialize a new map for the RegisteredSealProof
			v = make(map[abi.RegisteredSealProof][]pollTask)
			batchMap[tsks[i].SpID] = v
		}
		// Append the task to the correct RegisteredSealProof
		v[tsks[i].RegisteredSealProof] = append(v[tsks[i].RegisteredSealProof], tsks[i])
	}

	// Send batches per MinerID and per Proof type based on the following logic:
	// 1. Check if Slack for any sector is reaching, if yes then send full batch
	// 2. Check if timeout is reaching for any sector in the batch, if yes, then send the batch
	// 3. Check if baseFee below set threshold. If yes then send all batches

	for spid, sealProofMap := range batchMap {
		for _, pts := range sealProofMap {
			// Break into batches
			var batches []sectorBatch
			for i := 0; i < len(pts); i += s.cfg.commit.MaxCommitBatch {
				// Create a batch of size `maxBatchSize` or smaller for the last batch
				end := i + s.cfg.commit.MaxCommitBatch
				if end > len(pts) {
					end = len(pts)
				}
				var batch []int64
				cutoff := abi.ChainEpoch(0)
				earliest := time.Now()
				for _, pt := range pts[i:end] {

					if cutoff == 0 || pt.StartEpoch < cutoff {
						cutoff = pt.StartEpoch
					}

					if pt.CommitReadyAt.Before(earliest) {
						earliest = *pt.CommitReadyAt
					}

					batch = append(batch, pt.SectorNumber)
				}

				batches = append(batches, sectorBatch{
					cutoff:  cutoff,
					sectors: batch,
				})
			}

			for i := range batches {
				batch := batches[i]
				//sectors := batch.sectors
				// Process batch if slack has reached
				if (time.Duration(batch.cutoff-ts.Height()) * time.Duration(build.BlockDelaySecs) * time.Second) < s.cfg.commit.Slack {
					s.sendCommitBatch(ctx, spid, batch.sectors)
					continue
				}
				// Process batch if timeout has reached
				if batch.earliest.Add(s.cfg.commit.Timeout).After(time.Now()) {
					s.sendCommitBatch(ctx, spid, batch.sectors)
					continue
				}
				// Process batch if base fee is low enough for us to send
				if ts.MinTicketBlock().ParentBaseFee.LessThan(s.cfg.commit.BaseFeeThreshold) {
					s.sendCommitBatch(ctx, spid, batch.sectors)
					continue
				}
			}
		}
	}
}

func (s *SealPoller) sendCommitBatch(ctx context.Context, spid int64, sectors []int64) {
	s.pollers[pollerCommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_commit_msg = $1 WHERE sp_id = $2 AND sector_number = ANY($3) AND task_id_commit_msg IS NULL AND after_commit_msg = FALSE`, id, spid, sectors)
		if err != nil {
			return false, xerrors.Errorf("update sectors_sdr_pipeline: %w", err)
		}
		if n > len(sectors) {
			return false, xerrors.Errorf("expected to update at most %d rows, updated %d", len(sectors), n)
		}

		return true, nil
	})
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

				n, err := s.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET sealed = TRUE WHERE sp_id = $1 AND sector = $2 AND sealed = FALSE`, task.SpID, task.SectorNumber)
				if err != nil {
					return xerrors.Errorf("update market_mk12_deal_pipeline: %w", err)
				}
				log.Debugw("marked deals as sealed", "sp", task.SpID, "sector", task.SectorNumber, "count", n)
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
