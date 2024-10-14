package unseal

import (
	"context"
	"math/rand/v2"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

var isDevnet = build.BlockDelaySecs < 30

type UnsealSDRApi interface {
	StateSectorGetInfo(context.Context, address.Address, abi.SectorNumber, types.TipSetKey) (*miner.SectorOnChainInfo, error)
}

type TaskUnsealSdr struct {
	max taskhelp.Limiter

	sc  *ffi.SealCalls
	db  *harmonydb.DB
	api UnsealSDRApi
}

func NewTaskUnsealSDR(sc *ffi.SealCalls, db *harmonydb.DB, max taskhelp.Limiter, api UnsealSDRApi) *TaskUnsealSdr {
	return &TaskUnsealSdr{
		max: max,
		sc:  sc,
		db:  db,
		api: api,
	}
}

func (t *TaskUnsealSdr) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

	err = t.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof
		FROM sectors_unseal_pipeline
		WHERE task_id_unseal_sdr = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) == 0 {
		return false, xerrors.Errorf("no sector params")
	}

	sectorParams := sectorParamsArr[0]

	var sectorMeta []struct {
		TicketValue     []byte `db:"ticket_value"`
		OrigUnsealedCID string `db:"orig_unsealed_cid"`
	}
	err = t.db.Select(ctx, &sectorMeta, `
		SELECT ticket_value, orig_unsealed_cid
		FROM sectors_meta
		WHERE sp_id = $1 AND sector_num = $2`, sectorParams.SpID, sectorParams.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("getting sector meta: %w", err)
	}

	if len(sectorMeta) != 1 {
		return false, xerrors.Errorf("expected 1 sector meta, got %d", len(sectorMeta))
	}

	// NOTE: Even for snap sectors for SDR we need the original unsealed CID
	commD, err := cid.Decode(sectorMeta[0].OrigUnsealedCID)
	if err != nil {
		return false, xerrors.Errorf("decoding commd: %w", err)
	}

	if len(sectorMeta[0].TicketValue) != abi.RandomnessLength {
		return false, xerrors.Errorf("invalid ticket value length %d", len(sectorMeta[0].TicketValue))
	}

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sectorParams.RegSealProof),
	}

	log.Infow("unseal generate sdr key", "sector", sref.ID, "proof", sref.ProofType, "task", taskID, "ticket", sectorMeta[0].TicketValue, "commD", commD)

	if err := t.sc.GenerateSDR(ctx, taskID, storiface.FTKey, sref, sectorMeta[0].TicketValue, commD); err != nil {
		return false, xerrors.Errorf("generate sdr: %w", err)
	}

	// Mark the task as done
	_, err = t.db.Exec(ctx, `UPDATE sectors_unseal_pipeline SET after_unseal_sdr = TRUE, task_id_unseal_sdr = NULL WHERE task_id_unseal_sdr = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating task: %w", err)
	}

	return true, nil
}

func (t *TaskUnsealSdr) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (t *TaskUnsealSdr) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if isDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	res := harmonytask.TaskTypeDetails{
		Max:  t.max,
		Name: "SDRKeyRegen",
		Cost: resources.Resources{
			Cpu:     4, // todo multicore sdr
			Gpu:     0,
			Ram:     54 << 30,
			Storage: t.sc.Storage(t.taskToSector, storiface.FTKey, storiface.FTNone, ssize, storiface.PathSealing, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 2,
		IAmBored: passcall.Every(MinSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}

	if isDevnet {
		res.Cost.Ram = 1 << 30
	}

	return res
}

func (t *TaskUnsealSdr) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// schedule at most one unseal when we're bored

	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		var tasks []struct {
			SpID         int64 `db:"sp_id"`
			SectorNumber int64 `db:"sector_number"`
		}

		err := t.db.Select(ctx, &tasks, `SELECT sp_id, sector_number FROM sectors_unseal_pipeline WHERE after_unseal_sdr = FALSE AND task_id_unseal_sdr IS NULL`)
		if err != nil {
			return false, xerrors.Errorf("getting tasks: %w", err)
		}

		if len(tasks) == 0 {
			return false, nil
		}

		// pick at random in case there are a bunch of schedules across the cluster
		t := tasks[rand.N(len(tasks))]

		_, err = tx.Exec(`UPDATE sectors_unseal_pipeline SET task_id_unseal_sdr = $1 WHERE sp_id = $2 AND sector_number = $3`, id, t.SpID, t.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("updating task id: %w", err)
		}

		return true, nil
	})

	return nil
}

func (t *TaskUnsealSdr) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (t *TaskUnsealSdr) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := t.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (t *TaskUnsealSdr) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_unseal_pipeline WHERE task_id_unseal_sdr = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

func (t *TaskUnsealSdr) taskToSector(id harmonytask.TaskID) (ffi.SectorRef, error) {
	var refs []ffi.SectorRef

	err := t.db.Select(context.Background(), &refs, `SELECT sp_id, sector_number, reg_seal_proof FROM sectors_unseal_pipeline WHERE task_id_unseal_sdr = $1`, id)
	if err != nil {
		return ffi.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

var _ = harmonytask.Reg(&TaskUnsealSdr{})
var _ harmonytask.TaskInterface = (*TaskUnsealSdr)(nil)
