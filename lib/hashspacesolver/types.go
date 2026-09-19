// Package hashspacesolver plans range moves across disks so each disk holds
// fewer than four contiguous hash-space ranges per space while moving as
// little data as possible.
//
// Disks are a list of total sizes (capacities) shared by all spaces. Each
// Space is an independent circular hash partition: ranges are half-open
// intervals from the previous EndHash to their own, with Size bytes of data.
// SliceSize reports how much of a range's data lies in a sub-interval.
//
// Solve returns a target State plus an order-independent Diff of absolute
// interval transfers for delayed application.
package hashspacesolver

// MAX_RANGES_PER_DISK is the maximum number of contiguous ranges a disk may
// hold in one space (fewer than 4). Caps are per disk per space.
const MAX_RANGES_PER_DISK = 3

// FILL_LIMIT_PERCENT is the rebalance target as a percent of physical
// capacity. Planning moves data to get disks to or under this fraction.
// A tight cluster may still sit above it after a best-effort plan; 100%
// remains the hard physical limit.
const FILL_LIMIT_PERCENT = 80

// Range is one contiguous hash-space interval. It covers hashes after the
// previous range's EndHash up to EndHash, and holds Size bytes.
type Range struct {
	EndHash []byte
	Size    int64
}

// Space is one independent hash circle assigned across disks.
type Space struct {
	Ranges []Range
	Owner  []int
}

// State is an assignment of ranges to disks across one or more spaces.
//
// Disks[i] is disk i's shared capacity. Spaces are independent circles that
// share that capacity; Owner[j] within a space is the disk holding Ranges[j].
type State struct {
	Disks  []int64
	Spaces []Space
}

// EventKind selects which disk-lifecycle problem to solve.
type EventKind int

const (
	// EventArrive uses a newly added disk to take overflow off other disks.
	// Steals grow an existing dest range when possible; a new fragment is
	// opened only while dest is under MAX_RANGES_PER_DISK.
	EventArrive EventKind = iota + 1
	// EventFull sheds from a disk that is above FILL_LIMIT_PERCENT of
	// physical capacity.
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

// Transfer moves an absolute hash interval from one disk to another within
// one space. Intervals are half-open (StartHash, EndHash].
type Transfer struct {
	Space     int
	StartHash []byte
	EndHash   []byte
	From      int
	To        int
	Size      int64
}

// Result is the finished assignment plus an order-independent work list.
type Result struct {
	State      State
	Diff       []Transfer
	BytesMoved int64
}
