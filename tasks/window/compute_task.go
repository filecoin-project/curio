package window

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/seal"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

var log = logging.Logger("curio/window")

var EpochsPerDeadline = miner.WPoStProvingPeriod() / abi.ChainEpoch(miner.WPoStPeriodDeadlines)

const daemonFailureGracePeriod = 5 * time.Minute

type WdPostTaskDetails struct {
	Ts       *types.TipSet
	Deadline *dline.Info
}

type WDPoStAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	ChainGetTipSet(context.Context, types.TipSetKey) (*types.TipSet, error)
	StateMinerProvingDeadline(context.Context, address.Address, types.TipSetKey) (*dline.Info, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	ChainGetTipSetAfterHeight(context.Context, abi.ChainEpoch, types.TipSetKey) (*types.TipSet, error)
	StateMinerPartitions(context.Context, address.Address, uint64, types.TipSetKey) ([]api.Partition, error)
	StateGetRandomnessFromBeacon(ctx context.Context, personalization crypto.DomainSeparationTag, randEpoch abi.ChainEpoch, entropy []byte, tsk types.TipSetKey) (abi.Randomness, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (network.Version, error)
	StateMinerSectors(context.Context, address.Address, *bitfield.BitField, types.TipSetKey) ([]*miner.SectorOnChainInfo, error)
}

type ProverPoSt interface {
	GenerateWindowPoStAdv(ctx context.Context, ppt abi.RegisteredPoStProof, mid abi.ActorID, sectors []storiface.PostSectorChallenge, partitionIdx int, randomness abi.PoStRandomness, allowSkip bool) (storiface.WindowPoStResult, error)
}

type WdPostTask struct {
	api WDPoStAPI
	db  *harmonydb.DB

	faultTracker FaultTracker
	storage      paths.Store
	verifier     storiface.Verifier
	paramsReady  func() (bool, error)

	windowPoStTF promise.Promise[harmonytask.AddTaskFunc]

	actors               *config.Dynamic[map[dtypes.MinerAddress]bool]
	max                  int
	parallel             chan struct{}
	challengeReadTimeout time.Duration
}

type wdTaskIdentity struct {
	SpID               uint64         `db:"sp_id"`
	ProvingPeriodStart abi.ChainEpoch `db:"proving_period_start"`
	DeadlineIndex      uint64         `db:"deadline_index"`
	PartitionIndex     uint64         `db:"partition_index"`
}

func NewWdPostTask(db *harmonydb.DB,
	api WDPoStAPI,
	faultTracker FaultTracker,
	storage paths.Store,
	verifier storiface.Verifier,
	paramck func() (bool, error),
	pcs *chainsched.CurioChainSched,
	actors *config.Dynamic[map[dtypes.MinerAddress]bool],
	max int,
	parallel int,
	challengeReadTimeout time.Duration,
) (*WdPostTask, error) {
	t := &WdPostTask{
		db:  db,
		api: api,

		faultTracker: faultTracker,
		storage:      storage,
		verifier:     verifier,
		paramsReady:  paramck,

		actors:               actors,
		max:                  max,
		challengeReadTimeout: challengeReadTimeout,
	}
	if parallel > 0 {
		t.parallel = make(chan struct{}, parallel)
	}

	if pcs != nil {
		if err := pcs.AddHandler(t.processHeadChange); err != nil {
			return nil, err
		}
	}

	return t, nil
}

func (t *WdPostTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	log.Debugw("WdPostTask.Do()", "taskID", taskID)

	var spID, pps, dlIdx, partIdx uint64

	err = t.db.QueryRow(context.Background(),
		`Select sp_id, proving_period_start, deadline_index, partition_index
			from wdpost_partition_tasks 
			where task_id = $1`, taskID).Scan(
		&spID, &pps, &dlIdx, &partIdx,
	)
	if err != nil {
		log.Errorf("WdPostTask.Do() failed to queryRow: %s", err.Error())
		return false, err
	}

	head, err := t.api.ChainHead(context.Background())
	if err != nil {
		log.Errorf("WdPostTask.Do() for SP %d on Deadline %d and Partition %d failed to get chain head: %s", spID, dlIdx, partIdx, err.Error())
		return false, xerrors.Errorf("SP %d on Deadline %d and Partition %d getting chain head: %w", spID, dlIdx, partIdx, err)
	}

	deadline := NewDeadlineInfo(abi.ChainEpoch(pps), dlIdx, head.Height())

	var testTask *int
	isTestTask := func() bool {
		if testTask != nil {
			return *testTask > 0
		}

		testTask = new(int)
		err := t.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM harmony_test WHERE task_id = $1`, taskID).Scan(testTask)
		if err != nil {
			log.Errorf("WdPostTask.Do() for SP %d on Deadline %d and Partition %d failed to queryRow: %s", spID, dlIdx, partIdx, err.Error())
			return false
		}

		return *testTask > 0
	}

	if deadline.PeriodElapsed() && !isTestTask() {
		log.Errorf("WdPost SP %d on Deadline %d and Partition %d removed stale task %d: %v", spID, dlIdx, partIdx, taskID, deadline)
		return true, nil
	}

	if deadline.Challenge > head.Height() {
		if isTestTask() {
			deadline = NewDeadlineInfo(abi.ChainEpoch(pps)-deadline.WPoStProvingPeriod, dlIdx, head.Height()-deadline.WPoStProvingPeriod)
			log.Warnw("Test task is in the future, adjusting to past", "taskID", taskID, "deadline", deadline)
		}
	}

	maddr, err := address.NewIDAddress(spID)
	if err != nil {
		log.Errorf("WdPostTask.Do() failed to NewIDAddress: %s", err.Error())
		return false, xerrors.Errorf("SP %d on Deadline %d and Partition %d getting miner address: %w", spID, dlIdx, partIdx, err)
	}

	ts, err := t.api.ChainGetTipSetAfterHeight(context.Background(), deadline.Challenge, head.Key())
	if err != nil {
		log.Errorf("WdPostTask.Do() SP %d on Deadline %d and Partition %d failed to ChainGetTipSetAfterHeight: %s", spID, dlIdx, partIdx, err.Error())
		return false, xerrors.Errorf("SP %d on Deadline %d and Partition %d getting tipset: %w", spID, dlIdx, partIdx, err)
	}

	// Set up a context with cancel so we can cancel the task if deadline is closed
	ctx, cancel := context.WithCancelCause(context.Background())
	finish := make(chan struct{})
	defer close(finish)

	// Monitor the current height to cancel the task with correct error
	go func(ctx context.Context, stAPI WDPoStAPI, cancel context.CancelCauseFunc, finish chan struct{}) {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done(): // To avoid goroutine leaks if any goes wrong with finish channel
				return
			case <-finish:
				return
			case <-ticker.C:
				h, err := stAPI.ChainHead(context.Background())
				if err != nil {
					log.Warnf("WdPostTask.Do() SP %d on Deadline %d and Partition %d failed to get chain head: %s", spID, dlIdx, partIdx, err.Error())
					failedAt := time.Now()
					for time.Now().After(failedAt.Add(daemonFailureGracePeriod)) { // In case daemon not reachable temporarily, allow 5 minutes grace period
						h, err = stAPI.ChainHead(context.Background())
						if err == nil {
							break
						}
						time.Sleep(2 * time.Second)
					}
					if err != nil {
						log.Errorf("WdPostTask.Do() SP %d on Deadline %d and Partition %d failed to get chain head, cancelling context: %s", spID, dlIdx, partIdx, err.Error())
						cancel(xerrors.Errorf("WdPostTask.Do() SP %d on Deadline %d and Partition %d failed to get chain head: %w", spID, dlIdx, partIdx, err))
						return
					}
				}
				if h.Height() > deadline.Close {
					if !isTestTask() {
						log.Errorf("WdPostTask.Do() SP %d on Deadline %d and Partition %d deadline closed at %d, cancelling context", spID, dlIdx, partIdx, h.Height())
						cancel(xerrors.Errorf("WdPostTask.Do() SP %d on Deadline %d and Partition %d cancelling context as head %d is greater then deadline close %d", spID, dlIdx, partIdx, h.Height(), deadline.Close))
						return
					}
				}
			}
		}
	}(ctx, t.api, cancel, finish)

	postOut, err := t.DoPartition(ctx, ts, maddr, deadline, partIdx, isTestTask())
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false, context.Cause(ctx) // Let's not return true here just in case. This will cause a retry and if deadline is truly closed then deadline check will mark this as complete
		}
		if errors.Is(err, errEmptyPartition) {
			log.Warnf("WdPostTask.Do() SP %d on Deadline %d and Partition %d failed to doPartition: %s", spID, dlIdx, partIdx, err.Error())
			return true, nil
		}
		log.Errorf("WdPostTask.Do() SP %d on Deadline %d and Partition %d failed to doPartition: %s", spID, dlIdx, partIdx, err.Error())
		return false, xerrors.Errorf("SP %d on Deadline %d and Partition %d doing PoSt: %w", spID, dlIdx, partIdx, err)
	}

	var msgbuf bytes.Buffer
	if err := postOut.MarshalCBOR(&msgbuf); err != nil {
		return false, xerrors.Errorf("SP %d on Deadline %d and Partition %d marshaling PoSt: %w", spID, dlIdx, partIdx, err)
	}

	if isTestTask() {
		// Do not send test tasks to the chain but to harmony_test & stdout.

		data, err := json.MarshalIndent(map[string]any{
			"sp_id":                spID,
			"proving_period_start": pps,
			"deadline":             deadline.Index,
			"partition":            partIdx,
			"submit_at_epoch":      deadline.Open,
			"submit_by_epoch":      deadline.Close,
			"post_out":             postOut,
			"proof_params":         msgbuf.Bytes(),
		}, "", "  ")
		if err != nil {
			return false, xerrors.Errorf("marshaling message: %w", err)
		}
		_, err = t.db.Exec(context.Background(), `UPDATE harmony_test SET result=$1 WHERE task_id=$2`, string(data), taskID)
		if err != nil {
			return false, xerrors.Errorf("updating harmony_test: %w", err)
		}
		log.Infof("SKIPPED sending test message to chain. SELECT * FROM harmony_test WHERE task_id= %d", taskID)
		return true, nil // nothing committed
	}
	// Insert into wdpost_proofs table
	n, err := t.db.Exec(context.Background(),
		`INSERT INTO wdpost_proofs (
                               sp_id,
                               proving_period_start,
	                           deadline,
	                           partition,
	                           submit_at_epoch,
	                           submit_by_epoch,
                               proof_params)
	    			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		spID,
		pps,
		deadline.Index,
		partIdx,
		deadline.Open,
		deadline.Close,
		msgbuf.Bytes(),
	)

	if err != nil {
		log.Errorf("WdPostTask.Do() SP %s on Deadline %d and Partition %d failed to insert into wdpost_proofs: %s", spID, dlIdx, partIdx, err.Error())
		return false, xerrors.Errorf("SP %d on Deadline %d and Partition %d inserting into wdpost_proofs: %w", spID, dlIdx, partIdx, err)
	}
	if n != 1 {
		log.Errorf("WdPostTask.Do() SP %s on Deadline %d and Partition %d failed to insert into wdpost_proofs: %v", spID, dlIdx, partIdx, err)
		return false, xerrors.Errorf("SP %d on Deadline %d and Partition %d inserting into wdpost_proofs: %w", spID, dlIdx, partIdx, err)
	}

	return true, nil
}

func (t *WdPostTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	rdy, err := t.paramsReady()
	if err != nil {
		return []harmonytask.TaskID{}, xerrors.Errorf("failed to setup params: %w", err)
	}
	if !rdy {
		log.Infow("WdPostTask.CanAccept() params not ready, not scheduling")
		return []harmonytask.TaskID{}, nil
	}

	// GetEpoch
	ts, err := t.api.ChainHead(context.Background())

	if err != nil {
		return []harmonytask.TaskID{}, err
	}

	// GetData for tasks
	type wdTaskDef struct {
		TaskID             harmonytask.TaskID
		SpID               uint64
		ProvingPeriodStart abi.ChainEpoch
		DeadlineIndex      uint64
		PartitionIndex     uint64

		dlInfo *dline.Info `pgx:"-"`
	}
	var tasks []wdTaskDef

	err = t.db.Select(context.Background(), &tasks,
		`SELECT 
			task_id,
			sp_id,
			proving_period_start,
			deadline_index,
			partition_index
	FROM wdpost_partition_tasks 
	WHERE task_id = ANY($1)`, lo.Map(ids, func(t harmonytask.TaskID, _ int) int { return int(t) }))
	if err != nil {
		return nil, err
	}

	result := []harmonytask.TaskID{}
	// Accept those past deadline, then delete them in Do().
	for i := range tasks {
		tasks[i].dlInfo = NewDeadlineInfo(tasks[i].ProvingPeriodStart, tasks[i].DeadlineIndex, ts.Height())

		if tasks[i].dlInfo.PeriodElapsed() {
			// note: Those may be test tasks
			result = append(result, tasks[i].TaskID)
		}
	}
	if len(result) > 0 {
		return result, nil
	}

	// todo fix the block below
	//  workAdderMutex is held by taskTypeHandler.considerWork, which calls this CanAccept
	//  te.ResourcesAvailable will try to get that lock again, which will deadlock

	// Discard those too big for our free RAM
	/*freeRAM := te.ResourcesAvailable().Ram
	tasks = lo.Filter(tasks, func(d wdTaskDef, _ int) bool {
		maddr, err := address.NewIDAddress(tasks[0].Sp_id)
		if err != nil {
			log.Errorf("WdPostTask.CanAccept() failed to NewIDAddress: %v", err)
			return false
		}

		mi, err := t.api.StateMinerInfo(context.Background(), maddr, ts.Key())
		if err != nil {
			log.Errorf("WdPostTask.CanAccept() failed to StateMinerInfo: %v", err)
			return false
		}

		spt, err := policy.GetSealProofFromPoStProof(mi.WindowPoStProofType)
		if err != nil {
			log.Errorf("WdPostTask.CanAccept() failed to GetSealProofFromPoStProof: %v", err)
			return false
		}

		return res[spt].MaxMemory <= freeRAM
	})*/
	if len(tasks) == 0 {
		log.Infof("RAM too small for any WDPost task")
		return result, nil
	}

	// Ignore those with too many failures unless they are the only ones left.
	tasks, _ = taskhelp.SliceIfFound(tasks, func(d wdTaskDef) bool {
		var r int
		err := t.db.QueryRow(context.Background(), `SELECT COUNT(*) 
		FROM harmony_task_history 
		WHERE task_id = $1 AND result = false`, d.TaskID).Scan(&r)
		if err != nil {
			log.Errorf("WdPostTask.CanAccept() failed to queryRow: %s", err.Error())
		}
		return r < 2
	})

	// Select the one closest to the deadline
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].dlInfo.Open < tasks[j].dlInfo.Open
	})

	result = lo.Map(tasks, func(d wdTaskDef, _ int) harmonytask.TaskID {
		return d.TaskID
	})
	return result, nil
}

