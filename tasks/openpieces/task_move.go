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

// MoveTask copies rebalance intervals onto their destination disks and then
// hands each interval over. The task is created in the map transaction that
// plans its moves; it runs on a node holding one of the destinations, and
// moves to other nodes' disks are handed to a new task.
type MoveTask struct {
	db *harmonydb.DB
	hs *hashspace.Cluster

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewMoveTask(db *harmonydb.DB, hs *hashspace.Cluster) *MoveTask {
	return &MoveTask{db: db, hs: hs}
}

func (t *MoveTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	moves, err := t.hs.MoveSourcesByTask(ctx, int64(taskID))
	if err != nil {
		return false, xerrors.Errorf("reading hash space move sources: %w", err)
	}
	if len(moves) == 0 {
		return true, nil
	}
	if err := t.hs.Refresh(ctx); err != nil {
		return false, xerrors.Errorf("refreshing hash space map: %w", err)
	}

	var remote []int64
	for _, m := range moves {
		if !t.hs.HasLocal(m.ToStorage) {
			remote = append(remote, m.ID)
		}
	}
	if len(remote) > 0 {
		if err := t.handoff(ctx, taskID, remote); err != nil {
			return false, err
		}
	}

	for _, m := range moves {
		if !t.hs.HasLocal(m.ToStorage) {
			continue
		}
		if err := t.moveOne(ctx, m, stillOwned); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (t *MoveTask) moveOne(ctx context.Context, m *hashspace.MoveSource, stillOwned func() bool) error {
	var prev []string
	for {
		if !stillOwned() {
			return xerrors.Errorf("lost ownership of move source %d", m.ID)
		}
		hashes, err := t.hs.PendingCopy(ctx, m, COPY_BATCH)
		if err != nil {
			return xerrors.Errorf("listing move source %d pieces: %w", m.ID, err)
		}
		if len(hashes) == 0 {
			break
		}
		if slices.Equal(hashes, prev) {
			return xerrors.Errorf("move source %d made no progress on %d pieces", m.ID, len(hashes))
		}
		for _, h := range hashes {
			if err := t.hs.CopyOne(ctx, m, h); err != nil {
				return xerrors.Errorf("move source %d: %w", m.ID, err)
			}
		}
		prev = hashes
	}

	if err := t.hs.CompleteMoveSource(ctx, m); err != nil {
		return xerrors.Errorf("completing move source %d: %w", m.ID, err)
	}
	log.Infow("hash space move complete", "move_source", m.ID, "from", m.FromStorage, "to", m.ToStorage)
	return nil
}

// handoff moves move sources whose destination is on another node to a new
// task, so a node holding those disks picks them up.
func (t *MoveTask) handoff(ctx context.Context, from harmonytask.TaskID, ids []int64) error {
	_, err := harmonytask.TxWithTask(ctx, t.db, t.TF.Val(ctx), func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error) {
		if id == 0 {
			return false, harmonytask.ErrNeedTask
		}
		n, err := tx.Exec(`UPDATE hash_space_move_source SET task_id = $1 WHERE id = ANY($2) AND task_id = $3`, id, ids, from)
		return n > 0, err
	})
	if err != nil {
		return xerrors.Errorf("handing off hash space move sources %v: %w", ids, err)
	}
	return nil
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
		return nil, xerrors.Errorf("reading hash space move sources: %w", err)
	}

	// A task is taken when any of its destinations is local; tasks with no
	// rows left are taken anywhere so they finish.
	accept := map[int64]bool{}
	for _, id := range taskIDs {
		accept[id] = true
	}
	for _, r := range rows {
		accept[r.TaskID] = false
	}
	for _, r := range rows {
		if t.hs.HasLocal(r.ToStorage) {
			accept[r.TaskID] = true
		}
	}
	var out []harmonytask.TaskID
	for _, id := range ids {
		if accept[int64(id)] {
			out = append(out, id)
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
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
	}
}

func (t *MoveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &MoveTask{}
var _ = harmonytask.Reg(&MoveTask{})
