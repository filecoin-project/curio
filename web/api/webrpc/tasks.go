package webrpc

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"

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

type TaskStatus struct {
	TaskID   int64   `json:"task_id"`
	Status   string  `json:"status"` // "pending", "running", "done", "failed"
	OwnerID  *int64  `json:"owner_id,omitempty"`
	Name     string  `json:"name"`
	PostedAt *string `json:"posted_at,omitempty"`
}

func (a *WebRPC) GetTaskStatus(ctx context.Context, taskID int64) (*TaskStatus, error) {
	status := &TaskStatus{TaskID: taskID}

	// Check if task is present in harmony_task
	var ownerID sql.NullInt64
	var name string
	var postedTime time.Time
	err := a.deps.DB.QueryRow(ctx, `
        SELECT owner_id, name, posted_time FROM harmony_task WHERE id = $1
    `, taskID).Scan(&ownerID, &name, &postedTime)

	if err == nil {
		status.Name = name
		status.PostedAt = strPtr(postedTime.Format(time.RFC3339))
		if ownerID.Valid {
			status.Status = "running"
			status.OwnerID = &ownerID.Int64
		} else {
			status.Status = "pending"
		}
		return status, nil
	}

	if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("failed to query harmony_task: %w", err)
	}

	// Not found in harmony_task, check harmony_task_history
	var result bool
	err = a.deps.DB.QueryRow(ctx, `
        SELECT result, name, posted FROM harmony_task_history WHERE task_id = $1 ORDER BY id DESC LIMIT 1
    `, taskID).Scan(&result, &name, &postedTime)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("task not found")
	} else if err != nil {
		return nil, fmt.Errorf("failed to query harmony_task_history: %w", err)
	}

	status.Name = name
	status.PostedAt = strPtr(postedTime.Format(time.RFC3339))
	if result {
		status.Status = "done"
	} else {
		status.Status = "failed"
	}

	return status, nil
}

func strPtr(s string) *string {
	return &s
}

func (a *WebRPC) RestartFailedTask(ctx context.Context, taskID int64) error {
	// Check if task is present in harmony_task
	var exists bool
	err := a.deps.DB.QueryRow(ctx, `
        SELECT 1 FROM harmony_task WHERE id = $1
    `, taskID).Scan(&exists)

	if err != pgx.ErrNoRows {
		if err != nil {
			return fmt.Errorf("failed to check harmony_task: %w", err)
		}
		return fmt.Errorf("task is already pending or running")
	}

	// Check the most recent entry in harmony_task_history
	var name string
	var postedTime time.Time
	var result bool
	err = a.deps.DB.QueryRow(ctx, `
        SELECT name, posted, result FROM harmony_task_history WHERE task_id = $1 ORDER BY id DESC LIMIT 1
    `, taskID).Scan(&name, &postedTime, &result)

	if err == pgx.ErrNoRows {
		return fmt.Errorf("task not found in history")
	} else if err != nil {
		return fmt.Errorf("failed to query harmony_task_history: %w", err)
	}

	if result {
		return fmt.Errorf("task was successful, cannot restart")
	}

	// Insert into harmony_task
	_, err = a.deps.DB.Exec(ctx, `
        INSERT INTO harmony_task (id, initiated_by, update_time, posted_time, owner_id, added_by, previous_task, name)
        VALUES ($1, NULL, NOW(), $2, NULL, $3, NULL, $4)
    `, taskID, postedTime, a.deps.MachineID, name)

	if err != nil {
		return fmt.Errorf("failed to insert into harmony_task: %w", err)
	}

	return nil
}
