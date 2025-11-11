package types

import (
	"database/sql"
	"time"
)

type NodeInfo struct {
	ID            int
	CPU           int
	RAM           int64
	GPU           float64
	HostPort      string
	LastContact   time.Time
	Unschedulable bool

	Name        sql.NullString // Can be NULL from harmony_machine_details
	StartupTime sql.NullTime   // Can be NULL from harmony_machine_details
	Tasks       sql.NullString // Can be NULL from harmony_machine_details
	Layers      sql.NullString // Can be NULL from harmony_machine_details
	Miners      sql.NullString // Can be NULL from harmony_machine_details
}
