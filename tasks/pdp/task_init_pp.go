package pdp

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type InitProvingPeriodTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH

	fil NextProvingPeriodTaskChainApi

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

type InitProvingPeriodTaskChainApi interface {
	ChainHead(context.Context) (*chainTypes.TipSet, error)
}

func NewInitProvingPeriodTask(db *harmonydb.DB, ethClient *ethclient.Client, fil NextProvingPeriodTaskChainApi, chainSched *chainsched.CurioChainSched, sender *message.SenderETH) *InitProvingPeriodTask {
	ipp := &InitProvingPeriodTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		fil:       fil,
	}

	_ = chainSched.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		if apply == nil {
			return nil
		}

		// Now query the db for proof sets needing nextProvingPeriod inital call
		var toCallInit []struct {
			ProofSetID int64 `db:"id"`
		}

		err := db.Select(ctx, &toCallInit, `
                SELECT id
                FROM pdp_proof_sets
                WHERE challenge_request_task_id IS NULL
                AND init_ready AND prove_at_epoch IS NULL
            `)
		if err != nil && err != sql.ErrNoRows {
			return xerrors.Errorf("failed to select proof sets needing nextProvingPeriod: %w", err)
		}

		for _, ps := range toCallInit {
			ipp.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Update pdp_proof_sets to set challenge_request_task_id = id
				affected, err := tx.Exec(`
                        UPDATE pdp_proof_sets
                        SET challenge_request_task_id = $1
                        WHERE id = $2 AND challenge_request_task_id IS NULL
                    `, id, ps.ProofSetID)
				if err != nil {
					return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
				}
				if affected == 0 {
					// Someone else might have already scheduled the task
					return false, nil
				}

				return true, nil
			})
		}

		return nil
	})

	return ipp
}

func (ipp *InitProvingPeriodTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Select the proof set where challenge_request_task_id = taskID
	var proofSetID int64

	err = ipp.db.QueryRow(ctx, `
        SELECT id
        FROM pdp_proof_sets
        WHERE challenge_request_task_id = $1 
    `, taskID).Scan(&proofSetID)
	if err == sql.ErrNoRows {
		// No matching proof set, task is done (something weird happened, and e.g another task was spawned in place of this one)
		return true, nil
	}
	if err != nil {
		return false, xerrors.Errorf("failed to query pdp_proof_sets: %w", err)
	}

	// Get the listener address for this proof set from the PDPVerifier contract
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ipp.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	listenerAddr, err := pdpVerifier.GetProofSetListener(nil, big.NewInt(proofSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get listener address for proof set %d: %w", proofSetID, err)
	}

	// Determine the next challenge window start by consulting the listener
	fmt.Printf("Listener addr: %v\n", listenerAddr.Hex())
	provingSchedule, err := contract.NewIPDPProvingSchedule(listenerAddr, ipp.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to create proving schedule binding, check that listener has proving schedule methods: %w", err)
	}

	period, err := provingSchedule.GetMaxProvingPeriod(&bind.CallOpts{Context: ctx})
	if err != nil {
		return false, xerrors.Errorf("failed to get proving period: %w", err)
	}

	// ChallengeWindow
	challengeWindow, err := provingSchedule.ChallengeWindow(&bind.CallOpts{Context: ctx})
	if err != nil {
		return false, xerrors.Errorf("failed to get challenge window: %w", err)
	}
	fmt.Printf("successfully got period %v and challengeWindow %v\n", period, challengeWindow)

	init_prove_at, err := provingSchedule.InitChallengeWindowStart(&bind.CallOpts{Context: ctx})
	if err != nil {
		return false, xerrors.Errorf("failed to get next challenge window start: %w", err)
	}
	init_prove_at = init_prove_at.Add(init_prove_at, challengeWindow) // we just need to be inside the challenge window so put next prove at the very end

	// Instantiate the PDPVerifier contract
	pdpContracts := contract.ContractAddresses()
	pdpVeriferAddress := pdpContracts.PDPVerifier

	// Prepare the transaction data
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	data, err := abiData.Pack("nextProvingPeriod", big.NewInt(proofSetID), init_prove_at, []byte{})
	if err != nil {
		return false, xerrors.Errorf("failed to pack data: %w", err)
	}

	// Prepare the transaction
	txEth := types.NewTransaction(
		0,                 // nonce (will be set by sender)
		pdpVeriferAddress, // to
		big.NewInt(0),     // value
		0,                 // gasLimit (to be estimated)
		nil,               // gasPrice (to be set by sender)
		data,              // data
	)

	if !stillOwned() {
		// Task was abandoned, don't send the transaction
		return false, nil
	}

	fromAddress, _, err := pdpVerifier.GetProofSetOwner(nil, big.NewInt(proofSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get default sender address: %w", err)
	}

	// Get the current tipset
	ts, err := ipp.fil.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}

	// Send the transaction
	reason := "pdp-proving-period"
	txHash, err := ipp.sender.Send(ctx, fromAddress, txEth, reason)
	if err != nil {
		return false, xerrors.Errorf("failed to send transaction: %w", err)
	}

	// Update the database in a transaction
	_, err = ipp.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Update pdp_proof_sets
		affected, err := tx.Exec(`
            UPDATE pdp_proof_sets
            SET challenge_request_msg_hash = $1,
                prev_challenge_request_epoch = $2,
				prove_at_epoch = $3
            WHERE id = $3
        `, txHash.Hex(), ts.Height(), init_prove_at.Uint64())
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
		}
		if affected == 0 {
			return false, xerrors.Errorf("pdp_proof_sets update affected 0 rows")
		}

		// Insert into message_waits_eth
		_, err = tx.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, 'pending') ON CONFLICT DO NOTHING
        `, txHash.Hex())
		if err != nil {
			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
		}

		return true, nil
	})
	if err != nil {
		return false, xerrors.Errorf("failed to perform database transaction: %w", err)
	}

	// Task completed successfully
	return true, nil
}

func (ipp *InitProvingPeriodTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (ipp *InitProvingPeriodTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPInitPP",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
	}
}

func (ipp *InitProvingPeriodTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	ipp.addFunc.Set(taskFunc)
}

var _ = harmonytask.Reg(&InitProvingPeriodTask{})
