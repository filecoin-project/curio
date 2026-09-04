//go:build (linux || darwin) && cgo

// Package fs2 sums logical sizes of regular files in a single directory.
package fs2

// Result describes the regular files successfully included in a range sum.
type Result struct {
	Bytes    uint64
	Files    uint64
	Vanished uint64
}

// Supports:
//func SumFileSizesRange(directory, low, high string, queueDepth uint32) (Result, error)
