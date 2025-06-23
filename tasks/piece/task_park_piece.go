package piece

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/hashicorp/go-multierror"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/storiface"
)

var log = logging.Logger("cu-piece")
var PieceParkPollInterval = time.Second * 5

const ParkMinFreeStoragePercent = 20

// ParkPieceTask gets a piece from some origin, and parks it in storage
// Pieces are always f00, piece ID is mapped to pieceCID in the DB
type ParkPieceTask struct {
	db     *harmonydb.DB
	sc     *ffi2.SealCalls
	remote *paths.Remote

	TF promise.Promise[harmonytask.AddTaskFunc]

	max int

	longTerm bool // Indicates if the task is for long-term pieces
}

func NewParkPieceTask(db *harmonydb.DB, sc *ffi2.SealCalls, max int) (*ParkPieceTask, error) {
	return newPieceTask(db, sc, nil, max, false)
}

func NewStorePieceTask(db *harmonydb.DB, sc *ffi2.SealCalls, remote *paths.Remote, max int) (*ParkPieceTask, error) {
	return newPieceTask(db, sc, remote, max, true)
}

func newPieceTask(db *harmonydb.DB, sc *ffi2.SealCalls, remote *paths.Remote, max int, longTerm bool) (*ParkPieceTask, error) {
	pt := &ParkPieceTask{
		db:       db,
		sc:       sc,
		remote:   remote,
		max:      max,
		longTerm: longTerm,
	}

	ctx := context.Background()

	go pt.pollPieceTasks(ctx)
	return pt, nil
}

func (p *ParkPieceTask) pollPieceTasks(ctx context.Context) {
	for {
		// Select parked pieces with no task_id and matching longTerm flag
		var pieceIDs []struct {
			ID storiface.PieceNumber `db:"id"`
		}

		err := p.db.Select(ctx, &pieceIDs, `
            SELECT id 
            FROM parked_pieces 
            WHERE long_term = $1 
              AND complete = FALSE 
              AND task_id IS NULL
        `, p.longTerm)
		if err != nil {
			log.Errorf("failed to get parked pieces: %s", err)
			time.Sleep(PieceParkPollInterval)
			continue
		}

		if len(pieceIDs) == 0 {
			time.Sleep(PieceParkPollInterval)
			continue
		}

		for _, pieceID := range pieceIDs {
			pieceID := pieceID

			// Create a task for each piece
			p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				// Update
				n, err := tx.Exec(
					`UPDATE parked_pieces SET task_id = $1 WHERE id = $2 AND complete = FALSE AND task_id IS NULL AND long_term = $3`,
					id, pieceID.ID, p.longTerm)
				if err != nil {
					return false, xerrors.Errorf("updating parked piece: %w", err)
				}

				// Commit only if we updated the piece
				return n > 0, nil
			})
		}
	}
}

