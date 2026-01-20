package pdp

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
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
)

type PDPTaskDeleteDataSet struct {
	db        *harmonydb.DB
	sender    *message.SenderETH
	ethClient api.EthClientInterface
	filClient PDPServiceNodeApi
}

func NewPDPTaskDeleteDataSet(db *harmonydb.DB, sender *message.SenderETH, ethClient api.EthClientInterface, filClient PDPServiceNodeApi) *PDPTaskDeleteDataSet {
	return &PDPTaskDeleteDataSet{
		db:        db,
		sender:    sender,
		ethClient: ethClient,
		filClient: filClient,
	}
}

func (p *PDPTaskDeleteDataSet) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()
	var pdeletes []struct {
		SetID     int64  `db:"set_id"`
		ExtraData []byte `db:"extra_data"`
	}

	err = p.db.Select(ctx, &pdeletes, `SELECT set_id, extra_data FROM pdp_data_set_delete WHERE task_id = $1 AND tx_hash IS NULL`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to get task details from DB: %w", err)
	}

	if len(pdeletes) != 1 {
		return false, xerrors.Errorf("incorrect rows for dataset delete found for taskID %d", taskID)
	}

	pdelete := pdeletes[0]

	extraDataBytes := []byte{}

	dataSetID := new(big.Int).SetUint64(uint64(pdelete.SetID))

	if pdelete.ExtraData != nil {
		extraDataBytes = pdelete.ExtraData
	}

	pdpContracts := contract.ContractAddresses()
	pdpVerifierAddress := pdpContracts.PDPVerifier

	pdpVerifier, err := contract.NewPDPVerifier(pdpVerifierAddress, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract at %s: %w", pdpVerifierAddress.Hex(), err)
	}

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	// Get the sender address for this dataset
	owner, _, err := pdpVerifier.GetDataSetStorageProvider(callOpts, dataSetID)
	if err != nil {
		return false, xerrors.Errorf("failed to get owner: %w", err)
	}

	// Manually create the transaction without requiring a Signer
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("getting PDPVerifier ABI: %w", err)
	}

	// Pack the method call data
	data, err := abiData.Pack("deleteDataSet", dataSetID, extraDataBytes)
	if err != nil {
		return false, xerrors.Errorf("packing data: %w", err)
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	tx := types.NewTransaction(
		0,
		contract.ContractAddresses().PDPVerifier,
		big.NewInt(0),
		0,
		nil,
		data,
	)

	// Send the transaction using SenderETH
	reason := "pdp-delete-data-set"
	txHash, err := p.sender.Send(ctx, owner, tx, reason)
	if err != nil {
		return false, xerrors.Errorf("sending transaction: %w", err)
	}

	// Insert into message_waits_eth and pdp_data_set_delete
	txHashLower := strings.ToLower(txHash.Hex())

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE pdp_data_set_delete SET tx_hash = $1, task_id = NULL WHERE task_id = $2`, txHashLower, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_data_set_delete: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("incorrect number of rows updated for pdp_data_set_delete: %d", n)
		}

		_, err = tx.Exec(`INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, $2)`, txHashLower, "pending")
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

func (p *PDPTaskDeleteDataSet) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
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

			var did string
			err := tx.QueryRow(`SELECT id FROM pdp_data_set_delete WHERE task_id IS NULL AND tx_hash IS NULL LIMIT 1`).Scan(&did)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("failed to query pdp_data_set_delete: %w", err)
			}
			if did == "" {
				return false, xerrors.Errorf("no valid id found for taskID")
			}

			_, err = tx.Exec(`UPDATE pdp_data_set_delete SET task_id = $1 WHERE id = $2 AND tx_hash IS NULL`, id, did)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_data_set_delete: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (p *PDPTaskDeleteDataSet) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPTaskDeleteDataSet{}
var _ = harmonytask.Reg(&PDPTaskDeleteDataSet{})
