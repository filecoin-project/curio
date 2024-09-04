package scrub

import (
	"context"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
	"math/rand/v2"
	"runtime"
	"time"
)

const MinSchedInterval = 100 * time.Second

type ScrubCommDTask struct {
	db *harmonydb.DB
	sc *ffi.SealCalls
}

func NewCommDCheckTask(db *harmonydb.DB, sc *ffi.SealCalls) *ScrubCommDTask {
	return &ScrubCommDTask{db: db, sc: sc}
}

func (c *ScrubCommDTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var checkReq []struct {
		CheckID           int64  `db:"check_id"`
		SpID              int64  `db:"sp_id"`
		SectorNumber      int64  `db:"sector_number"`
		ExpectUnsealedCID string `db:"expected_unsealed_cid"`
		RegSealProof      int64  `db:"reg_seal_proof"`
	}

	err = c.db.Select(ctx, &checkReq, `
		SELECT u.check_id, u.sp_id, u.sector_number, u.expected_unsealed_cid, sm.reg_seal_proof
		FROM scrub_unseal_commd_check u
		INNER JOIN sectors_meta sm ON sm.sp_id = u.sp_id AND sm.sector_num = u.sector_number
		WHERE task_id = $1
	`, taskID)
	if err != nil {
		return false, xerrors.Errorf("fetching check request: %w", err)
	}
	if len(checkReq) == 0 {
		return false, xerrors.Errorf("no check requests found")
	}

	check := checkReq[0]

	s := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(check.SpID),
			Number: abi.SectorNumber(check.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(check.RegSealProof),
	}

	expectUnsealedCID, err := cid.Parse(check.ExpectUnsealedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing expected unsealed CID: %w", err)
	}

	actual, err := c.sc.CheckUnsealedCID(ctx, s)
	if err != nil {
		return false, xerrors.Errorf("checking unsealed CID: %w", err)
	}

	storeResult := func(ok bool, actualCID *cid.Cid, message string) error {
		_, err := c.db.Exec(ctx, `
			UPDATE scrub_unseal_commd_check
			SET ok = $1, actual_unsealed_cid = $2, message = $3
			WHERE check_id = $4
		`, ok, actualCID, message, check.CheckID)
		return err
	}

	if actual != expectUnsealedCID {
		err := storeResult(false, &actual, "unsealed CID mismatch")
		if err != nil {
			return false, xerrors.Errorf("storing result (mismatch %s != %s): %w", actual, expectUnsealedCID, err)
		}

		return true, nil
	}

	err = storeResult(true, &actual, "")
	if err != nil {
		return false, xerrors.Errorf("storing result (correct): %w", err)
	}

	return true, nil
}

func (c *ScrubCommDTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (c *ScrubCommDTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "ScrubCommDCheck",
		Cost: resources.Resources{
			Cpu: min(1, runtime.NumCPU()/4),
			Ram: uint64(runtime.NumCPU())*(8<<20) + 128<<20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(MinSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return c.schedule(context.Background(), taskFunc)
		}),
	}
}

func (c *ScrubCommDTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (c *ScrubCommDTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		var checks []struct {
			CheckID      int64 `db:"check_id"`
			SpID         int64 `db:"sp_id"`
			SectorNumber int64 `db:"sector_number"`
		}

		err := c.db.Select(ctx, &checks, `
			SELECT check_id, sp_id, sector_number
			FROM scrub_unseal_commd_check
			WHERE task_id IS NULL LIMIT 20
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
			UPDATE scrub_unseal_commd_check
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

var _ = harmonytask.Reg(&ScrubCommDTask{})
var _ harmonytask.TaskInterface = &ScrubCommDTask{}
