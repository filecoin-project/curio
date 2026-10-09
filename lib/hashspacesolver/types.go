// Package hashspacesolver plans range moves across disks so each disk holds
// at most eight contiguous hash-space ranges per space while moving as
// little data as possible.
//
// Disks are a list of total sizes (capacities) shared by all spaces. Each
// Space is an independent circular hash partition: ranges are half-open
// intervals (StartHash, EndHash] with Size bytes of data, and together they
// tile the circle once. SliceSize reports how much of a range's data lies in
// a sub-interval.
//
// Solve returns a target State plus an order-independent Diff of absolute
// interval transfers. Each byte moves at most once, from its original disk
// to the disk that holds it at the end.
package hashspacesolver

// MAX_RANGES_PER_DISK is the maximum number of contiguous ranges a disk may
// hold in one space. Caps are per disk per space.
const MAX_RANGES_PER_DISK = 8

// FILL_LIMIT_PERCENT is the rebalance target as a percent of physical
// capacity. Planning moves data to get disks to or under this fraction.
// A tight cluster may still sit above it after a best-effort plan; 100%
// remains the hard physical limit.
const FILL_LIMIT_PERCENT = 80

// SPREAD_POINTS is the fill-percentage gap that starts a balance. A disk at
// 70% and a disk at 20% differ by 50 and rebalance; a smaller gap does not.
const SPREAD_POINTS = 50

// CLAIM_STEP_PERCENT caps one EventClaim step: bytes moved between a pair of
// disks stay within this percent of the smaller disk's capacity.
const CLAIM_STEP_PERCENT = 10

// Range is the half-open hash interval (StartHash, EndHash] holding Size
// bytes. StartHash == EndHash is the full circle. Within a Space, each
// StartHash must equal the EndHash of the range before it.
type Range struct {
	StartHash []byte
	EndHash   []byte
	Size      int64
}

// Space is one independent hash circle assigned across disks.
type Space struct {
	Ranges []Range
	Owner  []int
}

// State is an assignment of ranges to disks across one or more spaces.
//
// MountpointCapacity[i] is disk i's shared capacity. Spaces are independent circles that
// share that capacity; Owner[j] within a space is the disk holding Ranges[j].
type State struct {
	MountpointCapacity []int64
	Spaces             []Space
	// Vacating marks disks that must not receive ranges. Existing ranges
	// stay until EventVacate moves them off.
	Vacating []bool
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
	// EventAbsorb moves overflow onto a disk that gained capacity. Bytes move
	// only from disks above FILL_LIMIT_PERCENT, and only until those disks
	// are back at the limit or the destination is at its own limit.
	EventAbsorb
	// EventBalance moves half the fill-percentage gap from a fuller disk
	// onto a disk at least SPREAD_POINTS behind it.
	EventBalance
	// EventClaim moves ranges onto a disk that already holds pieces from the
	// whole circle, one CLAIM_STEP_PERCENT step toward its capacity-weighted
	// share of all used bytes. Callers repeat it until it moves nothing.
	EventClaim
)

// Event asks the solver to react to one disk arriving, filling, or vacating.
// Disk is an index into State.MountpointCapacity.
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
