// Package hashspacesolver plans how hash ranges move when an empty mount
// connects, a disk fills, a mount's capacity increases, the cluster
// balances, a disk claims a share, or a disk must leave. Solve handles one
// Event. A returned plan is a success: it meets the rules below, and it
// meets each event's goal as far as a legal cut allows. Planning is
// greedy. It prefers less data moved, then fewer fragments, and does not
// search for a globally smallest move.
//
// A mount's capacity is its FreeSpace plus the hash-space bytes already on
// it, and that capacity stays fixed for the solve. Both hash spaces share
// it. A range is the half-open interval (StartHash, EndHash]. The file at
// StartHash belongs to the previous range, whose EndHash is that same hash.
// StartHash == EndHash is the whole circle except that one hash. Ranges in
// one space tile the circle once.
//
// # Rules
//
// These hold for every successful plan. Breaking one is an error, or the
// cut is not considered.
//
// Every active disk ends with free space at or above zero, and with at most
// MAX_RANGES_PER_DISK ranges in each space. A cut that would exceed either
// limit is illegal. Repair runs before every event except vacate, and
// again before Solve returns. A repair that cannot lower an over-cap
// count fails the event.
//
// A frozen disk accepts nothing. A mount marked Vacating starts frozen.
// Vacate freezes the named disk for the rest of the plan and leaves its
// capacity out of the room the other disks have. Shedding from a frozen
// disk is an error.
//
// An EmptyMountConnected, MountCapacityIncreased, or fill cut stays within
// the receiver's room under REBALANCE_TARGET_PERCENT. A steal is no larger
// than the source's excess over that target. Vacate, range repair, and
// freeing a fragment slot may
// fill a receiver until its free space is zero.
//
// Result.Diff names each moved byte once, from the disk that owned it at
// the start to the disk that holds it at the end. Adjacent transfers with
// the same endpoints are joined. Apply accepts the list in any order.
//
// # Goals
//
// EmptyMountConnected steals onto the new disk from disks above
// FILL_LIMIT_PERCENT,
// stopping when the new disk is at the rebalance target or no legal cut
// remains. Disks may stay above the fill line. The new disk need not reach
// the target.
//
// Fill leaves a disk at or under the fill line alone, aside from repair.
// A disk above the line sheds toward the rebalance target. Stopping with
// no legal cut is success, so the disk may remain above the target.
//
// Vacate must empty the disk. Empty ranges go to a neighbor and move no
// bytes. A circle of one empty range goes to any other active disk.
// Remaining ranges are placed largest first. Not enough capacity on the
// other disks, or a range that cannot be placed, fails the event.
//
// MountCapacityIncreased steals the same way EmptyMountConnected does,
// onto the named disk.
//
// Claim moves one step onto the named disk toward its capacity-weighted
// share of the bytes in use. Each source gives at most CLAIM_STEP_PERCENT
// of the smaller capacity, and only bytes above its own share. Callers
// repeat the event until it moves nothing. A frozen disk is left as it is.
//
// Balance moves half the widest fill-percentage gap onto the emptier disk
// when that gap is at least SPREAD_POINTS. A smaller gap moves nothing.
// A frozen disk is neither end. The named disk is ignored.
//
// # Priorities
//
// A cut is a whole range, a prefix, or a suffix. Landing against a neighbor
// already on the destination absorbs, so that disk's range count stays the
// same or drops. Any other cut opens a fragment.
//
// EmptyMountConnected and MountCapacityIncreased try absorbing cuts
// first, and open a fragment only when no absorbing cut remains. Among
// the cuts that fit, it prefers a larger cut,
// then more excess on the source, then a smaller range-count change, then
// the lower space, range, and cut kind.
//
// Fill prefers a cut that covers the bytes still above the target, and
// among those the smaller cut. A cut that falls short prefers the larger
// one. Ties prefer a smaller range-count change, then the lower
// destination, space, and range. When nothing can be shed, the plan first
// donates a range off some other disk already at the range cap, and
// otherwise moves one byte off the fullest other disk, then retries.
//
// Vacate places a range by the first of these that works: a whole move
// onto the disk that sits on both sides; a whole move with the smallest
// range-count change, then the most free space, then the lower disk index;
// a split across the two neighbors; a split onto any disk that has room,
// preferring a cut that adds no range and then a larger cut.
//
// Claim uses the same cut order as balance, and takes from the source
// furthest above its share first.
//
// Balance takes cuts from the fuller disk up to that byte budget. Among
// cuts that fit, it prefers one that adds no range, then a larger cut,
// then a smaller range-count change, then the lower space, range, and
// cut kind.
//
// Repair, used by every event, gives away the smallest range another disk
// can take whole. Ties prefer a smaller range-count change, then the lower
// disk index. When no whole move fits, it splits that smallest range.
package hashspacesolver
