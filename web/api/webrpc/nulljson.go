package webrpc

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"time"
)

// NullString preserves the convenience of sql.NullString for scanning but
// marshals to JSON as a plain string, returning "" when the value is invalid.
type NullString sql.NullString

func (ns *NullString) Scan(value any) error {
	return (*sql.NullString)(ns).Scan(value)
}

func (ns NullString) Value() (driver.Value, error) {
	return (sql.NullString)(ns).Value()
}

func (ns NullString) MarshalJSON() ([]byte, error) {
	if ns.Valid {
		return json.Marshal(ns.String)
	}
	return json.Marshal("")
}

// NullInt64 mirrors sql.NullInt64 but marshals to JSON as an int64 with 0 as
// the zero-value when the DB value is NULL.
type NullInt64 sql.NullInt64

func (ni *NullInt64) Scan(value any) error {
	return (*sql.NullInt64)(ni).Scan(value)
}

func (ni NullInt64) Value() (driver.Value, error) {
	return (sql.NullInt64)(ni).Value()
}

func (ni NullInt64) MarshalJSON() ([]byte, error) {
	if ni.Valid {
		return json.Marshal(ni.Int64)
	}
	return []byte("0"), nil
}

// NullBool mirrors sql.NullBool but marshals to JSON as a bool with false as
// the zero-value when the DB value is NULL.
type NullBool sql.NullBool

func (nb *NullBool) Scan(value any) error {
	return (*sql.NullBool)(nb).Scan(value)
}

func (nb NullBool) Value() (driver.Value, error) {
	return (sql.NullBool)(nb).Value()
}

func (nb NullBool) MarshalJSON() ([]byte, error) {
	if nb.Valid && nb.Bool {
		return []byte("true"), nil
	}
	return []byte("false"), nil
}

// NullTime mirrors sql.NullTime but marshals to JSON as a time.Time, falling
// back to the zero time when the DB value is NULL.
type NullTime sql.NullTime

func (nt *NullTime) Scan(value any) error {
	return (*sql.NullTime)(nt).Scan(value)
}

func (nt NullTime) Value() (driver.Value, error) {
	return (sql.NullTime)(nt).Value()
}

func (nt NullTime) MarshalJSON() ([]byte, error) {
	if nt.Valid {
		return json.Marshal(nt.Time)
	}
	return json.Marshal(time.Time{})
}
