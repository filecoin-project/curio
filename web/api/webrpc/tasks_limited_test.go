package webrpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

type memoryClusterTaskSummarySource struct {
	rows         []clusterTaskSummaryLimitedRow
	observedAt   time.Time
	snapshotErr  error
	taskTypesErr error
	applied      []ClusterTaskSummaryApplied
	typeIncludes []bool
}

func (m *memoryClusterTaskSummarySource) LoadSnapshot(ctx context.Context, applied ClusterTaskSummaryApplied) (clusterTaskSummarySnapshot, error) {
	if err := ctx.Err(); err != nil {
		return clusterTaskSummarySnapshot{}, err
	}
	m.applied = append(m.applied, applied)
	if m.snapshotErr != nil {
		return clusterTaskSummarySnapshot{}, m.snapshotErr
	}

	var running, pending []clusterTaskSummaryLimitedRow
	for _, row := range m.rows {
		if !applied.IncludeBackground && strings.HasPrefix(row.Name, "bg:") {
			continue
		}
		if applied.TaskName != nil && row.Name != *applied.TaskName {
			continue
		}

		if row.OwnerID != nil {
			row.State = "running"
			running = append(running, row)
		} else {
			row.State = "pending"
			pending = append(pending, row)
		}
	}

	sort.Slice(running, func(i, j int) bool {
		left, right := running[i], running[j]
		if left.WorkStart.Valid != right.WorkStart.Valid {
			return !left.WorkStart.Valid
		}
		if left.WorkStart.Valid && !left.WorkStart.Time.Equal(right.WorkStart.Time) {
			return left.WorkStart.Time.Before(right.WorkStart.Time)
		}
		return left.ID < right.ID
	})
	sort.Slice(pending, func(i, j int) bool {
		left, right := pending[i], pending[j]
		if !left.PostedTime.Equal(right.PostedTime) {
			return left.PostedTime.Before(right.PostedTime)
		}
		return left.ID < right.ID
	})

	runningTotal, pendingTotal := len(running), len(pending)
	running = running[:min(len(running), applied.MaxTasks)]
	pendingLimit := min(applied.MaxPending, applied.MaxTasks-len(running))
	pending = pending[:min(len(pending), pendingLimit)]
	selected := append(append([]clusterTaskSummaryLimitedRow{}, running...), pending...)
	return clusterTaskSummarySnapshot{
		Rows:         selected,
		RunningTotal: int64(runningTotal),
		PendingTotal: int64(pendingTotal),
		ObservedAt:   m.observedAt,
	}, nil
}

func (m *memoryClusterTaskSummarySource) LoadTaskTypes(ctx context.Context, includeBackground bool) ([]ClusterTaskTypeCount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.typeIncludes = append(m.typeIncludes, includeBackground)
	if m.taskTypesErr != nil {
		return nil, m.taskTypesErr
	}

	byName := map[string]*ClusterTaskTypeCount{}
	for _, row := range m.rows {
		if !includeBackground && strings.HasPrefix(row.Name, "bg:") {
			continue
		}
		count, ok := byName[row.Name]
		if !ok {
			count = &ClusterTaskTypeCount{Name: row.Name}
			byName[row.Name] = count
		}
		if row.OwnerID != nil {
			count.Running++
		} else {
			count.Pending++
		}
		count.Total++
	}

	taskTypes := make([]ClusterTaskTypeCount, 0, len(byName))
	for _, count := range byName {
		taskTypes = append(taskTypes, *count)
	}
	sort.Slice(taskTypes, func(i, j int) bool { return taskTypes[i].Name < taskTypes[j].Name })
	return taskTypes, nil
}

type failingSpidGetter struct {
	err   error
	calls [][]int64
}

func (f *failingSpidGetter) GetSpids(_ context.Context, _ *harmonydb.DB, taskIDs []int64) ([]harmonytask.TaskSPID, error) {
	f.calls = append(f.calls, append([]int64(nil), taskIDs...))
	return nil, f.err
}

type fixedSpidGetter struct {
	resolved []harmonytask.TaskSPID
}

