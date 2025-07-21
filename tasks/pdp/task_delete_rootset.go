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

type PDPTaskDeleteProofSet struct {
	db        *harmonydb.DB
	sender    *message.SenderETH
	ethClient *ethclient.Client
	filClient PDPServiceNodeApi
}

func NewPDPTaskDeleteProofSet(db *harmonydb.DB, sender *message.SenderETH, ethClient *ethclient.Client, filClient PDPServiceNodeApi) *PDPTaskDeleteProofSet {
	return &PDPTaskDeleteProofSet{
		db:        db,
		sender:    sender,
		ethClient: ethClient,
		filClient: filClient,
	}
}

func (p *PDPTaskDeleteProofSet) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()
	var pdeletes []struct {
		SetID     int64  `db:"set_id"`
		ExtraData []byte `db:"extra_data"`
	}

	err = p.db.Select(ctx, &pdeletes, `SELECT set_id, extra_data FROM pdp_proof_set_create WHERE task_id = $1 AND tx_hash IS NULL`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to get task details from DB: %w", err)
	}

	if len(pdeletes) != 0 {
		return false, xerrors.Errorf("incorrect rows for proofset delete found for taskID %d", taskID)
	}

	pdelete := pdeletes[0]

	extraDataBytes := []byte{}

	proofSetID := new(big.Int).SetUint64(uint64(pdelete.SetID))

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

	// Get the sender address for this proofset
	owner, _, err := pdpVerifier.GetProofSetOwner(callOpts, proofSetID)
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
	data, err := abiData.Pack("deleteProofSet", proofSetID, extraDataBytes)
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
	reason := "pdp-rmproofset"
	txHash, err := p.sender.Send(ctx, owner, tx, reason)
	if err != nil {
		return false, xerrors.Errorf("sending transaction: %w", err)
	}

	// Insert into message_waits_eth and pdp_proof_set_delete
	txHashLower := strings.ToLower(txHash.Hex())
	n, err := p.db.Exec(ctx, `UPDATE pdp_proof_set_delete SET tx_hash = $1, task_id = NULL WHERE task_id = $2`, txHashLower, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to update pdp_proof_set_delete: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("incorrect number of rows updated for pdp_proof_set_delete: %d", n)
	}
	return true, nil
}

func (p *PDPTaskDeleteProofSet) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (p *PDPTaskDeleteProofSet) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PDPAddProofSet",
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

func (p *PDPTaskDeleteProofSet) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var did string
			err := tx.QueryRow(`SELECT id FROM pdp_proof_set_delete WHERE task_id IS NULL AND tx_hash IS NULL`).Scan(&id)
			if err != nil {
				return false, xerrors.Errorf("failed to query pdp_proof_set_delete: %w", err)
			}
			if did == "" {
				return false, xerrors.Errorf("no valid id found for taskID")
			}

			_, err = tx.Exec(`UPDATE pdp_proof_set_delete SET task_id = $1 WHERE id = $2 AND tx_hash IS NULL`, id, did)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_proof_set_delete: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (p *PDPTaskDeleteProofSet) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPTaskDeleteProofSet{}
