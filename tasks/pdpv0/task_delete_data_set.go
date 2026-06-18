package pdpv0

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

type DeleteDataSetTask struct {
	db        *harmonydb.DB
	ethClient api.EthClientInterface
	sender    *message.SenderETH
}

func NewDeleteDataSetTask(db *harmonydb.DB, ethClient api.EthClientInterface, sender *message.SenderETH) *DeleteDataSetTask {
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

	state, err := readDataSetCleanupState(ctx, t.ethClient, dataSetId)
	if err != nil {
		return false, xerrors.Errorf("failed to read PDP cleanup state: %w", err)
	}

	if state.Finalized() {
		if err := cleanupFinalizedDataSet(ctx, t.db, dataSetId); err != nil {
			return false, err
		}
		return true, nil
	}

	if state.CleanupMode {
		n, err := t.db.Exec(ctx, `UPDATE pdp_delete_data_set SET 
                               after_delete_data_set = TRUE,
                               delete_data_set_task_id = NULL,
                               delete_tx_hash = NULL
                           WHERE delete_data_set_task_id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
		}

		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 row but got %d", n)
		}

		return true, nil
	}

	if !state.Live {
		return false, xerrors.Errorf("data set %d is not live, in cleanup mode, or finalized", dataSetId)
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	pdpABi, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	data, err := pdpABi.Pack("deleteDataSet", big.NewInt(dataSetId), []byte{})
	if err != nil {
		return false, xerrors.Errorf("failed to pack data: %w", err)
	}

	// deleteDataSet is nonpayable; the cleanup deposit was prepaid at creation.
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

	comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE pdp_delete_data_set SET 
                               delete_tx_hash = $2, 
                               after_delete_data_set = TRUE,
                               delete_data_set_task_id = NULL
                           WHERE delete_data_set_task_id = $1`, taskID, txHash.Hex())
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
		}

		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 row but got %d", n)
		}

		_, err = tx.Exec(`INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, $2)`, txHash.Hex(), "pending")
		if err != nil {
			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("failed to commit transaction: %w", err)
	}

	if !comm {
		return false, xerrors.Errorf("failed to commit transaction")
	}

	return true, nil
}

func (t *DeleteDataSetTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *DeleteDataSetTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(15),
		Name: tasknames.PDPv0_DelDataSet,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(time.Minute*10, func(taskFunc harmonytask.AddTaskFunc) error {
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
				TerminationEpoch int64 `db:"service_termination_epoch"`
			}

			err = tx.Select(&pendings, `SELECT id,
       												service_termination_epoch
											FROM pdp_delete_data_set
											WHERE delete_data_set_task_id IS NULL
											  AND after_delete_data_set = FALSE
											  AND service_termination_epoch IS NOT NULL
											  AND service_termination_epoch <= $1
											  AND deletion_allowed = TRUE`, current)

			if err != nil {
				return false, xerrors.Errorf("failed to select pending data sets: %w", err)
			}

			if len(pendings) == 0 {
				log.Debugw("no pending data sets to terminate")
				return false, nil
			}

			pending := pendings[0]

			n, err := tx.Exec(`UPDATE pdp_delete_data_set
									SET delete_data_set_task_id = $1
									WHERE id = $2
									  AND delete_data_set_task_id IS NULL
									  AND after_delete_data_set = FALSE
									  AND after_terminate_service = TRUE
									  AND deletion_allowed = TRUE`, taskID, pending.ID)

			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
			}

			if n != 1 {
				return false, xerrors.Errorf("updated %d rows", n)
			}

			log.Debugw("scheduled terminate data set task", "dataSetId", pending.ID)

			stop = false
			return true, nil
		})
	}
	return nil
}

func (t *DeleteDataSetTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&DeleteDataSetTask{})
var _ harmonytask.TaskInterface = &DeleteDataSetTask{}
