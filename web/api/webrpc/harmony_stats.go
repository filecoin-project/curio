package webrpc

import (
	"context"
	"time"

	"github.com/filecoin-project/go-address"
)

// SELECT name, count(case when result = 'true' then 1 end) as true_count,
//    count(case when result = 'false' then 1 end) as false_count, count(*) as total_count
//    from harmony_task_history where work_end > current_timestamp - interval '1 day'
//    group by name order by total_count desc

type HarmonyTaskStats struct {
	Name       string `db:"name"`
	TrueCount  int    `db:"true_count"`
	FalseCount int    `db:"false_count"`
	TotalCount int    `db:"total_count"`
}

func (a *WebRPC) HarmonyTaskStats(ctx context.Context) ([]HarmonyTaskStats, error) {
	var stats []HarmonyTaskStats
	err := a.deps.DB.Select(ctx, &stats, `SELECT name, count(case when result = 'true' then 1 end) as true_count,
		count(case when result = 'false' then 1 end) as false_count, count(*) as total_count
		from harmony_task_history where work_end > current_timestamp - interval '1 day'
		group by name order by total_count desc`)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

type HarmonyMachineDesc struct {
	MachineID   int64  `db:"machine_id"`
	Name        string `db:"machine_name"`
	MachineAddr string `db:"host_and_port"`
	Actors      string `db:"miners"`
}

func (a *WebRPC) HarmonyTaskMachines(ctx context.Context, taskName string) ([]HarmonyMachineDesc, error) {
	var stats []HarmonyMachineDesc
	// note: LIKE is inefficient, but beats 2 queries given small enough list size.
	err := a.deps.DB.Select(ctx, &stats, `
	SELECT md.machine_id, md.machine_name, hm.host_and_port, md.miners 
	FROM harmony_machine_details md
	INNER JOIN harmony_machines hm ON md.machine_id = hm.id
	WHERE md.tasks LIKE $1
	ORDER BY md.miners DESC`, "%,"+taskName+",%")
	if err != nil {
		return nil, err
	}

	return stats, nil
}

type SectorEvent struct {
	SpID         uint64 `db:"sp_id"`
	SectorNumber uint64 `db:"sector_number"`

	Addr address.Address `db:"-"`
}

type HarmonyTaskHistory struct {
	ID     int64  `db:"id"`
	TaskID int64  `db:"task_id"`
	Name   string `db:"name"`

	WorkStart time.Time `db:"work_start"`
	WorkEnd   time.Time `db:"work_end"`
	Posted    time.Time `db:"posted"`

	Took string `db:"-"`

	Result bool   `db:"result"`
	Err    string `db:"err"`

	CompletedBy     string  `db:"completed_by_host_and_port"`
	CompletedById   *int64  `db:"completed_by_machine"`
	CompletedByName *string `db:"completed_by_machine_name"`

	Events []*SectorEvent `db:"-"`
}

func (a *WebRPC) HarmonyTaskHistory(ctx context.Context, taskName string, fails bool) ([]*HarmonyTaskHistory, error) {
	var stats []*HarmonyTaskHistory
	err := a.deps.DB.Select(ctx, &stats, `SELECT
	hist.task_id, hist.name, hist.work_start, hist.work_end, hist.posted, hist.result, hist.err,
	hist.completed_by_host_and_port, mach.id as completed_by_machine, hmd.machine_name as completed_by_machine_name
    FROM harmony_task_history hist
    LEFT JOIN harmony_machines mach ON hist.completed_by_host_and_port = mach.host_and_port
    LEFT JOIN curio.harmony_machine_details hmd on mach.id = hmd.machine_id
    WHERE name = $1 AND work_end > current_timestamp - interval '1 day' AND ($2 OR NOT hist.result) ORDER BY work_end DESC LIMIT 30`, taskName, !fails)
	if err != nil {
		return nil, err
	}

	for _, stat := range stats {
		stat.Took = stat.WorkEnd.Sub(stat.WorkStart).Truncate(100 * time.Millisecond).String()
	}

	return stats, nil
}

// HarmonyTask represents the current state of a task.
type HarmonyTask struct {
	ID         int64     `db:"id"`
	Name       string    `db:"name"`
	UpdateTime time.Time `db:"update_time"`
	PostedTime time.Time `db:"posted_time"`
	OwnerID    *int64    `db:"owner_id"`
	OwnerAddr  *string   `db:"owner_addr"`
	OwnerName  *string   `db:"owner_name"`
}

// HarmonyTaskDetails returns the current state of a task by ID.
func (a *WebRPC) HarmonyTaskDetails(ctx context.Context, taskID int64) (*HarmonyTask, error) {
	var task []*HarmonyTask
	err := a.deps.DB.Select(ctx, &task, `
        SELECT
            t.id,
            t.name,
            t.update_time,
            t.posted_time,
            t.owner_id,
            hm.host_and_port AS owner_addr,
            hmd.machine_name AS owner_name
        FROM harmony_task t
		LEFT JOIN harmony_machines hm ON t.owner_id = hm.id
        LEFT JOIN harmony_machine_details hmd ON t.owner_id = hmd.machine_id
        WHERE t.id = $1
    `, taskID)
	if err != nil {
		return nil, err
	}
	return firstOrZero(task), nil
}

// HarmonyTaskHistoryById returns the history of a task by task ID.
func (a *WebRPC) HarmonyTaskHistoryById(ctx context.Context, taskID int64) ([]*HarmonyTaskHistory, error) {
	var history []*HarmonyTaskHistory
	err := a.deps.DB.Select(ctx, &history, `
        SELECT
            hist.id,
            hist.task_id,
            hist.name,
            hist.work_start,
            hist.work_end,
            hist.posted,
            hist.result,
            hist.err,
            hist.completed_by_host_and_port AS completed_by_host_and_port,
            mach.id AS completed_by_machine,
            hmd.machine_name AS completed_by_machine_name
        FROM harmony_task_history hist
        LEFT JOIN harmony_machines mach ON hist.completed_by_host_and_port = mach.host_and_port
        LEFT JOIN harmony_machine_details hmd ON mach.id = hmd.machine_id
        WHERE hist.task_id = $1
        ORDER BY hist.work_end DESC
    `, taskID)
	if err != nil {
		return nil, err
	}

	for _, h := range history {
		var events []*SectorEvent
		err := a.deps.DB.Select(ctx, &events, `
			SELECT sp_id, sector_number
			FROM sectors_pipeline_events
			WHERE task_history_id = $1
		`, h.ID)
		if err != nil {
			return nil, err
		}

		for _, event := range events {
			event.Addr, err = address.NewIDAddress(event.SpID)
			if err != nil {
				return nil, err
			}
		}

		h.Events = events
	}

	return history, nil
}
