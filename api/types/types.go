package types

import "time"

type NodeInfo struct {
	ID            int
	CPU           int
	RAM           int64
	GPU           float64
	HostPort      string
	LastContact   time.Time
	Unschedulable bool

	Name        string
	StartupTime time.Time
	Tasks       string
	Layers      string
	Miners      string
}
