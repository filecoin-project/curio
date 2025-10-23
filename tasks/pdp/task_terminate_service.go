package pdp

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
)

type TerminateServiceTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH
}

func NewTerminateServiceTask(db *harmonydb.DB, ethClient *ethclient.Client, sender *message.SenderETH) *TerminateServiceTask {
	return &TerminateServiceTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
	}
}

func (t *TerminateServiceTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var dataSetId int64
	err = t.db.QueryRow(ctx, `SELECT id FROM pdp_delete_data_set WHERE terminate_service_task_id = $1`, taskID).Scan(&dataSetId)
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

	txHash, err := t.sender.Send(ctx, sender, txEth, "pdp-terminate-service")
	if err != nil {
		return false, xerrors.Errorf("failed to send transaction: %w", err)
	}

	n, err := t.db.Exec(ctx, `UPDATE pdp_delete_data_set 
									SET terminate_tx_hash = $2, 
									    after_terminate_service = TRUE,
									    terminate_service_task_id = NULL
									WHERE terminate_service_task_id = $1`, taskID, txHash.Hex())
	if err != nil {
		return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
	}

	if n != 1 {
		return false, xerrors.Errorf("expected to update 1 row but got %d", n)
	}

	return true, nil
}

func (t *TerminateServiceTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (t *TerminateServiceTask) TypeDetails() harmonytask.TaskTypeDetails {
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

func (t *TerminateServiceTask) schedule(ctx context.Context, addTaskFunc harmonytask.AddTaskFunc) error {
	var stop bool

	for !stop {
		addTaskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true

			var pendings []int64

			err := tx.Select(&pendings, `SELECT id FROM pdp_delete_data_set WHERE terminate_service_task_id IS NULL AND after_terminate_service = FALSE`)

			if err != nil {
				return false, xerrors.Errorf("failed to select pending data sets: %w", err)
			}

			if len(pendings) == 0 {
				log.Debugw("no pending data sets to terminate service")
				return false, nil
			}

			pending := pendings[0]

			n, err := tx.Exec(`UPDATE pdp_delete_data_set SET terminate_service_task_id = $1 WHERE id = $2 AND terminate_service_task_id IS NULL AND after_terminate_service = FALSE`, taskID, pending)

			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
			}

			if n != 1 {
				return false, xerrors.Errorf("updated %d rows", n)
			}

			log.Debugw("scheduled terminate service task", "dataSetID", pending)

			stop = false
			return true, nil
		})
	}
	return nil
}

func (t *TerminateServiceTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &TerminateServiceTask{}
var _ = harmonytask.Reg(&TerminateServiceTask{})

func getPDPOwner(ctx context.Context, db *harmonydb.DB) (common.Address, error) {
	var owner string
	err := db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&owner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.Address{}, xerrors.Errorf("no sender address with role 'pdp' found")
		}
		return common.Address{}, xerrors.Errorf("failed to get pdp owner: %w", err)
	}
	return common.HexToAddress(owner), nil
}
