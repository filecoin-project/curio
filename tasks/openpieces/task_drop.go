package openpieces

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
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

// DropTask removes a piece whose last PDP reference is gone from every
// open-pieces location, in-flight move destinations included.
type DropTask struct {
	db *harmonydb.DB
	hs *hashspace.Cluster

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewDropTask(db *harmonydb.DB, hs *hashspace.Cluster) *DropTask {
	t := &DropTask{db: db, hs: hs}
	go t.poll(context.Background())
	return t
}

func (t *DropTask) poll(ctx context.Context) {
	ticker := time.NewTicker(POLL_INTERVAL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := t.schedule(ctx); err != nil {
			log.Errorw("scheduling open-pieces delete", "error", err)
		}
	}
}

// A parked piece whose piece-park file was moved into open-pieces has its
// bytes only there. While such a piece still has references the delete
// waits; once it has none, its row is dropped in the claim so no new
// reference can attach to bytes that are about to go away.
// hash_space_delete holds piece CID v2; PDP refs and parked pieces are keyed
// by the v1 CID plus padded size, derived here.
func (t *DropTask) schedule(ctx context.Context) error {
	var cids []string
	err := t.db.Select(ctx, &cids, `SELECT piece_cid FROM hash_space_delete
		WHERE task_id IS NULL
		ORDER BY created_at
		LIMIT $1`, POLL_BATCH)
	if err != nil {
		return xerrors.Errorf("selecting open-pieces deletes: %w", err)
	}
	for _, c := range cids {
		v2, err := cid.Parse(c)
		if err != nil {
			log.Warnw("bad piece cid in hash_space_delete", "cid", c, "error", err)
			continue
		}
		v1, rawSize, err := commcid.PieceCidV1FromV2(v2)
		if err != nil {
			log.Warnw("hash_space_delete cid is not a piece cid v2", "cid", c, "error", err)
			continue
		}
		v1s := v1.String()
		padded := int64(padreader.PaddedSize(rawSize).Padded())

		t.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`UPDATE hash_space_delete SET task_id = $1
				WHERE piece_cid = $2 AND task_id IS NULL
				  AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.piece_cid = $3)
				  AND NOT EXISTS (
					SELECT 1 FROM parked_pieces pp
					WHERE pp.piece_cid = $3 AND pp.piece_padded_size = $4 AND pp.ref_count > 0
					  AND NOT EXISTS (SELECT 1 FROM sector_location l
						WHERE l.miner_id = 0 AND l.sector_num = pp.id AND l.sector_filetype = 32))`, id, c, v1s, padded)
			if err != nil {
				return false, xerrors.Errorf("claiming open-pieces delete: %w", err)
			}
			if n == 0 {
				return false, nil
			}
			_, err = tx.Exec(`DELETE FROM parked_pieces pp
				WHERE pp.piece_cid = $1 AND pp.piece_padded_size = $2
				  AND pp.cleanup_task_id IS NULL AND pp.complete = TRUE
				  AND NOT EXISTS (SELECT 1 FROM parked_piece_refs r WHERE r.piece_id = pp.id)
				  AND NOT EXISTS (SELECT 1 FROM sector_location l
					WHERE l.miner_id = 0 AND l.sector_num = pp.id AND l.sector_filetype = 32)`, v1s, padded)
			if err != nil {
				return false, xerrors.Errorf("dropping emptied parked pieces: %w", err)
			}
			return true, nil
		})
	}
	return nil
}

func (t *DropTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	var cids []string
	if err := t.db.Select(ctx, &cids, `SELECT piece_cid FROM hash_space_delete WHERE task_id = $1`, taskID); err != nil {
		return false, xerrors.Errorf("reading open-pieces delete: %w", err)
	}
	if len(cids) == 0 {
		return true, nil
	}
	if err := t.hs.DeleteCID(ctx, cids[0]); err != nil {
		return false, err
	}
	if _, err := t.db.Exec(ctx, `DELETE FROM hash_space_delete WHERE task_id = $1`, taskID); err != nil {
		return false, xerrors.Errorf("removing open-pieces delete: %w", err)
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
	}
}

func (t *DropTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &DropTask{}
var _ = harmonytask.Reg(&DropTask{})
