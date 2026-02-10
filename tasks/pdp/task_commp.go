package pdp

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-commp-utils/writer"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/mk20"
)

type PDPCommpTask struct {
	db  *harmonydb.DB
	sc  *ffi.SealCalls
	max int
}

func NewPDPCommpTask(db *harmonydb.DB, sc *ffi.SealCalls, max int) *PDPCommpTask {
	return &PDPCommpTask{
		db:  db,
		sc:  sc,
		max: max,
	}
}

func (c *PDPCommpTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var pieces []struct {
		Pcid string `db:"piece_cid_v2"`
		Ref  int64  `db:"piece_ref"`
		ID   string `db:"id"`
	}

	err = c.db.Select(ctx, &pieces, `SELECT id, piece_cid_v2, piece_ref FROM pdp_pipeline 
										WHERE commp_task_id = $1 
										  AND downloaded = TRUE;`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting piece details: %w", err)
	}
	if len(pieces) != 1 {
		return false, xerrors.Errorf("expected 1 piece, got %d", len(pieces))
	}
	piece := pieces[0]

	pcid, err := cid.Parse(piece.Pcid)
	if err != nil {
		return false, xerrors.Errorf("parsing piece: %w", err)
	}

	pi, err := mk20.GetPieceInfo(pcid)
	if err != nil {
		return false, xerrors.Errorf("getting piece info: %w", err)
	}

	// get pieceID
	var pieceID []struct {
		PieceID storiface.PieceNumber `db:"piece_id"`
	}
	err = c.db.Select(ctx, &pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, piece.Ref)
	if err != nil {
		return false, xerrors.Errorf("getting pieceID: %w", err)
	}

	if len(pieceID) != 1 {
		return false, xerrors.Errorf("expected 1 pieceID, got %d", len(pieceID))
	}

	pr, err := c.sc.PieceReader(ctx, pieceID[0].PieceID)
	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}

	pReader, pSz := padreader.New(pr, pi.RawSize)

	defer func() {
		_ = pr.Close()
	}()

	wr := new(commp.Calc)
	defer wr.Reset()
	written, err := io.CopyBuffer(wr, pReader, make([]byte, writer.CommPBuf))
	if err != nil {
		return false, xerrors.Errorf("copy into commp writer: %w", err)
	}

	if written != int64(pSz) {
		return false, xerrors.Errorf("number of bytes written to CommP writer %d not equal to the file size %d", written, pSz)
	}

	digest, size, err := wr.Digest()
	if err != nil {
		return false, xerrors.Errorf("computing commP failed: %w", err)
	}

	calculatedCommp, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		return false, xerrors.Errorf("computing commP failed: %w", err)
	}

	if !calculatedCommp.Equals(pi.PieceCIDV1) {
		return false, xerrors.Errorf("commp mismatch: calculated %s and expected %s", calculatedCommp, pi.PieceCIDV1)
	}

	if pi.Size != abi.PaddedPieceSize(size) {
		return false, xerrors.Errorf("pieceSize mismatch: expected %d, got %d", pi.Size, abi.PaddedPieceSize(size))
	}

	n, err := c.db.Exec(ctx, `UPDATE pdp_pipeline SET after_commp = TRUE, commp_task_id = NULL
										 	WHERE id = $1 
											  AND piece_cid_v2 = $2
											  AND downloaded = TRUE
											  AND after_commp = FALSE 
										 	  AND commp_task_id = $3`,
		piece.ID, piece.Pcid, taskID)

	if err != nil {
		return false, xerrors.Errorf("store commp success: updating pdp pipeline: %w", err)
	}

	if n != 1 {
		return false, xerrors.Errorf("store commp success: updated %d rows", n)
	}

	return true, nil

}

func (c *PDPCommpTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	// CommP task can be of 2 types
	// 1. Using ParkPiece pieceRef
	// 2. Using remote HTTP reader
	// ParkPiece should be scheduled on same node which has the piece
	// Remote HTTP ones can be scheduled on any node

	if storiface.FTPiece != 32 {
		panic("storiface.FTPiece != 32")
	}

	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	var selected []harmonytask.TaskID

	err := c.db.QueryRow(ctx, `SELECT COALESCE(array_agg(commp_task_id), '{}')::bigint[] AS commp_task_ids FROM 
										(
										    SELECT p.commp_task_id
											FROM pdp_pipeline p
											JOIN parked_piece_refs ppr
											  ON p.piece_ref IS NOT NULL
											 AND p.piece_ref = ppr.ref_id
											JOIN sector_location sl
											  ON sl.miner_id = 0
											 AND sl.sector_num = ppr.piece_id
											 AND sl.sector_filetype = 32
											JOIN storage_path sp
											  ON sp.storage_id = sl.storage_id
											WHERE p.commp_task_id = ANY($1::bigint[])
											  AND sp.urls IS NOT NULL
											  AND sp.urls LIKE '%' || $2 || '%'
											LIMIT 100
										) s;`, indIDs).Scan(&selected)
	if err != nil {
		return nil, nil
	}

	return selected, nil
}

func (c *PDPCommpTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(c.max),
		Name: "PDPCommP",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 1 << 30,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(3*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return c.schedule(context.Background(), taskFunc)
		}),
	}
}

func (c *PDPCommpTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var did string
			err := tx.QueryRow(`SELECT id FROM pdp_pipeline 
								  WHERE commp_task_id IS NULL 
									AND after_commp = FALSE
									AND downloaded = TRUE`).Scan(&did)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("failed to query pdp_pipeline: %w", err)
			}
			if did == "" {
				return false, xerrors.Errorf("no valid deal ID found for scheduling")
			}

			_, err = tx.Exec(`UPDATE pdp_pipeline SET commp_task_id = $1 WHERE id = $2 AND commp_task_id IS NULL AND after_commp = FALSE AND downloaded = TRUE`, id, did)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_pipeline: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (c *PDPCommpTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&PDPCommpTask{})
var _ harmonytask.TaskInterface = &PDPCommpTask{}