func (f fixedSpidGetter) GetSpids(_ context.Context, _ *harmonydb.DB, _ []int64) ([]harmonytask.TaskSPID, error) {
	return f.resolved, nil
}

func clusterTaskTestInt(value int) *int {
	return &value
}

func TestNormalizeClusterTaskSummaryRequest(t *testing.T) {
	empty := ""
	tests := []struct {
		name               string
		request            ClusterTaskSummaryLimitedRequest
		wantMaxTasks       int
		wantMaxPending     int
		wantTaskName       *string
		wantIncludeBacklog bool
	}{
		{name: "omitted defaults", wantMaxTasks: 500, wantMaxPending: 500},
		{name: "explicit zero tasks clamps to one", request: ClusterTaskSummaryLimitedRequest{MaxTasks: clusterTaskTestInt(0)}, wantMaxTasks: 1, wantMaxPending: 1},
		{name: "tasks above hard cap", request: ClusterTaskSummaryLimitedRequest{MaxTasks: clusterTaskTestInt(10_000)}, wantMaxTasks: 500, wantMaxPending: 500},
		{name: "explicit zero pending", request: ClusterTaskSummaryLimitedRequest{MaxPending: clusterTaskTestInt(0)}, wantMaxTasks: 500, wantMaxPending: 0},
		{name: "negative pending", request: ClusterTaskSummaryLimitedRequest{MaxPending: clusterTaskTestInt(-5)}, wantMaxTasks: 500, wantMaxPending: 0},
		{name: "pending constrained by tasks", request: ClusterTaskSummaryLimitedRequest{MaxTasks: clusterTaskTestInt(20), MaxPending: clusterTaskTestInt(100)}, wantMaxTasks: 20, wantMaxPending: 20},
		{name: "empty exact task name retained", request: ClusterTaskSummaryLimitedRequest{TaskName: &empty}, wantMaxTasks: 500, wantMaxPending: 500, wantTaskName: &empty},
		{name: "background retained", request: ClusterTaskSummaryLimitedRequest{IncludeBackground: true}, wantMaxTasks: 500, wantMaxPending: 500, wantIncludeBacklog: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeClusterTaskSummaryRequest(test.request)
			if got.MaxTasks != test.wantMaxTasks || got.MaxPending != test.wantMaxPending {
				t.Fatalf("limits = %d/%d, want %d/%d", got.MaxTasks, got.MaxPending, test.wantMaxTasks, test.wantMaxPending)
			}
			if got.IncludeBackground != test.wantIncludeBacklog {
				t.Fatalf("IncludeBackground = %v, want %v", got.IncludeBackground, test.wantIncludeBacklog)
			}
			if (got.TaskName == nil) != (test.wantTaskName == nil) {
				t.Fatalf("TaskName = %v, want %v", got.TaskName, test.wantTaskName)
			}
			if got.TaskName != nil && *got.TaskName != *test.wantTaskName {
				t.Fatalf("TaskName = %q, want %q", *got.TaskName, *test.wantTaskName)
			}
		})
	}
}

func TestClusterTaskSummaryRequestRejectsMalformedLimits(t *testing.T) {
	var request ClusterTaskSummaryLimitedRequest
	err := json.Unmarshal([]byte(`{"MaxTasks":"500"}`), &request)
	if err == nil || !strings.Contains(err.Error(), "cannot unmarshal string") {
		t.Fatalf("unexpected malformed request error: %v", err)
	}
}

func TestClusterTaskSummaryLimitedQueryIsBoundedAndParameterized(t *testing.T) {
	for _, fragment := range []string{
		"WHERE ($1::BOOLEAN OR t.name NOT LIKE 'bg:%')",
		"AND ($2::TEXT IS NULL OR t.name = $2)",
		"LIMIT $3",
		"LEAST($4::BIGINT, pending_slots)",
		"LEFT JOIN selected s ON TRUE",
	} {
		if !strings.Contains(clusterTaskSummaryLimitedQuery, fragment) {
			t.Fatalf("limited query is missing %q", fragment)
		}
	}

	name := tasknames.SDR
	applied := ClusterTaskSummaryApplied{MaxTasks: 123, MaxPending: 12, IncludeBackground: true, TaskName: &name}
	args := clusterTaskSummaryLimitedQueryArgs(applied)
	if len(args) != 4 || args[0] != true || args[1] != name || args[2] != 123 || args[3] != 12 {
		t.Fatalf("unexpected query args: %#v", args)
	}
}

