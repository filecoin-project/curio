package webrpc

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/filecoin-project/go-address"
)

type WinStats struct {
	Actor       int64        `db:"sp_id"`
	Epoch       int64        `db:"epoch"`
	Block       string       `db:"mined_cid"`
	TaskID      int64        `db:"task_id"`
	SubmittedAt sql.NullTime `db:"submitted_at"`
	Included    sql.NullBool `db:"included"`

	BaseComputeTime sql.NullTime `db:"base_compute_time"`
	MinedAt         sql.NullTime `db:"mined_at"`

	SubmittedAtStr string `db:"-"`
	TaskSuccess    string `db:"-"`
	IncludedStr    string `db:"-"`
	ComputeTime    string `db:"-"`

	Miner string
}

func (a *WebRPC) WinStats(ctx context.Context) ([]WinStats, error) {
	var marks []WinStats
	err := a.deps.DB.Select(ctx, &marks, `SELECT sp_id, epoch, mined_cid, task_id, submitted_at, included, base_compute_time, mined_at FROM mining_tasks WHERE won = true ORDER BY epoch DESC LIMIT 8`)
	if err != nil {
		return nil, err
	}
	for i := range marks {
		if !marks[i].SubmittedAt.Valid {
			marks[i].SubmittedAtStr = "Not Submitted"
		} else {
			marks[i].SubmittedAtStr = marks[i].SubmittedAt.Time.Format(time.RFC822)
		}

		if !marks[i].Included.Valid {
			marks[i].IncludedStr = "Not Checked"
		} else if marks[i].Included.Bool {
			marks[i].IncludedStr = "Included"
		} else {
			marks[i].IncludedStr = "Not Included"
		}

		if marks[i].BaseComputeTime.Valid && marks[i].MinedAt.Valid {
			marks[i].ComputeTime = marks[i].MinedAt.Time.Sub(marks[i].BaseComputeTime.Time).Truncate(10 * time.Millisecond).String()
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
		maddr, err := address.NewIDAddress(uint64(marks[i].Actor))
		if err != nil {
			return nil, err
		}
		marks[i].Miner = maddr.String()
	}

	return marks, nil
}
