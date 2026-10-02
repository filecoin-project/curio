package openpieces

import (
	"context"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/sync/errgroup"
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
	MOVE_MAX      = 2
	COPY_PARALLEL = 4
)

var log = logging.Logger("cu-openpieces")

// MoveTask copies one destination disk's rebalance intervals onto that disk
// and then hands each interval over. The task is created in the map
// transaction that plans those moves, and it runs on the node that holds the
// destination.
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
	// Refresh rewrites layout.json from the map, including this move, before
	// any bytes are copied.
	if err := t.hs.Refresh(ctx); err != nil {
		return false, xerrors.Errorf("refreshing hash space map: %w", err)
	}

	for _, m := range moves {
		if err := t.moveOne(ctx, m, stillOwned); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (t *MoveTask) moveOne(ctx context.Context, m *hashspace.MoveSource, stillOwned func() bool) error {
	for {
		if !stillOwned() {
			return xerrors.Errorf("lost ownership of move source %d", m.ID)
		}
		// Copies of one group finish before the next hash is pulled. PendingCopy
		// reads the next directory page only when asked for the next hash, so
		// those deletes are not running during that read.
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(COPY_PARALLEL)
		n := 0
		started := 0
		var pullErr error
		for h, err := range t.hs.PendingCopy(ctx, m) {
			if err != nil {
				pullErr = err
				break
			}
			if gctx.Err() != nil {
				break
			}
			n++
			started++
			g.Go(func() error {
				if !stillOwned() {
					return xerrors.Errorf("lost ownership of move source %d", m.ID)
				}
				if err := t.hs.CopyOne(gctx, m, h); err != nil {
					return xerrors.Errorf("move source %d: %w", m.ID, err)
				}
				return nil
			})
			if started == COPY_PARALLEL {
				if err := g.Wait(); err != nil {
					return err
				}
				g, gctx = errgroup.WithContext(ctx)
				g.SetLimit(COPY_PARALLEL)
				started = 0
			}
		}
		if started > 0 {
			if err := g.Wait(); err != nil {
				return err
			}
		}
		if pullErr != nil {
			return xerrors.Errorf("listing move source %d pieces: %w", m.ID, pullErr)
		}
		if n == 0 {
			break
		}
	}

	if err := t.hs.CompleteMoveSource(ctx, m); err != nil {
		return xerrors.Errorf("completing move source %d: %w", m.ID, err)
	}
	log.Infow("hash space move complete", "move_source", m.ID, "from", m.FromStorage, "to", m.ToStorage)
	return nil
}

func (t *MoveTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	taskIDs := make([]int64, len(ids))
	for i, id := range ids {
		taskIDs[i] = int64(id)
	}
	// A task is taken when every destination is on this node. A task with no
	// rows left is taken here so it can finish.
	var rows []harmonytask.TaskID
	err := t.db.Select(context.Background(), &rows, `
		SELECT t.id
		FROM unnest($1::bigint[]) AS t(id)
		WHERE NOT EXISTS (
			SELECT 1 FROM hash_space_move_source m
			WHERE m.task_id = t.id AND m.to_storage <> ALL($2)
		)`, taskIDs, t.hs.LocalIDs())
	if err != nil {
		return nil, xerrors.Errorf("reading hash space move sources: %w", err)
	}
	return rows, nil
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
