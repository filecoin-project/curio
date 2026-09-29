package harmonytask

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/taskhelp"
)

type workerReadinessTask struct {
	*admissionFixtureTask
	health taskhelp.WorkerBackoff
}

func TestWorkerBackoffDoesNotBlockOtherTaskTypeWaterfall(t *testing.T) {
	porep, impl, _, _ := newAdmissionFixture(t, func(context.Context, []TaskID) ([]TaskID, error) {
		t.Fatal("blocked backend reached candidate preparation")
		return nil, nil
	})
	worker := &workerReadinessTask{admissionFixtureTask: impl}
	worker.health.Result(0, &taskhelp.WorkerUnavailable{Cause: errors.New("fixture")})
	porep.TaskInterface = worker
	porep.Name = "PoRep"
	visited := false
	other, _, _, _ := newAdmissionFixture(t, func(context.Context, []TaskID) ([]TaskID, error) {
		visited = true
		return nil, nil // Stop before SQL/native work in this database-free test.
	})
	other.Name, other.TimeSensitive = "WindowPost", true
	e := porep.TaskEngine
	other.TaskEngine = e
	e.handlers = []*taskTypeHandler{porep, other}
	e.taskMap = map[string]*taskTypeHandler{porep.Name: porep, other.Name: other}
	available := map[string]*taskSchedule{
		porep.Name: {hasID: map[TaskID]task{1: {ID: 1, PostedTime: time.Unix(1, 0)}}},
		other.Name: {hasID: map[TaskID]task{2: {ID: 2, PostedTime: time.Unix(2, 0)}}},
	}
	require.NoError(t, e.pollerTryAllWork(taskSourceLocal{available}, eventEmitter{}))
	require.True(t, visited, "a PoRep hold must not halt another task's scheduler path")
}

func (s *workerReadinessTask) TaskStartBlocked() bool { return s.health.Blocked() }
func TestWorkerBackoffCannotBypassCachedOrRecoveryAdmission(t *testing.T) {
	for _, source := range []string{workSourcePoller, workSourceRecover, workSourcePreempt} {
		t.Run(source, func(t *testing.T) {
			h, impl, _, _ := newAdmissionFixture(t, func(context.Context, []TaskID) ([]TaskID, error) {
				t.Fatal("blocked worker queried candidates")
				return nil, nil
			})
			worker := &workerReadinessTask{admissionFixtureTask: impl}
			worker.health.Result(0, &taskhelp.WorkerUnavailable{Cause: errors.New("fixture backend")})
			h.TaskInterface = worker
			h.accept.Add([]int64{1, 2})
			require.False(t, h.considerWorkWithOwnership(source, []task{{ID: 1}, {ID: 2}}, eventEmitter{}, func([]TaskID, int) ([]TaskID, error) { t.Fatal("blocked worker claimed"); return nil, nil }, nil, newMemoryAttemptStore()))
			require.Zero(t, h.Max.Active())
			require.Empty(t, h.running.Snapshot())
		})
	}
}