func (t *WdPostTask) TypeDetails() harmonytask.TaskTypeDetails {
	gpu := 1.0
	ram := uint64(25 << 30)
	if seal.IsDevnet {
		gpu = 0
		ram = 1 << 30
	}

	return harmonytask.TaskTypeDetails{
		Name:        "WdPost",
		Max:         taskhelp.Max(t.max),
		MaxFailures: 5,
		Follows:     nil,
		Cost: resources.Resources{
			Cpu: 1,

			Gpu: gpu,

			// RAM of smallest proof's max is listed here
			Ram: ram,
		},
	}
}

func (t *WdPostTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	var spid string
	err := db.QueryRow(context.Background(), `SELECT sp_id FROM wdpost_partition_tasks WHERE task_id = $1`, taskID).Scan(&spid)
	if err != nil {
		log.Errorf("getting spid: %s", err.Error())
		return ""
	}
	return spid
}

var _ = harmonytask.Reg(&WdPostTask{})

func (t *WdPostTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.windowPoStTF.Set(taskFunc)
}

func (t *WdPostTask) processHeadChange(ctx context.Context, revert, apply *types.TipSet) error {
	for act := range t.actors.Get() {
		maddr := address.Address(act)

		aid, err := address.IDFromAddress(maddr)
		if err != nil {
			return xerrors.Errorf("getting miner ID: %w", err)
		}

		di, err := t.api.StateMinerProvingDeadline(ctx, maddr, apply.Key())
		if err != nil {
			return err
		}

		if !di.PeriodStarted() {
			return nil // not proving anything yet
		}

		partitions, err := t.api.StateMinerPartitions(ctx, maddr, di.Index, apply.Key())
		if err != nil {
			return xerrors.Errorf("getting partitions: %w", err)
		}

		// TODO: Batch Partitions??

		for pidx := range partitions {
			tid := wdTaskIdentity{
				SpID:               aid,
				ProvingPeriodStart: di.PeriodStart,
				DeadlineIndex:      di.Index,
				PartitionIndex:     uint64(pidx),
			}

			tf := t.windowPoStTF.Val(ctx)
			if tf == nil {
				return xerrors.Errorf("no task func")
			}

			tf(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
				return t.addTaskToDB(id, tid, tx)
			})
		}
	}

	return nil
}

func (t *WdPostTask) addTaskToDB(taskId harmonytask.TaskID, taskIdent wdTaskIdentity, tx *harmonydb.Tx) (bool, error) {

	_, err := tx.Exec(
		`INSERT INTO wdpost_partition_tasks (
                         task_id,
                          sp_id,
                          proving_period_start,
                          deadline_index,
                          partition_index
                        ) VALUES ($1, $2, $3, $4, $5)`,
		taskId,
		taskIdent.SpID,
		taskIdent.ProvingPeriodStart,
		taskIdent.DeadlineIndex,
		taskIdent.PartitionIndex,
	)
	if err != nil {
		return false, xerrors.Errorf("insert partition task: %w", err)
	}

	return true, nil
}

var _ harmonytask.TaskInterface = &WdPostTask{}

func NewDeadlineInfo(periodStart abi.ChainEpoch, deadlineIdx uint64, currEpoch abi.ChainEpoch) *dline.Info {
	return dline.NewInfo(periodStart, deadlineIdx, currEpoch, miner.WPoStPeriodDeadlines, miner.WPoStProvingPeriod(), miner.WPoStChallengeWindow(), miner.WPoStChallengeLookback, miner.FaultDeclarationCutoff)
}
