package webrpc

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	// SessionState: "in-window", "upcoming", "overdue", or "idle"
	SessionState string `json:"sessionState"`
}

func (a *WebRPC) PDPProvingStatus(ctx context.Context) (PDPProvingStatus, error) {
	out := PDPProvingStatus{SessionState: "idle"}

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

	// Track the soonest actionable window — never the oldest overdue prove_at.
	var soonestInWindowStart *int64
	var soonestInWindowDeadline *int64
	var soonestFutureStart *int64
	var soonestFutureDeadline *int64

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

		switch {
		case head >= start && head <= deadline:
			out.InWindowCount++
			if soonestInWindowStart == nil || start < *soonestInWindowStart {
				v := start
				soonestInWindowStart = &v
			}
			if soonestInWindowDeadline == nil || deadline < *soonestInWindowDeadline {
				v := deadline
				soonestInWindowDeadline = &v
			}
		case head > deadline:
			out.OverdueCount++
		default: // start > head — upcoming window
			if soonestFutureStart == nil || start < *soonestFutureStart {
				v := start
				soonestFutureStart = &v
				d := deadline
				soonestFutureDeadline = &d
			}
		}
	}

	zero := int64(0)
	switch {
	case out.InWindowCount > 0:
		out.SessionState = "in-window"
		out.NextProveAtEpoch = soonestInWindowStart
		out.NextDeadlineEpoch = soonestInWindowDeadline
		out.EpochsUntilNextSession = &zero
		out.SecondsUntilNextSession = &zero
	case soonestFutureStart != nil:
		out.SessionState = "upcoming"
		out.NextProveAtEpoch = soonestFutureStart
		out.NextDeadlineEpoch = soonestFutureDeadline
		delta := *soonestFutureStart - head
		if delta < 0 {
			delta = 0
		}
		out.EpochsUntilNextSession = &delta
		secs := delta * int64(build.BlockDelaySecs)
		out.SecondsUntilNextSession = &secs
	case out.OverdueCount > 0:
		// No open or upcoming windows — only stale prove_at values remain.
		out.SessionState = "overdue"
		out.EpochsUntilNextSession = &zero
		out.SecondsUntilNextSession = &zero
	}

	return out, nil
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

