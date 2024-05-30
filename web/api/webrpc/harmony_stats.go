package webrpc

import "context"

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
