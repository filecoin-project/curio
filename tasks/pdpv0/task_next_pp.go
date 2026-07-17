package pdpv0

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

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

const alertNameNextPP = "NextProvingPeriod"

type NextProvingPeriodTask struct {
	db        *harmonydb.DB
	ethClient ethchain.EthClient
	sender    *message.SenderETH

	fil NextProvingPeriodTaskChainApi

	al curioalerting.AlertingInterface

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

type NextProvingPeriodTaskChainApi interface {
	ChainHead(context.Context) (*chainTypes.TipSet, error)
}

func NewNextProvingPeriodTask(db *harmonydb.DB, ethClient ethchain.EthClient, fil NextProvingPeriodTaskChainApi, w *Watcher, sender *message.SenderETH) *NextProvingPeriodTask {
	n := &NextProvingPeriodTask{
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

		// Now query the db for data sets needing nextProvingPeriod
		var toCallNext []struct {
			DataSetId int64 `db:"id"`
		}

		currentHeight := apply.Height()
		err := db.Select(ctx, &toCallNext, `
                SELECT id
                FROM pdp_data_sets
                WHERE challenge_request_task_id IS NULL
                  AND (prove_at_epoch + challenge_window) <= $1
                  AND unrecoverable_proving_failure_epoch IS NULL
	            `, currentHeight)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameNextPP,
				Message:   fmt.Sprintf("failed to select data sets needing nextProvingPeriod: %s", err),
			})
			return
		}

		for _, ps := range toCallNext {
			n.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Update pdp_data_sets to set challenge_request_task_id = id
				affected, err := tx.Exec(`
                        UPDATE pdp_data_sets
                        SET challenge_request_task_id = $1
                        WHERE id = $2 AND challenge_request_task_id IS NULL
                    `, id, ps.DataSetId)
				if err != nil {
					return false, xerrors.Errorf("failed to update pdp_data_sets: %w", err)
				}
				if affected == 0 {
					// Someone else might have already scheduled the task
					return false, nil
				}

				return true, nil
			})
		}
	}, WatcherOrderProving)

	return n
}

