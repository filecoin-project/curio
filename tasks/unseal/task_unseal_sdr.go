package unseal

import (
	"context"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
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
	max int

	sc  *ffi.SealCalls
	db  *harmonydb.DB
	api UnsealSDRApi
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

	commK, err := cid.Decode(sectorMeta[0].OrigSealedCID)
	if err != nil {
		return false, xerrors.Errorf("decoding commk: %w", err)
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

	if err := t.sc.GenerateSDR(ctx, taskID, storiface.FTKey, sref, sectorMeta[0].TicketValue, commK); err != nil {
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
		Follows:     nil,
	}

	if isDevnet {
		res.Cost.Ram = 1 << 30
	}

	return res
}

func (t *TaskUnsealSdr) Adder(taskFunc harmonytask.AddTaskFunc) {
	//TODO implement me
	panic("implement me")
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

var _ harmonytask.TaskInterface = (*TaskUnsealSdr)(nil)
