package seal

import (
	"context"
	"database/sql"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	miner15 "github.com/filecoin-project/go-state-types/builtin/v15/miner"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/promise"

	apitypes "github.com/filecoin-project/lotus/api/types"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("cu/seal")

const (
	pollerSDR = iota
	pollerTreeD
	pollerTreeRC
	pollerSyntheticProofs
	pollerPrecommitMsg
	pollerPoRep
	pollerCommitMsg
	pollerFinalize
	pollerMoveStorage

	numPollers
)

const sealPollerInterval = 10 * time.Second
const seedEpochConfidence = 3

type SealPollerAPI interface {
	StateSectorPreCommitInfo(context.Context, address.Address, abi.SectorNumber, types.TipSetKey) (*miner.SectorPreCommitOnChainInfo, error)
	StateSectorGetInfo(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tsk types.TipSetKey) (*miner.SectorOnChainInfo, error)
	ChainHead(context.Context) (*types.TipSet, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (apitypes.NetworkVersion, error)
}

type preCommitBatchingConfig struct {
	MaxPreCommitBatch int
	Slack             time.Duration
	Timeout           time.Duration
}

type commitBatchingConfig struct {
	MinCommitBatch int
	MaxCommitBatch int
	Slack          time.Duration
	Timeout        time.Duration
}

type pollerConfig struct {
	preCommit preCommitBatchingConfig
	commit    commitBatchingConfig
}

type SealPoller struct {
	db  *harmonydb.DB
	api SealPollerAPI
	cfg pollerConfig

	pollers [numPollers]promise.Promise[harmonytask.AddTaskFunc]
}

func NewPoller(db *harmonydb.DB, api SealPollerAPI, cfg *config.CurioConfig) *SealPoller {
	c := pollerConfig{
		commit: commitBatchingConfig{
			MinCommitBatch: miner.MinAggregatedSectors,
			MaxCommitBatch: 256,
			Slack:          cfg.Batching.Commit.Slack,
			Timeout:        cfg.Batching.Commit.Timeout,
		},
		preCommit: preCommitBatchingConfig{
			MaxPreCommitBatch: miner15.PreCommitSectorBatchMaxSize,
			Slack:             cfg.Batching.PreCommit.Slack,
			Timeout:           cfg.Batching.PreCommit.Timeout,
		},
	}

	return &SealPoller{
		db:  db,
		api: api,
		cfg: c,
	}
}

func (s *SealPoller) RunPoller(ctx context.Context) {
	ticker := time.NewTicker(sealPollerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.poll(ctx); err != nil {
				log.Errorw("polling failed", "error", err)
			}
		}
	}
}

/*
NOTE: TaskIDs are ONLY set while the tasks are executing or waiting to execute.
      This means that there are ~4 states each task can be in:
* Not run, and dependencies not solved (dependencies are 'After' fields of previous stages), task is null, After is false
* Not run, and dependencies solved, task is null, After is false
* Running or queued, task is set, After is false
* Finished, task is null, After is true
*/

type pollTask struct {
	// Cache line 1 (bytes 0-64): Hot path - identification and early checks
	SpID                int64                   `db:"sp_id"`          // 8 bytes (0-8) - used in all UPDATE statements
	SectorNumber        int64                   `db:"sector_number"`  // 8 bytes (8-16) - used in all UPDATE statements
	RegisteredSealProof abi.RegisteredSealProof `db:"reg_seal_proof"` // 8 bytes (16-24)
	TaskSDR             sql.NullInt64           `db:"task_id_sdr"`    // 16 bytes (25-41, some padding)
	TaskTreeD           sql.NullInt64           `db:"task_id_tree_d"` // 16 bytes (crosses into cache line 2)
	Failed              bool                    `db:"failed"`         // 1 byte (24-25) - checked first in line 215 filter
	AfterSDR            bool                    `db:"after_sdr"`      // 1 byte - checked with TaskSDR
	AfterTreeD          bool                    `db:"after_tree_d"`   // 1 byte
	// Cache line 2 (bytes 64-128): Tree stage task IDs (checked in sequence)
	TaskTreeC   sql.NullInt64 `db:"task_id_tree_c"` // 16 bytes
	TaskTreeR   sql.NullInt64 `db:"task_id_tree_r"` // 16 bytes
	TaskSynth   sql.NullInt64 `db:"task_id_synth"`  // 16 bytes
	TicketEpoch sql.NullInt64 `db:"ticket_epoch"`   // 16 bytes
	AfterTreeC  bool          `db:"after_tree_c"`   // 1 byte
	AfterTreeR  bool          `db:"after_tree_r"`   // 1 byte
	AfterSynth  bool          `db:"after_synth"`    // 1 byte
	// Cache line 3 (bytes 128-192): PreCommit and timing
	TaskPrecommitMsg         sql.NullInt64 `db:"task_id_precommit_msg"`       // 16 bytes
	AfterPrecommitMsg        bool          `db:"after_precommit_msg"`         // 1 byte
	AfterPrecommitMsgSuccess bool          `db:"after_precommit_msg_success"` // 1 byte
	SeedEpoch                sql.NullInt64 `db:"seed_epoch"`                  // 16 bytes
	PreCommitReadyAt         sql.NullTime  `db:"precommit_ready_at"`          // 32 bytes (crosses into cache line 4)
	// Cache line 4 (bytes 192-256): PoRep and Finalize stages
	TaskPoRep     sql.NullInt64 `db:"task_id_porep"`    // 16 bytes
	TaskFinalize  sql.NullInt64 `db:"task_id_finalize"` // 16 bytes
	StartEpoch    sql.NullInt64 `db:"start_epoch"`      // 16 bytes
	AfterPoRep    bool          `db:"after_porep"`      // 1 byte
	AfterFinalize bool          `db:"after_finalize"`   // 1 byte
	// Cache line 5 (bytes 256-320): Commit and storage stages
	TaskMoveStorage       sql.NullInt64 `db:"task_id_move_storage"`     // 16 bytes
	CommitReadyAt         sql.NullTime  `db:"commit_ready_at"`          // 32 bytes
	TaskCommitMsg         sql.NullInt64 `db:"task_id_commit_msg"`       // 16 bytes
	AfterMoveStorage      bool          `db:"after_move_storage"`       // 1 byte
	AfterCommitMsg        bool          `db:"after_commit_msg"`         // 1 byte
	AfterCommitMsgSuccess bool          `db:"after_commit_msg_success"` // 1 byte
	// Remote seal flag
	IsRemote bool `db:"is_remote"` // true when sector has rseal_client_pipeline entry
	// Larger fields at end
	PoRepProof   []byte `db:"porep_proof"`   // 24 bytes - only used in specific stages
	FailedReason string `db:"failed_reason"` // 16 bytes - only used when Failed=true
}

func (s *SealPoller) poll(ctx context.Context) error {
	var tasks []pollTask

	err := s.db.Select(ctx, &tasks, `SELECT
												p.sp_id,
												p.sector_number,
												p.reg_seal_proof,
												p.ticket_epoch,
												p.task_id_sdr,
												p.after_sdr,
												p.task_id_tree_d,
												p.after_tree_d,
												p.task_id_tree_c,
												p.after_tree_c,
												p.task_id_tree_r,
												p.after_tree_r,
												p.task_id_synth,
												p.after_synth,
												p.precommit_ready_at,
												p.task_id_precommit_msg,
												p.after_precommit_msg,
												p.after_precommit_msg_success,
												p.seed_epoch,
												p.task_id_porep,
												p.porep_proof,
												p.after_porep,
												p.task_id_finalize,
												p.after_finalize,
												p.task_id_move_storage,
												p.after_move_storage,
												p.commit_ready_at,
												p.task_id_commit_msg,
												p.after_commit_msg,
												p.after_commit_msg_success,
												p.failed,
												p.failed_reason,
												p.start_epoch,
												(c.sp_id IS NOT NULL) AS is_remote
											FROM
												sectors_sdr_pipeline p
											LEFT JOIN rseal_client_pipeline c ON p.sp_id = c.sp_id AND p.sector_number = c.sector_number
											WHERE
												p.after_commit_msg_success != TRUE
												OR p.after_move_storage != TRUE;`)
	if err != nil {
		return err
	}

	tasks = lo.Filter(tasks, func(t pollTask, _ int) bool {
		return !t.Failed
	})
	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		return xerrors.Errorf("getting chain head: %w", err)
	}

	for _, task := range tasks {
		task := task

		if !task.IsRemote {
			// Local sectors: run SDR, TreeD, TreeRC, Synth locally.
			// Remote sectors skip these - they are handled by the provider
			// and the client poller (RSealClientPoller) manages the remote pipeline.
			s.pollStartSDR(ctx, task)
			s.pollStartSDRTreeD(ctx, task)
			s.pollStartSDRTreeRC(ctx, task)
			s.pollStartSynth(ctx, task)
		}
		// PreCommit, PoRep, Finalize, MoveStorage, Commit run for both local and remote sectors
		s.mustPoll(s.pollPrecommitMsgLanded(ctx, task))
		s.pollStartPoRep(ctx, task, ts)
		s.mustPoll(s.pollerAddStartEpoch(ctx, task))
		s.pollStartFinalize(ctx, task, ts)
		s.pollStartMoveStorage(ctx, task)
		s.mustPoll(s.pollCommitMsgLanded(ctx, task))
	}

	// Aggregate/Batch PreCommit and Commit
	s.pollStartBatchPrecommitMsg(ctx)
	s.pollStartBatchCommitMsg(ctx)

	return nil
}

