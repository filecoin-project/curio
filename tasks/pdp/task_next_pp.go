package pdp

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type NextProvingPeriodTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH

	fil NextProvingPeriodTaskChainApi

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

type NextProvingPeriodTaskChainApi interface {
	ChainHead(context.Context) (*chainTypes.TipSet, error)
}

func NewNextProvingPeriodTask(db *harmonydb.DB, ethClient *ethclient.Client, fil NextProvingPeriodTaskChainApi, chainSched *chainsched.CurioChainSched, sender *message.SenderETH) *NextProvingPeriodTask {
	n := &NextProvingPeriodTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		fil:       fil,
	}

	_ = chainSched.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		if apply == nil {
			return nil
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
                  AND terminated_at_epoch IS NULL
                  AND (next_prove_attempt_at IS NULL OR next_prove_attempt_at <= $1)
            `, currentHeight)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return xerrors.Errorf("failed to select data sets needing nextProvingPeriod: %w", err)
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

		return nil
	})

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

	listenerAddr, err := pdpVerifier.GetDataSetListener(nil, big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to get listener address for data set %d: %w", dataSetId, err)
	}

	// Get the proving schedule from the listener (handles view contract indirection)
	provingSchedule, err := contract.GetProvingScheduleFromListener(listenerAddr, n.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to get proving schedule from listener: %w", err)
	}
	next_prove_at, err := provingSchedule.NextPDPChallengeWindowStart(nil, big.NewInt(dataSetId))
	if err != nil {
		// not my favourite way to handle this but pragmatic
		// for some reason we are in a proving loop running but it is not initialized
		if strings.Contains(err.Error(), "0x999010d5") { // Error.ProvingPeriodNotInitialized
			if err := ResetDatasetToInit(ctx, n.db, dataSetId); err != nil {
				return false, xerrors.Errorf("failed to reset to init: %w", err)
			}
			return true, nil // true as this task is done
		}
		return false, xerrors.Errorf("failed to get next challenge window start: %w", err)
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

	fromAddress, _, err := pdpVerifier.GetDataSetStorageProvider(nil, big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to get default sender address: %w", err)
	}

	// Get the current tipset
	ts, err := n.fil.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}

	// Send the transaction
	reason := "pdp-proving-period"
	txHash, err := n.sender.Send(ctx, fromAddress, txEth, reason)
	if err != nil {
		currentHeight := int64(ts.Height())
		done, handleErr := HandleProvingSendError(ctx, n.db, dataSetId, currentHeight, err)
		if done {
			return true, nil
		}
		return false, xerrors.Errorf("failed to send transaction: %w", handleErr)
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

func (n *NextProvingPeriodTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (n *NextProvingPeriodTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPv0_ProvPeriod",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 3, // Set retry limit to 3 attempts
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
	}
}

func (n *NextProvingPeriodTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	n.addFunc.Set(taskFunc)
}

var _ = harmonytask.Reg(&NextProvingPeriodTask{})
