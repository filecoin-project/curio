package webrpc

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
)

type recordingSpidGetter struct {
	calls   [][]int64
	resolve func(int64) []int64
}

func (g *recordingSpidGetter) GetSpids(_ context.Context, _ *harmonydb.DB, taskIDs []int64) ([]harmonytask.TaskSPID, error) {
	call := append([]int64(nil), taskIDs...)
	g.calls = append(g.calls, call)

	var result []harmonytask.TaskSPID
	for _, taskID := range taskIDs {
		for _, spid := range g.resolve(taskID) {
			result = append(result, harmonytask.TaskSPID{TaskID: taskID, SPID: spid})
		}
	}
	return result, nil
}

func TestLimitedTaskPreservesAllProviders(t *testing.T) {
	rows := []clusterTaskSummaryLimitedRow{{ID: 1, Name: "synthetic"}}
	g := &recordingSpidGetter{resolve: func(int64) []int64 { return []int64{1002, 1001, 1002} }}
	spids, _, err := resolveLimitedTaskSPIDs(context.Background(), nil, rows, map[string]BatchSpidGetter{"synthetic": g})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spids[1], []int64{1001, 1002}) {
		t.Fatalf("providers=%v", spids)
	}
	if len(g.calls) != 1 {
		t.Fatalf("calls=%v", g.calls)
	}
}

func TestLimitedTaskIsolatesUnknownOwnershipAge(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	owner := int64(7)
	rows := []clusterTaskSummaryLimitedRow{
		{ID: 1, Name: "task", State: "running", OwnerID: &owner, PostedTime: now.Add(-time.Hour)},
		{ID: 2, Name: "task", State: "running", OwnerID: &owner, WorkStart: sql.NullTime{Time: now.Add(-time.Minute), Valid: true}, WorkStartSource: sql.NullString{String: "claim", Valid: true}},
		{ID: 3, Name: "task", State: "running", OwnerID: &owner, WorkStart: sql.NullTime{Time: now.Add(-time.Minute), Valid: true}},
		{ID: 4, Name: "task", State: "running", OwnerID: &owner, WorkStart: sql.NullTime{Time: now.Add(time.Minute), Valid: true}, WorkStartSource: sql.NullString{String: "claim", Valid: true}},
	}
	for _, row := range rows {
		task := buildLimitedTaskSummary(row, now, nil)
		if row.ID == 2 {
			if task.AgeSeconds == nil || *task.AgeSeconds != 60 {
				t.Fatalf("known row: %+v", task)
			}
		} else if task.AgeSeconds != nil {
			t.Fatalf("unknown/future row: %+v", task)
		}
	}
}
