package webrpc

import (
	"context"

	"github.com/samber/lo"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

type TaskSummary struct {
	MinerID        string
	Name           string
	SincePosted    string
	Owner, OwnerID *string
	ID             int64
}

func (a *WebRPC) ClusterTaskSummary(ctx context.Context) ([]TaskSummary, error) {
	var ts []TaskSummary
	err := a.deps.DB.Select(ctx, &ts, `SELECT 
		t.id as id, t.name as name, t.update_time as SincePosted, t.owner_id as owner_id, hm.host_and_port as owner
	FROM harmony_task t LEFT JOIN curio.harmony_machines hm ON hm.id = t.owner_id 
	ORDER BY t.update_time ASC, t.owner_id`)
	if err != nil {
		return nil, err // Handle error
	}
	grouped := lo.GroupBy(ts, func(t TaskSummary) string {
		return t.Name
	})

	// Populate MinerID
	for _, g := range grouped {
		if v, ok := a.taskSPIDs[g[0].Name]; ok {
			for i, t := range g {
				g[i].MinerID = v.GetSpid(t.ID)
			}
		}
	}
	return ts, nil
}

type SpidGetter interface {
	GetSpid(taskID int64) string
}

func makeTaskSPIDs(tasks []harmonytask.TaskInterface) map[string]SpidGetter {
	spidGetters := lo.Filter(tasks, func(t harmonytask.TaskInterface, _ int) bool {
		_, ok := t.(SpidGetter)
		return ok
	})
	spids := make(map[string]SpidGetter)
	for _, t := range spidGetters {
		ttd := t.TypeDetails()
		spids[ttd.Name] = t.(SpidGetter)
	}
	return spids
}
