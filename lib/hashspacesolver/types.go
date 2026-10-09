// Package hashspacesolver plans range moves across disks so each disk holds
// at most eight contiguous hash-space ranges per space while moving as
// little data as possible.
//
// MountPoints are disks, named by StorageID, whose capacities are shared by
// all spaces. Each hash space is an independent circular partition: ranges are half-open
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

// Range is the half-open hash interval (StartHash, EndHash] holding Size
// bytes on the mountpoint StorageID. StartHash == EndHash is the full circle.
// Within a hash space, each StartHash must equal the EndHash of the range
// before it.
type Range struct {
	StartHash []byte
	EndHash   []byte
	Size      int64
	StorageID string
}

// MountPoint is one disk's capacity, named by its storage path ID.
type MountPoint struct {
	Capacity  int64
	StorageID string
}

// State is an assignment of ranges to mountpoints across the hash spaces.
//
// Each HashSpaces entry is an independent circle; a nil entry is an unused
// space. All spaces share MountPoints capacity.
type State struct {
	MountPoints []MountPoint
	HashSpaces  [2][]Range
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

// Event asks the solver to react to the mountpoint StorageID arriving,
// filling, or vacating.
type Event struct {
	Kind      EventKind
	StorageID string
}

// Transfer moves an absolute hash interval from one mountpoint to another
// within one space. Intervals are half-open (StartHash, EndHash]. From and
// To are StorageIDs.
type Transfer struct {
	Space     int
	StartHash []byte
	EndHash   []byte
	From      string
	To        string
	Size      int64
}

// Result is the finished assignment plus an order-independent work list.
type Result struct {
	State      State
	Diff       []Transfer
	BytesMoved int64
}