func TestClusterTaskSummaryLimitedSelectionRules(t *testing.T) {
	observedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	ownerID := int64(9)
	rows := []clusterTaskSummaryLimitedRow{
		{ID: 10, Name: tasknames.StorePiece, PostedTime: observedAt.Add(-24 * time.Hour), WorkStart: sql.NullTime{Time: observedAt.Add(-10 * time.Hour), Valid: true}, OwnerID: &ownerID},
		{ID: 5, Name: tasknames.SDR, PostedTime: observedAt.Add(-time.Hour), WorkStart: sql.NullTime{Time: observedAt.Add(-time.Minute), Valid: true}, OwnerID: &ownerID},
		{ID: 4, Name: tasknames.TreeRC, PostedTime: observedAt.Add(-time.Hour), OwnerID: &ownerID},
		{ID: 3, Name: tasknames.PoRep, PostedTime: observedAt.Add(-time.Hour), WorkStart: sql.NullTime{Time: observedAt.Add(-2 * time.Hour), Valid: true}, OwnerID: &ownerID},
		{ID: 2, Name: tasknames.PoRep, PostedTime: observedAt.Add(-time.Hour), WorkStart: sql.NullTime{Time: observedAt.Add(-2 * time.Hour), Valid: true}, OwnerID: &ownerID},
		{ID: 1, Name: tasknames.StorePiece, PostedTime: observedAt.Add(-48 * time.Hour)},
	}
	source := &memoryClusterTaskSummarySource{rows: rows, observedAt: observedAt}

	response, err := buildClusterTaskSummaryLimited(context.Background(), ClusterTaskSummaryLimitedRequest{}, source, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	wantRunning := []int64{4, 10, 2, 3, 5}
	for i, id := range wantRunning {
		if response.Running[i].ID != id {
			t.Fatalf("running[%d] = %d, want %d", i, response.Running[i].ID, id)
		}
	}
	if response.Running[0].AgeSeconds != nil {
		t.Fatal("unknown running start unexpectedly acquired an age")
	}
	if response.Pending[0].ID != 1 {
		t.Fatalf("pending ID = %d, want 1", response.Pending[0].ID)
	}
	if response.Pending[0].AgeSeconds == nil || *response.Pending[0].AgeSeconds != int64((48*time.Hour)/time.Second) {
		t.Fatalf("pending age = %v", response.Pending[0].AgeSeconds)
	}
	if len(response.Running)+len(response.Pending) > response.Applied.MaxTasks {
		t.Fatalf("response exceeded hard cap: %d", len(response.Running)+len(response.Pending))
	}
}

func TestClusterTaskSummaryLimitedAlwaysReservesCapacityForRunning(t *testing.T) {
	observedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	ownerID := int64(1)
	source := &memoryClusterTaskSummarySource{
		observedAt: observedAt,
		rows: []clusterTaskSummaryLimitedRow{
			{ID: 1, Name: tasknames.StorePiece, WorkStart: sql.NullTime{Time: observedAt.Add(-time.Minute), Valid: true}, OwnerID: &ownerID},
			{ID: 2, Name: tasknames.SDR, PostedTime: observedAt.Add(-365 * 24 * time.Hour)},
		},
	}
	maxTasks, maxPending := 1, 1

	response, err := buildClusterTaskSummaryLimited(context.Background(), ClusterTaskSummaryLimitedRequest{MaxTasks: &maxTasks, MaxPending: &maxPending}, source, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Running) != 1 || response.Running[0].ID != 1 || len(response.Pending) != 0 {
		t.Fatalf("running did not retain the only slot: running=%+v pending=%+v", response.Running, response.Pending)
	}
	if response.PendingTotal != 1 {
		t.Fatalf("PendingTotal = %d, want 1", response.PendingTotal)
	}
}

func TestClusterTaskSummaryLimitedPendingPreviewExamples(t *testing.T) {
	observedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		running     int
		pending     int
		request     ClusterTaskSummaryLimitedRequest
		wantRunning int
		wantPending int
	}{
		{name: "20 plus 30", running: 20, pending: 40_000, request: ClusterTaskSummaryLimitedRequest{MaxPending: clusterTaskTestInt(30)}, wantRunning: 20, wantPending: 30},
		{name: "480 plus 20", running: 480, pending: 40_000, wantRunning: 480, wantPending: 20},
		{name: "500 plus zero", running: 600, pending: 40_000, wantRunning: 500, wantPending: 0},
		{name: "explicit zero pending", running: 20, pending: 40_000, request: ClusterTaskSummaryLimitedRequest{MaxPending: clusterTaskTestInt(0)}, wantRunning: 20, wantPending: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ownerID := int64(1)
			rows := make([]clusterTaskSummaryLimitedRow, 0, test.running+test.pending)
			for i := 0; i < test.running; i++ {
				rows = append(rows, clusterTaskSummaryLimitedRow{ID: int64(i + 1), Name: tasknames.SDR, WorkStart: sql.NullTime{Time: observedAt.Add(-time.Minute), Valid: true}, OwnerID: &ownerID})
			}
			for i := 0; i < test.pending; i++ {
				rows = append(rows, clusterTaskSummaryLimitedRow{ID: int64(test.running + i + 1), Name: tasknames.SDR, PostedTime: observedAt.Add(-time.Hour)})
			}
			source := &memoryClusterTaskSummarySource{rows: rows, observedAt: observedAt}

			response, err := buildClusterTaskSummaryLimited(context.Background(), test.request, source, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(response.Running) != test.wantRunning || len(response.Pending) != test.wantPending {
				t.Fatalf("shown = %d/%d, want %d/%d", len(response.Running), len(response.Pending), test.wantRunning, test.wantPending)
			}
			if response.RunningTotal != int64(test.running) || response.PendingTotal != int64(test.pending) {
				t.Fatalf("totals = %d/%d, want %d/%d", response.RunningTotal, response.PendingTotal, test.running, test.pending)
			}
		})
	}
}

