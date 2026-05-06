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

func (f *FixParkPieceTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

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

	// For each duplicate group, pick a winner (complete+readable, else
	// lowest-id non-active), move non-active refs onto it, delete the
	// now-refless losers. Active rows are left alone and revisited next
	// pass. Then drop and recreate the index unconditionally - IF NOT
	// EXISTS is a no-op against an existing INVALID index.
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
		if !stillOwned() {
			return false, nil
		}
		dup := duplicates[key]

		// Prefer complete + readable; ORDER BY puts complete rows first, so
		// the first non-active row is the lowest-id fallback.
		var winner, fallback *int64
		for _, row := range dup {
			if row.TaskActive {
				continue
			}
			if fallback == nil {
				fallback = new(row.ID)
			}
			if !row.Complete {
				continue
			}
			pr, err := f.sc.PieceReader(ctx, storiface.PieceNumber(row.ID))
			if err != nil {
				continue
			}
			_ = pr.Close()
			winner = new(row.ID)
			break
		}
		if winner == nil {
			winner = fallback
		}
		if winner == nil {
			// All rows active; retry next pass.
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

		// Move refs and delete refless losers atomically so the partial
		// index sees a clean state.
		_, err = f.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			if _, terr := tx.Exec(`UPDATE parked_piece_refs SET piece_id = $1 WHERE piece_id = ANY($2::bigint[]);`, *winner, loserIDs); terr != nil {
				return false, xerrors.Errorf("updating parked_piece_refs: %w", terr)
			}
			if _, terr := tx.Exec(`DELETE FROM parked_pieces WHERE id = ANY($1::bigint[]);`, loserIDs); terr != nil {
				return false, xerrors.Errorf("deleting refless loser rows: %w", terr)
			}
			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return false, xerrors.Errorf("consolidating duplicate group: %w", err)
		}

		// Best-effort: remove the on-disk piece data for losers we just
		// deleted from the DB. Errors are logged; orphan cleanup will
		// catch any stragglers.
		for _, loserID := range loserIDs {
			if rerr := f.sc.RemovePiece(ctx, storiface.PieceNumber(loserID)); rerr != nil {
				log.Errorw("removing piece after consolidation", "piece_id", loserID, "error", rerr)
			}
		}
	}

	// IF NOT EXISTS is a no-op against an existing INVALID index, so drop
	// unconditionally before creating.
	if _, err = f.db.Exec(ctx, `DROP INDEX IF EXISTS parked_pieces_active_piece_key;`); err != nil {
		return false, xerrors.Errorf("dropping parked_pieces_active_piece_key: %w", err)
	}
	if _, err = f.db.Exec(ctx, `CREATE UNIQUE INDEX parked_pieces_active_piece_key ON parked_pieces (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL;`); err != nil {
		if harmonydb.IsErrUniqueContraint(err) {
			// Active rows in some groups blocked consolidation; retry next pass.
			log.Warnf("parked_pieces_active_piece_key not yet creatable: %s", err)
			return true, nil
		}
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
