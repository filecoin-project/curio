package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/tasknames"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

const alertNameProcessDeletions = "ProcessDeletions"

// reasonPDPProcessDeletions is the SenderETH reason for processPieceDeletions
// sends. Also registered with the reorg checker.
const reasonPDPProcessDeletions = "pdp-process-deletions"

// processDeletionsBatchSize is the starting number of queue entries drained per
// transaction. It matches PDPVerifier's PiecesRemoved event chunk size, and sits
// well under the block gas limit.
//
// With ConservativeEnqueuedRemovalsLimit at 35 this never binds in steady state
// -- one message drains a period's whole queue. It exists for migration-seeded
// backlogs, which predate that limit and can run to a few hundred pieces. The
// halving loop below, not this constant, is what guarantees progress: the
// listener's gas cost is invisible to PDPVerifier, which is the root cause of
// FilOzone/pdp#283.
const processDeletionsBatchSize = 100

// processDeletionsScheduleLimit bounds how many data sets are claimed per tipset.
const processDeletionsScheduleLimit = 16

type ProcessDeletionsTask struct {
	db        *harmonydb.DB
	ethClient ethchain.EthClient
	sender    *message.SenderETH

	fil ProcessDeletionsChainApi

	al curioalerting.AlertingInterface

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

type ProcessDeletionsChainApi interface {
	ChainHead(context.Context) (*chainTypes.TipSet, error)
}

// NewProcessDeletionsTask drains PDPVerifier scheduled-removal queues.
//
// Scheduling is driven by pdpv0_deletion_drain coordination rows, gated on the
// same prove_at_epoch + challenge_window deadline as nextProvingPeriod. A NULL
// task_id means no Harmony task currently owns the row; a NULL msg_hash means no
// processPieceDeletions transaction is waiting for confirmation.
func NewProcessDeletionsTask(db *harmonydb.DB, ethClient ethchain.EthClient, fil ProcessDeletionsChainApi, w *Watcher, sender *message.SenderETH) *ProcessDeletionsTask {
	p := &ProcessDeletionsTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		fil:       fil,
		al:        w.al,
	}

	_ = w.AddWatcher(func(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, al curioalerting.AlertingInterface, revert, apply *chainTypes.TipSet) {
		if apply == nil {
			return
		}

		var candidates []struct {
			DataSetID int64 `db:"data_set"`
		}

		currentHeight := apply.Height()
		err := db.Select(ctx, &candidates, `
			SELECT d.data_set
			FROM pdpv0_deletion_drain d
			JOIN pdp_data_sets ds ON ds.id = d.data_set
			WHERE d.task_id IS NULL
			  AND d.msg_hash IS NULL
			  AND (ds.prove_at_epoch + ds.challenge_window) <= $1
			ORDER BY d.data_set
			LIMIT $2
		`, currentHeight, processDeletionsScheduleLimit)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProcessDeletions,
				Message:   fmt.Sprintf("failed to select data sets needing removal draining: %s", err),
			})
			return
		}

		for _, candidate := range candidates {
			dataSetID := candidate.DataSetID
			p.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				affected, err := tx.Exec(`
					UPDATE pdpv0_deletion_drain
					SET task_id = $1
					WHERE data_set = $2 AND task_id IS NULL AND msg_hash IS NULL
				`, id, dataSetID)
				if err != nil {
					return false, xerrors.Errorf("failed to claim deletion drain row: %w", err)
				}
				if affected == 0 {
					// Claimed elsewhere.
					return false, nil
				}
				return true, nil
			})
		}
	}, WatcherOrderProcessDeletions)

	return p
}

