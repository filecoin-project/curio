package webrpc

import (
	"context"
	"strconv"
	"time"

	"github.com/samber/lo"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
)

type TaskSummary struct {
	ID             int64
	Name           string
	SpID           string
	SincePosted    time.Time `db:"since_posted"`
	Owner, OwnerID *string

	// db ignored
	SincePostedStr string `db:"-"`

	Miner string
}

func (a *WebRPC) ClusterTaskSummary(ctx context.Context) ([]TaskSummary, error) {
	var ts = []TaskSummary{}
	err := a.deps.DB.Select(ctx, &ts, `SELECT 
		t.id as id, t.name as name, t.update_time as since_posted, t.owner_id as owner_id, hm.host_and_port as owner
	FROM harmony_task t LEFT JOIN harmony_machines hm ON hm.id = t.owner_id 
	ORDER BY
	    CASE WHEN t.owner_id IS NULL THEN 1 ELSE 0 END, t.update_time ASC`)
	if err != nil {
		return nil, err // Handle error
	}

	// Populate MinerID
	for i := range ts {
		ts[i].SincePostedStr = time.Since(ts[i].SincePosted).Truncate(time.Second).String()

		if v, ok := a.taskSPIDs[ts[i].Name]; ok {
			ts[i].SpID = v.GetSpid(a.deps.DB, ts[i].ID)
		}

		if ts[i].SpID != "" {
			spid, err := strconv.ParseInt(ts[i].SpID, 10, 64)
			if err != nil {
				return nil, err
			}

			if spid > 0 {
				maddr, err := address.NewIDAddress(uint64(spid))
				if err != nil {
					return nil, err
				}
				ts[i].Miner = maddr.String()
			} else {
				ts[i].Miner = ""
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