func TestClusterTaskSummaryLimitedFiltersBeforeLimits(t *testing.T) {
	observedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	ownerID := int64(1)
	rows := make([]clusterTaskSummaryLimitedRow, 0, 602)
	for i := 0; i < 600; i++ {
		rows = append(rows, clusterTaskSummaryLimitedRow{ID: int64(i + 1), Name: "bg:Hidden", WorkStart: sql.NullTime{Time: observedAt.Add(-time.Hour), Valid: true}, OwnerID: &ownerID})
	}
	rows = append(rows,
		clusterTaskSummaryLimitedRow{ID: 1001, Name: tasknames.StorePiece, WorkStart: sql.NullTime{Time: observedAt.Add(-time.Minute), Valid: true}, OwnerID: &ownerID},
		clusterTaskSummaryLimitedRow{ID: 1002, Name: tasknames.SDR, WorkStart: sql.NullTime{Time: observedAt.Add(-time.Minute), Valid: true}, OwnerID: &ownerID},
	)
	source := &memoryClusterTaskSummarySource{rows: rows, observedAt: observedAt}
	maxTasks := 1
	taskName := tasknames.StorePiece
	getter := &recordingSpidGetter{resolve: func(int64) []int64 { return []int64{1234} }}

	response, err := buildClusterTaskSummaryLimited(
		context.Background(),
		ClusterTaskSummaryLimitedRequest{MaxTasks: &maxTasks, TaskName: &taskName},
		source,
		nil,
		map[string]BatchSpidGetter{tasknames.StorePiece: getter},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Running) != 1 || response.Running[0].ID != 1001 || response.RunningTotal != 1 {
		t.Fatalf("filtered response = %+v, total %d", response.Running, response.RunningTotal)
	}
	if !response.TaskTypesAvailable || slices.ContainsFunc(response.TaskTypes, func(count ClusterTaskTypeCount) bool { return count.Name == "bg:Hidden" }) {
		t.Fatalf("background type leaked into choices: %+v", response.TaskTypes)
	}
	if !slices.ContainsFunc(response.TaskTypes, func(count ClusterTaskTypeCount) bool { return count.Name == tasknames.SDR }) {
		t.Fatalf("outside-subset task type missing: %+v", response.TaskTypes)
	}
	if len(getter.calls) != 1 || !slices.Equal(getter.calls[0], []int64{1001}) {
		t.Fatalf("enriched IDs = %v, want [1001]", getter.calls)
	}
}

