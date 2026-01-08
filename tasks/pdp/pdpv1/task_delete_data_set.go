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
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

type PDPTaskDeleteDataSet struct {
	db        *harmonydb.DB
	sender    *message.SenderETH
	ethClient *ethclient.Client
}

func NewPDPTaskDeleteDataSet(db *harmonydb.DB, sender *message.SenderETH, ethClient *ethclient.Client) *PDPTaskDeleteDataSet {
	return &PDPTaskDeleteDataSet{
		db:        db,
		sender:    sender,
		ethClient: ethClient,
	}
}

func (p *PDPTaskDeleteDataSet) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var dataSetId int64
	err = p.db.QueryRow(ctx, `SELECT set_id FROM pdp_data_set_delete WHERE task_id = $1`, taskID).Scan(&dataSetId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return false, xerrors.Errorf("failed to select data set: %w", err)
	}

	sender, err := getSenderAddress(ctx, p.db)
	if err != nil {
		return false, xerrors.Errorf("failed to get pdp owner: %w", err)
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	live, err := verifier.DataSetLive(&bind.CallOpts{Context: ctx}, big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to check if data set is live: %w", err)
	}

	if !live {
		n, err := p.db.Exec(ctx, `UPDATE pdp_data_set_delete SET 
                               after_delete_data_set = TRUE,
                               task_id = NULL,
                               terminated  = TRUE
                           WHERE task_id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_data_set_delete: %w", err)
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

	txHash, err := p.sender.Send(ctx, sender, txEth, "pdpv1-terminate-data-set")
	if err != nil {
		return false, xerrors.Errorf("failed to send transaction: %w", err)
	}

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE pdp_data_set_delete SET 
                               tx_hash = $2, 
                               after_delete_data_set = TRUE,
                               task_id = NULL
                           WHERE task_id = $1`, taskID, txHash.Hex())
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_data_set_delete: %w", err)
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

func (p *PDPTaskDeleteDataSet) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (p *PDPTaskDeleteDataSet) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PDPDelDataSet",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(3*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return p.schedule(context.Background(), taskFunc)
		}),
	}
}

func (p *PDPTaskDeleteDataSet) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			current, err := p.ethClient.BlockNumber(ctx)
			if err != nil {
				return false, xerrors.Errorf("failed to get current block number: %w", err)
			}

			var did sql.NullString

			err = tx.QueryRow(`SELECT id 
									FROM pdp_data_set_delete 
									WHERE task_id IS NULL 
									  AND after_delete_data_set = FALSE 
									  AND service_termination_epoch IS NOT NULL
									  AND service_termination_epoch >= $1 LIMIT 1`, current).Scan(&did)

			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Debugw("no pending data sets to terminate")
					return false, nil
				}
				return false, xerrors.Errorf("failed to select pending data sets: %w", err)
			}

			if !did.Valid {
				log.Debugw("no pending data sets to terminate")
				return false, nil
			}

			n, err := tx.Exec(`UPDATE pdp_data_set_delete 
									SET task_id = $1 
									WHERE id = $2 
									  AND task_id IS NULL 
									  AND after_delete_data_set = FALSE
									  AND after_terminate_service = TRUE`, id, did)

			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_data_set_delete: %w", err)
			}

			if n != 1 {
				return false, xerrors.Errorf("updated %d rows", n)
			}

			log.Debugw("scheduled terminate data set task", "dataSetId", did.String)

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (p *PDPTaskDeleteDataSet) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPTaskDeleteDataSet{}
var _ = harmonytask.Reg(&PDPTaskDeleteDataSet{})
