package webrpc

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTaskTookCurrentAttemptOnly(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	owner := int64(7)
	base := clusterTaskSummaryLimitedRow{ID: 1, Name: "task", State: "running", OwnerID: &owner, PostedTime: now.Add(-3 * time.Hour), WorkStart: sql.NullTime{Time: now.Add(-2 * time.Hour), Valid: true}, WorkStartSource: sql.NullString{String: "claim", Valid: true}, AttemptID: sql.NullString{String: "new-attempt", Valid: true}, AttemptStartedAt: sql.NullTime{Time: now.Add(-10 * time.Minute), Valid: true}, AttemptStartSource: sql.NullString{String: "do_entry", Valid: true}}
	for _, tc := range []struct {
		name    string
		change  func(*clusterTaskSummaryLimitedRow)
		state   string
		seconds int64
		known   bool
	}{
		{"old posted fresh attempt", func(*clusterTaskSummaryLimitedRow) {}, "running", 600, true},
		{"pending", func(r *clusterTaskSummaryLimitedRow) { r.OwnerID = nil; r.State = "pending" }, "pending", 0, false},
		{"claimed before Do", func(r *clusterTaskSummaryLimitedRow) {
			r.AttemptStartedAt = sql.NullTime{}
			r.AttemptStartSource = sql.NullString{String: "claimed", Valid: true}
		}, "awaiting-start", 0, false},
		{"prepared not started", func(r *clusterTaskSummaryLimitedRow) {
			r.AttemptStartedAt = sql.NullTime{}
			r.AttemptStartSource.String = "prepared"
		}, "awaiting-start", 0, false},
		{"missing start", func(r *clusterTaskSummaryLimitedRow) { r.AttemptStartedAt = sql.NullTime{} }, "unknown", 0, false},
		{"legacy backfill", func(r *clusterTaskSummaryLimitedRow) { r.AttemptStartSource = sql.NullString{} }, "unknown", 0, false},
		{"migration provenance", func(r *clusterTaskSummaryLimitedRow) { r.AttemptStartSource.String = "migration" }, "unknown", 0, false},
		{"missing identity", func(r *clusterTaskSummaryLimitedRow) { r.AttemptID = sql.NullString{} }, "unknown", 0, false},
		{"future clock", func(r *clusterTaskSummaryLimitedRow) { r.AttemptStartedAt.Time = now.Add(time.Minute) }, "future-start", 0, false},
		{"same owner recovered", func(r *clusterTaskSummaryLimitedRow) {
			r.AttemptID.String = "restart"
			r.AttemptStartedAt.Time = now.Add(-time.Second)
		}, "running", 1, true},
		{"owner changed", func(r *clusterTaskSummaryLimitedRow) {
			other := int64(8)
			r.OwnerID = &other
			r.AttemptID.String = "new-owner"
			r.AttemptStartedAt.Time = now.Add(-2 * time.Second)
		}, "running", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := base
			tc.change(&row)
			got := buildLimitedTaskSummary(row, now, nil)
			if got.TookState != tc.state || (got.TookSeconds != nil) != tc.known {
				t.Fatalf("got=%+v", got)
			}
			if tc.known && *got.TookSeconds != tc.seconds {
				t.Fatalf("seconds=%d", *got.TookSeconds)
			}
		})
	}
	encoded, err := json.Marshal(buildLimitedTaskSummary(base, now, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"AgeSeconds":7200`, `"TookSeconds":600`, `"AttemptID":"new-attempt"`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("compatibility field %s missing from %s", field, encoded)
		}
	}
}
