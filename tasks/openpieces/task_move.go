package openpieces

import (
	"context"
	"slices"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/hashspace"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

const (
	MOVE_MAX   = 2
	COPY_BATCH = 64
)

// MoveTask copies one rebalance interval onto its destination disk and then
// hands the interval over. It runs on the node holding the destination.
type MoveTask struct {
	db *harmonydb.DB
	hs *hashspace.Cluster

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewMoveTask(db *harmonydb.DB, hs *hashspace.Cluster) *MoveTask {
	t := &MoveTask{db: db, hs: hs}
	go t.poll(context.Background())
	return t
}

func (t *MoveTask) poll(ctx context.Context) {
	ticker := time.NewTicker(POLL_INTERVAL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := t.schedule(ctx); err != nil {
			log.Errorw("scheduling open-pieces move", "error", err)
		}
	}
}

func (t *MoveTask) schedule(ctx context.Context) error {
	var ids []int64
	if err := t.db.Select(ctx, &ids, `SELECT id FROM hash_space_move_source WHERE task_id IS NULL ORDER BY id`); err != nil {
		return xerrors.Errorf("selecting hash space moveSources: %w", err)
	}
	for _, mid := range ids {
		t.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`UPDATE hash_space_move_source SET task_id = $1 WHERE id = $2 AND task_id IS NULL`, id, mid)
			if err != nil {
				return false, xerrors.Errorf("claiming hash space move source: %w", err)
			}
			return n > 0, nil
		})
	}
	return nil
}

func (t *MoveTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	m, err := t.hs.MoveSourceByTask(ctx, int64(taskID))
	if err != nil {
		return false, xerrors.Errorf("reading hash space move source: %w", err)
	}
	if m == nil {
		return true, nil
	}
	if err := t.hs.Refresh(ctx); err != nil {
		return false, xerrors.Errorf("refreshing hash space map: %w", err)
	}

	var prev []string
	for {
		if !stillOwned() {
			return false, xerrors.Errorf("lost ownership of move source %d", m.ID)
		}
		cids, err := t.hs.PendingCopy(ctx, m, COPY_BATCH)
		if err != nil {
			return false, xerrors.Errorf("listing move source %d pieces: %w", m.ID, err)
		}
		if len(cids) == 0 {
			break
		}
		if slices.Equal(cids, prev) {
			return false, xerrors.Errorf("move source %d made no progress on %d pieces", m.ID, len(cids))
		}
		for _, c := range cids {
			if err := t.hs.CopyOne(ctx, m, c); err != nil {
				return false, xerrors.Errorf("move source %d: %w", m.ID, err)
			}
		}
		prev = cids
	}

	if err := t.hs.CompleteMoveSource(ctx, m); err != nil {
		return false, xerrors.Errorf("completing move source %d: %w", m.ID, err)
	}
	log.Infow("hash space move complete", "move_source", m.ID, "from", m.FromStorage, "to", m.ToStorage)
	return true, nil
}

func (t *MoveTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	taskIDs := make([]int64, len(ids))
	for i, id := range ids {
		taskIDs[i] = int64(id)
	}
	var rows []struct {
		TaskID    int64  `db:"task_id"`
		ToStorage string `db:"to_storage"`
	}
	if err := t.db.Select(context.Background(), &rows, `SELECT task_id, to_storage FROM hash_space_move_source WHERE task_id = ANY($1)`, taskIDs); err != nil {
		return nil, xerrors.Errorf("reading hash space moveSources: %w", err)
	}
	var out []harmonytask.TaskID
	for _, r := range rows {
		if t.hs.HasLocal(r.ToStorage) {
			out = append(out, harmonytask.TaskID(r.TaskID))
		}
	}
	return out, nil
}

func (t *MoveTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(MOVE_MAX),
		Name: tasknames.HashSpaceMove,
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 100,
	}
}

func (t *MoveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &MoveTask{}
var _ = harmonytask.Reg(&MoveTask{})
