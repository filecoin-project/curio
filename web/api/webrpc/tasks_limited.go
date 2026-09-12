package webrpc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	CLUSTER_TASK_SUMMARY_DEFAULT_MAX_TASKS   = 500
	CLUSTER_TASK_SUMMARY_DEFAULT_MAX_PENDING = 500
	CLUSTER_TASK_SUMMARY_MAX_TASKS           = 500
)

type ClusterTaskSummaryLimitedRequest struct {
	MaxTasks          *int
	MaxPending        *int
	IncludeBackground bool
	TaskName          *string
}

type ClusterTaskSummaryApplied struct {
	MaxTasks          int
	MaxPending        int
	IncludeBackground bool
	TaskName          *string
}

type ClusterTaskSummaryWarning struct {
	Code     string
	Message  string
	TaskName string
}

type ClusterTaskTypeCount struct {
	Name    string
	Running int64
	Pending int64
	Total   int64
}

type ClusterTaskSummaryLimitedTask struct {
	ID                   int64
	Name                 string
	SpID                 string
	SpIDs                []string
	Miners               []string
	OwnershipStartSource string
	Miner                string
	State                string
	AgeSeconds           *int64
	TookSeconds          *int64
	TookState            string
	AttemptID            *string
	Owner                *string
	OwnerID              *int64
}

type ClusterTaskSummaryLimitedResponse struct {
	Running            []ClusterTaskSummaryLimitedTask
	Pending            []ClusterTaskSummaryLimitedTask
	Applied            ClusterTaskSummaryApplied
	RunningTotal       int64
	PendingTotal       int64
	TotalsAvailable    bool
	ObservedAt         time.Time
	TaskTypes          []ClusterTaskTypeCount
	TaskTypesAvailable bool
	Partial            bool
	Warnings           []ClusterTaskSummaryWarning
}

type clusterTaskSummaryLimitedRow struct {
	ID                 int64
	Name               string
	PostedTime         time.Time
	WorkStart          sql.NullTime
	WorkStartSource    sql.NullString
	AttemptStartedAt   sql.NullTime
	AttemptID          sql.NullString
	AttemptStartSource sql.NullString
	Owner              *string
	OwnerID            *int64
	State              string
}

type clusterTaskSummarySnapshot struct {
	Rows         []clusterTaskSummaryLimitedRow
	RunningTotal int64
	PendingTotal int64
	ObservedAt   time.Time
}

type clusterTaskSummarySource interface {
	LoadSnapshot(context.Context, ClusterTaskSummaryApplied) (clusterTaskSummarySnapshot, error)
	LoadTaskTypes(context.Context, bool) ([]ClusterTaskTypeCount, error)
}

type harmonyClusterTaskSummarySource struct {
	db *harmonydb.DB
}

type clusterTaskSummaryDBRow struct {
	ID                 sql.NullInt64  `db:"id"`
	Name               sql.NullString `db:"name"`
	PostedTime         sql.NullTime   `db:"posted_time"`
	WorkStart          sql.NullTime   `db:"work_start"`
	WorkStartSource    sql.NullString `db:"work_start_source"`
	AttemptStartedAt   sql.NullTime   `db:"attempt_started_at"`
	AttemptID          sql.NullString `db:"attempt_id"`
	AttemptStartSource sql.NullString `db:"attempt_start_source"`
	Owner              sql.NullString `db:"owner"`
	OwnerID            sql.NullInt64  `db:"owner_id"`
	State              sql.NullString `db:"state"`
	RunningTotal       int64          `db:"running_total"`
	PendingTotal       int64          `db:"pending_total"`
	ObservedAt         time.Time      `db:"observed_at"`
}

