package snap

import (
	"context"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/tasks/seal"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type ProveTask struct {
	max int

	sc          *ffi.SealCalls
	db          *harmonydb.DB
	paramsReady func() (bool, error)
}

func NewProveTask(sc *ffi.SealCalls, db *harmonydb.DB, paramck func() (bool, error), max int) *ProveTask {
	return &ProveTask{
		max: max,

		sc: sc,
		db: db,
	}
}

func (p *ProveTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		UpdateProof  int64 `db:"upgrade_proof"`

		RegSealProof int64 `db:"reg_seal_proof"`

		OrigSealedCID     string `db:"orig_sealed_cid"`
		UpdateSealedCID   string `db:"update_sealed_cid"`
		UpdateUnsealedCID string `db:"update_unsealed_cid"`
	}

	ctx := context.Background()

	err = p.db.Select(ctx, &tasks, `
		SELECT snp.sp_id, snp.sector_number, snp.upgrade_proof, sm.reg_seal_proof, sm.orig_sealed_cid, snp.update_sealed_cid, snp.update_unsealed_cid
		FROM sectors_snap_pipeline snp
		INNER JOIN sectors_meta sm ON snp.sp_id = sm.sp_id AND snp.sector_number = sm.sector_num
		WHERE snp.task_id_prove = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(tasks))
	}

	sectorParams := tasks[0]

	cidKey, err := cid.Parse(sectorParams.OrigSealedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing orig sealed cid: %w", err)
	}
	cidUnsealed, err := cid.Parse(sectorParams.UpdateUnsealedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing update unsealed cid: %w", err)
	}
	cidSealed, err := cid.Parse(sectorParams.UpdateSealedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing update sealed cid: %w", err)

	}

	sector := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sectorParams.RegSealProof),
	}

	proof, err := p.sc.ProveUpdate(ctx, abi.RegisteredUpdateProof(sectorParams.UpdateProof), sector, cidKey, cidSealed, cidUnsealed)
	if err != nil {
		return false, xerrors.Errorf("update prove: %w", err)
	}

	_, err = p.db.Exec(ctx, `UPDATE sectors_snap_pipeline SET proof = $1, task_id_prove = NULL, after_prove = TRUE WHERE task_id_prove = $2`, proof, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating sector params: %w", err)
	}

	return true, nil
}

func (p *ProveTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	rdy, err := p.paramsReady()
	if err != nil {
		return nil, xerrors.Errorf("failed to setup params: %w", err)
	}
	if !rdy {
		log.Infow("PoRepTask.CanAccept() params not ready, not scheduling")
		return nil, nil
	}

	id := ids[0]
	return &id, nil
}

func (p *ProveTask) TypeDetails() harmonytask.TaskTypeDetails {
	gpu := 1.0
	if seal.IsDevnet {
		gpu = 0
	}
	return harmonytask.TaskTypeDetails{
		Max:  p.max,
		Name: "UpdateProve",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: gpu,
			Ram: 50 << 30, // todo correct value
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(MinSnapSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return p.schedule(context.Background(), taskFunc)
		}),
	}
}

func (p *ProveTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
	}

	err := p.db.Select(ctx, &tasks, `SELECT sp_id, sector_number FROM sectors_snap_pipeline WHERE after_encode = TRUE AND after_prove = FALSE AND task_id_prove IS NULL`)
	if err != nil {
		return xerrors.Errorf("getting tasks: %w", err)
	}

	for _, t := range tasks {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			_, err := tx.Exec(`UPDATE sectors_snap_pipeline SET task_id_prove = $1 WHERE sp_id = $2 AND sector_number = $3`, id, t.SpID, t.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating task id: %w", err)
			}

			return true, nil
		})
	}

	return nil
}

func (p *ProveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ harmonytask.TaskInterface = &ProveTask{}
