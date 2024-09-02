package unseal

import (
	"context"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

var log = logging.Logger("unseal")

type TaskUnsealDecode struct {
	max int

	sc  *ffi.SealCalls
	db  *harmonydb.DB
	api UnsealSDRApi
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
		TicketValue   []byte `db:"ticket_value"`
		OrigSealedCID string `db:"orig_sealed_cid"`
		CurSealedCID  string `db:"cur_sealed_cid"`
	}
	err = t.db.Select(ctx, &sectorMeta, `
		SELECT ticket_value, orig_sealed_cid, cur_sealed_cid
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
	commR, err := cid.Decode(smeta.CurSealedCID)
	if err != nil {
		return false, xerrors.Errorf("decoding CurSealedCID: %w", err)
	}

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sectorParams.RegSealProof),
	}

	isSnap := commK != commR
	log.Infow("unseal decode", "snap", isSnap, "task", taskID, "commK", commK, "commR", commR)
	if isSnap {
		err := t.sc.DecodeSnap(ctx, taskID, commR, commK, sref)
		if err != nil {
			return false, xerrors.Errorf("DecodeSnap: %w", err)
		}
	} else {
		err = t.sc.DecodeSDR(ctx, taskID, sref)
		if err != nil {
			return false, xerrors.Errorf("DecodeSDR: %w", err)
		}
	}

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

	return harmonytask.TaskTypeDetails{
		Max:  t.max,
		Name: "UnsealDecode",
		Cost: resources.Resources{
			Cpu:     4, // todo multicore sdr
			Gpu:     0,
			Ram:     54 << 30,
			Storage: t.sc.Storage(t.taskToSector, storiface.FTUnsealed, storiface.FTNone, ssize, storiface.PathStorage, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 2,
		Follows:     nil,
	}
}

func (t *TaskUnsealDecode) Adder(taskFunc harmonytask.AddTaskFunc) {
	//TODO implement me
	panic("implement me")
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

var _ harmonytask.TaskInterface = &TaskUnsealDecode{}
