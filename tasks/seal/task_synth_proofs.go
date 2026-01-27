package seal

import (
	"context"
	"strings"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

type SyntheticProofTask struct {
	sp  *SealPoller
	db  *harmonydb.DB
	sc  *ffi.SealCalls
	max int
}

func NewSyntheticProofTask(sp *SealPoller, db *harmonydb.DB, sc *ffi.SealCalls, maxSynths int) *SyntheticProofTask {
	return &SyntheticProofTask{
		sp:  sp,
		db:  db,
		sc:  sc,
		max: maxSynths,
	}
}

func (s *SyntheticProofTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64                   `db:"sp_id"`
		SectorNumber int64                   `db:"sector_number"`
		RegSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`
		SealedCID    string                  `db:"tree_r_cid"`
		UnsealedCID  string                  `db:"tree_d_cid"`
		TicketValue  []byte                  `db:"ticket_value"`
	}

	err = s.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof, tree_d_cid, tree_r_cid, ticket_value
		FROM sectors_sdr_pipeline
		WHERE task_id_synth = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(sectorParamsArr))
	}
	sectorParams := sectorParamsArr[0]

	// Exit here successfully if synthetic proofs are not required
	_, ok := abi.Synthetic[sectorParams.RegSealProof]
	if !ok {
		serr := s.markFinished(ctx, sectorParams.SpID, sectorParams.SectorNumber)
		if serr != nil {
			return false, serr
		}
		log.Infof("Not generating synthetic proofs for miner %d sector %d", sectorParams.SpID, sectorParams.SectorNumber)
		return true, nil
	}

	var keepUnsealed bool

	if err := s.db.QueryRow(ctx, `SELECT COALESCE(BOOL_OR(NOT data_delete_on_finalize), FALSE) FROM sectors_sdr_initial_pieces WHERE sp_id = $1 AND sector_number = $2`, sectorParams.SpID, sectorParams.SectorNumber).Scan(&keepUnsealed); err != nil {
		return false, err
	}

	sealed, err := cid.Parse(sectorParams.SealedCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse sealed cid: %w", err)
	}

	unsealed, err := cid.Parse(sectorParams.UnsealedCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse unsealed cid: %w", err)
	}

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorParams.SpID),
			Number: abi.SectorNumber(sectorParams.SectorNumber),
		},
		ProofType: sectorParams.RegSealProof,
	}

	dealData, err := dealdata.DealDataSDRPoRep(ctx, s.db, s.sc, sectorParams.SpID, sectorParams.SectorNumber, sectorParams.RegSealProof, true)
	if err != nil {
		return false, xerrors.Errorf("getting deal data: %w", err)
	}

	err = s.sc.SyntheticProofs(ctx, &taskID, sref, sealed, unsealed, sectorParams.TicketValue, dealData.PieceInfos, keepUnsealed)
	if err != nil {
		serr := resetSectorSealingState(ctx, sectorParams.SpID, sectorParams.SectorNumber, err, s.db, s.TypeDetails().Name)
		if serr != nil {
			return false, xerrors.Errorf("generating synthetic proofs: %w", err)
		}
	}

	err = s.markFinished(ctx, sectorParams.SpID, sectorParams.SectorNumber)
	if err != nil {
		return false, err
	}

	return true, nil
}

func resetSectorSealingState(ctx context.Context, spid, secNum int64, err error, db *harmonydb.DB, name string) error {
	if err != nil {
		if strings.Contains(err.Error(), "checking PreCommit") {
			n, serr := db.Exec(ctx, `UPDATE sectors_sdr_pipeline 
						SET after_tree_d = false, tree_d_cid = NULL, after_tree_r = false, after_tree_c = false, task_id_tree_r = NULL, task_id_tree_c = NULL,
						    after_synth = false, task_id_synth = null
						WHERE sp_id = $1 AND sector_number = $2`, spid, secNum)
			if serr != nil {
				return xerrors.Errorf("store %s failure: updating pipeline: Original error %w: DB error %w", name, err, serr)
			}
			if n != 1 {
				return xerrors.Errorf("store %s failure: Original error %w: updated %d rows", name, err, n)
			}
		}
		return err
	}
	return nil
}

func (s *SyntheticProofTask) markFinished(ctx context.Context, spid, sector int64) error {
	n, err := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET after_synth = true, task_id_synth = NULL 
                            WHERE sp_id = $1 AND sector_number = $2`,
		spid, sector)
	if err != nil {
		return xerrors.Errorf("store SyntheticProofs success: updating pipeline: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("store SyntheticProofs success: updated %d rows", n)
	}
	return nil
}

func (s *SyntheticProofTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (s *SyntheticProofTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30)
	ram := uint64(8 << 30)
	if IsDevnet {
		ssize = abi.SectorSize(2 << 20)
		ram = uint64(1 << 30)
	}

	res := harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(s.max),
		Name: "SyntheticProofs",
		Cost: resources.Resources{
			Cpu:     1,
			Gpu:     0,
			Ram:     ram,
			Storage: s.sc.Storage(s.taskToSector, storiface.FTNone, storiface.FTCache|storiface.FTSealed, ssize, storiface.PathSealing, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 5,
	}

	return res
}

func (s *SyntheticProofTask) taskToSector(id harmonytask.TaskID) (ffi.SectorRef, error) {
	var refs []ffi.SectorRef

	err := s.db.Select(context.Background(), &refs, `SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_synth = $1`, id)
	if err != nil {
		return ffi.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

func (s *SyntheticProofTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	s.sp.pollers[pollerSyntheticProofs].Set(taskFunc)
}

var _ harmonytask.TaskInterface = &SyntheticProofTask{}

func (s *SyntheticProofTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := s.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (s *SyntheticProofTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_sdr_pipeline WHERE task_id_synth = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&SyntheticProofTask{})
