package pdp

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ipfs/go-cid"
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

	types2 "github.com/filecoin-project/lotus/chain/types"
)

type PDPServiceNodeApi interface {
	ChainHead(ctx context.Context) (*types2.TipSet, error)
}

type PDPTaskAddPiece struct {
	db        *harmonydb.DB
	sender    *message.SenderETH
	ethClient api.EthClientInterface
}

func NewPDPTaskAddPiece(db *harmonydb.DB, sender *message.SenderETH, ethClient api.EthClientInterface) *PDPTaskAddPiece {
	return &PDPTaskAddPiece{
		db:        db,
		sender:    sender,
		ethClient: ethClient,
	}
}

func (p *PDPTaskAddPiece) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var addPieces []struct {
		ID        string `db:"id"`
		PieceCid2 string `db:"piece_cid_v2"`
		DataSetID int64  `db:"data_set_id"`
		ExtraData []byte `db:"extra_data"`
		PieceRef  string `db:"piece_ref"`
	}

	err = p.db.Select(ctx, &addPieces, `SELECT id, piece_cid_v2, data_set_id, extra_data, piece_ref FROM pdp_pipeline WHERE add_piece_task_id = $1 AND after_add_piece = FALSE`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to select add piece: %w", err)
	}

	if len(addPieces) == 0 {
		return false, xerrors.Errorf("no add piece found for taskID %d", taskID)
	}

	if len(addPieces) > 1 {
		return false, xerrors.Errorf("multiple add piece found for taskID %d", taskID)
	}

	addPiece := addPieces[0]

	pcid2, err := cid.Parse(addPiece.PieceCid2)
	if err != nil {
		return false, xerrors.Errorf("failed to parse piece cid: %w", err)
	}

	// Prepare the Ethereum transaction data outside the DB transaction
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("getting PDPVerifier ABI: %w", err)
	}

	pieceDataArray := []contract.CidsCid{
		{
			Data: pcid2.Bytes(),
		},
	}

	dataSetID := new(big.Int).SetUint64(uint64(addPiece.DataSetID))

	// Prepare the Ethereum transaction
	// Pack the method call data
	// The extraDataBytes variable is now correctly populated above
	data, err := abiData.Pack("addPieces", dataSetID, pieceDataArray, addPiece.ExtraData)
	if err != nil {
		return false, xerrors.Errorf("packing data: %w", err)
	}

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	pdpVerifierAddress := contract.ContractAddresses().PDPVerifier

	pdpVerifier, err := contract.NewPDPVerifier(pdpVerifierAddress, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract at %s: %w", pdpVerifierAddress.Hex(), err)
	}

	// Get the sender address for this dataset
	owner, _, err := pdpVerifier.GetDataSetStorageProvider(callOpts, dataSetID)
	if err != nil {
		return false, xerrors.Errorf("failed to get owner: %w", err)
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	txEth := types.NewTransaction(
		0,
		contract.ContractAddresses().PDPVerifier,
		big.NewInt(0),
		0,
		nil,
		data,
	)

	// Send the transaction using SenderETH
	reason := "pdp-add-piece"
	txHash, err := p.sender.Send(ctx, owner, txEth, reason)
	if err != nil {
		return false, xerrors.Errorf("sending transaction: %w", err)
	}

	txHashLower := strings.ToLower(txHash.Hex())

	// Insert into message_waits_eth and pdp_dataset_piece
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		_, err = tx.Exec(`
          INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
          VALUES ($1, $2)
      `, txHashLower, "pending")
		if err != nil {
			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
		}

		n, err := tx.Exec(`UPDATE pdp_pipeline SET 
								after_add_piece = TRUE, 
								add_piece_task_id = NULL,
								add_message_hash = $2
							WHERE add_piece_task_id = $1`, taskID, txHashLower)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("incorrect number of rows updated for pdp_pipeline: %d", n)
		}

		// Return true to commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("failed to save details to DB: %w", err)
	}
	return true, nil
}

func (p *PDPTaskAddPiece) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (p *PDPTaskAddPiece) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PDPAddPiece",
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

func (p *PDPTaskAddPiece) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var did string
			err := tx.QueryRow(`SELECT id FROM pdp_pipeline 
								  WHERE add_piece_task_id IS NULL 
									AND after_add_piece = FALSE 
									AND after_add_piece_msg = FALSE
									AND aggregated = TRUE
									LIMIT 1`).Scan(&did)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("failed to query pdp_pipeline: %w", err)
			}
			if did == "" {
				return false, xerrors.Errorf("no valid deal ID found for scheduling")
			}

			_, err = tx.Exec(`UPDATE pdp_pipeline SET add_piece_task_id = $1 WHERE id = $2 AND after_add_piece = FALSE AND after_add_piece_msg = FALSE AND aggregated = TRUE`, id, did)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_pipeline: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (p *PDPTaskAddPiece) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPTaskAddPiece{}
var _ = harmonytask.Reg(&PDPTaskAddPiece{})
