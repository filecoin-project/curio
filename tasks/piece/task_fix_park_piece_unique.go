package piece

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/curiostorage/harmonyquery"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/storiface"
)

type FixParkPieceTask struct {
	db *harmonydb.DB
	sc *ffi.SealCalls

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewFixParkPieceTask(db *harmonydb.DB, sc *ffi.SealCalls) *FixParkPieceTask {
	return &FixParkPieceTask{
		db: db,
		sc: sc,
	}
}

func (f *FixParkPieceTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Check index first
	var exists bool
	err = f.db.QueryRow(ctx, `
					SELECT EXISTS (
					  SELECT 1
					  FROM pg_indexes
					  WHERE schemaname = current_schema()
						AND tablename = 'parked_pieces'
						AND indexname = 'parked_pieces_active_piece_key'
						AND indexdef LIKE '%UNIQUE INDEX parked_pieces_active_piece_key%'
						AND indexdef LIKE '%(piece_cid, piece_padded_size, long_term)%'
						AND indexdef LIKE '%WHERE cleanup_task_id IS NULL%'
					);`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking if index exists: %w", err)
	}

	if exists {
		return true, nil
	}

	var duplicates []struct {
		PieceCid  string  `db:"piece_cid"`
		PieceSize int64   `db:"piece_padded_size"`
		LongTerm  bool    `db:"long_term"`
		RowCount  int64   `db:"row_count"`
		IDs       []int64 `db:"ids"`
	}

	err = f.db.Select(ctx, &duplicates, `SELECT piece_cid, piece_padded_size, long_term,
										   count(*) AS row_count,
										   array_agg(id ORDER BY id) AS ids
									FROM parked_pieces
									WHERE cleanup_task_id IS NULL
									  AND complete = true
									GROUP BY 1,2,3
									HAVING count(*) > 1;`)

	if err != nil {
		return false, xerrors.Errorf("querying parked piece: %w", err)
	}

	for _, dup := range duplicates {
		if stillOwned() {
			comm, err := f.db.BeginTransaction(ctx, func(tx *harmonyquery.Tx) (commit bool, err error) {
				for _, id := range dup.IDs {
					// Verify that the piece is still parked
					// If piece exists then check if we can access the data
					pr, err := f.sc.PieceReader(ctx, storiface.PieceNumber(id))
					if err != nil {
						// If piece does not exist then we will park it otherwise fail here
						if !errors.Is(err, storiface.ErrSectorNotFound) {
							continue
						}
					}
					defer func() {
						_ = pr.Close()
					}()

					_, err = tx.Exec(`UPDATE parked_piece_refs 
										SET piece_id = $1 
										WHERE piece_id = ANY($2::bigint[]);`, id)
					if err != nil {
						return false, xerrors.Errorf("updating parked piece refs: %w", err)
					}

					return true, nil
				}

				return false, xerrors.Errorf("no suitable piece found for ids: %d", dup.IDs)
			}, harmonydb.OptionRetry())
			if err != nil {
				return false, xerrors.Errorf("updating DB: %w", err)
			}
			if !comm {
				return false, xerrors.Errorf("failed to commit the transaction")
			}
		}
	}

	_, err = f.db.Exec(ctx, `CREATE UNIQUE INDEX parked_pieces_active_piece_key ON parked_pieces (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL;`)
	if err != nil {
		return false, xerrors.Errorf("creating index: %w", err)
	}

	return true, nil
}

func (f *FixParkPieceTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (f *FixParkPieceTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "FixParkPiece",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 10,
		RetryWait: func(retries int) time.Duration {
			const baseWait, maxWait, factor = 5 * time.Second, time.Minute, 1.5
			// Use math.Pow for exponential backoff
			return min(time.Duration(float64(baseWait)*math.Pow(factor, float64(retries))), maxWait)
		},
		IAmBored: harmonytask.SingletonTaskAdder(time.Hour, f),
	}
}

func (f *FixParkPieceTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &FixParkPieceTask{}
var _ = harmonytask.Reg(&FixParkPieceTask{})
