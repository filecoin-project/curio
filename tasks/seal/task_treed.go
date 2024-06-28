package seal

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/dealdata"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type TreeDTask struct {
	sp *SealPoller
	db *harmonydb.DB
	sc *ffi2.SealCalls

	max int
}

func (t *TreeDTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if IsDevnet {
		return &ids[0], nil
	}
	if engine.Resources().Gpu > 0 {
		return &ids[0], nil
	}
	return nil, nil
}

func (t *TreeDTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	return harmonytask.TaskTypeDetails{
		Max:  t.max,
		Name: "TreeD",
		Cost: resources.Resources{
			Cpu:     1,
			Ram:     1 << 30,
			Gpu:     0,
			Storage: t.sc.Storage(t.taskToSector, storiface.FTNone, storiface.FTCache, ssize, storiface.PathSealing, 1.0),
		},
		MaxFailures: 3,
		Follows:     nil,
	}
}

func (t *TreeDTask) taskToSector(id harmonytask.TaskID) (ffi2.SectorRef, error) {
	var refs []ffi2.SectorRef

	err := t.db.Select(context.Background(), &refs, `SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_tree_d = $1`, id)
	if err != nil {
		return ffi2.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi2.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

func (t *TreeDTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.sp.pollers[pollerTreeD].Set(taskFunc)
}

func NewTreeDTask(sp *SealPoller, db *harmonydb.DB, sc *ffi2.SealCalls, maxTrees int) *TreeDTask {
	return &TreeDTask{
		sp: sp,
		db: db,
		sc: sc,

		max: maxTrees,
	}
}

func (t *TreeDTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64                   `db:"sp_id"`
		SectorNumber int64                   `db:"sector_number"`
		RegSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`
	}

	err = t.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof
		FROM sectors_sdr_pipeline
		WHERE task_id_tree_d = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(sectorParamsArr))
	}
	sectorParams := sectorParamsArr[0]

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: sectorParams.RegSealProof,
	}

	// Fetch the Sector to local storage
	fsPaths, pathIds, release, err := t.sc.PreFetch(ctx, sref, &taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to prefetch sectors: %w", err)
	}
	defer release()

	dealData, err := dealdata.DealDataSDRPoRep(ctx, t.db, t.sc, sectorParams.SpID, sectorParams.SectorNumber, sectorParams.RegSealProof)
	if err != nil {
		return false, xerrors.Errorf("getting deal data: %w", err)
	}
	defer dealData.Close()

	ssize, err := sectorParams.RegSealProof.SectorSize()
	if err != nil {
		return false, xerrors.Errorf("getting sector size: %w", err)
	}

	// Generate Tree D
	err = t.sc.TreeD(ctx, sref, dealData.CommD, abi.PaddedPieceSize(ssize), dealData.Data, dealData.IsUnpadded, fsPaths, pathIds)
	if err != nil {
		return false, xerrors.Errorf("failed to generate TreeD: %w", err)
	}

	n, err := t.db.Exec(ctx, `UPDATE sectors_sdr_pipeline
		SET after_tree_d = true, tree_d_cid = $3, task_id_tree_d = NULL WHERE sp_id = $1 AND sector_number = $2`,
		sectorParams.SpID, sectorParams.SectorNumber, dealData.CommD)
	if err != nil {
		return false, xerrors.Errorf("store TreeD success: updating pipeline: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("store TreeD success: updated %d rows", n)
	}

	return true, nil
}

var _ harmonytask.TaskInterface = &TreeDTask{}
