package webrpc

import (
	"database/sql"
	"time"
)

// knownTaskAge never substitutes queue age for missing or future timestamps.
func knownTaskAge(observedAt time.Time, start sql.NullTime, source sql.NullString) *int64 {
	if !start.Valid || !source.Valid || source.String != "claim" || start.Time.After(observedAt) {
		return nil
	}
	age := int64(observedAt.Sub(start.Time) / time.Second)
	return &age
}
