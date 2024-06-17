package seal

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/filler"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

type SyntheticProofTask struct {
	sp *SealPoller
	db *harmonydb.DB
	sc *ffi.SealCalls

	//max int
}

func NewSyntheticProofTask(sp *SealPoller, db *harmonydb.DB, sc *ffi.SealCalls) *SyntheticProofTask {
	return &SyntheticProofTask{
		sp: sp,
		db: db,
		sc: sc,

		//max: maxTrees,
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

	var pieces []struct {
		PieceIndex  int64  `db:"piece_index"`
		PieceCID    string `db:"piece_cid"`
		PieceSize   int64  `db:"piece_size"`
		DataRawSize *int64 `db:"data_raw_size"`
	}

	err = s.db.Select(ctx, &pieces, `
		SELECT piece_index, piece_cid, piece_size, data_raw_size
		FROM sectors_sdr_initial_pieces
		WHERE sp_id = $1 AND sector_number = $2 ORDER BY piece_index ASC`, sectorParams.SpID, sectorParams.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("getting pieces: %w", err)
	}

	ssize, err := sectorParams.RegSealProof.SectorSize()
	if err != nil {
		return false, xerrors.Errorf("getting sector size: %w", err)
	}

	var offset abi.UnpaddedPieceSize
	var pieceInfos []abi.PieceInfo

	if len(pieces) > 0 {
		for _, p := range pieces {
			c, err := cid.Parse(p.PieceCID)
			if err != nil {
				return false, xerrors.Errorf("parsing piece cid: %w", err)
			}

			pads, padLength := ffiwrapper.GetRequiredPadding(offset.Padded(), abi.PaddedPieceSize(p.PieceSize))
			offset += padLength.Unpadded()

			for _, pad := range pads {
				pieceInfos = append(pieceInfos, abi.PieceInfo{
					Size:     pad,
					PieceCID: zerocomm.ZeroPieceCommitment(pad.Unpadded()),
				})
			}

			pieceInfos = append(pieceInfos, abi.PieceInfo{
				Size:     abi.PaddedPieceSize(p.PieceSize),
				PieceCID: c,
			})
			offset += abi.UnpaddedPieceSize(*p.DataRawSize)
		}

		fillerSize, err := filler.FillersFromRem(abi.PaddedPieceSize(ssize).Unpadded() - offset)
		if err != nil {
			return false, xerrors.Errorf("failed to calculate the final padding: %w", err)
		}
		for _, fil := range fillerSize {
			pieceInfos = append(pieceInfos, abi.PieceInfo{
				Size:     fil.Padded(),
				PieceCID: zerocomm.ZeroPieceCommitment(fil),
			})
		}
	} else {
		fillerSize, err := filler.FillersFromRem(abi.PaddedPieceSize(ssize).Unpadded())
		if err != nil {
			return false, xerrors.Errorf("failed to calculate the filler size: %w", err)
		}
		for _, fil := range fillerSize {
			pieceInfos = append(pieceInfos, abi.PieceInfo{
				Size:     fil.Padded(),
				PieceCID: zerocomm.ZeroPieceCommitment(fil),
			})
		}
	}

	err = s.sc.SyntheticProofs(ctx, &taskID, sref, sealed, unsealed, sectorParams.TicketValue, pieceInfos)
	if err != nil {
		return false, xerrors.Errorf("generating synthetic proofs: %w", err)
	}

	err = s.markFinished(ctx, sectorParams.SpID, sectorParams.SectorNumber)
	if err != nil {
		return false, err
	}

	return true, nil
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

func (s *SyntheticProofTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SyntheticProofTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30)
	ram := uint64(8 << 30)
	if isDevnet {
		ssize = abi.SectorSize(2 << 20)
		ram = uint64(1 << 30)
	}

	res := harmonytask.TaskTypeDetails{
		Max:  50,
		Name: "SyntheticProofs",
		Cost: resources.Resources{
			Cpu:     1,
			Gpu:     0,
			Ram:     ram,
			Storage: s.sc.Storage(s.taskToSector, storiface.FTNone, storiface.FTCache|storiface.FTSealed, ssize, storiface.PathSealing, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 5,
		Follows:     nil,
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