const clusterTaskSummaryLimitedQuery = `
WITH matching AS (
	SELECT
		t.id,
		t.name,
		t.posted_time,
		t.work_start,
		t.work_start_source,
		t.attempt_started_at,
		t.attempt_id,
		t.attempt_start_source,
		t.owner_id
	FROM harmony_task t
	WHERE ($1::BOOLEAN OR t.name NOT LIKE 'bg:%')
		AND ($2::TEXT IS NULL OR t.name = $2)
),
matching_counts AS (
	SELECT
		COUNT(*) FILTER (WHERE owner_id IS NOT NULL) AS running_total,
		COUNT(*) FILTER (WHERE owner_id IS NULL) AS pending_total
	FROM matching
),
running AS (
	SELECT *, 'running'::TEXT AS state, 0::BIGINT AS section_order
	FROM matching
	WHERE owner_id IS NOT NULL
	ORDER BY work_start ASC NULLS FIRST, id ASC
	LIMIT $3
),
remaining AS (
	SELECT GREATEST($3::BIGINT - COUNT(*)::BIGINT, 0::BIGINT) AS pending_slots
	FROM running
),
pending AS (
	SELECT *, 'pending'::TEXT AS state, 1::BIGINT AS section_order
	FROM matching
	WHERE owner_id IS NULL
	ORDER BY posted_time ASC, id ASC
	LIMIT (SELECT LEAST($4::BIGINT, pending_slots) FROM remaining)
),
selected AS (
	SELECT * FROM running
	UNION ALL
	SELECT * FROM pending
),
observed AS (
	SELECT statement_timestamp() AS observed_at
)
SELECT
	s.id,
	s.name,
	s.posted_time,
	s.work_start,
	s.work_start_source,
	s.attempt_started_at,
	s.attempt_id,
	s.attempt_start_source,
	hm.host_and_port AS owner,
	s.owner_id,
	s.state,
	c.running_total,
	c.pending_total,
	o.observed_at
FROM matching_counts c
CROSS JOIN observed o
LEFT JOIN selected s ON TRUE
LEFT JOIN harmony_machines hm ON hm.id = s.owner_id
ORDER BY
	s.section_order ASC NULLS LAST,
	CASE WHEN s.section_order = 0 THEN s.work_start ELSE s.posted_time END ASC NULLS FIRST,
	s.id ASC`

func (s harmonyClusterTaskSummarySource) LoadSnapshot(ctx context.Context, applied ClusterTaskSummaryApplied) (clusterTaskSummarySnapshot, error) {
	var rows []clusterTaskSummaryDBRow
	err := s.db.Select(ctx, &rows, clusterTaskSummaryLimitedQuery, clusterTaskSummaryLimitedQueryArgs(applied)...)
	if err != nil {
		return clusterTaskSummarySnapshot{}, err
	}
	if len(rows) == 0 {
		return clusterTaskSummarySnapshot{}, fmt.Errorf("cluster task summary query returned no metadata row")
	}

	snapshot := clusterTaskSummarySnapshot{
		Rows:         make([]clusterTaskSummaryLimitedRow, 0, len(rows)),
		RunningTotal: rows[0].RunningTotal,
		PendingTotal: rows[0].PendingTotal,
		ObservedAt:   rows[0].ObservedAt,
	}
	for _, row := range rows {
		if !row.ID.Valid {
			continue
		}
		if !row.Name.Valid || !row.PostedTime.Valid || !row.State.Valid {
			return clusterTaskSummarySnapshot{}, fmt.Errorf("cluster task summary row %d is missing required data", row.ID.Int64)
		}

		var owner *string
		if row.Owner.Valid {
			value := row.Owner.String
			owner = &value
		}
		var ownerID *int64
		if row.OwnerID.Valid {
			value := row.OwnerID.Int64
			ownerID = &value
		}

		snapshot.Rows = append(snapshot.Rows, clusterTaskSummaryLimitedRow{
			ID:                 row.ID.Int64,
			Name:               row.Name.String,
			PostedTime:         row.PostedTime.Time,
			WorkStart:          row.WorkStart,
			WorkStartSource:    row.WorkStartSource,
			AttemptStartedAt:   row.AttemptStartedAt,
			AttemptID:          row.AttemptID,
			AttemptStartSource: row.AttemptStartSource,
			Owner:              owner,
			OwnerID:            ownerID,
			State:              row.State.String,
		})
	}

	return snapshot, nil
}

func clusterTaskSummaryLimitedQueryArgs(applied ClusterTaskSummaryApplied) []any {
	var taskName any
	if applied.TaskName != nil {
		taskName = *applied.TaskName
	}
	return []any{
		applied.IncludeBackground,
		taskName,
		applied.MaxTasks,
		applied.MaxPending,
	}
}

