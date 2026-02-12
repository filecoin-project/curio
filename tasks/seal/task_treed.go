package seal

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/storiface"
)

type TreeDTask struct {
	sp    *SealPoller
	db    *harmonydb.DB
	sc    *ffi2.SealCalls
	bound bool

	max int
}

func (t *TreeDTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if IsDevnet {
		return ids, nil
	}

	if engine.Resources().Gpu > 0 {
		if !t.bound {
			return ids, nil
		}

		var tasks []struct {
			TaskID       harmonytask.TaskID `db:"task_id_tree_d"`
			SpID         int64              `db:"sp_id"`
			SectorNumber int64              `db:"sector_number"`
			StorageID    string             `db:"storage_id"`
		}

		if storiface.FTCache != 4 {
			panic("storiface.FTCache != 4")
		}

		ctx := context.Background()

		indIDs := make([]int64, len(ids))
		for i, id := range ids {
			indIDs[i] = int64(id)
		}

		err := t.db.Select(ctx, &tasks, `
		SELECT p.task_id_tree_d, p.sp_id, p.sector_number, l.storage_id FROM sectors_sdr_pipeline p
			INNER JOIN sector_location l ON p.sp_id = l.miner_id AND p.sector_number = l.sector_num
			WHERE task_id_tree_d = ANY ($1) AND l.sector_filetype = 4`, indIDs)
		if err != nil {
			return []harmonytask.TaskID{}, xerrors.Errorf("getting tasks: %w", err)
		}

		ls, err := t.sc.LocalStorage(ctx)
		if err != nil {
			return []harmonytask.TaskID{}, xerrors.Errorf("getting local storage: %w", err)
		}

		acceptables := map[harmonytask.TaskID]bool{}

		for _, t := range ids {
			acceptables[t] = true
		}

		result := []harmonytask.TaskID{}
		for _, t := range tasks {
			if _, ok := acceptables[t.TaskID]; !ok {
				continue
			}

			for _, l := range ls {
				if string(l.ID) == t.StorageID {
					result = append(result, t.TaskID)
				}
			}
		}
		return result, nil
	}
	return ids, nil
}

func (t *TreeDTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(t.max),
		Name: "TreeD",
		Cost: resources.Resources{
			Cpu:     1,
			Ram:     1 << 30,
			Gpu:     0,
			Storage: t.sc.Storage(t.taskToSector, storiface.FTNone, storiface.FTCache, ssize, storiface.PathSealing, 1.0),
		},
		MaxFailures: 3,
	}
}

func (t *TreeDTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := t.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (t *TreeDTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_sdr_pipeline WHERE task_id_tree_d = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&TreeDTask{})

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

func NewTreeDTask(sp *SealPoller, db *harmonydb.DB, sc *ffi2.SealCalls, maxTrees int, bound bool) *TreeDTask {
	return &TreeDTask{
		sp: sp,
		db: db,
		sc: sc,

		max:   maxTrees,
		bound: bound,
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

	dealData, err := dealdata.DealDataSDRPoRep(ctx, t.db, t.sc, sectorParams.SpID, sectorParams.SectorNumber, sectorParams.RegSealProof, false)
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

	// Record metric
	if maddr, err := address.NewIDAddress(uint64(sectorParams.SpID)); err == nil {
		if err := stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(MinerTag, maddr.String()),
		}, SealMeasures.TreeDCompleted.M(1)); err != nil {
			log.Errorf("recording metric: %s", err)
		}
	}

	return true, nil
}

var _ harmonytask.TaskInterface = &TreeDTask{}
