package hashspacesolver

// MAX_RANGES_PER_DISK is the maximum number of contiguous ranges a disk may
// hold in one space. Caps are per disk per space.
const MAX_RANGES_PER_DISK = 8

// FILL_LIMIT_PERCENT is the rebalance line as a percent of a mount's
// capacity, FreeSpace plus the hash-space bytes on it. Only a disk above
// this line gives up data. A tight cluster may still sit above it after a
// best-effort plan; zero FreeSpace is the hard limit.
const FILL_LIMIT_PERCENT = 90

// REBALANCE_TARGET_PERCENT is where a rebalance takes a disk above the line,
// and how full a receiving disk may get. The gap below FILL_LIMIT_PERCENT
// keeps a just-rebalanced disk from crossing the line on its next write.
const REBALANCE_TARGET_PERCENT = 80

// SPREAD_POINTS is the fill-percentage gap that starts a balance. A disk at
// 70% and a disk at 20% differ by 50 and rebalance; a smaller gap does not.
const SPREAD_POINTS = 50

// CLAIM_STEP_PERCENT caps one EventClaim step: bytes moved between a pair of
// disks stay within this percent of the smaller disk's capacity.
const CLAIM_STEP_PERCENT = 10

// Range is the half-open hash interval (StartHash, EndHash] holding Size
// bytes on the mountpoint StorageID. StartHash == EndHash is the full circle.
// Within a hash space, each StartHash must equal the EndHash of the range
// before it. Also lists other disks that own the same range. Problem lists
// owners of this range that a stuck move should give up. The circle still
// has one range per end hash; Also is not a second range.
type Range struct {
	StartHash []byte
	EndHash   []byte
	Size      int64
	StorageID string
	Also      []string
	Problem   []string
}

// MountPoint is one disk, named by its storage path ID. FreeSpace is the
// bytes free on the drive now, not counting the hash-space ranges already
// stored there.
type MountPoint struct {
	FreeSpace int64
	StorageID string
	// Vacating marks a disk that must not receive ranges. Existing ranges
	// stay until EventVacate moves them off.
	Vacating bool
	// Misplaced marks a disk that holds piece files outside its ranges, such as
	// pieces migrated from piece-park. It is not a stuck hash-space range.
	Misplaced bool
}

// State is an assignment of ranges to mountpoints across the hash spaces.
//
// Each HashSpaces entry is an independent circle; a nil entry is an unused
// space. All spaces draw on the same mount free space.
type State struct {
	MountPoints []MountPoint
	HashSpaces  [2][]Range
}

// EventKind selects which disk-lifecycle problem to solve.
type EventKind int

const (
	// EventEmptyMountConnected uses a newly added disk to take overflow off
	// other disks.
	// Steals grow an existing dest range when possible; a new fragment is
	// opened only while dest is under MAX_RANGES_PER_DISK.
	EventEmptyMountConnected EventKind = iota + 1
	// EventFull sheds from a disk that is above FILL_LIMIT_PERCENT of its
	// capacity, down to REBALANCE_TARGET_PERCENT.
	EventFull
	// EventVacate empties a disk so it can leave the cluster.
	EventVacate
	// EventMountCapacityIncreased moves overflow onto a disk that gained
	// capacity. Bytes move only from disks above FILL_LIMIT_PERCENT, and
	// only until the destination
	// is at REBALANCE_TARGET_PERCENT or no legal cut remains.
	EventMountCapacityIncreased
	// EventBalance moves half the fill-percentage gap from a fuller disk
	// onto a disk at least SPREAD_POINTS behind it.
	EventBalance
	// EventClaim moves ranges onto a disk that already holds pieces from the
	// whole circle, one CLAIM_STEP_PERCENT step toward its capacity-weighted
	// share of all used bytes. Callers repeat it until it moves nothing.
	EventClaim
)

// Event asks the solver to react to the mountpoint StorageID connecting
// empty, filling, vacating, gaining capacity, or claiming. EventBalance
// ignores StorageID.
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