func (p *ProcessDeletionsTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var dataSetID int64
	err = p.db.QueryRow(ctx, `SELECT data_set FROM pdpv0_deletion_drain WHERE task_id = $1`, taskID).Scan(&dataSetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, xerrors.Errorf("failed to query deletion drain row: %w", err)
	}

	defer func() {
		if err != nil {
			log.Errorw("Removal queue draining failed", "dataSetId", dataSetID, "error", err)
		}
	}()

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	// The deployed contract may predate processPieceDeletions, in which case
	// nextProvingPeriod still drains the queue itself and this task must not
	// send anything.
	supported, err := contract.SupportsPieceDeletionProcessing(ctx, verifier)
	if err != nil {
		return false, xerrors.Errorf("failed to determine PDPVerifier removal support: %w", err)
	}
	if !supported {
		if dropErr := p.dropDrainRow(ctx, dataSetID, taskID); dropErr != nil {
			return false, dropErr
		}
		log.Debugw("PDPVerifier predates processPieceDeletions; leaving removals to nextProvingPeriod",
			"dataSetId", dataSetID)
		return true, nil
	}

	queued, err := verifier.GetScheduledRemovals(contract.EthCallOpts(ctx), big.NewInt(dataSetID))
	if err != nil {
		// A data set in deletion or cleanup is no longer live, so its queue is
		// moot. Drop the row rather than burning retries against it.
		if IsPDPVerifierDataSetNotFound(err) || IsPDPVerifierDataSetNotLive(err) {
			if dropErr := p.dropDrainRow(ctx, dataSetID, taskID); dropErr != nil {
				return false, dropErr
			}
			log.Infow("dropping removal drain for data set that is no longer live", "dataSetId", dataSetID)
			return true, nil
		}
		return false, xerrors.Errorf("failed to read scheduled removals for data set %d: %w", dataSetID, err)
	}

	if len(queued) == 0 {
		if err := processPendingPieceDeletes(ctx, p.db, verifier, dataSetID, nil); err != nil {
			return false, xerrors.Errorf("failed to reconcile drained piece deletes for data set %d: %w", dataSetID, err)
		}
		if dropErr := p.dropDrainRow(ctx, dataSetID, taskID); dropErr != nil {
			return false, dropErr
		}
		return true, nil
	}

	if !stillOwned() {
		return false, nil
	}

	fromAddress, _, err := verifier.GetDataSetStorageProvider(contract.EthCallOpts(ctx), big.NewInt(dataSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get storage provider for data set %d: %w", dataSetID, err)
	}

	pabi, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier metadata: %w", err)
	}

	txHash, batchSize, err := p.sendProcessPieceDeletions(ctx, pabi, fromAddress, dataSetID, len(queued))
	if err != nil {
		return false, xerrors.Errorf("failed to send processPieceDeletions: %w", err)
	}

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`
			UPDATE pdpv0_deletion_drain
			SET msg_hash = $1, task_id = NULL
			WHERE task_id = $2
		`, txHash.Hex(), taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to record drain message: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 deletion drain row, updated %d", n)
		}

		_, err = tx.Exec(`
			INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
			VALUES ($1, 'pending') ON CONFLICT DO NOTHING
		`, txHash.Hex())
		if err != nil {
			return false, xerrors.Errorf("failed to insert drain message wait: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("failed to commit drain state: %w", err)
	}
	if !comm {
		return false, xerrors.Errorf("failed to commit drain state")
	}

	log.Infow("submitted PDP processPieceDeletions",
		"dataSetId", dataSetID,
		"txHash", txHash.Hex(),
		"batchSize", batchSize,
		"queueLength", len(queued))

	return true, nil
}

type drainProvingSchedule struct {
	ProveAtEpoch    sql.NullInt64 `db:"prove_at_epoch"`
	ChallengeWindow sql.NullInt64 `db:"challenge_window"`
}

func (p *ProcessDeletionsTask) loadProvingSchedule(ctx context.Context, dataSetID int64) (drainProvingSchedule, error) {
	var schedule drainProvingSchedule
	err := p.db.QueryRow(ctx, `
		SELECT prove_at_epoch, challenge_window
		FROM pdp_data_sets
		WHERE id = $1
	`, dataSetID).Scan(&schedule.ProveAtEpoch, &schedule.ChallengeWindow)
	if err != nil {
		return drainProvingSchedule{}, xerrors.Errorf("failed to load proving schedule for data set %d: %w", dataSetID, err)
	}
	return schedule, nil
}

