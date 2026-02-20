package proofshare

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/filecoin-project/curio/lib/cuzk"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/tasks/seal"
)

const ProveAdderInterval = 10 * time.Second
const ProveCleanupInterval = 5 * time.Minute

type TaskProvideSnark struct {
	db          *harmonydb.DB
	paramsReady func() (bool, error)

	cuzkClient *cuzk.Client

	max int
}

func NewTaskProvideSnark(db *harmonydb.DB, paramck func() (bool, error), max int, cuzkClient *cuzk.Client) *TaskProvideSnark {
	t := &TaskProvideSnark{
		db:          db,
		paramsReady: paramck,
		max:         max,
		cuzkClient:  cuzkClient,
	}
	t.cleanupWorker()
	return t
}

func (t *TaskProvideSnark) cleanupAbandonedTasks() error {
	_, err := t.db.Exec(context.Background(), `
		DELETE FROM proofshare_queue
		WHERE compute_done = FALSE AND compute_task_id IS NOT NULL AND compute_task_id NOT IN (SELECT id FROM harmony_task)
	`)
	return err
}

func (t *TaskProvideSnark) cleanupWorker() {
	ticker := time.NewTicker(ProveCleanupInterval)
	go func() {
		for range ticker.C {
			err := t.cleanupAbandonedTasks()
			if err != nil {
				log.Errorf("failed to cleanup abandoned tasks: %v", err)
			}
		}
	}()
}

// Adder implements harmonytask.TaskInterface.
func (t *TaskProvideSnark) Adder(add harmonytask.AddTaskFunc) {
	ticker := time.NewTicker(ProveAdderInterval)
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
				_, err = tx.Exec(`
					UPDATE proofshare_queue 
					SET compute_task_id = $1
					WHERE service_id = $2
				`, taskID, serviceID)
				if err != nil {
					return false, xerrors.Errorf("failed to update queue: %w", err)
				}

				return true, nil
			})
		}
	}()
}

// CanAccept implements harmonytask.TaskInterface.
func (t *TaskProvideSnark) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return []harmonytask.TaskID{}, nil
	}

	// When cuzk is enabled, delegate capacity checks to the daemon
	if t.cuzkClient != nil && t.cuzkClient.Enabled() {
		// PSProve handles both PoRep and Snap; use POREP kind for generic capacity check
		capacity, err := t.cuzkClient.HasCapacity(context.Background(), cuzk.ProofKind_POREP_SEAL_COMMIT)
		if err != nil {
			log.Warnw("PSProve.CanAccept() cuzk capacity check failed, rejecting", "error", err)
			return []harmonytask.TaskID{}, nil
		}
		if capacity == 0 {
			log.Debugw("PSProve.CanAccept() cuzk pipeline full, backpressuring")
			return []harmonytask.TaskID{}, nil
		}
		if capacity < len(ids) {
			ids = ids[:capacity]
		}
		// fall through to ordering logic below
	} else {
		rdy, err := t.paramsReady()
		if err != nil {
			return nil, xerrors.Errorf("failed to setup params: %w", err)
		}
		if !rdy {
			log.Infow("PSProve.CanAccept() params not ready, not scheduling")
			return []harmonytask.TaskID{}, nil
		}
	}

	// Convert task IDs to int64 slice for SQL query
	taskIDs := make([]int64, len(ids))
	for i, id := range ids {
		taskIDs[i] = int64(id)
	}

	// Query to find the task with the oldest obtained_at
	var oldestTaskIDs []harmonytask.TaskID
	err := t.db.Select(context.Background(), &oldestTaskIDs, `
		SELECT compute_task_id 
		FROM proofshare_queue 
		WHERE compute_task_id = ANY($1) 
		ORDER BY obtained_at ASC 
	`, taskIDs)

	if err != nil {
		if err == pgx.ErrNoRows {
			// No matching tasks found, fallback to first ID
			return ids, nil
		}
		return nil, xerrors.Errorf("failed to query oldest task: %w", err)
	}

	return oldestTaskIDs, nil
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

	proof, err := computeProof(ctx, taskID, request, t.cuzkClient)
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
	ram := uint64(50 << 30) // todo correct value
	if seal.IsDevnet {
		gpu = 0
	}
	if t.cuzkClient != nil && t.cuzkClient.Enabled() {
		gpu = 0
		ram = 1 << 30
	}
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(t.max),
		Name: "PSProve",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: gpu,
			Ram: ram,
		},
		MaxFailures: 20,
		RetryWait: func(retries int) time.Duration {
			return time.Second * 10 * time.Duration(retries)
		},
	}
}

