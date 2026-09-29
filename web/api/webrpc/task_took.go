package webrpc

import "time"

func taskTookAge(row clusterTaskSummaryLimitedRow, observedAt time.Time) (*int64, string) {
	if row.OwnerID == nil {
		return nil, "pending"
	}
	if row.AttemptStartSource.Valid && (row.AttemptStartSource.String == "claimed" || row.AttemptStartSource.String == "prepared") {
		return nil, "awaiting-start"
	}
	if !row.AttemptID.Valid || row.AttemptID.String == "" || !row.AttemptStartedAt.Valid || !row.AttemptStartSource.Valid || row.AttemptStartSource.String != "do_entry" {
		return nil, "unknown"
	}
	if row.AttemptStartedAt.Time.After(observedAt) {
		return nil, "future-start"
	}
	age := int64(observedAt.Sub(row.AttemptStartedAt.Time) / time.Second)
	return &age, "running"
}