func TestClusterTaskSummaryLimitedEnrichesOnlySelectedRows(t *testing.T) {
	observedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	ownerID := int64(1)
	rows := make([]clusterTaskSummaryLimitedRow, 0, 40_020)
	for i := 0; i < 20; i++ {
		rows = append(rows, clusterTaskSummaryLimitedRow{ID: int64(i + 1), Name: tasknames.SDR, WorkStart: sql.NullTime{Time: observedAt.Add(-time.Minute), Valid: true}, OwnerID: &ownerID})
	}
	for i := 0; i < 40_000; i++ {
		rows = append(rows, clusterTaskSummaryLimitedRow{ID: int64(i + 21), Name: tasknames.SDR, PostedTime: observedAt.Add(-time.Hour)})
	}
	source := &memoryClusterTaskSummarySource{rows: rows, observedAt: observedAt}
	getter := &recordingSpidGetter{resolve: func(taskID int64) []int64 { return []int64{taskID + 1000} }}

	response, err := buildClusterTaskSummaryLimited(context.Background(), ClusterTaskSummaryLimitedRequest{MaxPending: clusterTaskTestInt(30)}, source, nil, map[string]BatchSpidGetter{tasknames.SDR: getter})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Running) != 20 || len(response.Pending) != 30 {
		t.Fatalf("selected = %d/%d, want 20/30", len(response.Running), len(response.Pending))
	}
	if len(getter.calls) != 1 || len(getter.calls[0]) != 50 {
		t.Fatalf("enrichment calls = %d with %d IDs", len(getter.calls), len(getter.calls[0]))
	}
}