func (s harmonyClusterTaskSummarySource) LoadTaskTypes(ctx context.Context, includeBackground bool) ([]ClusterTaskTypeCount, error) {
	var taskTypes []ClusterTaskTypeCount
	err := s.db.Select(ctx, &taskTypes, `
		SELECT
			name,
			COUNT(*) FILTER (WHERE owner_id IS NOT NULL) AS running,
			COUNT(*) FILTER (WHERE owner_id IS NULL) AS pending,
			COUNT(*) AS total
		FROM harmony_task
		WHERE ($1::BOOLEAN OR name NOT LIKE 'bg:%')
		GROUP BY name
		ORDER BY name ASC`, includeBackground)
	if err != nil {
		return nil, err
	}
	sort.Slice(taskTypes, func(i, j int) bool {
		return taskTypes[i].Name < taskTypes[j].Name
	})
	return taskTypes, nil
}

func normalizeClusterTaskSummaryRequest(request ClusterTaskSummaryLimitedRequest) ClusterTaskSummaryApplied {
	maxTasks := CLUSTER_TASK_SUMMARY_DEFAULT_MAX_TASKS
	if request.MaxTasks != nil {
		maxTasks = min(max(*request.MaxTasks, 1), CLUSTER_TASK_SUMMARY_MAX_TASKS)
	}

	maxPending := CLUSTER_TASK_SUMMARY_DEFAULT_MAX_PENDING
	if request.MaxPending != nil {
		maxPending = max(*request.MaxPending, 0)
	}
	maxPending = min(maxPending, maxTasks)

	var taskName *string
	if request.TaskName != nil {
		value := *request.TaskName
		taskName = &value
	}

	return ClusterTaskSummaryApplied{
		MaxTasks:          maxTasks,
		MaxPending:        maxPending,
		IncludeBackground: request.IncludeBackground,
		TaskName:          taskName,
	}
}

func clusterTaskContextError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func resolveLimitedTaskSPIDs(ctx context.Context, db *harmonydb.DB, rows []clusterTaskSummaryLimitedRow, getters map[string]BatchSpidGetter) (map[int64][]int64, []ClusterTaskSummaryWarning, error) {
	taskIDsByName := make(map[string][]int64)
	taskNames := make([]string, 0)
	for _, row := range rows {
		if _, ok := getters[row.Name]; !ok {
			continue
		}
		if _, ok := taskIDsByName[row.Name]; !ok {
			taskNames = append(taskNames, row.Name)
		}
		taskIDsByName[row.Name] = append(taskIDsByName[row.Name], row.ID)
	}

	spids := make(map[int64][]int64)
	warnings := make([]ClusterTaskSummaryWarning, 0)
	sort.Strings(taskNames)
	for _, name := range taskNames {
		ids := taskIDsByName[name]
		for start := 0; start < len(ids); start += CLUSTER_TASK_SPID_BATCH_SIZE {
			end := min(start+CLUSTER_TASK_SPID_BATCH_SIZE, len(ids))
			batchIDs := ids[start:end]
			allowedIDs := make(map[int64]struct{}, len(batchIDs))
			for _, taskID := range batchIDs {
				allowedIDs[taskID] = struct{}{}
			}

			resolved, err := getters[name].GetSpids(ctx, db, batchIDs)
			if err != nil {
				if cancellationErr := clusterTaskContextError(ctx, err); cancellationErr != nil {
					return nil, nil, cancellationErr
				}
				log.Warnw("cluster task SpID enrichment failed", "task_type", name, "task_count", end-start, "error", err)
				warnings = append(warnings, ClusterTaskSummaryWarning{
					Code:     "spid_enrichment_failed",
					Message:  fmt.Sprintf("Provider IDs are unavailable for %s tasks in this snapshot.", name),
					TaskName: name,
				})
				break
			}

			for _, resolvedTask := range resolved {
				if _, ok := allowedIDs[resolvedTask.TaskID]; !ok {
					continue
				}
				if !slices.Contains(spids[resolvedTask.TaskID], resolvedTask.SPID) {
					spids[resolvedTask.TaskID] = append(spids[resolvedTask.TaskID], resolvedTask.SPID)
				}
			}
		}
	}

	for _, ids := range spids {
		slices.Sort(ids)
	}
	return spids, warnings, nil
}

func clusterTaskAgeSeconds(observedAt, startedAt time.Time) int64 {
	age := observedAt.Sub(startedAt)
	if age < 0 {
		return 0
	}
	return int64(age / time.Second)
}

