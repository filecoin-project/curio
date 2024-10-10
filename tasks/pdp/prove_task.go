package pdp

import (
	"context"
	"database/sql"
	"log"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
)

type ProveTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

func NewProveTask(chainSched *chainsched.CurioChainSched, db *harmonydb.DB, ethClient *ethclient.Client) *ProveTask {
	pt := &ProveTask{
		db:        db,
		ethClient: ethClient,
	}

	err := chainSched.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		if apply == nil {
			return nil
		}

		for {
			more := false

			pt.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Select proof sets ready for proving
				var proofSets []struct {
					ID                 int64         `db:"id"`
					NextChallengeEpoch sql.NullInt64 `db:"next_challenge_epoch"`
				}

				err := tx.Select(&proofSets, `
                    SELECT id, next_challenge_epoch
                    FROM pdp_proof_sets
                    WHERE next_challenge_epoch IS NOT NULL
                      AND next_challenge_epoch <= $1
                      AND next_challenge_possible = TRUE
                    LIMIT 2
                `, apply.Height())
				if err != nil {
					return false, xerrors.Errorf("failed to select proof sets: %w", err)
				}

				if len(proofSets) == 0 {
					// No proof sets to process
					return false, nil
				}

				// Determine if there might be more proof sets to process
				more = len(proofSets) == 2

				// Process the first proof set
				todo := proofSets[0]

				// Insert a new task into pdp_prove_tasks
				affected, err := tx.Exec(`
                    INSERT INTO pdp_prove_tasks (proofset, challenge_epoch, task_id)
                    VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
                `, todo.ID, todo.NextChallengeEpoch.Int64, id)
				if err != nil {
					return false, xerrors.Errorf("failed to insert into pdp_prove_tasks: %w", err)
				}
				if affected == 0 {
					more = false
					return false, nil
				}

				// Update pdp_proof_sets to set next_challenge_possible = FALSE
				affected, err = tx.Exec(`
                    UPDATE pdp_proof_sets
                    SET next_challenge_possible = FALSE
                    WHERE id = $1 AND next_challenge_possible = TRUE
                `, todo.ID)
				if err != nil {
					return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
				}
				if affected == 0 {
					more = false
					return false, nil
				}

				return true, nil
			})

			if !more {
				break
			}
		}

		return nil
	})
	if err != nil {
		// Handler registration failed
		panic(err)
	}

	return pt
}

func (p *ProveTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Retrieve proof set and challenge epoch for the task
	var proofSetID int64
	var challengeEpoch int64

	err = p.db.QueryRow(context.Background(), `
        SELECT proofset, challenge_epoch
        FROM pdp_prove_tasks
        WHERE task_id = $1
    `, taskID).Scan(&proofSetID, &challengeEpoch)
	if err != nil {
		return false, xerrors.Errorf("failed to get task details: %w", err)
	}

	// (Stub) Query the contract and generate the proof
	// TODO: Implement actual contract interactions and proof generation
	log.Printf("Generating proof for proof set %d at challenge epoch %d", proofSetID, challengeEpoch)

	// Simulate proof generation delay
	// time.Sleep(time.Second * 5)

	// After proof is generated, update the database

	// Start a transaction
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Remove the task from pdp_prove_tasks
		_, err := tx.Exec(`
            DELETE FROM pdp_prove_tasks
            WHERE task_id = $1
        `, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to delete task: %w", err)
		}

		// Optionally, update pdp_proof_sets.next_challenge_epoch = NULL and next_challenge_possible = FALSE
		_, err = tx.Exec(`
            UPDATE pdp_proof_sets
            SET next_challenge_epoch = NULL, next_challenge_possible = FALSE
            WHERE id = $1
        `, proofSetID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return false, err
	}

	// Task completed successfully
	return true, nil
}

func (p *ProveTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	id := ids[0]
	return &id, nil
}

func (p *ProveTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPProve",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 128 << 20, // 128 MB
		},
		MaxFailures: 5,
	}
}

func (p *ProveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.addFunc.Set(taskFunc)
}

var _ = harmonytask.Reg(&ProveTask{})
var _ harmonytask.TaskInterface = &ProveTask{}
