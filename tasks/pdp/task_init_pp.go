package pdp

import (
	"context"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type InitProvingPeriodTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH

	fil NextProvingPeriodTaskChainApi

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

type InitProvingPeriodTaskChainApi interface {
	ChainHead(context.Context) (*chainTypes.TipSet, error)
}

func NewInitProvingPeriodTask(db *harmonydb.DB, ethClient *ethclient.Client, fil NextProvingPeriodTaskChainApi, chainSched *chainsched.CurioChainSched, sender *message.SenderETH) *InitProvingPeriodTask {
	ipp := &InitProvingPeriodTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		fil:       fil,
	}

	_ = chainSched.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		if apply == nil {
			return nil
		}

		// Now query the db for data sets needing nextProvingPeriod initial call
		var toCallInit []struct {
			DataSetID int64 `db:"id"`
		}

		err := db.Select(ctx, &toCallInit, `
                SELECT id
                FROM pdp_data_set
                WHERE challenge_request_task_id IS NULL
                AND init_ready AND prove_at_epoch IS NULL AND removed = FALSE
            `)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return xerrors.Errorf("failed to select data sets needing nextProvingPeriod: %w", err)
		}

		for _, ps := range toCallInit {
			ipp.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Update pdp_data_set to set challenge_request_task_id = id
				affected, err := tx.Exec(`
                        UPDATE pdp_data_set
                        SET challenge_request_task_id = $1
                        WHERE id = $2 AND challenge_request_task_id IS NULL AND removed = FALSE
                    `, id, ps.DataSetID)
				if err != nil {
					return false, xerrors.Errorf("failed to update pdp_data_set: %w", err)
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

	return ipp
}

func (ipp *InitProvingPeriodTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Select the data set where challenge_request_task_id = taskID
	var dataSetID int64

	err = ipp.db.QueryRow(ctx, `
        SELECT id
        FROM pdp_data_set
        WHERE challenge_request_task_id = $1 
    `, taskID).Scan(&dataSetID)
	if errors.Is(err, pgx.ErrNoRows) {
		// No matching data set, task is done (something weird happened, and e.g. another task was spawned in place of this one)
		return true, nil
	}
	if err != nil {
		return false, xerrors.Errorf("failed to query pdp_data_set: %w", err)
	}

	// Get the listener address for this data set from the PDPVerifier contract
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ipp.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	// Check if the data set has any leaves (pieces) before attempting to initialize proving period
	leafCount, err := pdpVerifier.GetDataSetLeafCount(nil, big.NewInt(dataSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get leaf count for data set %d: %w", dataSetID, err)
	}
	if leafCount.Cmp(big.NewInt(0)) == 0 {
		// No leaves in the data set yet, skip initialization
		// Return done=false to retry later (the task will be retried by the scheduler)
		log.Warnf("no leaves in data set %d, skipping initialization with task %d", dataSetID, taskID)
		return false, nil
	}

	listenerAddr, err := pdpVerifier.GetDataSetListener(nil, big.NewInt(dataSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get listener address for data set %d: %w", dataSetID, err)
	}

	// Get the proving schedule from the listener (handles view contract indirection)
	provingSchedule, err := contract.GetProvingScheduleFromListener(listenerAddr, ipp.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to get proving schedule from listener: %w", err)
	}

	config, err := provingSchedule.GetPDPConfig(&bind.CallOpts{Context: ctx})
	if err != nil {
		return false, xerrors.Errorf("failed to get pdp config: %w", err)
	}

	// ChallengeWindow
	challengeWindow := big.NewInt(config.ChallengeWindow.Int64())

	init_prove_at := big.NewInt(config.InitChallengeWindowStart.Int64())
	buffer := challengeWindow.Uint64() / 2
	init_prove_at = init_prove_at.Add(init_prove_at, big.NewInt(int64(buffer))) // Give a buffer of 1/2 challenge window epochs so that we are still within challenge window
	// Instantiate the PDPVerifier contract
	pdpContracts := contract.ContractAddresses()
	pdpVeriferAddress := pdpContracts.PDPVerifier

	// Prepare the transaction data
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	data, err := abiData.Pack("nextProvingPeriod", big.NewInt(dataSetID), init_prove_at, []byte{})
	if err != nil {
		return false, xerrors.Errorf("failed to pack data: %w", err)
	}

	currentBlock, err := ipp.ethClient.BlockNumber(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get current block number: %w", err)
	}

	expectedInitWdStart := currentBlock + config.MaxProvingPeriod - config.ChallengeWindow.Uint64()
	expectedMid := expectedInitWdStart + config.ChallengeWindow.Uint64()/2

	// Prepare the transaction
	txEth := types.NewTransaction(
		0,                 // nonce (will be set by sender)
		pdpVeriferAddress, // to
		big.NewInt(0),     // value
		0,                 // gasLimit (to be estimated)
		nil,               // gasPrice (to be set by sender)
		data,              // data
	)

	if !stillOwned() {
		// Task was abandoned, don't send the transaction
		return false, nil
	}

	fromAddress, _, err := pdpVerifier.GetDataSetStorageProvider(nil, big.NewInt(dataSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get default sender address: %w", err)
	}

	// Get the current tipset
	ts, err := ipp.fil.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}

	// Send the transaction
	reason := "pdp-proving-init"
	txHash, err := ipp.sender.Send(ctx, fromAddress, txEth, reason)
	if err != nil {
		log.Errorf("failed to send transaction at %d, InitChallengeWindowStart %d, ChallengeWindow %d, MaxProvingPeriod %d, Buffer %d, InitReadyAt %d, ExpectedInitChallenge %d, ExpectedInitReadyAt %d: %w", currentBlock, config.InitChallengeWindowStart.Uint64(), config.ChallengeWindow.Uint64(), config.MaxProvingPeriod, buffer, init_prove_at.Uint64(), expectedInitWdStart, expectedMid, err)
		return false, xerrors.Errorf("failed to send transaction: %w", err)
	}

	txHashLower := strings.ToLower(txHash.Hex())

	// Update the database in a transaction
	_, err = ipp.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Update pdp_data_set
		affected, err := tx.Exec(`
            UPDATE pdp_data_set
            SET challenge_request_msg_hash = $1,
                prev_challenge_request_epoch = $2,
				prove_at_epoch = $3
            WHERE id = $4
        `, txHashLower, ts.Height(), init_prove_at.Uint64(), dataSetID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_data_set: %w", err)
		}
		if affected == 0 {
			return false, xerrors.Errorf("pdp_data_set update affected 0 rows")
		}

		// Insert into message_waits_eth
		_, err = tx.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, 'pending') ON CONFLICT DO NOTHING
        `, txHashLower)
		if err != nil {
			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
		}

		return true, nil
	})
	if err != nil {
		return false, xerrors.Errorf("failed to perform database transaction: %w", err)
	}

	// Task completed successfully
	return true, nil
}

func (ipp *InitProvingPeriodTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (ipp *InitProvingPeriodTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPInitPP",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 3, // Set retry limit to 3 attempts
	}
}

func (ipp *InitProvingPeriodTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	ipp.addFunc.Set(taskFunc)
}

var _ = harmonytask.Reg(&InitProvingPeriodTask{})
