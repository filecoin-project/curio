// Package hashspacesolver plans range moves across disks so each disk holds
// fewer than four contiguous hash-space ranges while moving as little data
// as possible.
//
// Disks are a list of total sizes (capacities). Ranges are a circular
// partition of hash space: each range is the half-open interval from the
// previous range's EndHash to its own, with Size bytes of data. SliceSize
// reports how much of a range's data lies in a sub-interval.
package hashspacesolver

// MAX_RANGES_PER_DISK is the maximum number of contiguous hash-space ranges
// a disk may hold (fewer than 4).
const MAX_RANGES_PER_DISK = 3

// Range is one contiguous hash-space interval. It covers hashes after the
// previous range's EndHash up to EndHash, and holds Size bytes.
type Range struct {
	EndHash []byte
	Size    int64
}

// State is an assignment of ranges to disks.
//
// Disks[i] is disk i's total size (capacity). Owner[j] is the disk index
// holding Ranges[j]. Ranges are ordered by EndHash around the circle.
type State struct {
	Disks  []int64
	Ranges []Range
	Owner  []int
}

// EventKind selects which disk-lifecycle problem to solve.
type EventKind int

const (
	// EventArrive fills a newly added disk toward its capacity-weighted fair
	// share, stealing prefixes or suffixes of existing ranges.
	EventArrive EventKind = iota + 1
	// EventFull sheds the minimum data from an over-capacity disk.
	EventFull
	// EventVacate empties a disk so it can leave the cluster.
	EventVacate
)

// Event asks the solver to react to one disk arriving, filling, or vacating.
// Disk is an index into State.Disks.
type Event struct {
	Kind EventKind
	Disk int
}

// Move transfers a whole range, or a prefix/suffix slice of one, between disks.
// EndHash identifies the source range at the time of the move. Split is the
// EndHash of the moved slice; if nil or equal to EndHash, the whole range moves.
type Move struct {
	From    int
	To      int
	EndHash []byte
	Split   []byte
	Prefix  bool
	Size    int64
}

// Plan is a sequentially applicable list of range moves.
type Plan struct {
	Moves      []Move
	BytesMoved int64
}
