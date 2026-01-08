package pdpv1

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
	"github.com/filecoin-project/curio/tasks/message"
)

type PDPv1TerminateFWSSTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH
}

func NewPDPv1TerminateFWSSTask(db *harmonydb.DB, ethClient *ethclient.Client, sender *message.SenderETH) *PDPv1TerminateFWSSTask {
	return &PDPv1TerminateFWSSTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
	}
}

func (t *PDPv1TerminateFWSSTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var dataSetId int64
	err = t.db.QueryRow(ctx, `SELECT set_id FROM pdp_data_set_delete WHERE terminate_service_task_id = $1`, taskID).Scan(&dataSetId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return false, xerrors.Errorf("failed to select data set: %w", err)
	}

	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(sAddr, t.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate FWSS service state view: %w", err)
	}

	ds, err := fwssv.GetDataSet(&bind.CallOpts{Context: ctx}, big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to get data set %d: %w", dataSetId, err)
	}

	if ds.PdpEndEpoch.Int64() != 0 {
		n, err := t.db.Exec(ctx, `UPDATE pdp_data_set_delete 
									SET after_terminate_service = TRUE,
									    terminate_service_task_id = NULL,
									    service_termination_epoch = $2
									WHERE terminate_service_task_id = $1`, taskID, ds.PdpEndEpoch.Int64())
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_data_set_delete: %w", err)
		}

		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 row but got %d", n)
		}
	}

	sender, err := getSenderAddress(ctx, t.db)
	if err != nil {
		return false, xerrors.Errorf("failed to get pdp owner: %w", err)
	}

	address := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService

	fwssABi, err := FWSS.FilecoinWarmStorageServiceMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get FWSS ABI: %w", err)
	}

	data, err := fwssABi.Pack("terminateService", big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to pack data: %w", err)
	}

	txEth := types.NewTransaction(
		0,
		address,
		big.NewInt(0),
		0,
		nil,
		data,
	)

	txHash, err := t.sender.Send(ctx, sender, txEth, "pdpv1-terminate-service")
	if err != nil {
		return false, xerrors.Errorf("failed to send transaction: %w", err)
	}

	n, err := t.db.Exec(ctx, `UPDATE pdp_data_set_delete 
									SET terminate_tx_hash = $2, 
									    after_terminate_service = TRUE,
									    terminate_service_task_id = NULL
									WHERE terminate_service_task_id = $1`, taskID, txHash.Hex())
	if err != nil {
		return false, xerrors.Errorf("failed to update pdp_data_set_delete: %w", err)
	}

	if n != 1 {
		return false, xerrors.Errorf("expected to update 1 row but got %d", n)
	}

	return true, nil
}

func (t *PDPv1TerminateFWSSTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (t *PDPv1TerminateFWSSTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "TerminateFWSS",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(time.Minute, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *PDPv1TerminateFWSSTask) schedule(ctx context.Context, addTaskFunc harmonytask.AddTaskFunc) error {
	var stop bool

	for !stop {
		addTaskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true

			var pending sql.NullString

			err := tx.QueryRow(`SELECT id 
									FROM pdp_data_set_delete 
									WHERE terminate_service_task_id IS NULL 
									  AND after_terminate_service = FALSE LIMIT 1`).Scan(&pending)

			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Debugw("no pending data sets to terminate service")
					return false, nil
				}
				return false, xerrors.Errorf("failed to select pending data sets: %w", err)
			}

			if !pending.Valid {
				log.Debugw("no pending data sets to terminate service")
				return false, nil
			}

			n, err := tx.Exec(`UPDATE pdp_data_set_delete SET terminate_service_task_id = $1 WHERE id = $2 AND terminate_service_task_id IS NULL AND after_terminate_service = FALSE`, taskID, pending.String)

			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_data_set_delete: %w", err)
			}

			if n != 1 {
				return false, xerrors.Errorf("updated %d rows", n)
			}

			log.Debugw("scheduled terminate service task", "dataSetId", pending)

			stop = false
			return true, nil
		})
	}
	return nil
}

func (t *PDPv1TerminateFWSSTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPv1TerminateFWSSTask{}
var _ = harmonytask.Reg(&PDPv1TerminateFWSSTask{})
