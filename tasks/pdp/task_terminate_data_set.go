package pdp

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
)

type DeleteDataSetTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH
}

func NewDeleteDataSetTask(db *harmonydb.DB, ethClient *ethclient.Client, sender *message.SenderETH) *DeleteDataSetTask {
	return &DeleteDataSetTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
	}
}

func (t *DeleteDataSetTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var dataSetId int64
	err = t.db.QueryRow(ctx, `SELECT id FROM pdp_delete_data_set WHERE delete_data_set_task_id = $1`, taskID).Scan(&dataSetId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return false, xerrors.Errorf("failed to select data set: %w", err)
	}

	sender, err := getPDPOwner(ctx, t.db)
	if err != nil {
		return false, xerrors.Errorf("failed to get pdp owner: %w", err)
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, t.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	live, err := verifier.DataSetLive(&bind.CallOpts{Context: ctx}, big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to check if data set is live: %w", err)
	}

	if !live {
		n, err := t.db.Exec(ctx, `UPDATE pdp_delete_data_set SET 
                               after_delete_data_set = TRUE,
                               delete_data_set_task_id = NULL,
                               terminated  = TRUE
                           WHERE terminate_service_task_id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
		}

		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 row but got %d", n)
		}

		return true, nil
	}

	pdpABi, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	data, err := pdpABi.Pack("deleteDataSet", big.NewInt(dataSetId), []byte{})
	if err != nil {
		return false, xerrors.Errorf("failed to pack data: %w", err)
	}

	txEth := types.NewTransaction(
		0,
		pdpAddress,
		big.NewInt(0),
		0,
		nil,
		data,
	)

	txHash, err := t.sender.Send(ctx, sender, txEth, "pdp-terminate-data-set")
	if err != nil {
		return false, xerrors.Errorf("failed to send transaction: %w", err)
	}

	n, err := t.db.Exec(ctx, `UPDATE pdp_delete_data_set SET 
                               delete_tx_hash = $2, 
                               after_delete_data_set = TRUE,
                               delete_data_set_task_id = NULL
                           WHERE terminate_service_task_id = $1`, taskID, txHash.Hex())
	if err != nil {
		return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
	}

	if n != 1 {
		return false, xerrors.Errorf("expected to update 1 row but got %d", n)
	}

	return true, nil
}

func (t *DeleteDataSetTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (t *DeleteDataSetTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "DeleteDataSet",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(time.Hour, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *DeleteDataSetTask) schedule(ctx context.Context, addTaskFunc harmonytask.AddTaskFunc) error {
	var stop bool

	for !stop {
		addTaskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true

			current, err := t.ethClient.BlockNumber(ctx)
			if err != nil {
				return false, xerrors.Errorf("failed to get current block number: %w", err)
			}

			var pendings []struct {
				ID               int64 `db:"id"`
				TerminationEpoch int64 `db:"termination_epoch"`
			}

			err = tx.Select(&pendings, `SELECT id, 
       												termination_epoch 
											FROM pdp_delete_data_set 
											WHERE delete_data_set_task_id IS NULL 
											  AND after_delete_data_set = FALSE 
											  AND termination_epoch IS NOT NULL
											  AND termination_epoch >= $1`, current)

			if err != nil {
				return false, xerrors.Errorf("failed to select pending data sets: %w", err)
			}

			if len(pendings) == 0 {
				log.Debugw("no pending data sets to terminate")
				return false, nil
			}

			pending := pendings[0]

			n, err := tx.Exec(`UPDATE pdp_delete_data_set 
									SET terminate_service_task_id = $1 
									WHERE id = $2 
									  AND delete_data_set_task_id IS NULL 
									  AND after_delete_data_set = FALSE
									  AND after_terminate_service = TRUE`, taskID, pending)

			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
			}

			if n != 1 {
				return false, xerrors.Errorf("updated %d rows", n)
			}

			log.Debugw("scheduled terminate data set task", "dataSetID", pending)

			stop = false
			return true, nil
		})
	}
	return nil
}

func (t *DeleteDataSetTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&DeleteDataSetTask{})
var _ harmonytask.TaskInterface = &DeleteDataSetTask{}
