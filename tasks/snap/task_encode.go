package snap

import (
	"context"
	"math/rand/v2"
	"net/url"
	"strconv"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	storiface "github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/seal"
)

const MinSnapSchedInterval = 10 * time.Second

type EncodeTask struct {
	max        int
	bindToData bool
	allowEncodeGPUOverprovision bool

	sc *ffi.SealCalls
	db *harmonydb.DB
}

func NewEncodeTask(sc *ffi.SealCalls, db *harmonydb.DB, max int, bindToData bool, allowEncodeGPUOverprovision bool) *EncodeTask {
	return &EncodeTask{
		max:        max,
		sc:         sc,
		db:         db,
		bindToData: bindToData,
		allowEncodeGPUOverprovision: allowEncodeGPUOverprovision,
	}
}

func (e *EncodeTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		UpdateProof  int64 `db:"upgrade_proof"`

		RegSealProof int64 `db:"reg_seal_proof"`

		OrigSealedCID string `db:"orig_sealed_cid"`
	}

	ctx := context.Background()

	err = e.db.Select(ctx, &tasks, `
		SELECT snp.sp_id, snp.sector_number, snp.upgrade_proof, sm.reg_seal_proof, sm.orig_sealed_cid
		FROM sectors_snap_pipeline snp
		INNER JOIN sectors_meta sm ON snp.sp_id = sm.sp_id AND snp.sector_number = sm.sector_num
		WHERE snp.task_id_encode = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(tasks))
	}

	sectorParams := tasks[0]

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sectorParams.RegSealProof),
	}

	keyCid, err := cid.Parse(sectorParams.OrigSealedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing key cid: %w", err)
	}

	data, err := dealdata.DealDataSnap(ctx, e.db, e.sc, sectorParams.SpID, sectorParams.SectorNumber, abi.RegisteredSealProof(sectorParams.RegSealProof))
	if err != nil {
		return false, xerrors.Errorf("getting deal data: %w", err)
	}
	defer data.Close()

	if !data.IsUnpadded {
		// we always expect deal data which is always unpadded
		return false, xerrors.Errorf("expected unpadded data")
	}

	sealed, unsealed, err := e.sc.EncodeUpdate(ctx, keyCid, taskID, abi.RegisteredUpdateProof(sectorParams.UpdateProof), sref, data.Data, data.PieceInfos, data.KeepUnsealed)
	if err != nil {
		return false, xerrors.Errorf("ffi update encode: %w", err)
	}

	if data.CommD != unsealed {
		return false, xerrors.Errorf("unsealed cid mismatch")
	}

	_, err = e.db.Exec(ctx, `UPDATE sectors_snap_pipeline SET update_unsealed_cid = $1, update_sealed_cid = $2, after_encode = TRUE, task_id_encode = NULL
                             WHERE sp_id = $3 AND sector_number = $4`,
		unsealed.String(), sealed.String(), sectorParams.SpID, sectorParams.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("updating sector pipeline: %w", err)
	}

	if err := DropSectorPieceRefsSnap(ctx, e.db, sref.ID); err != nil {
		return true, xerrors.Errorf("dropping piece refs: %w", err)
	}

	return true, nil
}

func (e *EncodeTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if !e.bindToData {
		id := ids[0]
		return &id, nil
	}

	// debug log
	log.Infow("encode task can accept", "ids", ids, "bindToData", e.bindToData)

	ctx := context.Background()

	// Build a list of candidate tasks for the provided ids
	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	var tasks []struct {
		TaskID       harmonytask.TaskID `db:"task_id_encode"`
		SpID         int64              `db:"sp_id"`
		SectorNumber int64              `db:"sector_number"`

		StorageID   string
		NoPieceRefs bool
	}

	_, err := e.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		if err := tx.Select(&tasks, `
			SELECT task_id_encode, sp_id, sector_number
			FROM sectors_snap_pipeline
			WHERE task_id_encode = ANY ($1)`, indIDs); err != nil {
			return false, xerrors.Errorf("selecting snap encode tasks: %w", err)
		}

		for i := range tasks {
			var pieceURLs []struct {
				Url *string `db:"data_url"`
			}
			if err := tx.Select(&pieceURLs, `
				SELECT data_url FROM sectors_snap_initial_pieces
				WHERE sp_id = $1 AND sector_number = $2`, tasks[i].SpID, tasks[i].SectorNumber); err != nil {
				return false, xerrors.Errorf("selecting snap piece urls: %w", err)
			}

			hasPieceRef := false
			for _, pu := range pieceURLs {
				if pu.Url == nil || *pu.Url == "" {
					continue
				}
				u, err := url.Parse(*pu.Url)
				if err != nil {
					continue
				}
				if u.Scheme != "pieceref" {
					continue
				}
				hasPieceRef = true

				refNum, err := strconv.ParseInt(u.Opaque, 10, 64)
				if err != nil {
					continue
				}

				var pieceID []struct {
					PieceID storiface.PieceNumber `db:"piece_id"`
				}
				if err := tx.Select(&pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refNum); err != nil || len(pieceID) != 1 {
					continue
				}

				var sLocation string
				if err := tx.QueryRow(`
					SELECT storage_id FROM sector_location
					WHERE miner_id = $1 AND sector_num = $2 AND sector_filetype = $3
					LIMIT 1`, 0, pieceID[0].PieceID, storiface.FTPiece).Scan(&sLocation); err != nil {
					continue
				}

				if sLocation != "" {
					tasks[i].StorageID = sLocation
					break
				}
			}

			if !hasPieceRef {
				tasks[i].NoPieceRefs = true
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return nil, err
	}

	// Load local storage IDs
	ls, err := e.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}
	local := map[string]struct{}{}
	for _, l := range ls {
		local[string(l.ID)] = struct{}{}
	}

	// debug log
	log.Infow("encode task can accept", "tasks", tasks, "bindToData", e.bindToData, "local", local)

	// Prefer tasks where at least one pieceref is present on local storage
	for _, t := range tasks {
		if t.StorageID == "" {
			continue
		}
		if _, ok := local[t.StorageID]; ok {
			id := t.TaskID
			log.Infow("encode task can accept did accept", "task", t)
			return &id, nil
		}
	}

	// Fallback: if task has no pieceref pieces, it can run anywhere
	for _, t := range tasks {
		if t.NoPieceRefs {
			id := t.TaskID
			log.Infow("encode task can accept accepting non-pieceref task (anywhere)", "task", t)
			return &id, nil
		}
	}

	// No acceptable tasks for this node
	return nil, nil
}

func (e *EncodeTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if seal.IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}
	gpu := 1.0
	if seal.IsDevnet || e.allowEncodeGPUOverprovision {
		gpu = 0
	}

	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(e.max),
		Name: "UpdateEncode",
		Cost: resources.Resources{
			Cpu:     1,
			Ram:     1 << 30, // todo correct value
			Gpu:     gpu,
			Storage: e.sc.Storage(e.taskToSector, storiface.FTUpdate|storiface.FTUpdateCache, storiface.FTNone, ssize, storiface.PathSealing, 1.0),
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(MinSnapSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return e.schedule(context.Background(), taskFunc)
		}),
	}
}

func (e *EncodeTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (e *EncodeTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var tasks []struct {
				SpID         int64 `db:"sp_id"`
				SectorNumber int64 `db:"sector_number"`
			}

			err := tx.Select(&tasks, `SELECT sp_id, sector_number FROM sectors_snap_pipeline WHERE data_assigned = true AND after_encode = FALSE AND task_id_encode IS NULL`)
			if err != nil {
				return false, xerrors.Errorf("getting tasks: %w", err)
			}

			if len(tasks) == 0 {
				return false, nil
			}

			// pick at random in case there are a bunch of schedules across the cluster
			t := tasks[rand.N(len(tasks))]

			_, err = tx.Exec(`UPDATE sectors_snap_pipeline SET task_id_encode = $1 WHERE sp_id = $2 AND sector_number = $3`, id, t.SpID, t.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating task id: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (e *EncodeTask) taskToSector(id harmonytask.TaskID) (ffi.SectorRef, error) {
	var refs []ffi.SectorRef

	err := e.db.Select(context.Background(), &refs, `SELECT snp.sp_id, snp.sector_number, sm.reg_seal_proof
		FROM sectors_snap_pipeline snp INNER JOIN sectors_meta sm ON snp.sp_id = sm.sp_id AND snp.sector_number = sm.sector_num
		WHERE snp.task_id_encode = $1`, id)
	if err != nil {
		return ffi.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

func (e *EncodeTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := e.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (e *EncodeTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_snap_pipeline WHERE task_id_encode = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&EncodeTask{})
var _ harmonytask.TaskInterface = &EncodeTask{}
