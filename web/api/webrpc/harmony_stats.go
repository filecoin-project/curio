package webrpc

import (
	"context"
	"time"
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

type HarmonyTaskHistory struct {
	TaskID int64  `db:"task_id"`
	Name   string `db:"name"`

	WorkStart time.Time `db:"work_start"`
	WorkEnd   time.Time `db:"work_end"`
	Posted    time.Time `db:"posted"`

	Result bool   `db:"result"`
	Err    string `db:"err"`

	CompletedBy     string  `db:"completed_by_host_and_port"`
	CompletedById   *int64  `db:"completed_by_machine"`
	CompletedByName *string `db:"completed_by_machine_name"`
}

func (a *WebRPC) HarmonyTaskHistory(ctx context.Context, taskName string) ([]HarmonyTaskHistory, error) {
	var stats []HarmonyTaskHistory
	err := a.deps.DB.Select(ctx, &stats, `SELECT
	hist.task_id, hist.name, hist.work_start, hist.work_end, hist.posted, hist.result, hist.err,
	hist.completed_by_host_and_port, mach.id as completed_by_machine, hmd.machine_name as completed_by_machine_name
    FROM harmony_task_history hist
    LEFT JOIN harmony_machines mach ON hist.completed_by_host_and_port = mach.host_and_port
    LEFT JOIN curio.harmony_machine_details hmd on mach.id = hmd.machine_id
    WHERE name = $1 AND work_end > current_timestamp - interval '1 day' ORDER BY work_end DESC LIMIT 30`, taskName)
	if err != nil {
		return nil, err
	}
	return stats, nil
}
