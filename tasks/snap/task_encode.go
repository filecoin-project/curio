package snap

import (
	"context"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/tasks/seal"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-commp-utils/nonffi"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type EncodeTask struct {
	max int

	sc *ffi.SealCalls
	db *harmonydb.DB
}

func NewEncodeTask(sc *ffi.SealCalls, db *harmonydb.DB, max int) *EncodeTask {
	return &EncodeTask{
		max: max,
		sc:  sc,
		db:  db,
	}
}

/*
CREATE TABLE sectors_snap_pipeline (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    start_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    upgrade_proof INT NOT NULL,
    -- preload
    -- todo sector preload logic
    data_assigned BOOLEAN NOT NULL DEFAULT FALSE,
    -- encode
    update_unsealed_cid TEXT,
    update_sealed_cid TEXT,
    task_id_encode BIGINT,
    after_encode BOOLEAN NOT NULL DEFAULT FALSE,
    -- prove
    task_id_prove BIGINT,
    after_prove BOOLEAN NOT NULL DEFAULT FALSE,
    -- submit
    task_id_submit BIGINT,
    after_submit BOOLEAN NOT NULL DEFAULT FALSE,
    -- move storage
    task_id_move_storage BIGINT,
    after_move_storage BOOLEAN NOT NULL DEFAULT FALSE,
    FOREIGN KEY (sp_id, sector_number) REFERENCES sectors_meta (sp_id, sector_num),
    PRIMARY KEY (sp_id, sector_number)
)
*/

func (e *EncodeTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		UpdateProof  int64 `db:"update_proof"`

		RegSealProof int64 `db:"reg_seal_proof"`
	}

	ctx := context.Background()

	err = e.db.Select(ctx, &tasks, `
		SELECT snp.sp_id, snp.sector_number, snp.upgrade_proof, sm.reg_seal_proof
		FROM sectors_snap_pipeline snp INNER JOIN sectors_meta sm ON snp.sp_id = sm.sp_id AND snp.sector_number = sm.sector_num
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

	var pieces []struct {
		PieceIndex int64  `db:"piece_index"`
		PieceCID   string `db:"piece_cid"`
		PieceSize  int64  `db:"piece_size"`
	}
	err = e.db.Select(ctx, &pieces, `
		SELECT piece_index, piece_cid, piece_size
		FROM sectors_snap_initial_pieces
		WHERE sp_id = $1 AND sector_number = $2 ORDER BY piece_index ASC`, sectorParams.SpID, sectorParams.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("getting pieces: %w", err)
	}

	pieceInfos := make([]abi.PieceInfo, len(pieces))
	for i, p := range pieces {
		c, err := cid.Parse(p.PieceCID)
		if err != nil {
			return false, xerrors.Errorf("parsing piece cid: %w", err)
		}

		pieceInfos[i] = abi.PieceInfo{
			Size:     abi.PaddedPieceSize(p.PieceSize),
			PieceCID: c,
		}
	}

	dealUnsealedCID, err := nonffi.GenerateUnsealedCID(abi.RegisteredSealProof(sectorParams.RegSealProof), pieceInfos)
	if err != nil {
		return false, xerrors.Errorf("computing CommD: %w", err)
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

	sealed, unsealed, err := e.sc.EncodeUpdate(ctx, taskID, abi.RegisteredUpdateProof(sectorParams.UpdateProof), sref, data.Data, pieceInfos)
	if err != nil {
		return false, xerrors.Errorf("ffi update encode: %w", err)
	}

	if dealUnsealedCID != unsealed {
		return false, xerrors.Errorf("unsealed cid mismatch")
	}

	_, err = e.db.Exec(ctx, `UPDATE sectors_snap_pipeline SET update_unsealed_cid = $1, update_sealed_cid = $2, after_encode = TRUE
                             WHERE sp_id = $3 AND sector_number = $4`,
		unsealed.String(), sealed.String(), sectorParams.SpID, sectorParams.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("updating sector pipeline: %w", err)
	}

	return true, nil
}

func (e *EncodeTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (e *EncodeTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if seal.IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}
	gpu := 1.0
	if seal.IsDevnet {
		gpu = 0
	}

	return harmonytask.TaskTypeDetails{
		Max:  e.max,
		Name: "UpdateEncode",
		Cost: resources.Resources{
			Cpu:     1,
			Ram:     1 << 30, // todo correct value
			Gpu:     gpu,
			Storage: e.sc.Storage(e.taskToSector, storiface.FTUpdate|storiface.FTUpdateCache, storiface.FTNone, ssize, storiface.PathSealing, 1.0),
		},
		MaxFailures: 3,
	}
}

func (e *EncodeTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	return
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

var _ harmonytask.TaskInterface = &EncodeTask{}
