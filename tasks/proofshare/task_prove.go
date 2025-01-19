package proofshare

import (
	"context"
	"encoding/json"
	"time"

	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	proof2 "github.com/filecoin-project/go-state-types/proof"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/tasks/seal"
)

type TaskProvideSnark struct {
	db          *harmonydb.DB
	paramsReady func() (bool, error)

	max int
}

func NewTaskProvideSnark(db *harmonydb.DB, paramck func() (bool, error), max int) *TaskProvideSnark {
	return &TaskProvideSnark{
		db:          db,
		paramsReady: paramck,
		max:         max,
	}
}

// Adder implements harmonytask.TaskInterface.
func (t *TaskProvideSnark) Adder(add harmonytask.AddTaskFunc) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		for range ticker.C {
			add(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Find unprocessed proofs to compute
				var serviceID int64
				err := tx.QueryRow(`
					SELECT service_id
					FROM proofshare_queue q
					WHERE compute_done = false AND compute_task_id IS NULL
					LIMIT 1
				`).Scan(&serviceID)
				if err == pgx.ErrNoRows {
					return false, nil
				}
				if err != nil {
					return false, xerrors.Errorf("failed to query queue: %w", err)
				}

				// Create task
				err = tx.QueryRow(`
					UPDATE proofshare_queue 
					SET compute_task_id = $1
					WHERE service_id = $2
					RETURNING service_id
				`, taskID, serviceID).Scan(&serviceID)
				if err != nil {
					return false, xerrors.Errorf("failed to update queue: %w", err)
				}

				return true, nil
			})
		}
	}()
}

// CanAccept implements harmonytask.TaskInterface.
func (t *TaskProvideSnark) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	rdy, err := t.paramsReady()
	if err != nil {
		return nil, xerrors.Errorf("failed to setup params: %w", err)
	}
	if !rdy {
		log.Infow("PoRepTask.CanAccept() params not ready, not scheduling")
		return nil, nil
	}
	// todo sort by priority

	id := ids[0]
	return &id, nil
}

/*
CREATE TABLE proofshare_queue (
    service_id BIGINT NOT NULL,

    obtained_at TIMESTAMPZ NOT NULL,

    request_data JSONB NOT NULL,
    response_data JSONB,

    compute_task_id BIGINT,
    compute_done BOOLEAN NOT NULL DEFAULT FALSE,

    submit_task_id BIGINT,
    submit_done BOOLEAN NOT NULL DEFAULT FALSE,

    PRIMARY KEY (service_id, obtained_at)
);

CREATE TABLE proofshare_meta (
    singleton BOOLEAN NOT NULL DEFAULT TRUE CHECK (singleton = TRUE) UNIQUE,

    enabled BOOLEAN NOT NULL DEFAULT FALSE,

    wallet TEXT,

    request_task_id BIGINT,

    PRIMARY KEY (singleton)
);

INSERT INTO proofshare_meta (singleton, enabled, wallet) VALUES (TRUE, FALSE, NULL);
*/

// Do implements harmonytask.TaskInterface.
func (t *TaskProvideSnark) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// fetch by compute_task_id
	var tasks []struct {
		RequestData json.RawMessage
	}

	err = t.db.Select(ctx, &tasks, "SELECT request_data FROM proofshare_queue WHERE compute_task_id = $1", taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to fetch task: %w", err)
	}
	if len(tasks) == 0 {
		return false, xerrors.Errorf("no task found")
	}
	task := tasks[0]

	var request common.ProofRequest
	err = json.Unmarshal(task.RequestData, &request)
	if err != nil {
		return false, xerrors.Errorf("failed to unmarshal request: %w", err)
	}

	proof, err := computeProof(ctx, request)
	if err != nil {
		return false, xerrors.Errorf("failed to compute proof: %w", err)
	}

	// store proof, mark as done
	n, err := t.db.Exec(ctx, "UPDATE proofshare_queue SET response_data = $1, compute_done = TRUE, compute_task_id = NULL WHERE compute_task_id = $2", proof, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to store proof: %w", err)
	}
	if n == 0 {
		return false, xerrors.Errorf("no task found")
	}

	return true, nil
}