func (s *SealPoller) pollStartSDR(ctx context.Context, task pollTask) {
	if !task.AfterSDR && !task.TaskSDR.Valid && s.pollers[pollerSDR].IsSet() {
		s.pollers[pollerSDR].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_sdr = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_sdr IS NULL`, id, task.SpID, task.SectorNumber)
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

func (t pollTask) afterSDR() bool {
	return t.AfterSDR
}

func (s *SealPoller) pollStartSDRTreeD(ctx context.Context, task pollTask) {
	if !task.AfterTreeD && !task.TaskTreeD.Valid && s.pollers[pollerTreeD].IsSet() && task.afterSDR() {
		s.pollers[pollerTreeD].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_tree_d = $1 WHERE sp_id = $2 AND sector_number = $3 AND after_sdr = TRUE AND task_id_tree_d IS NULL`, id, task.SpID, task.SectorNumber)
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

func (t pollTask) afterTreeD() bool {
	return t.AfterTreeD && t.afterSDR()
}

func (s *SealPoller) pollStartSDRTreeRC(ctx context.Context, task pollTask) {
	if !task.AfterTreeC && !task.AfterTreeR && !task.TaskTreeC.Valid && !task.TaskTreeR.Valid && s.pollers[pollerTreeRC].IsSet() && task.afterTreeD() {
		s.pollers[pollerTreeRC].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_tree_c = $1, task_id_tree_r = $1
                            WHERE sp_id = $2 AND sector_number = $3 AND after_tree_d = TRUE AND task_id_tree_c IS NULL AND task_id_tree_r IS NULL`, id, task.SpID, task.SectorNumber)
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

func (t pollTask) afterTreeRC() bool {
	return t.AfterTreeC && t.AfterTreeR && t.afterTreeD()
}

func (s *SealPoller) pollStartSynth(ctx context.Context, task pollTask) {
	if !task.AfterSynth && !task.TaskSynth.Valid && s.pollers[pollerSyntheticProofs].IsSet() && task.AfterTreeR && task.afterTreeRC() {
		s.pollers[pollerSyntheticProofs].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_synth = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_synth IS NULL`, id, task.SpID, task.SectorNumber)
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

func (t pollTask) afterSynth() bool {
	return t.AfterSynth && t.afterTreeRC()
}

func (t pollTask) afterPrecommitMsg() bool {
	return t.AfterPrecommitMsg && t.afterSynth()
}

func (t pollTask) afterPrecommitMsgSuccess() bool {
	return t.AfterPrecommitMsgSuccess && t.afterPrecommitMsg()
}

func (s *SealPoller) pollStartPoRep(ctx context.Context, task pollTask, ts *types.TipSet) {
	if s.pollers[pollerPoRep].IsSet() && task.afterPrecommitMsgSuccess() && task.SeedEpoch.Valid &&
		!task.TaskPoRep.Valid && !task.AfterPoRep &&
		ts.Height() >= abi.ChainEpoch(task.SeedEpoch.Int64+seedEpochConfidence) {

		s.pollers[pollerPoRep].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_porep = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_porep IS NULL`, id, task.SpID, task.SectorNumber)
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

func (t pollTask) afterPoRep() bool {
	return t.AfterPoRep && t.afterPrecommitMsgSuccess()
}

func (s *SealPoller) pollStartFinalize(ctx context.Context, task pollTask, ts *types.TipSet) {
	if s.pollers[pollerFinalize].IsSet() && task.afterPoRep() && !task.AfterFinalize && !task.TaskFinalize.Valid {
		s.pollers[pollerFinalize].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_finalize = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_finalize IS NULL`, id, task.SpID, task.SectorNumber)
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

func (t pollTask) afterFinalize() bool {
	return t.AfterFinalize && t.afterPoRep()
}

func (s *SealPoller) pollStartMoveStorage(ctx context.Context, task pollTask) {
	if s.pollers[pollerMoveStorage].IsSet() && task.afterFinalize() && !task.AfterMoveStorage && !task.TaskMoveStorage.Valid {
		s.pollers[pollerMoveStorage].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_move_storage = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_move_storage IS NULL`, id, task.SpID, task.SectorNumber)
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

func (s *SealPoller) mustPoll(err error) {
	if err != nil {
		log.Errorw("poller operation failed", "error", err)
	}
}
