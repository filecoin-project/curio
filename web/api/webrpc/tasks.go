package webrpc

import (
	"context"
	"time"

	"github.com/samber/lo"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
)

type TaskSummary struct {
	ID             int64
	Name           string
	MinerID        string
	SincePosted    time.Time `db:"since_posted"`
	Owner, OwnerID *string

	// db ignored
	SincePostedStr string `db:"-"`
}

func (a *WebRPC) ClusterTaskSummary(ctx context.Context) ([]TaskSummary, error) {
	var ts []TaskSummary
	err := a.deps.DB.Select(ctx, &ts, `SELECT 
		t.id as id, t.name as name, t.update_time as since_posted, t.owner_id as owner_id, hm.host_and_port as owner
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
				g[i].MinerID = v.GetSpid(a.deps.DB, t.ID)
			}
		}
	}

	return ts, nil
}

type SpidGetter interface {
	GetSpid(db *harmonydb.DB, taskID int64) string
}

func makeTaskSPIDs() map[string]SpidGetter {
	spidGetters := lo.Filter(lo.Values(harmonytask.Registry), func(t harmonytask.TaskInterface, _ int) bool {
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
