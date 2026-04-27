package piece

import (
	"context"
	"fmt"
	"math"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/storiface"
)

// TODO: This task should be removed at NV30 upgrade along with CTEs for parked_piece insert

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

func (f *FixParkPieceTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	// Check index first
	var exists bool
	err = f.db.QueryRow(ctx, `
					SELECT EXISTS (
						SELECT 1
						FROM pg_catalog.pg_index ix
						JOIN pg_catalog.pg_class idx ON idx.oid = ix.indexrelid
						JOIN pg_catalog.pg_class tbl ON tbl.oid = ix.indrelid
						JOIN pg_catalog.pg_namespace ns ON ns.oid = tbl.relnamespace
						JOIN pg_catalog.pg_am am ON am.oid = idx.relam
						WHERE ns.nspname = current_schema()
						  AND idx.relnamespace = ns.oid
						  AND tbl.relname = 'parked_pieces'
						  AND idx.relname = 'parked_pieces_active_piece_key'
						  AND idx.relkind = 'i'
						  AND am.amname = 'btree'
						  AND ix.indisunique
						  AND ix.indisvalid
						  AND ix.indisready
						  AND ix.indislive
						  AND ix.indnkeyatts = 3
						  AND ix.indnatts = 3
						  AND ix.indexprs IS NULL
						  AND ARRAY(
							  SELECT pg_catalog.pg_get_indexdef(ix.indexrelid, n, true)
							  FROM generate_series(1, ix.indnkeyatts) AS n
							  ORDER BY n
						  ) = ARRAY['piece_cid', 'piece_padded_size', 'long_term']
						  AND pg_catalog.pg_get_expr(ix.indpred, ix.indrelid, false) = '(cleanup_task_id IS NULL)'
					);`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking if index exists: %w", err)
	}

	if exists {
		return true, nil
	}

	/*
		Look at duplicate groups across all active parked_pieces rows, not just completed rows,
		but only processes groups that have at least one complete row.
		It picks a readable complete row as the winner, moves refs from stale/no-task losers to that winner,
		and skips losers whose task_id still exists in harmony_task.

		Covered Cases:
		1. complete + incomplete with task_id IS NULL
		2. complete + incomplete with stale task_id
		3. multiple complete rows
		4. active incomplete rows are left alone

		Ignored:
		1. unique incomplete rows
		2. duplicate groups with no complete row
	*/
	type duplicateKey struct {
		PieceCid  string
		PieceSize int64
		LongTerm  bool
	}

	type duplicateRow struct {
		PieceCid   string `db:"piece_cid"`
		PieceSize  int64  `db:"piece_padded_size"`
		LongTerm   bool   `db:"long_term"`
		ID         int64  `db:"id"`
		Complete   bool   `db:"complete"`
		TaskID     *int64 `db:"task_id"`
		TaskActive bool   `db:"task_active"`
	}

	var duplicateRows []duplicateRow
	err = f.db.Select(ctx, &duplicateRows, `
								SELECT pp.piece_cid, pp.piece_padded_size, pp.long_term,
									   pp.id, pp.complete, pp.task_id,
									   ht.id IS NOT NULL AS task_active
								FROM parked_pieces pp
								LEFT JOIN harmony_task ht ON ht.id = pp.task_id
								JOIN (
									SELECT piece_cid, piece_padded_size, long_term
									FROM parked_pieces
									WHERE cleanup_task_id IS NULL
									GROUP BY 1,2,3
									HAVING count(*) > 1
									   AND bool_or(complete)
								) dup ON dup.piece_cid = pp.piece_cid
									 AND dup.piece_padded_size = pp.piece_padded_size
									 AND dup.long_term = pp.long_term
								WHERE pp.cleanup_task_id IS NULL
								ORDER BY pp.piece_cid, pp.piece_padded_size, pp.long_term, pp.complete DESC, pp.id;`)

	if err != nil {
		return false, xerrors.Errorf("querying parked piece: %w", err)
	}

	duplicates := map[duplicateKey][]duplicateRow{}
	var keys []duplicateKey
	for _, row := range duplicateRows {
		key := duplicateKey{
			PieceCid:  row.PieceCid,
			PieceSize: row.PieceSize,
			LongTerm:  row.LongTerm,
		}
		if _, ok := duplicates[key]; !ok {
			keys = append(keys, key)
		}
		duplicates[key] = append(duplicates[key], row)
	}

	for _, key := range keys {
		dup := duplicates[key]
		if stillOwned() {
			var winner *int64
			for _, row := range dup {
				if !row.Complete {
					continue
				}

				// Verify that the piece is still parked
				// If piece exists then check if we can access the data
				pr, err := f.sc.PieceReader(ctx, storiface.PieceNumber(row.ID))
				if err != nil {
					// If piece does not exist then we check the next one
					continue
				}
				_ = pr.Close()
				winner = new(row.ID)
				break
			}

			if winner == nil {
				continue
			}

			var loserIDs []int64
			for _, row := range dup {
				if row.ID == *winner {
					continue
				}
				if row.TaskActive {
					continue
				}
				loserIDs = append(loserIDs, row.ID)
			}

			if len(loserIDs) == 0 {
				continue
			}

			_, err = f.db.Exec(ctx, `UPDATE parked_piece_refs SET piece_id = $1 WHERE piece_id = ANY($2::bigint[]);`, *winner, loserIDs)
			if err != nil {
				return false, xerrors.Errorf("updating parked piece refs: %w", err)
			}
		} else {
			return false, nil
		}
	}

	_, err = f.db.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS parked_pieces_active_piece_key ON parked_pieces (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL;`)
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