// TypeDetails implements harmonytask.TaskInterface.
func (t *TaskProvideSnark) TypeDetails() harmonytask.TaskTypeDetails {
	gpu := 1.0
	if seal.IsDevnet {
		gpu = 0
	}
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(t.max),
		Name: "PSProve",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: gpu,
			Ram: 50 << 30, // todo correct value
		},
		MaxFailures: 5,
		RetryWait: func(retries int) time.Duration {
			return time.Second * 10 * time.Duration(retries)
		},
	}
}

func computeProof(ctx context.Context, request common.ProofRequest) ([]byte, error) {
	if request.PoRep != nil {
		if request.SectorID == nil {
			return nil, xerrors.Errorf("sector id is required")
		}

		return computePoRep(ctx, request.PoRep, *request.SectorID)
	}
	return nil, xerrors.Errorf("unknown proof request type")
}

/*
	type Commit1OutRaw struct {
		CommD           Commitment                `json:"comm_d"`
		CommR           Commitment                `json:"comm_r"`
		RegisteredProof StringRegisteredProofType `json:"registered_proof"`
		ReplicaID       Commitment                `json:"replica_id"`
		Seed            Ticket                    `json:"seed"`
		Ticket          Ticket                    `json:"ticket"`

		// ProofType -> [partitions] -> [challenge_index?] -> Proof
		VanillaProofs map[StringRegisteredProofType][][]VanillaStackedProof `json:"vanilla_proofs"`
	}

	type SealVerifyInfo struct {
		SealProof abi.RegisteredSealProof
		abi.SectorID
		Randomness            abi.SealRandomness
		InteractiveRandomness abi.InteractiveSealRandomness
		Proof                 []byte

		// Safe because we get those from the miner actor
		SealedCID   cid.Cid `checked:"true"` // CommR
		UnsealedCID cid.Cid `checked:"true"` // CommD
	}
*/
func computePoRep(ctx context.Context, request *proof.Commit1OutRaw, sectorID abi.SectorID) ([]byte, error) {
	// Serialize the commit1out to JSON to pass as vanilla proof
	vproof, err := json.Marshal(request)
	if err != nil {
		return nil, xerrors.Errorf("marshaling vanilla proof: %w", err)
	}

	ctx = ffiselect.WithLogCtx(ctx, "sector", sectorID)

	proof, err := ffiselect.FFISelect.SealCommitPhase2(ctx, vproof, sectorID.Number, sectorID.Miner)
	if err != nil {
		return nil, xerrors.Errorf("computing seal proof failed: %w", err)
	}

	commR, err := commcid.ReplicaCommitmentV1ToCID(request.CommR[:])
	if err != nil {
		return nil, xerrors.Errorf("invalid CommR: %w", err)
	}
	commD, err := commcid.DataCommitmentV1ToCID(request.CommD[:])
	if err != nil {
		return nil, xerrors.Errorf("invalid CommD: %w", err)
	}

	spt, err := request.RegisteredProof.ToABI()
	if err != nil {
		return nil, xerrors.Errorf("invalid registered proof: %w", err)
	}

	ok, err := ffi.VerifySeal(proof2.SealVerifyInfo{
		SealProof:             spt,
		SectorID:              sectorID,
		DealIDs:               nil,
		Randomness:            abi.SealRandomness(request.Ticket[:]),
		InteractiveRandomness: abi.InteractiveSealRandomness(request.Seed[:]),
		Proof:                 proof,
		SealedCID:             commR,
		UnsealedCID:           commD,
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to verify proof: %w", err)
	}
	if !ok {
		return nil, xerrors.Errorf("porep failed to validate")
	}

	return proof, nil
}

var _ = harmonytask.Reg(&TaskProvideSnark{})
