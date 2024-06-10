package webrpc

import (
	"context"
	"fmt"
	"time"
)

type WinStats struct {
	Actor       int64      `db:"sp_id"`
	Epoch       int64      `db:"epoch"`
	Block       string     `db:"mined_cid"`
	TaskID      int64      `db:"task_id"`
	SubmittedAt *time.Time `db:"submitted_at"`
	Included    *bool      `db:"included"`

	BaseComputeTime *time.Time `db:"base_compute_time"`
	MinedAt         *time.Time `db:"mined_at"`

	SubmittedAtStr string `db:"-"`
	TaskSuccess    string `db:"-"`
	IncludedStr    string `db:"-"`
	ComputeTime    string `db:"-"`
}

func (a *WebRPC) WinStats(ctx context.Context) ([]WinStats, error) {
	var marks []WinStats
	err := a.deps.DB.Select(ctx, &marks, `SELECT sp_id, epoch, mined_cid, task_id, submitted_at, included, base_compute_time, mined_at FROM mining_tasks WHERE won = true ORDER BY epoch DESC LIMIT 8`)
	if err != nil {
		return nil, err
	}
	for i := range marks {
		if marks[i].SubmittedAt == nil {
			marks[i].SubmittedAtStr = "Not Submitted"
		} else {
			marks[i].SubmittedAtStr = marks[i].SubmittedAt.Format(time.RFC822)
		}

		if marks[i].Included == nil {
			marks[i].IncludedStr = "Not Checked"
		} else if *marks[i].Included {
			marks[i].IncludedStr = "Included"
		} else {
			marks[i].IncludedStr = "Not Included"
		}

		if marks[i].BaseComputeTime != nil && marks[i].MinedAt != nil {
			marks[i].ComputeTime = marks[i].MinedAt.Sub(*marks[i].BaseComputeTime).Truncate(10 * time.Millisecond).String()
		}

		var taskRes []struct {
			Result bool   `db:"result"`
			Err    string `db:"error"`
		}

		err := a.deps.DB.Select(ctx, &taskRes, `SELECT result FROM harmony_task_history WHERE task_id = $1`, marks[i].TaskID)
		if err != nil {
			return nil, err
		}

		var success, fail int

		for _, tr := range taskRes {
			if tr.Result {
				success++
			} else {
				fail++
			}
		}

		if fail > 0 {
			marks[i].TaskSuccess = fmt.Sprintf("Fail(%d) ", fail)
		}
		if success > 0 {
			marks[i].TaskSuccess += "Success"
		}
	}

	return marks, nil
}
