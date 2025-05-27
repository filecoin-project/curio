package proofshare

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	proof2 "github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc"
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

// Do implements harmonytask.TaskInterface.
func (t *TaskProvideSnark) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// fetch by compute_task_id
	var tasks []struct {
		RequestCid string
	}

	err = t.db.Select(ctx, &tasks, "SELECT request_cid FROM proofshare_queue WHERE compute_task_id = $1", taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to fetch task: %w", err)
	}
	if len(tasks) == 0 {
		return false, xerrors.Errorf("no task found")
	}
	task := tasks[0]

	rcid, err := cid.Parse(task.RequestCid)
	if err != nil {
		return false, xerrors.Errorf("failed to parse request cid: %w", err)
	}

	requestData, err := proofsvc.GetProof(rcid)
	if err != nil {
		return false, xerrors.Errorf("failed to get proof: %w", err)
	}

	var request common.ProofData
	err = json.Unmarshal(requestData, &request)
	if err != nil {
		return false, xerrors.Errorf("failed to unmarshal request: %w", err)
	}

	proof, err := computeProof(ctx, taskID, request)
	if err != nil {
		return false, xerrors.Errorf("failed to compute proof: %w", err)
	}

	err = request.CheckOutput(proof)
	if err != nil {
		return false, xerrors.Errorf("proof check failed: %w", err)
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

func computeProof(ctx context.Context, taskID harmonytask.TaskID, request common.ProofData) ([]byte, error) {
	if request.PoRep != nil {
		if request.SectorID == nil {
			return nil, xerrors.Errorf("sector id is required")
		}

		return computePoRep(ctx, request.PoRep, *request.SectorID)
	}

	if request.Snap != nil {
		if request.SectorID == nil {
			return nil, xerrors.Errorf("sector id is required")
		}

		return computeSnap(ctx, taskID, request.Snap, *request.SectorID)
	}

	return nil, xerrors.Errorf("unknown proof request type")
}

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

func computeSnap(ctx context.Context, taskID harmonytask.TaskID, request *proof.Snap, sectorID abi.SectorID) ([]byte, error) {
	oldR, err := commcid.ReplicaCommitmentV1ToCID(request.OldR[:])
	if err != nil {
		return nil, xerrors.Errorf("invalid OldR: %w", err)
	}
	newR, err := commcid.ReplicaCommitmentV1ToCID(request.NewR[:])
	if err != nil {
		return nil, xerrors.Errorf("invalid NewR: %w", err)
	}
	newD, err := commcid.DataCommitmentV1ToCID(request.NewD[:])
	if err != nil {
		return nil, xerrors.Errorf("invalid NewD: %w", err)
	}

	ctx = ffiselect.WithLogCtx(ctx, "sector", sectorID, "task", taskID, "oldR", oldR, "newR", newR, "newD", newD)
	proof, err := ffiselect.FFISelect.GenerateUpdateProofWithVanilla(ctx, request.ProofType, oldR, newR, newD, request.Proofs)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate update proof: %w", err)
	}

	return proof, nil
}

var _ = harmonytask.Reg(&TaskProvideSnark{})
