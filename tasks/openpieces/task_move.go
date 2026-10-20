package openpieces

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/hashfolder"
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
	hs *hashfolder.Cluster

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewMoveTask(db *harmonydb.DB, hs *hashfolder.Cluster) *MoveTask {
	return &MoveTask{db: db, hs: hs}
}

func (t *MoveTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	moves, err := t.hs.MoveSourcesByTask(ctx, int64(taskID))
	if err != nil {
		return false, xerrors.Errorf("reading hash space move sources: %w", err)
	}
	if len(moves) == 0 {
		log.Infow("hash space move has no intervals", "task", taskID)
		return true, nil
	}
	log.Infow("hash space move starting", "task", taskID, "moves", len(moves))
	// Refresh rewrites the layout from the map, including this move, before
	// any bytes are copied.
	if err := t.hs.Refresh(ctx); err != nil {
		return false, xerrors.Errorf("refreshing hash space map: %w", err)
	}

	for _, m := range moves {
		if err := t.moveOne(ctx, m, stillOwned); err != nil {
			if errors.Is(err, hashfolder.ErrDiskFull) {
				log.Errorw("hash space move stopped because the destination is full",
					"task", taskID, "move_source", m.ID, "space", m.Space, "from", m.FromStorage, "to", m.ToStorage, "error", err)
				if aerr := t.hs.AbandonMoveTask(ctx, int64(taskID), m.ToStorage); aerr != nil {
					return false, aerr
				}
				return true, nil
			}
			if errors.Is(err, hashfolder.ErrSourceUnavailable) {
				last, lerr := t.lastAttempt(ctx, taskID)
				log.Warnw("hash space move source is unavailable",
					"task", taskID, "move_source", m.ID, "space", m.Space, "from", m.FromStorage, "to", m.ToStorage,
					"last_attempt", lerr == nil && last, "error", err)
				if lerr != nil {
					return false, lerr
				}
				if last {
					if aerr := t.hs.AbandonMoveTask(ctx, int64(taskID), m.FromStorage); aerr != nil {
						return false, aerr
					}
					return true, nil
				}
			}
			log.Errorw("hash space move failed",
				"task", taskID, "move_source", m.ID, "space", m.Space, "from", m.FromStorage, "to", m.ToStorage, "error", err)
			return false, err
		}
	}
	return true, nil
}

// lastAttempt reports whether this run is the one that would exhaust
// MaxFailures. retries is the count stored before this attempt. A task with
// no failure cap never abandons for a missing source.
func (t *MoveTask) lastAttempt(ctx context.Context, id harmonytask.TaskID) (bool, error) {
	max := t.TypeDetails().MaxFailures
	if max == 0 {
		return false, nil
	}
	var retries uint
	err := t.db.QueryRow(ctx, `SELECT retries FROM harmony_task WHERE id = $1`, id).Scan(&retries)
	if err != nil {
		return false, err
	}
	return retries >= max-1, nil
}

// moveOne copies every file still on m's source, in groups of COPY_PARALLEL,
// then completes the move. Source files stay until that completion commits
// the range, so this is one pass: a file the destination already has is left
// in place. It stamps the row first so a later retry is not treated as a
// move that never started.
func (t *MoveTask) moveOne(ctx context.Context, m *hashfolder.MoveSource, stillOwned func() bool) error {
	if err := t.hs.MarkMoveStarted(ctx, m.ID); err != nil {
		return err
	}
	log.Infow("hash space move copying",
		"move_source", m.ID, "space", m.Space, "from", m.FromStorage, "to", m.ToStorage,
		"bytes", m.Size, "start", hex.EncodeToString(m.StartHash), "end", hex.EncodeToString(m.EndHash))
	if !stillOwned() {
		log.Warnw("lost ownership of hash space move", "move_source", m.ID, "from", m.FromStorage, "to", m.ToStorage, "pieces", 0)
		return xerrors.Errorf("lost ownership of move source %d", m.ID)
	}
	// Copies of one group finish before the next hash is pulled. PendingCopy
	// reads the next directory page only when asked for the next hash.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(COPY_PARALLEL)
	n := 0
	started := 0
	var pullErr error
	lastLog := time.Now()
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
				log.Warnw("hash space piece copy failed",
					"move_source", m.ID, "hash", h, "space", m.Space, "from", m.FromStorage, "to", m.ToStorage, "error", err)
				return xerrors.Errorf("move source %d hash %s: %w", m.ID, h, err)
			}
			return nil
		})
		if started == COPY_PARALLEL {
			if err := g.Wait(); err != nil {
				return err
			}
			if time.Since(lastLog) >= 30*time.Second {
				lastLog = time.Now()
				log.Infow("hash space move progress",
					"move_source", m.ID, "space", m.Space, "from", m.FromStorage, "to", m.ToStorage,
					"pieces", n, "planned_bytes", m.Size)
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

	if err := t.hs.CompleteMoveSource(ctx, m); err != nil {
		return xerrors.Errorf("completing move source %d: %w", m.ID, err)
	}
	log.Infow("hash space move complete",
		"move_source", m.ID, "space", m.Space, "from", m.FromStorage, "to", m.ToStorage,
		"pieces", n, "planned_bytes", m.Size,
		"start", hex.EncodeToString(m.StartHash), "end", hex.EncodeToString(m.EndHash))
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
		RetryWait:   taskhelp.RetryWaitExp(5 * time.Second),
	}
}

func (t *MoveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &MoveTask{}
var _ = harmonytask.Reg(&MoveTask{})
