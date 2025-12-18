package pdpv1

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"strconv"
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

func (c *PDPCommpTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	// CommP task can be of 2 types
	// 1. Using ParkPiece pieceRef
	// 2. Using remote HTTP reader
	// ParkPiece should be scheduled on same node which has the piece
	// Remote HTTP ones can be scheduled on any node

	ctx := context.Background()

	var tasks []struct {
		TaskID    harmonytask.TaskID `db:"commp_task_id"`
		StorageID string             `db:"storage_id"`
		Url       sql.NullString     `db:"url"`
	}

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	comm, err := c.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		err = tx.Select(&tasks, `  SELECT 
											commp_task_id, 
											url
										FROM 
											market_mk12_deal_pipeline
										WHERE 
											commp_task_id = ANY ($1)
										
										UNION ALL
										
										SELECT 
											commp_task_id, 
											url
										FROM 
											market_mk20_pipeline
										WHERE 
											commp_task_id = ANY ($1);
										`, indIDs)
		if err != nil {
			return false, xerrors.Errorf("failed to get deal details from DB: %w", err)
		}

		if storiface.FTPiece != 32 {
			panic("storiface.FTPiece != 32")
		}

		for _, task := range tasks {
			if task.Url.Valid {
				goUrl, err := url.Parse(task.Url.String)
				if err != nil {
					return false, xerrors.Errorf("parsing data URL: %w", err)
				}
				if goUrl.Scheme == "pieceref" {
					refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
					if err != nil {
						return false, xerrors.Errorf("parsing piece reference number: %w", err)
					}

					// get pieceID
					var pieceID []struct {
						PieceID storiface.PieceNumber `db:"piece_id"`
					}
					err = tx.Select(&pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refNum)
					if err != nil {
						return false, xerrors.Errorf("getting pieceID: %w", err)
					}

					var sLocation string

					err = tx.QueryRow(`
					SELECT storage_id FROM sector_location 
						WHERE miner_id = 0 AND sector_num = $1 AND sector_filetype = 32`, pieceID[0].PieceID).Scan(&sLocation)

					if err != nil {
						return false, xerrors.Errorf("failed to get storage location from DB: %w", err)
					}

					task.StorageID = sLocation
				}
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return nil, err
	}

	if !comm {
		return nil, xerrors.Errorf("failed to commit the transaction")
	}

	ls, err := c.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}

	acceptables := map[harmonytask.TaskID]bool{}

	for _, t := range ids {
		acceptables[t] = true
	}

	for _, t := range tasks {
		if _, ok := acceptables[t.TaskID]; !ok {
			continue
		}

		for _, l := range ls {
			if string(l.ID) == t.StorageID {
				return &t.TaskID, nil
			}
		}
	}

	// If no local pieceRef was found then just return first TaskID
	return &ids[0], nil
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
