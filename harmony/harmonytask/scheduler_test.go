package harmonytask

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func TestTaskFromSchedulerEventPreservesTimes(t *testing.T) {
	posted := time.Unix(100, 0).UTC()
	updated := time.Unix(200, 0).UTC()
	got := taskFromSchedulerEvent(schedulerEvent{
		TaskID:     42,
		Retries:    2,
		PostedTime: posted,
		UpdateTime: updated,
	})
	require.Equal(t, TaskID(42), got.ID)
	require.Equal(t, 2, got.Retries)
	require.True(t, posted.Equal(got.PostedTime))
	require.True(t, updated.Equal(got.UpdateTime))
}

func TestTaskFromSchedulerEventDefaultsZeroTimes(t *testing.T) {
	before := time.Now()
	got := taskFromSchedulerEvent(schedulerEvent{TaskID: 7, Retries: 0})
	require.False(t, got.PostedTime.IsZero())
	require.False(t, got.UpdateTime.IsZero())
	require.WithinDuration(t, before, got.PostedTime, 2*time.Second)
	require.WithinDuration(t, before, got.UpdateTime, 2*time.Second)
}

func TestTaskLessByPostedTime(t *testing.T) {
	t0 := time.Unix(100, 0)
	t1 := time.Unix(200, 0)

	tests := []struct {
		name string
		a, b task
		want bool // true when a should sort before b
	}{
		{
			name: "older posted_time first",
			a:    task{ID: 2, PostedTime: t0},
			b:    task{ID: 1, PostedTime: t1},
			want: true,
		},
		{
			name: "equal posted_time uses id",
			a:    task{ID: 1, PostedTime: t0},
			b:    task{ID: 2, PostedTime: t0},
			want: true,
		},
		{
			name: "unknown posted_time is newest",
			a:    task{ID: 1, PostedTime: t0},
			b:    task{ID: 2},
			want: true,
		},
		{
			name: "both unknown posted_time uses id",
			a:    task{ID: 1},
			b:    task{ID: 2},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, taskLessByPostedTime(tt.a, tt.b))
		})
	}
}

func TestEmitRetryTaskWaitsBeforeReemit(t *testing.T) {
	const wait = 80 * time.Millisecond
	h := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name:      "RetryWaitT",
			Max:       taskhelp.Max(5),
			RetryWait: func(int) time.Duration { return wait },
		},
	}
	ch := make(chan schedulerEvent, 1)
	ee := eventEmitter{schedulerChannel: ch}

	start := time.Now()
	h.emitRetryTask(ee, task{ID: 99, Retries: 0}, false)

	select {
	case ev := <-ch:
		elapsed := time.Since(start)
		require.GreaterOrEqual(t, elapsed, wait, "failed retry should respect RetryWait before re-emit")
		require.Equal(t, TaskID(99), ev.TaskID)
		require.Equal(t, "RetryWaitT", ev.TaskType)
		require.Equal(t, schedulerSourceAdded, ev.Source)
		require.Equal(t, 1, ev.Retries)
		require.False(t, ev.UpdateTime.IsZero())
	case <-time.After(wait + 500*time.Millisecond):
		t.Fatal("timed out waiting for retry emit")
	}
}

func TestEmitRetryTaskPreemptedReemitsImmediately(t *testing.T) {
	h := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name: "PreemptT",
			Max:  taskhelp.Max(5),
			RetryWait: func(int) time.Duration {
				return time.Hour
			},
		},
	}
	ch := make(chan schedulerEvent, 1)
	ee := eventEmitter{schedulerChannel: ch}

	start := time.Now()
	h.emitRetryTask(ee, task{ID: 5, Retries: 1, PostedTime: time.Unix(1, 0).UTC()}, true)

	select {
	case ev := <-ch:
		require.Less(t, time.Since(start), 50*time.Millisecond)
		require.Equal(t, TaskID(5), ev.TaskID)
		require.Equal(t, 1, ev.Retries)
		require.True(t, ev.PostedTime.Equal(time.Unix(1, 0).UTC()))
	case <-time.After(200 * time.Millisecond):
		t.Fatal("preempted task should re-emit immediately")
	}
}

func TestEmitRetryTaskStopsAtMaxFailures(t *testing.T) {
	h := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name:        "DropT",
			Max:         taskhelp.Max(5),
			MaxFailures: 3,
			RetryWait:   func(int) time.Duration { return time.Millisecond },
		},
	}
	ch := make(chan schedulerEvent, 1)
	ee := eventEmitter{schedulerChannel: ch}

	h.emitRetryTask(ee, task{ID: 11, Retries: 2}, false)

	select {
	case ev := <-ch:
		t.Fatalf("unexpected emit for dropped task: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}
