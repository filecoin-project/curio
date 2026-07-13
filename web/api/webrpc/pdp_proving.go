package webrpc

import (
	"context"
	"database/sql"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
)

type PDPProvingStatus struct {
	HeadEpoch               int64  `json:"headEpoch"`
	NextProveAtEpoch        *int64 `json:"nextProveAtEpoch"`
	NextDeadlineEpoch       *int64 `json:"nextDeadlineEpoch"`
	EpochsUntilNextSession  *int64 `json:"epochsUntilNextSession"`
	SecondsUntilNextSession *int64 `json:"secondsUntilNextSession"`
	ActiveDataSetCount      int    `json:"activeDataSetCount"`
	InWindowCount           int    `json:"inWindowCount"`
	OverdueCount            int    `json:"overdueCount"`
}

type PDPProvingTimelineEvent struct {
	TaskID    int64     `json:"taskId" db:"task_id"`
	DataSetID int64     `json:"dataSetId" db:"data_set"`
	Success   bool      `json:"success" db:"result"`
	Err       string    `json:"err" db:"err"`
	WorkEnd   time.Time `json:"workEnd" db:"work_end"`
}

type PDPProvingFailure struct {
	DataSetID                 int64      `json:"dataSetId"`
	TaskID                    *int64     `json:"taskId,omitempty"`
	Err                       string     `json:"err"`
	WorkEnd                   *time.Time `json:"workEnd,omitempty"`
	ConsecutiveProveFailures  int        `json:"consecutiveProveFailures"`
	NextProveAttemptAt        *int64     `json:"nextProveAttemptAt,omitempty"`
	UnrecoverableFailureEpoch *int64     `json:"unrecoverableFailureEpoch,omitempty"`
	ProveAtEpoch              *int64     `json:"proveAtEpoch,omitempty"`
	ChallengeWindow           *int64     `json:"challengeWindow,omitempty"`
	Reason                    string     `json:"reason"`
}

func (a *WebRPC) PDPProvingStatus(ctx context.Context) (PDPProvingStatus, error) {
	out := PDPProvingStatus{}

	net, err := a.NetSummary(ctx)
	if err != nil {
		return out, xerrors.Errorf("net summary: %w", err)
	}
	out.HeadEpoch = net.Epoch
	head := net.Epoch

	var rows []struct {
		ProveAtEpoch    sql.NullInt64 `db:"prove_at_epoch"`
		ChallengeWindow sql.NullInt64 `db:"challenge_window"`
	}
	err = a.Deps.DB.Select(ctx, &rows, `
		SELECT prove_at_epoch, challenge_window
		FROM pdp_data_sets
		WHERE unrecoverable_proving_failure_epoch IS NULL
		  AND prove_at_epoch IS NOT NULL`)
	if err != nil {
		return out, xerrors.Errorf("active datasets: %w", err)
	}

	out.ActiveDataSetCount = len(rows)
	var earliestDeadline *int64
	var earliestStart *int64

	for _, row := range rows {
		if !row.ProveAtEpoch.Valid {
			continue
		}
		start := row.ProveAtEpoch.Int64
		window := int64(0)
		if row.ChallengeWindow.Valid {
			window = row.ChallengeWindow.Int64
		}
		deadline := start + window

		if earliestStart == nil || start < *earliestStart {
			v := start
			earliestStart = &v
		}
		if earliestDeadline == nil || deadline < *earliestDeadline {
			v := deadline
			earliestDeadline = &v
		}

		if head >= start && head <= deadline {
			out.InWindowCount++
		} else if head > deadline {
			out.OverdueCount++
		}
	}

	out.NextProveAtEpoch = earliestStart
	out.NextDeadlineEpoch = earliestDeadline

	// Time until next proving session: soonest prove_at_epoch still in the future,
	// otherwise 0 if any dataset is currently in-window or overdue.
	if out.InWindowCount > 0 || out.OverdueCount > 0 {
		zero := int64(0)
		out.EpochsUntilNextSession = &zero
		out.SecondsUntilNextSession = &zero
	} else if earliestStart != nil {
		delta := *earliestStart - head
		if delta < 0 {
			delta = 0
		}
		out.EpochsUntilNextSession = &delta
		secs := delta * int64(build.BlockDelaySecs)
		out.SecondsUntilNextSession = &secs
	}

	return out, nil
}

func (a *WebRPC) PDPProvingTimeline24h(ctx context.Context) ([]PDPProvingTimelineEvent, error) {
	var events []PDPProvingTimelineEvent
	err := a.Deps.DB.Select(ctx, &events, `
		SELECT h.task_id, COALESCE(pt.data_set, 0) AS data_set, h.result, COALESCE(h.err, '') AS err, h.work_end
		FROM harmony_task_history h
		LEFT JOIN pdp_prove_tasks pt ON pt.task_id = h.task_id
		WHERE h.name IN ('PDPv0_Prove', 'PDPProve')
		  AND h.work_end > CURRENT_TIMESTAMP - INTERVAL '24 hours'
		ORDER BY h.work_end ASC`)
	if err != nil {
		return nil, xerrors.Errorf("proving timeline: %w", err)
	}
	if events == nil {
		events = []PDPProvingTimelineEvent{}
	}
	return events, nil
}