func TestClusterTaskSummaryLimitedPartialEnrichment(t *testing.T) {
	observedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	ownerID := int64(1)
	source := &memoryClusterTaskSummarySource{
		observedAt: observedAt,
		rows: []clusterTaskSummaryLimitedRow{
			{ID: 1, Name: tasknames.SDR, WorkStart: sql.NullTime{Time: observedAt.Add(-time.Hour), Valid: true}, OwnerID: &ownerID},
			{ID: 2, Name: tasknames.TreeD, WorkStart: sql.NullTime{Time: observedAt.Add(-time.Minute), Valid: true}, OwnerID: &ownerID},
			{ID: 3, Name: "NoSpid", WorkStart: sql.NullTime{Time: observedAt.Add(-time.Minute), Valid: true}, OwnerID: &ownerID},
		},
	}
	failing := &failingSpidGetter{err: errors.New("database unavailable")}
	success := &recordingSpidGetter{resolve: func(int64) []int64 { return []int64{1234} }}

	response, err := buildClusterTaskSummaryLimited(context.Background(), ClusterTaskSummaryLimitedRequest{}, source, nil, map[string]BatchSpidGetter{
		tasknames.SDR:   failing,
		tasknames.TreeD: success,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Partial || len(response.Warnings) != 1 || response.Warnings[0].Code != "spid_enrichment_failed" {
		t.Fatalf("partial warnings = %+v", response.Warnings)
	}
	if response.Running[0].SpID != "" || response.Running[1].SpID != "1234" || response.Running[2].SpID != "" {
		t.Fatalf("unexpected enrichment: %+v", response.Running)
	}
}

func TestResolveLimitedTaskSPIDsRejectsResultFromAnotherTaskType(t *testing.T) {
	rows := []clusterTaskSummaryLimitedRow{
		{ID: 1, Name: tasknames.SDR},
		{ID: 2, Name: tasknames.TreeD},
	}
	getter := fixedSpidGetter{resolved: []harmonytask.TaskSPID{
		{TaskID: 1, SPID: 1001},
		{TaskID: 2, SPID: 2002},
	}}

	spids, warnings, err := resolveLimitedTaskSPIDs(
		context.Background(),
		nil,
		rows,
		map[string]BatchSpidGetter{tasknames.SDR: getter},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", warnings)
	}
	if !slices.Equal(spids[1], []int64{1001}) {
		t.Fatalf("SpID for requested SDR task = %v, want 1001", spids[1])
	}
	if spid, ok := spids[2]; ok {
		t.Fatalf("SDR getter attached SpID %v to selected TreeD task", spid)
	}
}

func TestClusterTaskSummaryLimitedCancellationAndSnapshotFailures(t *testing.T) {
	observedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	ownerID := int64(1)
	row := clusterTaskSummaryLimitedRow{ID: 1, Name: tasknames.SDR, WorkStart: sql.NullTime{Time: observedAt, Valid: true}, OwnerID: &ownerID}

	t.Run("snapshot failure", func(t *testing.T) {
		source := &memoryClusterTaskSummarySource{snapshotErr: errors.New("snapshot failed")}
		if _, err := buildClusterTaskSummaryLimited(context.Background(), ClusterTaskSummaryLimitedRequest{}, source, nil, nil); err == nil {
			t.Fatal("expected snapshot error")
		}
	})

	t.Run("enrichment cancellation", func(t *testing.T) {
		source := &memoryClusterTaskSummarySource{rows: []clusterTaskSummaryLimitedRow{row}, observedAt: observedAt}
		getter := &failingSpidGetter{err: context.Canceled}
		if _, err := buildClusterTaskSummaryLimited(context.Background(), ClusterTaskSummaryLimitedRequest{}, source, nil, map[string]BatchSpidGetter{tasknames.SDR: getter}); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation", err)
		}
	})

	t.Run("task type failure is partial", func(t *testing.T) {
		source := &memoryClusterTaskSummarySource{rows: []clusterTaskSummaryLimitedRow{row}, observedAt: observedAt, taskTypesErr: errors.New("count failed")}
		response, err := buildClusterTaskSummaryLimited(context.Background(), ClusterTaskSummaryLimitedRequest{}, source, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !response.Partial || response.TaskTypesAvailable || len(response.Warnings) != 1 || response.Warnings[0].Code != "task_types_unavailable" {
			t.Fatalf("unexpected partial response: %+v", response)
		}
	})

	t.Run("task type cancellation is fatal", func(t *testing.T) {
		source := &memoryClusterTaskSummarySource{rows: []clusterTaskSummaryLimitedRow{row}, observedAt: observedAt, taskTypesErr: context.DeadlineExceeded}
		if _, err := buildClusterTaskSummaryLimited(context.Background(), ClusterTaskSummaryLimitedRequest{}, source, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want context deadline", err)
		}
	})
}

func TestBuildLimitedTaskSummaryDoesNotFallbackForUnknownRuntime(t *testing.T) {
	observedAt := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	ownerID := int64(1)
	row := clusterTaskSummaryLimitedRow{
		ID:         1,
		Name:       tasknames.SDR,
		PostedTime: observedAt.Add(-24 * time.Hour),
		OwnerID:    &ownerID,
		State:      "running",
	}

	task := buildLimitedTaskSummary(row, observedAt, nil)
	if task.AgeSeconds != nil {
		t.Fatalf("unknown runtime fell back to posted time: %d", *task.AgeSeconds)
	}
}

func TestClusterTaskAgeSecondsClampsFutureTimestamp(t *testing.T) {
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	if got := clusterTaskAgeSeconds(now, now.Add(time.Minute)); got != 0 {
		t.Fatalf("future age = %d, want 0", got)
	}
}