// sendProcessPieceDeletions submits one drain batch, halving on gas-estimate
// failure. SenderETH estimates gas before submission, so a failed estimate means
// nothing was sent and a smaller retry cannot double-drain.
func (p *ProcessDeletionsTask) sendProcessPieceDeletions(ctx context.Context, pabi *abi.ABI, from common.Address, dataSet int64, queueLength int) (common.Hash, int, error) {
	batchSize := processDeletionsBatchSize
	if queueLength < batchSize {
		batchSize = queueLength
	}

	dataSetID := big.NewInt(dataSet)

	for {
		data, err := pabi.Pack("processPieceDeletions", dataSetID, big.NewInt(int64(batchSize)))
		if err != nil {
			return common.Hash{}, 0, err
		}

		txEth := types.NewTransaction(
			0,
			contract.ContractAddresses().PDPVerifier,
			big.NewInt(0),
			0,
			nil,
			data,
		)

		txHash, err := p.sender.Send(ctx, from, txEth, reasonPDPProcessDeletions)
		if err == nil {
			return txHash, batchSize, nil
		}

		if !isCleanupPiecesGasEstimateOutOfGas(err) {
			return common.Hash{}, 0, err
		}
		if batchSize == 1 {
			return common.Hash{}, 0, xerrors.Errorf("processPieceDeletions gas estimate failed at batch size 1: %w", err)
		}

		next := batchSize / 2
		if next == 0 {
			next = 1
		}
		log.Warnw("processPieceDeletions gas estimate failed; retrying with smaller batch",
			"dataSetId", dataSetID, "batchSize", batchSize, "nextBatchSize", next, "err", err)
		batchSize = next
	}
}

func (p *ProcessDeletionsTask) dropDrainRow(ctx context.Context, dataSetID int64, taskID harmonytask.TaskID) error {
	_, err := p.db.Exec(ctx, `DELETE FROM pdpv0_deletion_drain WHERE data_set = $1 AND task_id = $2`, dataSetID, taskID)
	if err != nil {
		return xerrors.Errorf("failed to drop deletion drain row for data set %d: %w", dataSetID, err)
	}
	return nil
}

func (p *ProcessDeletionsTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (p *ProcessDeletionsTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:           taskhelp.Max(16),
		Name:          tasknames.PDPv0_ProcDel,
		TimeSensitive: true,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 3,
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
	}
}

func (p *ProcessDeletionsTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.addFunc.Set(taskFunc)
}

// enqueueDeletionDrain records that a data set may have removals to drain.
//
// Called from the proving-period tasks when PDPVerifier reports pending
// deletions, so that a data set whose drain row was lost -- or which had
// removals scheduled out of band -- is picked up rather than retrying a
// rollover that cannot succeed.
func enqueueDeletionDrain(tx *harmonydb.Tx, dataSetID int64) error {
	_, err := tx.Exec(`INSERT INTO pdpv0_deletion_drain (data_set) VALUES ($1) ON CONFLICT DO NOTHING`, dataSetID)
	if err != nil {
		return xerrors.Errorf("failed to enqueue removal drain for data set %d: %w", dataSetID, err)
	}
	return nil
}

// hasDrainInFlight reports whether a data set has a processPieceDeletions
// transaction awaiting confirmation. The proving-period tasks preflight on this
// so they do not send a nextProvingPeriod that PDPVerifier is certain to revert.
//
// Deliberately narrower than "has a drain row": the migration seeds a row for
// every data set, and those rows are only cleared once the drain task has
// confirmed each queue is empty. Gating rollover on row existence would stall
// proving fleet-wide until that sweep finished. An in-flight message is the one
// state where the rollover is guaranteed to fail, and PendingPieceDeletions
// handling covers everything else.
func hasDrainInFlight(ctx context.Context, db *harmonydb.DB, dataSetID int64) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pdpv0_deletion_drain WHERE data_set = $1 AND msg_hash IS NOT NULL)
	`, dataSetID).Scan(&exists)
	if err != nil {
		return false, xerrors.Errorf("failed to check removal drain state for data set %d: %w", dataSetID, err)
	}
	return exists, nil
}

var _ harmonytask.TaskInterface = &ProcessDeletionsTask{}
var _ = harmonytask.Reg(&ProcessDeletionsTask{})
