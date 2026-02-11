package scrub

import (
	"context"
	"math/rand/v2"
	"runtime"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/supraffi"
)

// ScrubCommRTask verifies sealed/update sector files by computing CommR
// using supraseal TreeR and comparing against the expected value.
type ScrubCommRTask struct {
	db *harmonydb.DB
	sc *ffi.SealCalls
}

// NewCommRCheckTask creates a new ScrubCommRTask
func NewCommRCheckTask(db *harmonydb.DB, sc *ffi.SealCalls) *ScrubCommRTask {
	return &ScrubCommRTask{db: db, sc: sc}
}

// CanRunSupraTreeR checks if the local node can run supraseal TreeR
// This requires both AVX512 CPU features and a usable CUDA GPU
func CanRunSupraTreeR() bool {
	return supraffi.HasAMD64v4() && supraffi.HasUsableCUDAGPU()
}

func (c *ScrubCommRTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var checkReq []struct {
		CheckID      int64  `db:"check_id"`
		SpID         int64  `db:"sp_id"`
		SectorNumber int64  `db:"sector_number"`
		FileType     string `db:"file_type"`
		ExpectedCID  string `db:"expected_comm_r"`
		RegSealProof int64  `db:"reg_seal_proof"`
	}

	err = c.db.Select(ctx, &checkReq, `
		SELECT c.check_id, c.sp_id, c.sector_number, c.file_type, c.expected_comm_r, sm.reg_seal_proof
		FROM scrub_commr_check c
		INNER JOIN sectors_meta sm ON sm.sp_id = c.sp_id AND sm.sector_num = c.sector_number
		WHERE c.task_id = $1
	`, taskID)
	if err != nil {
		return false, xerrors.Errorf("fetching check request: %w", err)
	}
	if len(checkReq) == 0 {
		return false, xerrors.Errorf("no check requests found")
	}

	check := checkReq[0]

	expectedCID, err := cid.Parse(check.ExpectedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing expected CommR CID: %w", err)
	}

	sectorRef := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(check.SpID),
			Number: abi.SectorNumber(check.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(check.RegSealProof),
	}

	storeResult := func(ok bool, actualCID *cid.Cid, message string) error {
		var actualStr *string
		if actualCID != nil {
			s := actualCID.String()
			actualStr = &s
		}
		_, err := c.db.Exec(ctx, `
			UPDATE scrub_commr_check
			SET ok = $1, actual_comm_r = $2, message = $3
			WHERE check_id = $4
		`, ok, actualStr, message, check.CheckID)
		return err
	}

	var actualCID cid.Cid
	var checkErr error

	switch check.FileType {
	case "sealed":
		actualCID, checkErr = c.sc.CheckSealedCommR(ctx, sectorRef)
	case "update":
		actualCID, checkErr = c.sc.CheckUpdateCommR(ctx, sectorRef)
	default:
		return false, xerrors.Errorf("unknown file type: %s", check.FileType)
	}

	if checkErr != nil {
		err = storeResult(false, nil, checkErr.Error())
		if err != nil {
			return false, xerrors.Errorf("storing error result: %w", err)
		}
		return true, nil
	}

	if actualCID != expectedCID {
		err := storeResult(false, &actualCID, "CommR mismatch")
		if err != nil {
			return false, xerrors.Errorf("storing result (mismatch %s != %s): %w", actualCID, expectedCID, err)
		}
		return true, nil
	}

	err = storeResult(true, &actualCID, "")
	if err != nil {
		return false, xerrors.Errorf("storing result (correct): %w", err)
	}

	return true, nil
}

func (c *ScrubCommRTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	// Only accept if this node can run supraseal TreeR
	if !CanRunSupraTreeR() {
		return nil, nil
	}
	return ids, nil
}

func (c *ScrubCommRTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "ScrubCommRCheck",
		Cost: resources.Resources{
			Cpu: min(4, runtime.NumCPU()/2),
			Gpu: 1,       // Requires GPU for supraseal TreeR
			Ram: 8 << 30, // 8 GiB for tree computation
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(MinSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return c.schedule(context.Background(), taskFunc)
		}),
	}
}

func (c *ScrubCommRTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (c *ScrubCommRTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		var checks []struct {
			CheckID      int64 `db:"check_id"`
			SpID         int64 `db:"sp_id"`
			SectorNumber int64 `db:"sector_number"`
		}

		err := tx.Select(&checks, `
			SELECT check_id, sp_id, sector_number
			FROM scrub_commr_check
			WHERE task_id IS NULL AND ok IS NULL LIMIT 20
		`)
		if err != nil {
			return false, xerrors.Errorf("getting tasks: %w", err)
		}

		if len(checks) == 0 {
			return false, nil
		}

		// pick at random in case there are a bunch of schedules across the cluster
		check := checks[rand.N(len(checks))]

		_, err = tx.Exec(`
			UPDATE scrub_commr_check
			SET task_id = $1
			WHERE check_id = $2 AND task_id IS NULL
		`, id, check.CheckID)
		if err != nil {
			return false, xerrors.Errorf("updating task id: %w", err)
		}

		return true, nil
	})

	return nil
}

var _ = harmonytask.Reg(&ScrubCommRTask{})
var _ harmonytask.TaskInterface = &ScrubCommRTask{}