func (p *ParkPieceTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Fetch piece data
	var piecesData []struct {
		PieceID         int64     `db:"id"`
		PieceCreatedAt  time.Time `db:"created_at"`
		PieceCID        string    `db:"piece_cid"`
		Complete        bool      `db:"complete"`
		PiecePaddedSize int64     `db:"piece_padded_size"`
		PieceRawSize    int64     `db:"piece_raw_size"`
	}

	// Select the piece data using the task ID and longTerm flag
	err = p.db.Select(ctx, &piecesData, `
        SELECT id, created_at, piece_cid, complete, piece_padded_size, piece_raw_size
        FROM parked_pieces
        WHERE task_id = $1 AND long_term = $2
    `, taskID, p.longTerm)
	if err != nil {
		return false, xerrors.Errorf("fetching piece data: %w", err)
	}

	if len(piecesData) == 0 {
		return false, xerrors.Errorf("no piece data found for task_id: %d", taskID)
	}

	pieceData := piecesData[0]

	if pieceData.Complete {
		log.Warnw("park piece task already complete", "task_id", taskID, "piece_cid", pieceData.PieceCID)
		return true, nil
	}

	// Fetch reference data
	var refData []struct {
		DataURL     string          `db:"data_url"`
		DataHeaders json.RawMessage `db:"data_headers"`
	}

	err = p.db.Select(ctx, &refData, `
        SELECT data_url, data_headers
        FROM parked_piece_refs
        WHERE piece_id = $1 AND data_url IS NOT NULL`, pieceData.PieceID)
	if err != nil {
		return false, xerrors.Errorf("fetching reference data: %w", err)
	}

	if len(refData) == 0 {
		return false, xerrors.Errorf("no refs found for piece_id: %d", pieceData.PieceID)
	}

	var merr error

	for i := range refData {
		if refData[i].DataURL != "" {
			hdrs := make(http.Header)
			err = json.Unmarshal(refData[i].DataHeaders, &hdrs)
			if err != nil {
				return false, xerrors.Errorf("unmarshaling reference data headers: %w", err)
			}
			upr := dealdata.NewUrlReader(p.remote, refData[i].DataURL, hdrs, pieceData.PieceRawSize)

			defer func() {
				_ = upr.Close()
			}()

			pnum := storiface.PieceNumber(pieceData.PieceID)

			storageType := storiface.PathSealing
			if p.longTerm {
				storageType = storiface.PathStorage
			}

			if err := p.sc.WritePiece(ctx, &taskID, pnum, pieceData.PieceRawSize, upr, storageType); err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("write piece: %w", err))
				continue
			}

			// Update the piece as complete after a successful write.
			_, err = p.db.Exec(ctx, `UPDATE parked_pieces SET complete = TRUE, task_id = NULL WHERE id = $1`, pieceData.PieceID)
			if err != nil {
				return false, xerrors.Errorf("marking piece as complete: %w", err)
			}

			return true, nil
		}
	}

	// If no suitable data URL is found
	return false, xerrors.Errorf("no suitable data URL found for piece_id %d: %w", pieceData.PieceID, merr)
}

func (p *ParkPieceTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (p *ParkPieceTask) TypeDetails() harmonytask.TaskTypeDetails {
	const maxSizePiece = 64 << 30

	taskName := "ParkPiece"
	if p.longTerm {
		taskName = "StorePiece"
	}

	storageType := storiface.PathSealing
	if p.longTerm {
		storageType = storiface.PathStorage
	}

	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(p.max),
		Name: taskName,
		Cost: resources.Resources{
			Cpu:     1,
			Gpu:     0,
			Ram:     64 << 20,
			Storage: p.sc.Storage(p.taskToRef, storiface.FTPiece, storiface.FTNone, maxSizePiece, storageType, ParkMinFreeStoragePercent),
		},
		MaxFailures: 10,
		RetryWait: func(retries int) time.Duration {
			const baseWait, maxWait, factor = 5 * time.Second, time.Minute, 1.5
			// Use math.Pow for exponential backoff
			return min(time.Duration(float64(baseWait)*math.Pow(factor, float64(retries))), maxWait)
		},
	}
}

func (p *ParkPieceTask) taskToRef(id harmonytask.TaskID) (ffi2.SectorRef, error) {
	var pieceIDs []struct {
		ID storiface.PieceNumber `db:"id"`
	}

	err := p.db.Select(context.Background(), &pieceIDs, `SELECT id FROM parked_pieces WHERE task_id = $1`, id)
	if err != nil {
		return ffi2.SectorRef{}, xerrors.Errorf("getting piece id: %w", err)
	}

	if len(pieceIDs) != 1 {
		return ffi2.SectorRef{}, xerrors.Errorf("expected 1 piece id, got %d", len(pieceIDs))
	}

	pref := pieceIDs[0].ID.Ref()

	return ffi2.SectorRef{
		SpID:         int64(pref.ID.Miner),
		SectorNumber: int64(pref.ID.Number),
		RegSealProof: pref.ProofType,
	}, nil
}

func (p *ParkPieceTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &ParkPieceTask{}
var _ = harmonytask.Reg(&ParkPieceTask{longTerm: false})
var _ = harmonytask.Reg(&ParkPieceTask{longTerm: true})
