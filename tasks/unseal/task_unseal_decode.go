package unseal

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

var log = logging.Logger("unseal")

const MinSchedInterval = 120 * time.Second

type TaskUnsealDecode struct {
	max int

	sc  *ffi.SealCalls
	db  *harmonydb.DB
	api UnsealSDRApi
}

func NewTaskUnsealDecode(sc *ffi.SealCalls, db *harmonydb.DB, max int, api UnsealSDRApi) *TaskUnsealDecode {
	return &TaskUnsealDecode{
		max: max,
		sc:  sc,
		db:  db,
		api: api,
	}
}

func (t *TaskUnsealDecode) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

	err = t.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof
		FROM sectors_unseal_pipeline
		WHERE task_id_decode_sector = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) == 0 {
		return false, xerrors.Errorf("no sector params")
	}

	sectorParams := sectorParamsArr[0]

	var sectorMeta []struct {
		TicketValue    []byte `db:"ticket_value"`
		OrigSealedCID  string `db:"orig_sealed_cid"`
		CurSealedCID   string `db:"cur_sealed_cid"`
		CurUnsealedCID string `db:"cur_unsealed_cid"`
	}
	err = t.db.Select(ctx, &sectorMeta, `
		SELECT ticket_value, orig_sealed_cid, cur_sealed_cid, cur_unsealed_cid
		FROM sectors_meta
		WHERE sp_id = $1 AND sector_num = $2`, sectorParams.SpID, sectorParams.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("getting sector meta: %w", err)
	}

	if len(sectorMeta) != 1 {
		return false, xerrors.Errorf("expected 1 sector meta, got %d", len(sectorMeta))
	}

	smeta := sectorMeta[0]
	commK, err := cid.Decode(smeta.OrigSealedCID)
	if err != nil {
		return false, xerrors.Errorf("decoding OrigSealedCID: %w", err)
	}

	var commD, commR cid.Cid
	if smeta.CurSealedCID == "" || smeta.CurSealedCID == "b" {
		// https://github.com/filecoin-project/curio/issues/191
		// <workaround>

		// "unsealed" actually stores the sealed CID, "sealed" is empty
		commR, err = cid.Decode(smeta.CurUnsealedCID)
		if err != nil {
			return false, xerrors.Errorf("decoding CurSealedCID: %w", err)
		}

		commD, err = dealdata.UnsealedCidFromPieces(ctx, t.db, sectorParams.SpID, sectorParams.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("getting deal data CID: %w", err)
		}

		log.Warnw("workaround for issue #191", "task", taskID, "commD", commD, "commK", commK, "commR", commR)

		// </workaround>
	} else {
		commD, err = cid.Decode(smeta.CurUnsealedCID)
		if err != nil {
			return false, xerrors.Errorf("decoding CurUnsealedCID (%s): %w", smeta.CurUnsealedCID, err)
		}
		commR, err = cid.Decode(smeta.CurSealedCID)
		if err != nil {
			return false, xerrors.Errorf("decoding CurSealedCID: %w", err)
		}
	}

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sectorParams.RegSealProof),
	}

	isSnap := commK != commR
	log.Infow("unseal decode", "snap", isSnap, "task", taskID, "commK", commK, "commR", commR, "commD", commD)
	if isSnap {
		err := t.sc.DecodeSnap(ctx, taskID, commD, commK, sref)
		if err != nil {
			return false, xerrors.Errorf("DecodeSnap: %w", err)
		}
	} else {
		err = t.sc.DecodeSDR(ctx, taskID, sref)
		if err != nil {
			return false, xerrors.Errorf("DecodeSDR: %w", err)
		}
	}

	// NOTE: Decode.. drops the sector key at the end

	_, err = t.db.Exec(ctx, `UPDATE sectors_unseal_pipeline SET after_decode_sector = TRUE, task_id_decode_sector = NULL WHERE task_id_decode_sector = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating task: %w", err)
	}

	return true, nil
}

func (t *TaskUnsealDecode) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (t *TaskUnsealDecode) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if isDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	res := harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(t.max),
		Name: "UnsealDecode",
		Cost: resources.Resources{
			Cpu:     4, // todo multicore sdr
			Gpu:     0,
			Ram:     54 << 30,
			Storage: t.sc.Storage(t.taskToSector, storiface.FTUnsealed, storiface.FTNone, ssize, storiface.PathStorage, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 2,
		IAmBored: passcall.Every(MinSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}

	if isDevnet {
		res.Cost.Cpu = 1
		res.Cost.Ram = 1 << 30
	}

	return res
}

func (t *TaskUnsealDecode) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// schedule at most one decode when we're bored

	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		var tasks []struct {
			SpID         int64 `db:"sp_id"`
			SectorNumber int64 `db:"sector_number"`
		}

		err := t.db.Select(ctx, &tasks, `SELECT sp_id, sector_number FROM sectors_unseal_pipeline WHERE after_unseal_sdr = TRUE AND after_decode_sector = FALSE AND task_id_decode_sector IS NULL`)
		if err != nil {
			return false, xerrors.Errorf("getting tasks: %w", err)
		}

		if len(tasks) == 0 {
			return false, nil
		}

		// pick at random in case there are a bunch of schedules across the cluster
		t := tasks[rand.N(len(tasks))]

		_, err = tx.Exec(`UPDATE sectors_unseal_pipeline SET task_id_decode_sector = $1 WHERE sp_id = $2 AND sector_number = $3`, id, t.SpID, t.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("updating task id: %w", err)
		}

		return true, nil
	})

	return nil
}

func (t *TaskUnsealDecode) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (t *TaskUnsealDecode) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := t.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (t *TaskUnsealDecode) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_unseal_pipeline WHERE task_id_decode_sector = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

func (t *TaskUnsealDecode) taskToSector(id harmonytask.TaskID) (ffi.SectorRef, error) {
	var refs []ffi.SectorRef

	err := t.db.Select(context.Background(), &refs, `SELECT sp_id, sector_number, reg_seal_proof FROM sectors_unseal_pipeline WHERE task_id_decode_sector = $1`, id)
	if err != nil {
		return ffi.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

var _ = harmonytask.Reg(&TaskUnsealDecode{})
var _ harmonytask.TaskInterface = &TaskUnsealDecode{}