func (n *NextProvingPeriodTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()
	// Select the data set where challenge_request_task_id = taskID
	var dataSetId int64

	err = n.db.QueryRow(ctx, `
        SELECT id
        FROM pdp_data_sets
        WHERE challenge_request_task_id = $1 AND prove_at_epoch IS NOT NULL
    `, taskID).Scan(&dataSetId)
	if errors.Is(err, pgx.ErrNoRows) {
		// No matching data set, task is done (something weird happened, and e.g another task was spawned in place of this one)
		return true, nil
	}
	if err != nil {
		return false, xerrors.Errorf("failed to query pdp_data_sets: %w", err)
	}

	defer func() {
		if err != nil {
			log.Errorw("Next challange window scheduling failed", "dataSetId", dataSetId, "error", err)
			err = fmt.Errorf("failed to set up next proving period for dataset %d: %w", dataSetId, err)
		}
	}()

	// Get the listener address for this data set from the PDPVerifier contract
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, n.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	// Preflight handlers need the current height when a contract lookup proves
	// the dataset is already terminal.
	ts, err := n.fil.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}
	currentHeight := int64(ts.Height())

	listenerAddr, err := pdpVerifier.GetDataSetListener(contract.EthCallOpts(ctx), big.NewInt(dataSetId))
	if err != nil {
		return n.handleNextProvingPeriodPreflightError(ctx, dataSetId, currentHeight, xerrors.Errorf("failed to get listener address for data set %d: %w", dataSetId, err))
	}

	// Get the proving schedule from the listener (handles view contract indirection)
	provingSchedule, err := contract.GetProvingScheduleFromListener(ctx, listenerAddr, n.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to get proving schedule from listener: %w", err)
	}

	// In case of contract migration update db schema with latest proving schedule
	err = n.refreshProvingPeriod(ctx, dataSetId, provingSchedule)
	if err != nil {
		return n.handleNextProvingPeriodPreflightError(ctx, dataSetId, currentHeight, xerrors.Errorf("failed to refresh proving period: %w", err))
	}

	next_prove_at, err := provingSchedule.NextPDPChallengeWindowStart(contract.EthCallOpts(ctx), big.NewInt(dataSetId))
	if err != nil {
		return n.handleNextProvingPeriodPreflightError(ctx, dataSetId, currentHeight, xerrors.Errorf("failed to get next challenge window start: %w", err))
	}

	// Instantiate the PDPVerifier contract
	pdpContracts := contract.ContractAddresses()
	pdpVerifierAddress := pdpContracts.PDPVerifier

	// Prepare the transaction data
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	data, err := abiData.Pack("nextProvingPeriod", big.NewInt(dataSetId), next_prove_at, []byte{})
	if err != nil {
		return false, xerrors.Errorf("failed to pack data: %w", err)
	}

	// Prepare the transaction
	txEth := types.NewTransaction(
		0,                  // nonce (will be set by sender)
		pdpVerifierAddress, // to
		big.NewInt(0),      // value
		0,                  // gasLimit (to be estimated)
		nil,                // gasPrice (to be set by sender)
		data,               // data
	)

	if !stillOwned() {
		// Task was abandoned, don't send the transaction
		return false, nil
	}

	fromAddress, _, err := pdpVerifier.GetDataSetStorageProvider(contract.EthCallOpts(ctx), big.NewInt(dataSetId))
	if err != nil {
		return n.handleNextProvingPeriodPreflightError(ctx, dataSetId, currentHeight, xerrors.Errorf("failed to get default sender address: %w", err))
	}

	// Send the transaction
	reason := "pdp-proving-period"
	txHash, sendErr := n.sender.Send(ctx, fromAddress, txEth, reason)
	if sendErr != nil {
		comm, err := n.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			handleErr := handleNextProvingPeriodSendError(ctx, tx, n.ethClient, n.al, alertNameNextPP, dataSetId, currentHeight, sendErr)
			if handleErr != nil {
				return false, xerrors.Errorf("failed to handle proving send error: %w", handleErr)
			}
			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return false, xerrors.Errorf("failed to send transaction: %w", err)
		}
		if !comm {
			return false, xerrors.Errorf("failed to commit transaction")
		}
		return true, nil
	}

	// Update the database in a transaction
	_, err = n.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Update pdp_data_sets
		affected, err := tx.Exec(`
            UPDATE pdp_data_sets
            SET challenge_request_msg_hash = $1,
                prev_challenge_request_epoch = $2,
				prove_at_epoch = $3
            WHERE id = $4
        `, txHash.Hex(), ts.Height(), next_prove_at.Uint64(), dataSetId)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_data_sets: %w", err)
		}
		if affected == 0 {
			return false, xerrors.Errorf("pdp_data_sets update affected 0 rows")
		}

		// Insert into message_waits_eth
		_, err = tx.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, 'pending') ON CONFLICT DO NOTHING
        `, txHash.Hex())
		if err != nil {
			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
		}

		return true, nil
	})
	if err != nil {
		return false, xerrors.Errorf("failed to perform database transaction: %w", err)
	}

	// Task completed successfully
	log.Infow("Next challenge window scheduled", "epoch", next_prove_at, "dataSetId", dataSetId)

	return true, nil
}

// Note: this function needs revisiting if we are ever *shrinking* proving period or challenge window values
func (n *NextProvingPeriodTask) refreshProvingPeriod(ctx context.Context, dataSetId int64, provingSchedule *contract.IPDPProvingSchedule) error {
	config, err := provingSchedule.GetPDPConfig(contract.EthCallOpts(ctx))
	if err != nil {
		return xerrors.Errorf("failed to GetPDPConfig: %w", err)
	}

	_, err = n.db.Exec(ctx, `UPDATE pdp_data_sets
								SET proving_period = $1,
									challenge_window = $2
								WHERE id = $3
								  AND (proving_period IS DISTINCT FROM $1 OR challenge_window IS DISTINCT FROM $2)`, config.MaxProvingPeriod, config.ChallengeWindow.Uint64(), dataSetId)
	return err
}

func (n *NextProvingPeriodTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (n *NextProvingPeriodTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name:          tasknames.PDPv0_ProvPeriod,
		TimeSensitive: true,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 3, // Set retry limit to 3 attempts
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
		Max:         taskhelp.Max(16),
	}
}

func (n *NextProvingPeriodTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	n.addFunc.Set(taskFunc)
}

var _ = harmonytask.Reg(&NextProvingPeriodTask{})

// resetDatasetToInitPP resets a dataset so that InitProvingPeriodTask picks it up.
// This is only appropriate for datasets whose on-chain proving period was never
// initialized (e.g. ProvingPeriodNotInitialized error from the contract). InitPP
// computes a fresh challenge window from config.InitChallengeWindowStart, which is
// only valid for first-time initialization.
func resetDatasetToInitPP(ctx context.Context, db *harmonydb.DB, dataSetId int64) error {
	log.Infow("resetting dataset to init proving period state", "dataSetId", dataSetId)
	_, err := db.Exec(ctx, `
             UPDATE pdp_data_sets
             SET challenge_request_msg_hash = NULL,
                     prove_at_epoch = NULL,
                     init_ready = TRUE,
                     prev_challenge_request_epoch = NULL
             WHERE id = $1
     `, dataSetId)
	if err != nil {
		return xerrors.Errorf("failed to reset dataset to init state: %w", err)
	}
	return nil
}

// disableProvingForEmptyDataset clears the local proving schedule for datasets
// that cannot start another proving period because PDPVerifier has no leaves to
// challenge. New piece-addition flow is responsible for setting init_ready back
// to true when the dataset has proving work again.
func disableProvingForEmptyDataset(tx *harmonydb.Tx, dataSetId int64) error {
	_, err := tx.Exec(`
		UPDATE pdp_data_sets
		SET challenge_request_msg_hash = NULL,
			prove_at_epoch = NULL,
			init_ready = FALSE,
			prev_challenge_request_epoch = NULL
		WHERE id = $1
	`, dataSetId)
	if err != nil {
		return xerrors.Errorf("failed to disable proving: %w", err)
	}
	return nil
}

var getPDPVerifierNextChallengeEpoch = func(ctx context.Context, ethClient ethchain.EthClient, dataSetId int64) (*big.Int, error) {
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("failed to instantiate PDPVerifier: %w", err)
	}

	challengeEpoch, err := pdpVerifier.GetNextChallengeEpoch(contract.EthCallOpts(ctx), big.NewInt(dataSetId))
	if err != nil {
		return nil, xerrors.Errorf("failed to get next challenge epoch: %w", err)
	}
	return challengeEpoch, nil
}

// skipCurrentOnChainProvingPeriod reconciles a dataset when initPP/nextPP learns
// that the contract has already scheduled the next challenge period.
//
// How: read PDPVerifier's authoritative next challenge epoch, clear the local
// challenge_request_msg_hash so the prove watcher cannot submit for this period,
// and store that on-chain epoch in prove_at_epoch. The nextPP watcher already
// schedules the following period after prove_at_epoch+challenge_window, so this
// leaves Curio in the normal path for the next period without inventing local
// message_waits_eth state.
//
// Why dropping this period is better: Curio proves only after its own
// challenge_request_msg_hash has a successful message_waits_eth row. If that
// local confirmation is missing, fabricating a hash/wait row would make Curio
// prove against state it did not confirm. Retrying initPP/nextPP can also loop
// on an already-applied period transition. Skipping one already-scheduled period
// avoids both cases and lets the existing scheduler recover at the next window.
func skipCurrentOnChainProvingPeriod(ctx context.Context, tx *harmonydb.Tx, ethClient ethchain.EthClient, dataSetId int64, currentHeight int64) error {
	challengeEpoch, err := getPDPVerifierNextChallengeEpoch(ctx, ethClient, dataSetId)
	if err != nil {
		return err
	}
	if challengeEpoch == nil {
		return xerrors.Errorf("next challenge epoch is nil for data set %d", dataSetId)
	}
	if challengeEpoch.Sign() == 0 {
		return xerrors.Errorf("data set %d has no scheduled challenge epoch on-chain", dataSetId)
	}
	if !challengeEpoch.IsInt64() {
		return xerrors.Errorf("next challenge epoch %s does not fit in int64 for data set %d", challengeEpoch.String(), dataSetId)
	}

	// prev_challenge_request_epoch normally records the epoch where Curio sent
	// initPP/nextPP. In this recovery path that exact send epoch is not useful,
	// because the local challenge_request_msg_hash is intentionally dropped; use
	// the reconciliation height as a marker while prove_at_epoch drives scheduling.
	affected, err := tx.Exec(`
			UPDATE pdp_data_sets
			SET challenge_request_msg_hash = NULL,
				prev_challenge_request_epoch = $2,
				prove_at_epoch = $3
			WHERE id = $1
			  AND unrecoverable_proving_failure_epoch IS NULL
	`, dataSetId, currentHeight, challengeEpoch.Int64())
	if err != nil {
		return xerrors.Errorf("failed to skip current proving period: %w", err)
	}
	if affected != 1 {
		return xerrors.Errorf("expected to skip current proving period for 1 data set, updated %d", affected)
	}

	return nil
}

func (n *NextProvingPeriodTask) handleNextProvingPeriodPreflightError(ctx context.Context, dataSetId int64, currentHeight int64, err error) (bool, error) {
	switch {
	case IsUnrecoverableError(err), IsPDPVerifierDataSetNotFound(err):
		committed, handleErr := n.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			if err := markDatasetProvingUnrecoverableAndTerminate(tx, dataSetId, currentHeight); err != nil {
				return false, err
			}
			return true, nil
		}, harmonydb.OptionRetry())
		if handleErr != nil {
			return false, xerrors.Errorf("failed to handle terminal proving period preflight error: %w", handleErr)
		}
		if !committed {
			return false, xerrors.Errorf("failed to commit terminal proving period preflight error handling")
		}
		log.Warnw("Terminal proving period preflight error, stopping proving attempts",
			"dataSetId", dataSetId, "height", currentHeight, "error", err)
		return true, nil
	case IsProvingPeriodNotInitializedError(err):
		if err := resetDatasetToInitPP(ctx, n.db, dataSetId); err != nil {
			return false, xerrors.Errorf("failed to reset dataset to init proving period state: %w", err)
		}
		return true, nil
	case IsUnexpectedProvingInvariantError(err):
		emitProvingSendErrorAlert(n.al, alertNameNextPP, err)
		return false, err
	case IsContractRevert(err):
		emitProvingSendErrorAlert(n.al, alertNameNextPP, err)
		return false, err
	default:
		return false, err
	}
}

func handleNextProvingPeriodSendError(ctx context.Context, tx *harmonydb.Tx, ethClient ethchain.EthClient, al curioalerting.AlertingInterface, alertSubsystem string, dataSetId int64, currentHeight int64, sendErr error) error {
	switch {
	case IsInsufficientChallengeDelayError(sendErr):
		// The challenge epoch was too close to the current block. Retry the
		// task so it recomputes challenge state and calldata instead of
		// resending the same transaction.
		emitProvingSendErrorAlert(al, alertSubsystem, sendErr)
		log.Warnw("Retrying proving period scheduling after insufficient challenge delay",
			"dataSetId", dataSetId, "subsystem", alertSubsystem, "height", currentHeight, "error", sendErr)
		return sendErr
	case IsUnrecoverableError(sendErr):
		// FWSS payment/dataset state says proving cannot recover. Mark local
		// state terminal and schedule FWSS termination instead of retrying.
		if err := markDatasetProvingUnrecoverableAndTerminate(tx, dataSetId, currentHeight); err != nil {
			return err
		}
		log.Warnw("Dataset unrecoverable, stopping proving period scheduling",
			"dataSetId", dataSetId, "subsystem", alertSubsystem, "error", sendErr)
		return nil
	case IsNextProvingPeriodAlreadyCalledError(sendErr):
		// The chain has already advanced the proving period, but Curio cannot
		// safely prove it without its own confirmed challenge request message.
		// Reconcile local schedule state from chain and complete this task so
		// the nextPP watcher can pick up after this proving window closes.
		log.Warnw("Proving period scheduling hit an already-applied period transition",
			"dataSetId", dataSetId, "subsystem", alertSubsystem, "height", currentHeight, "error", sendErr)
		if err := skipCurrentOnChainProvingPeriod(ctx, tx, ethClient, dataSetId, currentHeight); err != nil {
			return xerrors.Errorf("failed to skip current on-chain proving period: %w", err)
		}
		return nil
	case IsNextProvingPeriodEmptyDatasetError(sendErr):
		// PDPVerifier cannot start a new proving period without leaves. Disable
		// local proving until a later piece-addition path makes the dataset
		// eligible again.
		if err := disableProvingForEmptyDataset(tx, dataSetId); err != nil {
			return err
		}
		log.Warnw("Stopping proving period scheduling for empty dataset",
			"dataSetId", dataSetId, "subsystem", alertSubsystem, "height", currentHeight, "error", sendErr)
		return nil
	case IsRefreshProvingStateError(sendErr):
		// The selected challenge epoch is no longer valid. Retry the task so it
		// recomputes the proving schedule from chain/listener state.
		return sendErr
	case IsUnexpectedProvingInvariantError(sendErr):
		// Curio should not hit these in the normal initPP/nextPP path. Alert and
		// retry without mutating proving state so the condition can be investigated.
		emitProvingSendErrorAlert(al, alertSubsystem, sendErr)
		return sendErr
	case IsContractRevert(sendErr):
		// Fallback for unclassified contract reverts: alert and let Harmony
		// retry without mutating proving state.
		emitProvingSendErrorAlert(al, alertSubsystem, sendErr)
		return sendErr
	default:
		// Non-contract send failures are transport/sender/task errors.
		return sendErr
	}
}
