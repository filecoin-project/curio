package webrpc

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOwnershipAgeProvenance(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		start  sql.NullTime
		source sql.NullString
		known  bool
	}{
		{"known claim", sql.NullTime{Time: now.Add(-10 * time.Minute), Valid: true}, sql.NullString{String: "claim", Valid: true}, true},
		{"missing", sql.NullTime{}, sql.NullString{}, false},
		{"legacy backfill", sql.NullTime{Time: now.Add(-time.Hour), Valid: true}, sql.NullString{}, false},
		{"unrecognized provenance", sql.NullTime{Time: now, Valid: true}, sql.NullString{String: "migration", Valid: true}, false},
		{"future claim", sql.NullTime{Time: now.Add(time.Minute), Valid: true}, sql.NullString{String: "claim", Valid: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			age := knownTaskAge(now, tc.start, tc.source)
			if (age != nil) != tc.known {
				t.Fatalf("age=%v, known=%v", age, tc.known)
			}
			if tc.known && *age != 600 {
				t.Fatalf("age=%d", *age)
			}
		})
	}
}

func TestTaskSummaryLegacyFieldsRemain(t *testing.T) {
	data, err := json.Marshal(TaskSummary{SincePosted: time.Unix(10, 0), SincePostedStr: "2m0s"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"SincePosted":`, `"SincePostedStr":"2m0s"`, `"OwnershipAgeSeconds":null`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("missing legacy/additive field %s in %s", field, data)
		}
	}
}