func computeProof(ctx context.Context, taskID harmonytask.TaskID, request common.ProofData, cuzkClient *cuzk.Client) ([]byte, error) {
	if request.PoRep != nil {
		if request.SectorID == nil {
			return nil, xerrors.Errorf("sector id is required")
		}

		return computePoRep(ctx, request.PoRep, *request.SectorID, cuzkClient)
	}

	if request.Snap != nil {
		if request.SectorID == nil {
			return nil, xerrors.Errorf("sector id is required")
		}

		return computeSnap(ctx, taskID, request.Snap, *request.SectorID, cuzkClient)
	}

	return nil, xerrors.Errorf("unknown proof request type")
}

func computePoRep(ctx context.Context, request *proof.Commit1OutRaw, sectorID abi.SectorID, cuzkClient *cuzk.Client) ([]byte, error) {
	// Serialize the commit1out to JSON to pass as vanilla proof
	vproof, err := json.Marshal(request)
	if err != nil {
		return nil, xerrors.Errorf("marshaling vanilla proof: %w", err)
	}

	spt, err := request.RegisteredProof.ToABI()
	if err != nil {
		return nil, xerrors.Errorf("invalid registered proof: %w", err)
	}

	var snarkProof []byte

	if cuzkClient != nil && cuzkClient.Enabled() {
		ssize, err := spt.SectorSize()
		if err != nil {
			return nil, xerrors.Errorf("getting sector size: %w", err)
		}

		resp, err := cuzkClient.Prove(ctx, &cuzk.SubmitProofRequest{
			RequestId:       fmt.Sprintf("ps-porep-%d-%d", sectorID.Miner, sectorID.Number),
			ProofKind:       cuzk.ProofKind_POREP_SEAL_COMMIT,
			SectorSize:      uint64(ssize),
			RegisteredProof: uint64(spt),
			Priority:        cuzk.Priority_NORMAL,
			VanillaProof:    vproof,
			SectorNumber:    uint64(sectorID.Number),
			MinerId:         uint64(sectorID.Miner),
		})
		if err != nil {
			return nil, xerrors.Errorf("cuzk porep prove failed: %w", err)
		}
		snarkProof = resp.Proof
	} else {
		ctx = ffiselect.WithLogCtx(ctx, "sector", sectorID)
		snarkProof, err = ffiselect.FFISelect.SealCommitPhase2(ctx, vproof, sectorID.Number, sectorID.Miner)
		if err != nil {
			return nil, xerrors.Errorf("computing seal proof failed: %w", err)
		}
	}

	commR, err := commcid.ReplicaCommitmentV1ToCID(request.CommR[:])
	if err != nil {
		return nil, xerrors.Errorf("invalid CommR: %w", err)
	}
	commD, err := commcid.DataCommitmentV1ToCID(request.CommD[:])
	if err != nil {
		return nil, xerrors.Errorf("invalid CommD: %w", err)
	}

	ok, err := ffi.VerifySeal(proof2.SealVerifyInfo{
		SealProof:             spt,
		SectorID:              sectorID,
		DealIDs:               nil,
		Randomness:            abi.SealRandomness(request.Ticket[:]),
		InteractiveRandomness: abi.InteractiveSealRandomness(request.Seed[:]),
		Proof:                 snarkProof,
		SealedCID:             commR,
		UnsealedCID:           commD,
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to verify proof: %w", err)
	}
	if !ok {
		return nil, xerrors.Errorf("porep failed to validate")
	}

	return snarkProof, nil
}

func computeSnap(ctx context.Context, taskID harmonytask.TaskID, request *proof.Snap, sectorID abi.SectorID, cuzkClient *cuzk.Client) ([]byte, error) {
	if cuzkClient != nil && cuzkClient.Enabled() {
		resp, err := cuzkClient.Prove(ctx, &cuzk.SubmitProofRequest{
			RequestId:       fmt.Sprintf("ps-snap-%d-%d-%d", sectorID.Miner, sectorID.Number, taskID),
			ProofKind:       cuzk.ProofKind_SNAP_DEALS_UPDATE,
			RegisteredProof: uint64(request.ProofType),
			Priority:        cuzk.Priority_NORMAL,
			VanillaProofs:   request.Proofs,
			SectorNumber:    uint64(sectorID.Number),
			MinerId:         uint64(sectorID.Miner),
			CommROld:        request.OldR[:],
			CommRNew:        request.NewR[:],
			CommDNew:        request.NewD[:],
		})
		if err != nil {
			return nil, xerrors.Errorf("cuzk snap prove failed: %w", err)
		}
		return resp.Proof, nil
	}

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
	snarkProof, err := ffiselect.FFISelect.GenerateUpdateProofWithVanilla(ctx, request.ProofType, oldR, newR, newD, request.Proofs)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate update proof: %w", err)
	}

	return snarkProof, nil
}

var _ = harmonytask.Reg(&TaskProvideSnark{})
