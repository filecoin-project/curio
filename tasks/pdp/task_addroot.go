package pdp

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"

	types2 "github.com/filecoin-project/lotus/chain/types"
)

type PDPServiceNodeApi interface {
	ChainHead(ctx context.Context) (*types2.TipSet, error)
}

type PDPTaskAddRoot struct {
	db        *harmonydb.DB
	sender    *message.SenderETH
	ethClient *ethclient.Client
}

func NewPDPTaskAddRoot(db *harmonydb.DB, sender *message.SenderETH, ethClient *ethclient.Client) *PDPTaskAddRoot {
	return &PDPTaskAddRoot{
		db:        db,
		sender:    sender,
		ethClient: ethClient,
	}
}

func (p *PDPTaskAddRoot) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var addRoots []struct {
		ID         string `db:"id"`
		PieceCid   string `db:"piece_cid"`
		ProofSetID int64  `db:"proof_set_id"`
		ExtraData  []byte `db:"extra_data"`
		PieceRef   string `db:"piece_ref"`
	}

	err = p.db.Select(ctx, &addRoots, `SELECT id, piece_cid, proof_set_id, extra_data, piece_ref FROM pdp_pipeline WHERE add_root_task_id = $1 AND after_add_root = FALSE`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to select addRoot: %w", err)
	}

	if len(addRoots) == 0 {
		return false, xerrors.Errorf("no addRoot found for taskID %d", taskID)
	}

	if len(addRoots) > 0 {
		return false, xerrors.Errorf("multiple addRoot found for taskID %d", taskID)
	}

	addRoot := addRoots[0]

	pcid, err := cid.Parse(addRoot.PieceCid)
	if err != nil {
		return false, xerrors.Errorf("failed to parse piece cid: %w", err)
	}

	pi, err := mk20.GetPieceInfo(pcid)
	if err != nil {
		return false, xerrors.Errorf("failed to get piece info: %w", err)
	}

	// Prepare the Ethereum transaction data outside the DB transaction
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("getting PDPVerifier ABI: %w", err)
	}

	rootDataArray := []contract.RootData{
		{
			Root:    struct{ Data []byte }{Data: pcid.Bytes()},
			RawSize: new(big.Int).SetUint64(pi.RawSize),
		},
	}

	proofSetID := new(big.Int).SetUint64(uint64(addRoot.ProofSetID))

	// Prepare the Ethereum transaction
	// Pack the method call data
	// The extraDataBytes variable is now correctly populated above
	data, err := abiData.Pack("addRoots", proofSetID, rootDataArray, addRoot.ExtraData)
	if err != nil {
		return false, xerrors.Errorf("packing data: %w", err)
	}

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	pdpContracts := contract.ContractAddresses()
	pdpVerifierAddress := pdpContracts.PDPVerifier

	pdpVerifier, err := contract.NewPDPVerifier(pdpVerifierAddress, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract at %s: %w", pdpVerifierAddress.Hex(), err)
	}

	// Get the sender address for this proofset
	owner, _, err := pdpVerifier.GetProofSetOwner(callOpts, proofSetID)
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
	reason := "pdp-addroots"
	txHash, err := p.sender.Send(ctx, owner, txEth, reason)
	if err != nil {
		return false, xerrors.Errorf("sending transaction: %w", err)
	}

	// Insert into message_waits_eth and pdp_proofset_roots
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		_, err = tx.Exec(`
          INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
          VALUES ($1, $2)
      `, txHash.Hex(), "pending")
		if err != nil {
			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
		}

		// Update proof set for initialization upon first add
		_, err = tx.Exec(`
			UPDATE pdp_proof_sets SET init_ready = true
			WHERE id = $1 AND prev_challenge_request_epoch IS NULL AND challenge_request_msg_hash IS NULL AND prove_at_epoch IS NULL
			`, proofSetID.Uint64())
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
		}

		// Insert into pdp_proofset_roots
		n, err := tx.Exec(`
                  INSERT INTO pdp_proofset_root (
                      proofset,
                      piece_cid_v2,
					  piece_cid,
					  piece_size,
					  raw_size,
                      piece_ref,
                      add_deal_id,
                      add_message_hash
                  )
                  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
              `,
			proofSetID.Uint64(),
			pcid.String(),
			pi.PieceCIDV1.String(),
			pi.Size,
			pi.RawSize,
			addRoot.PieceRef,
			addRoot.ID,
			txHash.Hex(),
		)
		if err != nil {
			return false, xerrors.Errorf("failed to insert into pdp_proofset_root: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("incorrect number of rows inserted for pdp_proofset_root: %d", n)
		}

		n, err = tx.Exec(`UPDATE pdp_pipeline SET 
								after_add_root = TRUE, 
								add_root_task_id = NULL,
								add_message_hash = $2,
							WHERE add_root_task_id = $1`, taskID, txHash.Hex())
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

func (p *PDPTaskAddRoot) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (p *PDPTaskAddRoot) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PDPAddRoot",
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

func (p *PDPTaskAddRoot) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var did string
			err := tx.QueryRow(`SELECT id FROM pdp_pipeline 
								  WHERE add_root_task_id IS NULL 
									AND after_add_root = FALSE 
									AND after_add_root_msg = FALSE
									AND aggregated = TRUE`).Scan(&did)
			if err != nil {
				if err == pgx.ErrNoRows {
					return false, nil
				}
				return false, xerrors.Errorf("failed to query pdp_pipeline: %w", err)
			}
			if did == "" {
				return false, xerrors.Errorf("no valid deal ID found for scheduling")
			}

			_, err = tx.Exec(`UPDATE pdp_pipeline SET add_root_task_id = $1, WHERE piece_cid = $2 AND after_add_root = FALSE AND after_add_root_msg = FALSE AND aggregated = TRUE`, id, did)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_pipeline: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (p *PDPTaskAddRoot) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPTaskAddRoot{}
var _ = harmonytask.Reg(&PDPTaskAddRoot{})
