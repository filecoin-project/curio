package openpieces

import (
	"context"
	"errors"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/hashspace"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

const DROP_MAX = 4

// DropAdder is the AddTaskFunc to pass to harmonytask.TxWithTask with
// QueueDrop.
func DropAdder() harmonytask.AddTaskFunc {
	return harmonytask.AdderFor(tasknames.HashSpaceDrop)
}

// QueueDrop queues removal of piece CID v2 pieceCID from open-pieces from
// inside a harmonytask.TxWithTask body that uses DropAdder, in the
// transaction that drops its last PDP ref. With id 0 it asks for a task
// when some node has open-pieces disks and no drop is queued yet. The drop
// deletes whatever copies the hash map still names; a piece that was never
// placed is a no-op.
func QueueDrop(tx *harmonydb.Tx, id harmonytask.TaskID, pieceCID string) error {
	if id == 0 {
		var needed bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM hash_space_disk)
			AND NOT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1)`, pieceCID).Scan(&needed); err != nil {
			return xerrors.Errorf("checking open-pieces delete of %s: %w", pieceCID, err)
		}
		if needed {
			return harmonytask.ErrNeedTask
		}
		return nil
	}
	_, err := tx.Exec(`INSERT INTO hash_space_delete (piece_cid, task_id) VALUES ($1, $2)
		ON CONFLICT (piece_cid) DO NOTHING`, pieceCID, id)
	if err != nil {
		return xerrors.Errorf("queueing open-pieces delete of %s: %w", pieceCID, err)
	}
	return nil
}

// DropTask removes a piece whose last PDP reference is gone from the range
// owner and, when a move covers it, from both ends of that move.
type DropTask struct {
	db *harmonydb.DB
	hs *hashspace.Cluster

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewDropTask(db *harmonydb.DB, hs *hashspace.Cluster) *DropTask {
	return &DropTask{db: db, hs: hs}
}

func (t *DropTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	var cids []string
	if err := t.db.Select(ctx, &cids, `SELECT piece_cid FROM hash_space_delete WHERE task_id = $1`, taskID); err != nil {
		return false, xerrors.Errorf("reading open-pieces delete: %w", err)
	}
	for _, pieceCID := range cids {
		if !stillOwned() {
			return false, xerrors.Errorf("lost ownership of open-pieces delete task %d", taskID)
		}
		// dropOne starts the drop only while no PDP ref exists for the piece; a
		// placement of the same piece waits for a started drop. PDP refs and parked
		// pieces are keyed by the v1 CID plus padded size, derived from the v2 CID.
		v2, err := cid.Parse(pieceCID)
		if err != nil {
			return false, xerrors.Errorf("parsing open-pieces delete cid %s: %w", pieceCID, err)
		}
		v1, rawSize, err := commcid.PieceCidV1FromV2(v2)
		if err != nil {
			return false, xerrors.Errorf("open-pieces delete cid %s is not a piece cid v2: %w", pieceCID, err)
		}
		v1s := v1.String()
		padded := int64(padreader.PaddedSize(rawSize).Padded())

		skipCID := false
		for {
			var skip, wait bool
			_, err = t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				skip, wait = false, false
				var started bool
				if err := tx.QueryRow(`SELECT started FROM hash_space_delete WHERE piece_cid = $1 AND task_id = $2`, pieceCID, taskID).Scan(&started); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						skip = true
						return false, nil
					}
					return false, err
				}
				if started {
					return false, nil
				}
				var hasPDPRef bool
				if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM pdp_piecerefs WHERE piece_cid = $1)`, v1s).Scan(&hasPDPRef); err != nil {
					return false, err
				}
				if hasPDPRef {
					skip = true
					_, err := tx.Exec(`DELETE FROM hash_space_delete WHERE piece_cid = $1 AND task_id = $2`, pieceCID, taskID)
					return err == nil, err
				}
				// A parked piece whose piece-park file was moved into open-pieces has
				// its bytes only there; while such a piece still has references the
				// drop waits.
				if err := tx.QueryRow(`SELECT EXISTS (
				SELECT 1 FROM parked_pieces pp
				WHERE pp.piece_cid = $1 AND pp.piece_padded_size = $2 AND pp.ref_count > 0
				  AND NOT EXISTS (SELECT 1 FROM sector_location l
					WHERE l.miner_id = 0 AND l.sector_num = pp.id AND l.sector_filetype = 32))`, v1s, padded).Scan(&wait); err != nil {
					return false, err
				}
				if wait {
					return false, nil
				}
				// Drop emptied parked rows now so no new reference attaches to bytes
				// that are about to go away.
				if _, err := tx.Exec(`DELETE FROM parked_pieces pp
				WHERE pp.piece_cid = $1 AND pp.piece_padded_size = $2
				  AND pp.cleanup_task_id IS NULL AND pp.complete = TRUE
				  AND NOT EXISTS (SELECT 1 FROM parked_piece_refs r WHERE r.piece_id = pp.id)
				  AND NOT EXISTS (SELECT 1 FROM sector_location l
					WHERE l.miner_id = 0 AND l.sector_num = pp.id AND l.sector_filetype = 32)`, v1s, padded); err != nil {
					return false, xerrors.Errorf("dropping emptied parked pieces: %w", err)
				}
				_, err := tx.Exec(`UPDATE hash_space_delete SET started = TRUE WHERE piece_cid = $1 AND task_id = $2`, pieceCID, taskID)
				return err == nil, err
			}, harmonydb.OptionRetry())
			if err != nil {
				return false, xerrors.Errorf("starting open-pieces delete of %s: %w", pieceCID, err)
			}
			if skip {
				skipCID = true
				break
			}
			if !wait {
				break
			}
			if err := sleepTask(ctx, stillOwned); err != nil {
				return false, err
			}
		}
		if skipCID {
			continue
		}

		// A ref inserted after started was set still needs the bytes. Leave them
		// and drop the row so placement can proceed.
		var live bool
		if err := t.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pdp_piecerefs WHERE piece_cid = $1)`, v1s).Scan(&live); err != nil {
			return false, xerrors.Errorf("rechecking PDP refs for %s: %w", pieceCID, err)
		}
		if live {
			if _, err := t.db.Exec(ctx, `DELETE FROM hash_space_delete WHERE piece_cid = $1 AND task_id = $2`, pieceCID, taskID); err != nil {
				return false, xerrors.Errorf("removing open-pieces delete of %s: %w", pieceCID, err)
			}
			continue
		}

		if err := t.hs.DeleteCID(ctx, pieceCID); err != nil {
			return false, err
		}
		if _, err := t.db.Exec(ctx, `DELETE FROM hash_space_delete WHERE piece_cid = $1 AND task_id = $2`, pieceCID, taskID); err != nil {
			return false, xerrors.Errorf("removing open-pieces delete: %w", err)
		}
	}
	return true, nil
}

func (t *DropTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *DropTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(DROP_MAX),
		Name: tasknames.HashSpaceDrop,
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 16 << 20,
		},
		MaxFailures: 10,
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
	}
}

func (t *DropTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &DropTask{}
var _ = harmonytask.Reg(&DropTask{})