func buildLimitedTaskSummary(row clusterTaskSummaryLimitedRow, observedAt time.Time, spids map[int64][]int64) ClusterTaskSummaryLimitedTask {
	task := ClusterTaskSummaryLimitedTask{
		ID:      row.ID,
		Name:    row.Name,
		State:   row.State,
		Owner:   row.Owner,
		OwnerID: row.OwnerID,
	}

	if row.State == "pending" {
		ageSeconds := clusterTaskAgeSeconds(observedAt, row.PostedTime)
		task.AgeSeconds = &ageSeconds
	} else {
		task.AgeSeconds = knownTaskAge(observedAt, row.WorkStart, row.WorkStartSource)
		task.OwnershipStartSource = "unknown"
		if task.AgeSeconds != nil {
			task.OwnershipStartSource = "claim"
		}
	}

	task.TookSeconds, task.TookState = taskTookAge(row, observedAt)
	if row.AttemptID.Valid {
		id := row.AttemptID.String
		task.AttemptID = &id
	}

	for _, spid := range spids[row.ID] {
		task.SpIDs = append(task.SpIDs, strconv.FormatInt(spid, 10))
		if spid > 0 {
			if miner, err := address.NewIDAddress(uint64(spid)); err == nil {
				task.Miners = append(task.Miners, miner.String())
			}
		}
	}
	if len(task.SpIDs) > 0 {
		task.SpID = task.SpIDs[0]
	}
	if len(task.Miners) > 0 {
		task.Miner = task.Miners[0]
	}

	return task
}

func buildClusterTaskSummaryLimited(ctx context.Context, request ClusterTaskSummaryLimitedRequest, source clusterTaskSummarySource, db *harmonydb.DB, getters map[string]BatchSpidGetter) (ClusterTaskSummaryLimitedResponse, error) {
	applied := normalizeClusterTaskSummaryRequest(request)
	snapshot, err := source.LoadSnapshot(ctx, applied)
	if err != nil {
		return ClusterTaskSummaryLimitedResponse{}, fmt.Errorf("loading cluster task snapshot: %w", err)
	}

	spids, warnings, err := resolveLimitedTaskSPIDs(ctx, db, snapshot.Rows, getters)
	if err != nil {
		return ClusterTaskSummaryLimitedResponse{}, err
	}

	response := ClusterTaskSummaryLimitedResponse{
		Running:         make([]ClusterTaskSummaryLimitedTask, 0),
		Pending:         make([]ClusterTaskSummaryLimitedTask, 0),
		Applied:         applied,
		RunningTotal:    snapshot.RunningTotal,
		PendingTotal:    snapshot.PendingTotal,
		TotalsAvailable: true,
		ObservedAt:      snapshot.ObservedAt,
		TaskTypes:       make([]ClusterTaskTypeCount, 0),
		Warnings:        warnings,
		Partial:         len(warnings) > 0,
	}
	for _, row := range snapshot.Rows {
		task := buildLimitedTaskSummary(row, snapshot.ObservedAt, spids)
		if row.State == "running" {
			response.Running = append(response.Running, task)
		} else {
			response.Pending = append(response.Pending, task)
		}
	}

	taskTypes, err := source.LoadTaskTypes(ctx, applied.IncludeBackground)
	if err != nil {
		if cancellationErr := clusterTaskContextError(ctx, err); cancellationErr != nil {
			return ClusterTaskSummaryLimitedResponse{}, cancellationErr
		}
		log.Warnw("cluster task type summary failed", "error", err)
		response.Warnings = append(response.Warnings, ClusterTaskSummaryWarning{
			Code:    "task_types_unavailable",
			Message: "Task type choices are unavailable for this snapshot.",
		})
		response.Partial = true
	} else {
		if taskTypes == nil {
			taskTypes = make([]ClusterTaskTypeCount, 0)
		}
		response.TaskTypes = taskTypes
		response.TaskTypesAvailable = true
	}

	return response, nil
}

func (a *WebRPC) ClusterTaskSummaryLimited(ctx context.Context, request ClusterTaskSummaryLimitedRequest) (ClusterTaskSummaryLimitedResponse, error) {
	return buildClusterTaskSummaryLimited(
		ctx,
		request,
		harmonyClusterTaskSummarySource{db: a.Deps.DB},
		a.Deps.DB,
		makeBatchTaskSPIDs(a.TaskSPIDs),
	)
}
