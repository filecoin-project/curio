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

type sectorBatch struct {
	cutoff   abi.ChainEpoch
	earliest time.Time
	sectors  []int64
}

func (s *SealPoller) pollStartBatchPrecommitMsg(ctx context.Context, tasks []pollTask) {
	// Make batches based on Proof types
	var tsks []pollTask
	for i := range tasks {
		if tasks[i].TaskPrecommitMsg == nil && !tasks[i].AfterPrecommitMsg && tasks[i].afterSynth() && s.pollers[pollerPrecommitMsg].IsSet() {
			// If CC sector set StartEpoch/CutOff
			if tasks[i].StartEpoch == 0 {
				tasks[i].StartEpoch = abi.ChainEpoch(*tasks[i].TicketEpoch) + policy.MaxPreCommitRandomnessLookback
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
				cutoff := abi.ChainEpoch(0)
				earliest := time.Now()
				for _, pt := range pts[i:end] {
					batch = append(batch, pt.SectorNumber)
					if cutoff == 0 || pt.StartEpoch < cutoff {
						cutoff = pt.StartEpoch
					}
					if pt.PreCommitReadyAt.Before(earliest) {
						earliest = *pt.PreCommitReadyAt
					}
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
				if (time.Duration(batch.cutoff-ts.Height()) * time.Duration(build.BlockDelaySecs) * time.Second) < s.cfg.preCommit.Slack {
					s.sendPreCommitBatch(ctx, spid, batch.sectors)
				}
				// Process batch if timeout has reached
				if batch.earliest.Add(s.cfg.preCommit.Timeout).After(time.Now()) {
					s.sendPreCommitBatch(ctx, spid, batch.sectors)
				}
				// Process batch if base fee is low enough for us to send
				if ts.MinTicketBlock().ParentBaseFee.LessThan(s.cfg.preCommit.BaseFeeThreshold) {
					s.sendPreCommitBatch(ctx, spid, batch.sectors)
				}
			}
		}
	}
}

func (s *SealPoller) sendPreCommitBatch(ctx context.Context, spid int64, sectors []int64) {
	s.pollers[pollerPrecommitMsg].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_precommit_msg = $1 WHERE sp_id = $2 AND sector_number = ANY($3) AND task_id_precommit_msg IS NULL AND after_synth = TRUE`, id, spid, sectors)
		if err != nil {
			return false, xerrors.Errorf("update sectors_sdr_pipeline: %w", err)
		}
		if n != len(sectors) {
			return false, xerrors.Errorf("expected to update 1 row, updated %d", n)
		}

		return true, nil
	})
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