func (a *WebRPC) PDPProvingFailures(ctx context.Context) ([]PDPProvingFailure, error) {
	type histRow struct {
		TaskID    int64     `db:"task_id"`
		DataSetID int64     `db:"data_set"`
		Err       string    `db:"err"`
		WorkEnd   time.Time `db:"work_end"`
	}
	var failedTasks []histRow
	err := a.Deps.DB.Select(ctx, &failedTasks, `
		SELECT h.task_id, COALESCE(pt.data_set, 0) AS data_set, COALESCE(h.err, '') AS err, h.work_end
		FROM (
			SELECT task_id, BOOL_OR(result) AS any_ok, MAX(work_end) AS work_end
			FROM harmony_task_history
			WHERE name IN ('PDPv0_Prove', 'PDPProve')
			  AND work_end > CURRENT_TIMESTAMP - INTERVAL '7 days'
			GROUP BY task_id
			HAVING BOOL_OR(result) = FALSE
		) failed
		JOIN LATERAL (
			SELECT task_id, err, work_end
			FROM harmony_task_history
			WHERE task_id = failed.task_id AND result = FALSE
			ORDER BY work_end DESC
			LIMIT 1
		) h ON TRUE
		LEFT JOIN pdp_prove_tasks pt ON pt.task_id = h.task_id
		ORDER BY h.work_end DESC
		LIMIT 100`)
	if err != nil {
		return nil, xerrors.Errorf("failed prove tasks: %w", err)
	}

	type dsRow struct {
		ID                        int64         `db:"id"`
		ConsecutiveProveFailures  int           `db:"consecutive_prove_failures"`
		NextProveAttemptAt        sql.NullInt64 `db:"next_prove_attempt_at"`
		UnrecoverableFailureEpoch sql.NullInt64 `db:"unrecoverable_proving_failure_epoch"`
		ProveAtEpoch              sql.NullInt64 `db:"prove_at_epoch"`
		ChallengeWindow           sql.NullInt64 `db:"challenge_window"`
	}
	var unhealthy []dsRow
	err = a.Deps.DB.Select(ctx, &unhealthy, `
		SELECT id, consecutive_prove_failures, next_prove_attempt_at,
		       unrecoverable_proving_failure_epoch, prove_at_epoch, challenge_window
		FROM pdp_data_sets
		WHERE consecutive_prove_failures > 0
		   OR unrecoverable_proving_failure_epoch IS NOT NULL
		ORDER BY consecutive_prove_failures DESC, id ASC
		LIMIT 200`)
	if err != nil {
		return nil, xerrors.Errorf("unhealthy datasets: %w", err)
	}

	dsMeta := make(map[int64]dsRow, len(unhealthy))
	for _, ds := range unhealthy {
		dsMeta[ds.ID] = ds
	}

	seen := make(map[int64]struct{})
	out := make([]PDPProvingFailure, 0, len(failedTasks)+len(unhealthy))

	for _, ft := range failedTasks {
		f := PDPProvingFailure{
			DataSetID: ft.DataSetID,
			TaskID:    &ft.TaskID,
			Err:       ft.Err,
			WorkEnd:   &ft.WorkEnd,
			Reason:    "prove task failed",
		}
		if ds, ok := dsMeta[ft.DataSetID]; ok {
			f.ConsecutiveProveFailures = ds.ConsecutiveProveFailures
			f.NextProveAttemptAt = nullInt64Ptr(ds.NextProveAttemptAt)
			f.UnrecoverableFailureEpoch = nullInt64Ptr(ds.UnrecoverableFailureEpoch)
			f.ProveAtEpoch = nullInt64Ptr(ds.ProveAtEpoch)
			f.ChallengeWindow = nullInt64Ptr(ds.ChallengeWindow)
			if ds.UnrecoverableFailureEpoch.Valid {
				f.Reason = "unrecoverable proving failure"
			} else if ds.ConsecutiveProveFailures > 0 {
				f.Reason = "consecutive prove failures"
			}
			seen[ft.DataSetID] = struct{}{}
		}
		out = append(out, f)
	}

	for _, ds := range unhealthy {
		if _, ok := seen[ds.ID]; ok {
			continue
		}
		f := PDPProvingFailure{
			DataSetID:                 ds.ID,
			ConsecutiveProveFailures:  ds.ConsecutiveProveFailures,
			NextProveAttemptAt:        nullInt64Ptr(ds.NextProveAttemptAt),
			UnrecoverableFailureEpoch: nullInt64Ptr(ds.UnrecoverableFailureEpoch),
			ProveAtEpoch:              nullInt64Ptr(ds.ProveAtEpoch),
			ChallengeWindow:           nullInt64Ptr(ds.ChallengeWindow),
			Reason:                    "dataset needs attention",
		}
		if ds.UnrecoverableFailureEpoch.Valid {
			f.Reason = "unrecoverable proving failure"
		} else if ds.ConsecutiveProveFailures > 0 {
			f.Reason = "consecutive prove failures"
		}
		out = append(out, f)
	}

	return out, nil
}

func nullInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
