package harmonytask

import (
	"context"
	"errors"
	"sync/atomic"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var processEngine atomic.Pointer[TaskEngine]

// AdderFor returns an AddTaskFunc for the named task type on this process's
// task engine, for code that is not that task's Adder. The task type does
// not have to be registered locally; another node that runs it picks the
// task up. It returns nil before the engine exists (or when there is none,
// e.g. read-only database mode).
func AdderFor(taskName string) AddTaskFunc {
	e := processEngine.Load()
	if e == nil {
		return nil
	}
	return func(extra func(TaskID, *harmonydb.Tx) (bool, error)) {
		e.AddTaskByName(taskName, extra)
	}
}

// ErrNeedTask is returned by a TxWithTask body, run with id 0, once it finds
// its change needs a task.
var ErrNeedTask = errors.New("transaction needs a task")

// TxWithTask commits a DB change together with the task that acts on it, so
// no poller has to find the change later. fn first runs in a plain
// transaction with id 0. If it returns ErrNeedTask, that transaction rolls
// back and fn runs again inside addTask's transaction with the new task's id.
// fn may run several times and must not keep state between runs. addTask may
// be nil when the task type is not registered; fn must not ask for a task then.
func TxWithTask(ctx context.Context, db *harmonydb.DB, addTask AddTaskFunc, fn func(tx *harmonydb.Tx, id TaskID) (commit bool, err error)) (bool, error) {
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return fn(tx, 0)
	}, harmonydb.OptionRetry())
	if !errors.Is(err, ErrNeedTask) {
		return committed, err
	}
	if addTask == nil {
		return false, xerrors.Errorf("transaction needs a task, but no task engine is running")
	}

	var fnErr error
	committed = false
	addTask(func(id TaskID, tx *harmonydb.Tx) (bool, error) {
		fnErr = nil
		committed = false
		commit, err := fn(tx, id)
		if err != nil {
			if harmonydb.IsErrSerialization(err) {
				return false, err
			}
			fnErr = err
			return false, nil
		}
		committed = commit
		return commit, nil
	})
	if fnErr != nil {
		return false, fnErr
	}
	return committed, nil
}
