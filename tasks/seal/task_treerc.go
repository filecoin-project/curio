package seal

import (
	"context"

	"github.com/ipfs/go-cid"
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
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

type TreeRCTask struct {
	sp *SealPoller
	db *harmonydb.DB
	sc *ffi2.SealCalls

	max int
}

func NewTreeRCTask(sp *SealPoller, db *harmonydb.DB, sc *ffi2.SealCalls, maxTrees int) *TreeRCTask {
	return &TreeRCTask{
		sp: sp,
		db: db,
		sc: sc,

		max: maxTrees,
	}
}

func (t *TreeRCTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64                   `db:"sp_id"`
		SectorNumber int64                   `db:"sector_number"`
		RegSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`
		CommD        string                  `db:"tree_d_cid"`
		TicketValue  []byte                  `db:"ticket_value"`
		Pipeline     string                  `db:"pipeline"`
	}

	err = t.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof, tree_d_cid, ticket_value, 'local' as pipeline
		FROM sectors_sdr_pipeline
		WHERE task_id_tree_c = $1 AND task_id_tree_r = $1
		UNION ALL
		SELECT sp_id, sector_number, reg_seal_proof, tree_d_cid, ticket_value, 'remote' as pipeline
		FROM rseal_provider_pipeline
		WHERE task_id_tree_c = $1 AND task_id_tree_r = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(sectorParamsArr))
	}
	sectorParams := sectorParamsArr[0]

	commd, err := cid.Parse(sectorParams.CommD)
	if err != nil {
		return false, xerrors.Errorf("parsing unsealed CID: %w", err)
	}

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: sectorParams.RegSealProof,
	}

	dd, err := dealdata.DealDataSDRPoRep(ctx, t.db, t.sc, sectorParams.SpID, sectorParams.SectorNumber, sectorParams.RegSealProof, true)
	if err != nil {
		return false, xerrors.Errorf("getting deal data: %w", err)
	}

	// R / C
	sealed, unsealed, err := t.sc.TreeRC(ctx, &taskID, sref, commd, sectorParams.TicketValue, dd.PieceInfos)
	if err != nil {
		serr := resetSectorSealingState(ctx, sectorParams.SpID, sectorParams.SectorNumber, err, t.db, t.TypeDetails().Name, sectorParams.Pipeline)
		if serr != nil {
			return false, xerrors.Errorf("computing tree r and c: %w", err)
		}
	}

	if unsealed != commd {
		return false, xerrors.Errorf("commd %s does match unsealed %s", commd.String(), unsealed.String())
	}

	var n int
	if sectorParams.Pipeline == "remote" {
		n, err = t.db.Exec(ctx, `UPDATE rseal_provider_pipeline
			SET after_tree_r = true, after_tree_c = true, tree_r_cid = $3, task_id_tree_r = NULL, task_id_tree_c = NULL
			WHERE sp_id = $1 AND sector_number = $2`,
			sectorParams.SpID, sectorParams.SectorNumber, sealed)
	} else {
		n, err = t.db.Exec(ctx, `UPDATE sectors_sdr_pipeline
			SET after_tree_r = true, after_tree_c = true, tree_r_cid = $3, task_id_tree_r = NULL, task_id_tree_c = NULL
			WHERE sp_id = $1 AND sector_number = $2`,
			sectorParams.SpID, sectorParams.SectorNumber, sealed)
	}
	if err != nil {
		return false, xerrors.Errorf("store sdr-trees success: updating pipeline: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("store sdr-trees success: updated %d rows", n)
	}

	// Record metric
	if maddr, err := address.NewIDAddress(uint64(sectorParams.SpID)); err == nil {
		if err := stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(MinerTag, maddr.String()),
		}, SealMeasures.TreeRCCompleted.M(1)); err != nil {
			log.Errorf("recording metric: %s", err)
		}
	}

	return true, nil
}

func (t *TreeRCTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	var tasks []struct {
		TaskID       harmonytask.TaskID `db:"task_id_tree_c"`
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
		SELECT p.task_id_tree_c, p.sp_id, p.sector_number, l.storage_id FROM sectors_sdr_pipeline p
			INNER JOIN sector_location l ON p.sp_id = l.miner_id AND p.sector_number = l.sector_num
			WHERE task_id_tree_r = ANY ($1) AND l.sector_filetype = 4
		UNION ALL
		SELECT p.task_id_tree_c, p.sp_id, p.sector_number, l.storage_id FROM rseal_provider_pipeline p
			INNER JOIN sector_location l ON p.sp_id = l.miner_id AND p.sector_number = l.sector_num
			WHERE task_id_tree_r = ANY ($1) AND l.sector_filetype = 4
`, indIDs)
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

func (t *TreeRCTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}
	gpu := 1.0
	ram := uint64(8 << 30)
	if IsDevnet {
		gpu = 0
		ram = 512 << 20
	}

	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(t.max),
		Name: "TreeRC",
		Cost: resources.Resources{
			Cpu:     1,
			Gpu:     gpu,
			Ram:     ram,
			Storage: t.sc.Storage(t.taskToSector, storiface.FTSealed, storiface.FTCache, ssize, storiface.PathSealing, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 3,
		Follows:     nil,
	}
}

func (t *TreeRCTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := t.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (t *TreeRCTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id, sector_number FROM (
		SELECT sp_id, sector_number FROM sectors_sdr_pipeline WHERE task_id_tree_r = $1
		UNION ALL
		SELECT sp_id, sector_number FROM rseal_provider_pipeline WHERE task_id_tree_r = $1
	) s`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&TreeRCTask{})

func (t *TreeRCTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.sp.pollers[pollerTreeRC].Set(taskFunc)
}

func (t *TreeRCTask) taskToSector(id harmonytask.TaskID) (ffi2.SectorRef, error) {
	var refs []ffi2.SectorRef

	err := t.db.Select(context.Background(), &refs, `
		SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_tree_r = $1
		UNION ALL
		SELECT sp_id, sector_number, reg_seal_proof FROM rseal_provider_pipeline WHERE task_id_tree_r = $1`, id)
	if err != nil {
		return ffi2.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi2.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

var _ harmonytask.TaskInterface = &TreeRCTask{}