func (a *WebRPC) PDPProvingTimeline24h(ctx context.Context) ([]PDPProvingTimelineEvent, error) {
	var events []PDPProvingTimelineEvent
	// pdp_prove_tasks only exists while the task is in-flight (CASCADE on completion).
	// For completed tasks, recover data_set from the error text when present.
	err := a.Deps.DB.Select(ctx, &events, `
		SELECT h.task_id, COALESCE(pt.data_set, 0) AS data_set,
		       (h.result AND (h.err IS NULL OR h.err = '')) AS result,
		       COALESCE(h.err, '') AS err, h.work_end
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
	for i := range events {
		events[i].Err = cleanHistoryErr(events[i].Err)
		if events[i].DataSetID == 0 {
			if dsID, ok := parseDataSetIDFromErr(events[i].Err); ok {
				events[i].DataSetID = dsID
			}
		}
	}
	return events, nil
}

func (a *WebRPC) PDPProvingFailures(ctx context.Context) ([]PDPProvingFailure, error) {
	headEpoch := int64(0)
	if net, err := a.NetSummary(ctx); err == nil {
		headEpoch = net.Epoch
	}
	headTime := time.Now()
	blockDelay := time.Duration(build.BlockDelaySecs) * time.Second

	type histHit struct {
		TaskID  int64
		Err     string
		WorkEnd time.Time
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
	err := a.Deps.DB.Select(ctx, &unhealthy, `
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

	// Recent prove history with errors. Dataset ID is recovered from err text
	// (Do() wraps failures as "... dataset <id>: ...") — no need to retain pdp_prove_tasks.
	type histRow struct {
		TaskID    int64     `db:"task_id"`
		DataSetID int64     `db:"data_set"`
		Err       string    `db:"err"`
		WorkEnd   time.Time `db:"work_end"`
	}
	var recentErrs []histRow
	err = a.Deps.DB.Select(ctx, &recentErrs, `
		SELECT h.task_id, COALESCE(pt.data_set, 0) AS data_set, COALESCE(h.err, '') AS err, h.work_end
		FROM harmony_task_history h
		LEFT JOIN pdp_prove_tasks pt ON pt.task_id = h.task_id
		WHERE h.name IN ('PDPv0_Prove', 'PDPProve')
		  AND h.work_end > CURRENT_TIMESTAMP - INTERVAL '7 days'
		  AND h.err IS NOT NULL AND h.err <> ''
		ORDER BY h.work_end DESC
		LIMIT 500`)
	if err != nil {
		return nil, xerrors.Errorf("recent prove errors: %w", err)
	}

	lastByDataSet := make(map[int64]histHit, len(unhealthy))
	var orphanFailed []histRow
	for _, row := range recentErrs {
		dsID := row.DataSetID
		if dsID == 0 {
			if parsed, ok := parseDataSetIDFromErr(row.Err); ok {
				dsID = parsed
			}
		}
		cleaned := cleanHistoryErr(row.Err)
		if dsID == 0 {
			orphanFailed = append(orphanFailed, histRow{
				TaskID:  row.TaskID,
				Err:     cleaned,
				WorkEnd: row.WorkEnd,
			})
			continue
		}
		if _, ok := lastByDataSet[dsID]; ok {
			continue // already have newer hit
		}
		lastByDataSet[dsID] = histHit{
			TaskID:  row.TaskID,
			Err:     cleaned,
			WorkEnd: row.WorkEnd,
		}
	}

	seen := make(map[int64]struct{}, len(unhealthy))
	out := make([]PDPProvingFailure, 0, len(unhealthy)+len(orphanFailed))

	for _, ds := range unhealthy {
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

		if last, ok := lastByDataSet[ds.ID]; ok {
			tid := last.TaskID
			f.TaskID = &tid
			f.Err = last.Err
			we := last.WorkEnd
			f.WorkEnd = &we
		}

		if f.Err == "" {
			f.Err = synthesizeProvingFailureErr(f)
		}
		if f.WorkEnd == nil && ds.UnrecoverableFailureEpoch.Valid && headEpoch > 0 {
			we := epochToApproxTime(headTime, headEpoch, ds.UnrecoverableFailureEpoch.Int64, blockDelay)
			f.WorkEnd = &we
		}

		seen[ds.ID] = struct{}{}
		out = append(out, f)
	}

	// True harmony failures that could not be tied to an unhealthy dataset row.
	for _, ft := range orphanFailed {
		f := PDPProvingFailure{
			TaskID:  &ft.TaskID,
			Err:     ft.Err,
			WorkEnd: &ft.WorkEnd,
			Reason:  "prove task failed",
		}
		if f.Err == "" {
			f.Err = "prove task failed"
		}
		out = append(out, f)
	}

	sort.SliceStable(out, func(i, j int) bool {
		wi, wj := out[i].WorkEnd, out[j].WorkEnd
		switch {
		case wi != nil && wj != nil:
			return wi.After(*wj)
		case wi != nil:
			return true
		case wj != nil:
			return false
		default:
			return out[i].ConsecutiveProveFailures > out[j].ConsecutiveProveFailures
		}
	})

	return out, nil
}

var dataSetIDFromErrRe = regexp.MustCompile(`(?i)dataset\s+(\d+)`)

func parseDataSetIDFromErr(err string) (int64, bool) {
	m := dataSetIDFromErrRe.FindStringSubmatch(err)
	if len(m) < 2 {
		return 0, false
	}
	id, convErr := strconv.ParseInt(m[1], 10, 64)
	if convErr != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func epochToApproxTime(headTime time.Time, headEpoch, epoch int64, blockDelay time.Duration) time.Time {
	delta := headEpoch - epoch
	if delta < 0 {
		delta = 0
	}
	return headTime.Add(-time.Duration(delta) * blockDelay)
}

func cleanHistoryErr(err string) string {
	err = strings.TrimSpace(err)
	for _, prefix := range []string{"non-failing error: ", "error: "} {
		if strings.HasPrefix(err, prefix) {
			return strings.TrimPrefix(err, prefix)
		}
	}
	return err
}

func synthesizeProvingFailureErr(f PDPProvingFailure) string {
	switch {
	case f.UnrecoverableFailureEpoch != nil:
		return fmt.Sprintf("unrecoverable proving failure at epoch %d", *f.UnrecoverableFailureEpoch)
	case f.ConsecutiveProveFailures > 0 && f.NextProveAttemptAt != nil:
		return fmt.Sprintf("%d consecutive prove failure(s); backoff until epoch %d", f.ConsecutiveProveFailures, *f.NextProveAttemptAt)
	case f.ConsecutiveProveFailures > 0:
		return fmt.Sprintf("%d consecutive prove failure(s)", f.ConsecutiveProveFailures)
	default:
		return "dataset needs proving attention"
	}
}

func nullInt64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
