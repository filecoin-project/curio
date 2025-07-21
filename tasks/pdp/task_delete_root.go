package pdp

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

type PDPTaskDeleteRoot struct {
	db        *harmonydb.DB
	sender    *message.SenderETH
	ethClient *ethclient.Client
}

func (p *PDPTaskDeleteRoot) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var rdeletes []struct {
		ID        string  `db:"id"`
		SetID     int64   `db:"set_id"`
		Roots     []int64 `db:"roots"`
		ExtraData []byte  `db:"extra_data"`
	}

	err = p.db.Select(ctx, &rdeletes, `SELECT id, set_id, roots, extra_data FROM pdp_delete_root WHERE task_id = $1 AND tx_hash IS NULL`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to get task details from DB: %w", err)
	}

	if len(rdeletes) != 1 {
		return false, xerrors.Errorf("incorrect rows for delete root found for taskID %d", taskID)
	}

	rdelete := rdeletes[0]

	extraDataBytes := []byte{}

	if rdelete.ExtraData != nil {
		extraDataBytes = rdelete.ExtraData
	}

	proofSetID := new(big.Int).SetUint64(uint64(rdelete.SetID))

	pdpContracts := contract.ContractAddresses()
	pdpVerifierAddress := pdpContracts.PDPVerifier

	pdpVerifier, err := contract.NewPDPVerifier(pdpVerifierAddress, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract at %s: %w", pdpVerifierAddress.Hex(), err)
	}

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	// Get the sender address for this proofset
	owner, _, err := pdpVerifier.GetProofSetOwner(callOpts, proofSetID)
	if err != nil {
		return false, xerrors.Errorf("failed to get owner: %w", err)
	}

	var roots []*big.Int
	for _, root := range rdelete.Roots {
		roots = append(roots, new(big.Int).SetUint64(uint64(root)))
	}

	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("getting PDPVerifier ABI: %w", err)
	}

	// Pack the method call data
	data, err := abiData.Pack("scheduleRemovals", proofSetID, roots, extraDataBytes)
	if err != nil {
		return false, xerrors.Errorf("packing data: %w", err)
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	tx := types.NewTransaction(
		0,
		contract.ContractAddresses().PDPVerifier,
		contract.SybilFee(),
		0,
		nil,
		data,
	)

	// Send the transaction using SenderETH
	reason := "pdp-rmroot"
	txHash, err := p.sender.Send(ctx, owner, tx, reason)
	if err != nil {
		return false, xerrors.Errorf("sending transaction: %w", err)
	}

	// Insert into message_waits_eth and pdp_proof_set_delete
	txHashLower := strings.ToLower(txHash.Hex())
	n, err := p.db.Exec(ctx, `UPDATE pdp_delete_root SET tx_hash = $1, task_id = NULL WHERE task_id = $2`, txHashLower, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to update pdp_delete_root: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("incorrect number of rows updated for pdp_delete_root: %d", n)
	}

	return true, nil
}

func (p *PDPTaskDeleteRoot) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (p *PDPTaskDeleteRoot) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PDPDeleteRoot",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(5*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return p.schedule(context.Background(), taskFunc)
		}),
	}
}

func (p *PDPTaskDeleteRoot) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var did string
			err := tx.QueryRow(`SELECT id FROM pdp_delete_root 
								  WHERE task_id IS NULL 
									AND tx_hash IS NULL`).Scan(&did)
			if err != nil {
				return false, xerrors.Errorf("failed to query pdp_delete_root: %w", err)
			}
			if did == "" {
				return false, xerrors.Errorf("no valid deal ID found for scheduling")
			}

			_, err = tx.Exec(`UPDATE pdp_delete_root SET task_id = $1, WHERE id = $2 AND task_id IS NULL AND tx_hash IS NULL`, id, did)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_delete_root: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (p *PDPTaskDeleteRoot) Adder(taskFunc harmonytask.AddTaskFunc) {}

func NewPDPTaskDeleteRoot(db *harmonydb.DB, sender *message.SenderETH, ethClient *ethclient.Client) *PDPTaskDeleteRoot {
	return &PDPTaskDeleteRoot{
		db:        db,
		sender:    sender,
		ethClient: ethClient,
	}
}

var _ harmonytask.TaskInterface = &PDPTaskDeleteRoot{}
