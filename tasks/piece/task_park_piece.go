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
var PieceParkPollInterval = time.Second

const ParkMinFreeStoragePercent = 20

// ParkPieceTask gets a piece from some origin, and parks it in storage
// Pieces are always f00, piece ID is mapped to pieceCID in the DB
type ParkPieceTask struct {
	db     *harmonydb.DB
	sc     *ffi2.SealCalls
	remote *paths.Remote

	TF promise.Promise[harmonytask.AddTaskFunc]

	max int

	// maxInPark is the maximum number of pieces that should be in storage + active tasks writing to storage on this node
	maxInPark int

	longTerm bool // Indicates if the task is for long-term pieces

	// supraseal special interaction - during phase 2, we don't want to park pieces - gpu not available for hours
	p2Active func() bool
}

func NewParkPieceTask(db *harmonydb.DB, sc *ffi2.SealCalls, max int, maxInPark int, p2Active func() bool) (*ParkPieceTask, error) {
	return newPieceTask(db, sc, nil, max, maxInPark, false, p2Active)
}

func NewStorePieceTask(db *harmonydb.DB, sc *ffi2.SealCalls, remote *paths.Remote, max int) (*ParkPieceTask, error) {
	return newPieceTask(db, sc, remote, max, 0, true, nil)
}

func newPieceTask(db *harmonydb.DB, sc *ffi2.SealCalls, remote *paths.Remote, max int, maxInPark int, longTerm bool, p2Active func() bool) (*ParkPieceTask, error) {
	pt := &ParkPieceTask{
		db:        db,
		sc:        sc,
		remote:    remote,
		max:       max,
		maxInPark: maxInPark,
		longTerm:  longTerm,
		p2Active:  p2Active,
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
			upr := dealdata.NewUrlReader(p.remote, refData[i].DataURL, hdrs, pieceData.PieceRawSize, "parkpiece")

			defer func() {
				_ = upr.Close()
			}()

			pnum := storiface.PieceNumber(pieceData.PieceID)

			storageType := storiface.PathSealing
			if p.longTerm {
				storageType = storiface.PathStorage
			}

			if err := p.sc.WritePiece(ctx, &taskID, pnum, pieceData.PieceRawSize, upr, storageType); err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("write piece (read so far: %d): %w", upr.ReadSoFar(), err))
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
	if p.p2Active != nil && p.p2Active() {
		return nil, nil
	}

	if p.maxInPark <= 0 {
		id := ids[0]
		return &id, nil
	}

	ctx := context.Background()

	// Load local storage IDs
	ls, err := p.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}
	local := map[string]struct{}{}
	storageIDs := []string{}
	for _, l := range ls {
		local[string(l.ID)] = struct{}{}
		storageIDs = append(storageIDs, string(l.ID))
	}

	// Count pieces in storage
	// select count(1), storage_id from sector_location where sector_filetype = 32 and storage_id = ANY ($1) group by storage_id

	var count int64
	err = p.db.QueryRow(ctx, `
		SELECT count(1) FROM sector_location WHERE sector_filetype = $1 AND storage_id = ANY ($2)
	`, storiface.FTPiece, storageIDs).Scan(&count)
	if err != nil {
		return nil, xerrors.Errorf("counting pieces in storage: %w", err)
	}

	log.Infow("park piece task can accept", "ids", ids, "maxInPark", p.maxInPark, "count", count)
	if count >= int64(p.maxInPark) {
		log.Infow("park piece task can accept", "skip", "yes-in-storage", "ids", ids, "maxInPark", p.maxInPark, "count", count, "maxInPark", p.maxInPark)
		return nil, nil
	}

	// count tasks running on this node
	hostAndPort := engine.Host()

	var running int64
	err = p.db.QueryRow(ctx, `
		SELECT count(1)
		FROM harmony_task
		WHERE name = $1
		  AND owner_id = (
		    SELECT id FROM harmony_machines WHERE host_and_port = $2
		  )
	`, p.TypeDetails().Name, hostAndPort).Scan(&running)
	if err != nil {
		return nil, xerrors.Errorf("counting running piece tasks: %w", err)
	}

	if count+running >= int64(p.maxInPark) {
		log.Infow("park piece task can accept", "skip", "yes-in-running", "ids", ids, "running", running, "count+running", count+running, "maxInPark", p.maxInPark)
		return nil, nil
	}

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
