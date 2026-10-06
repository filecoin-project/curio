package hashspacesolver

import (
	"bytes"
	"slices"

	"golang.org/x/xerrors"
)

// cutKind is how a cut takes bytes from a range.
type cutKind byte

const (
	cutWhole cutKind = iota
	cutPrefix
	cutSuffix
)

// recommendedLayout is one piece of the layout the solver is building.
// The pieces under a range cover that range in order, so they are the
// partial result for that range. The full result is every range.
//
// (startHash, endHash] is the piece. mountPointIdx is the mount point that owned
// the bytes when the solve started, and cuts do not change it, so the diff
// can name that disk after the piece has moved. sourceRange is the input
// range the piece was cut from. Byte counts come from sourceRange, so every
// cut order yields the same bytes per hash.
type recommendedLayout struct {
	startHash     []byte
	endHash       []byte
	size          int64
	mountPointIdx int
	sourceRange   Range
}

// sizeIn returns how many of s's bytes lie in (from, to]. Both bounds must be
// on s.
func (s recommendedLayout) sizeIn(from, to []byte) int64 {
	return sliceSize(s.sourceRange, from, to)
}

// piece returns the part of s covering (start, end] with size bytes.
// start and end must lie on s, and size must be that window's byte count.
// Neither is checked.
func (s recommendedLayout) piece(start, end []byte, size int64) recommendedLayout {
	return recommendedLayout{
		startHash:     cloneHash(start),
		endHash:       cloneHash(end),
		size:          size,
		mountPointIdx: s.mountPointIdx,
		sourceRange:   cloneRange(s.sourceRange),
	}
}

// mountPoint is one disk. FreeSpace and StorageID come from State.
//
// capacity is FreeSpace plus the hash-space bytes on the disk at the start.
// It stays fixed for the solve; the drive's own size is never used. used is
// the bytes currently placed on the disk, summed across every hash space,
// so capacity - used is the free space left. frozen is set when the disk is
// vacating: it cannot accept ranges, and its capacity is left out of the
// room available to the rest of the cluster.
type mountPoint struct {
	MountPoint
	capacity int64
	used     int64
	frozen   bool
}

// hashSpace is one HashSpaces circle.
//
// ranges is the current tiling, sorted by EndHash. Each range names its
// disk with StorageID. Adjacent ranges on the same disk are merged, so a
// disk's range count is the number of separate ranges it holds. layouts is
// parallel to ranges: layouts[i] is the partial resulting layout of
// ranges[i], the pieces placed there. A piece keeps the mount point it
// started on, which is what the diff reports after the range moves.
type hashSpace struct {
	ranges  []Range
	layouts [][]recommendedLayout
}

// world is the solver's working copy of a State. mountPoints follows
// MountPoints order. mountOf maps a StorageID to that index. hashSpaces is
// one circle per HashSpaces entry, and that index is Transfer.Space.
type world struct {
	mountPoints []mountPoint
	mountOf     map[string]int
	hashSpaces  []hashSpace
}

// newWorld copies state into a working world. It rejects an empty or
// duplicate StorageID, negative FreeSpace, a bad range, mixed hash lengths
// or a duplicate EndHash in one space, an unknown StorageID, or a circle
// that does not tile. Ranges are sorted by EndHash before layouts are
// attached, and adjacent same-disk ranges are merged. Each mount's capacity
// is its FreeSpace plus the hash-space bytes already on it.
func newWorld(state State) (*world, error) {
	ids := make(map[string]struct{}, len(state.MountPoints))
	for i, mp := range state.MountPoints {
		if mp.StorageID == "" {
			return nil, xerrors.Errorf("mountpoint %d has empty StorageID", i)
		}
		if _, dup := ids[mp.StorageID]; dup {
			return nil, xerrors.Errorf("duplicate mountpoint StorageID %s", mp.StorageID)
		}
		ids[mp.StorageID] = struct{}{}
		if mp.FreeSpace < 0 {
			return nil, xerrors.Errorf("mountpoint %s has negative free space", mp.StorageID)
		}
	}
	for s, ranges := range state.HashSpaces {
		var hlen int
		seen := make(map[string]struct{}, len(ranges))
		for i, r := range ranges {
			if err := checkRange(r); err != nil {
				return nil, xerrors.Errorf("space %d range %d: %w", s, i, err)
			}
			if hlen == 0 {
				hlen = len(r.EndHash)
			} else if len(r.EndHash) != hlen {
				return nil, xerrors.Errorf("space %d range %d EndHash length %d != %d", s, i, len(r.EndHash), hlen)
			}
			key := string(r.EndHash)
			if _, dup := seen[key]; dup {
				return nil, xerrors.Errorf("space %d: duplicate EndHash at range %d", s, i)
			}
			seen[key] = struct{}{}
			if _, known := ids[r.StorageID]; !known {
				return nil, xerrors.Errorf("space %d range %d: unknown StorageID %q", s, i, r.StorageID)
			}
		}
		if err := checkTiling(ranges); err != nil {
			return nil, xerrors.Errorf("space %d: %w", s, err)
		}
	}
	n := len(state.MountPoints)
	w := &world{
		mountPoints: make([]mountPoint, n),
		mountOf:     make(map[string]int, n),
		hashSpaces:  make([]hashSpace, len(state.HashSpaces)),
	}
	for i, mp := range state.MountPoints {
		w.mountPoints[i].MountPoint = mp
		w.mountPoints[i].frozen = mp.Vacating
		w.mountOf[mp.StorageID] = i
	}
	for s, ranges := range state.HashSpaces {
		copied := make([]Range, len(ranges))
		for i, r := range ranges {
			copied[i] = cloneRange(r)
		}
		w.hashSpaces[s] = hashSpace{ranges: copied}
		slices.SortFunc(w.hashSpaces[s].ranges, func(a, b Range) int {
			return bytes.Compare(a.EndHash, b.EndHash)
		})
		w.hashSpaces[s].layouts = make([][]recommendedLayout, len(w.hashSpaces[s].ranges))
		for i, r := range w.hashSpaces[s].ranges {
			w.hashSpaces[s].layouts[i] = []recommendedLayout{{
				startHash:     cloneHash(r.StartHash),
				endHash:       cloneHash(r.EndHash),
				size:          r.Size,
				mountPointIdx: w.mountOf[r.StorageID],
				sourceRange:   cloneRange(r),
			}}
		}
		w.mergeSpace(s)
		for _, r := range w.hashSpaces[s].ranges {
			w.mountPoints[w.mountOf[r.StorageID]].used += r.Size
		}
	}
	for i := range w.mountPoints {
		w.mountPoints[i].capacity = w.mountPoints[i].FreeSpace + w.mountPoints[i].used
	}
	return w, nil
}

// cloneRange copies r and its hashes.
func cloneRange(r Range) Range {
	return Range{StartHash: cloneHash(r.StartHash), EndHash: cloneHash(r.EndHash), Size: r.Size, StorageID: r.StorageID}
}

// diskOf is the mountPoints index of r.StorageID. StorageID must already
// be in the world's mount map; an unknown id yields the zero index.
func (w *world) diskOf(r Range) int {
	return w.mountOf[r.StorageID]
}

// snapshot exports the current tiling. Each mount's FreeSpace is its
// capacity minus the bytes placed on it. The ranges must still tile each
// circle; that is not re-checked.
func (w *world) snapshot() State {
	st := State{MountPoints: make([]MountPoint, len(w.mountPoints))}
	for i, mp := range w.mountPoints {
		st.MountPoints[i] = MountPoint{FreeSpace: w.free(i), StorageID: mp.StorageID, Vacating: mp.Vacating}
	}
	for s, sp := range w.hashSpaces {
		if len(sp.ranges) == 0 {
			continue
		}
		ranges := make([]Range, len(sp.ranges))
		for i, r := range sp.ranges {
			ranges[i] = cloneRange(r)
		}
		st.HashSpaces[s] = ranges
	}
	return st
}

// startHash is a copy of ranges[i].StartHash. space and i must index a range.
func (w *world) startHash(space, i int) []byte {
	return cloneHash(w.hashSpaces[space].ranges[i].StartHash)
}

// free is capacity minus the bytes currently placed on d. d must be a mount index.
func (w *world) free(d int) int64 {
	return w.mountPoints[d].capacity - w.mountPoints[d].used
}

// percentOf is cap*pct/100. A non-positive cap yields 0.
func percentOf(cap, pct int64) int64 {
	if cap <= 0 {
		return 0
	}
	return cap * pct / 100
}

// fillLimitOf is FILL_LIMIT_PERCENT of cap, the line that triggers a rebalance.
func fillLimitOf(cap int64) int64 {
	return percentOf(cap, FILL_LIMIT_PERCENT)
}

// rebalanceTargetOf is REBALANCE_TARGET_PERCENT of cap, the level a rebalance aims for.
func rebalanceTargetOf(cap int64) int64 {
	return percentOf(cap, REBALANCE_TARGET_PERCENT)
}

// rebalanceTarget is rebalanceTargetOf for mount d. d must be a mount index.
func (w *world) rebalanceTarget(d int) int64 {
	return rebalanceTargetOf(w.mountPoints[d].capacity)
}

// targetHeadroom is how much more d may receive and stay at or under its
// rebalance target. d must be a mount index. A disk already at or over the
// target yields 0.
func (w *world) targetHeadroom(d int) int64 {
	n := w.rebalanceTarget(d) - w.mountPoints[d].used
	if n < 0 {
		return 0
	}
	return n
}

// overflow is how far d is above its fill limit, the line that triggers a
// rebalance. d must be a mount index. A disk at or under the line yields 0.
func (w *world) overflow(d int) int64 {
	n := w.mountPoints[d].used - fillLimitOf(w.mountPoints[d].capacity)
	if n < 0 {
		return 0
	}
	return n
}

// excess is how much d must give up to get down to its rebalance target.
// d must be a mount index. A disk already at or under the target yields 0.
func (w *world) excess(d int) int64 {
	n := w.mountPoints[d].used - w.rebalanceTarget(d)
	if n < 0 {
		return 0
	}
	return n
}

// ownsRange reports whether disk holds any range in any space.
// disk must be a mount index.
func (w *world) ownsRange(disk int) bool {
	for s := range w.hashSpaces {
		if w.rangeCount(s, disk) > 0 {
			return true
		}
	}
	return false
}

// rangeCount is how many ranges in space sit on d.
// space must index a hash space and d a mount.
func (w *world) rangeCount(space, d int) int {
	n := 0
	for _, r := range w.hashSpaces[space].ranges {
		if w.diskOf(r) == d {
			n++
		}
	}
	return n
}

// rangeIndexes lists the range indexes in space owned by d, in circle order.
// space must index a hash space and d a mount.
func (w *world) rangeIndexes(space, d int) []int {
	var out []int
	for i, r := range w.hashSpaces[space].ranges {
		if w.diskOf(r) == d {
			out = append(out, i)
		}
	}
	return out
}

// activeDisks lists mount indexes that are not frozen. A frozen disk is
// vacating: it is left out of placement and out of capacity checks.
func (w *world) activeDisks() []int {
	out := make([]int, 0, len(w.mountPoints))
	for i := range w.mountPoints {
		if !w.mountPoints[i].frozen {
			out = append(out, i)
		}
	}
	return out
}

// destDelta is how dest's range count in space changes if this cut lands:
// +1 when it opens a fragment, 0 when it merges with one neighbor, -1 when
// it merges with both. A single-range whole move returns 1 minus dest's
// current count, because that move replaces the circle. space and idx must
// index a range, kind is cutWhole, cutPrefix, or cutSuffix, and dest is a
// mount index. The world is not modified.
func (w *world) destDelta(space, idx int, kind cutKind, dest int) int {
	sp := &w.hashSpaces[space]
	n := len(sp.ranges)
	if n == 0 {
		return 1
	}
	if n == 1 && kind == cutWhole {
		return 1 - w.rangeCount(space, dest)
	}
	prev := (idx - 1 + n) % n
	next := (idx + 1) % n
	left := n > 1 && w.diskOf(sp.ranges[prev]) == dest && kind != cutSuffix
	right := n > 1 && w.diskOf(sp.ranges[next]) == dest && kind != cutPrefix
	switch {
	case left && right:
		return -1
	case left || right:
		return 0
	default:
		return 1
	}
}

// canTake reports whether cutting ranges[idx] onto dest is legal. dest must
// be a different mount from the range's owner, in range, and not frozen.
// size must fit in dest's free space, and dest's range count after the cut
// must stay within MAX_RANGES_PER_DISK. space and idx must index a range,
// and kind is a cut kind.
func (w *world) canTake(space, idx int, kind cutKind, dest int, size int64) bool {
	if dest == w.diskOf(w.hashSpaces[space].ranges[idx]) {
		return false
	}
	if dest < 0 || dest >= len(w.mountPoints) || w.mountPoints[dest].frozen {
		return false
	}
	if size > w.free(dest) {
		return false
	}
	return w.rangeCount(space, dest)+w.destDelta(space, idx, kind, dest) <= MAX_RANGES_PER_DISK
}

// applyCut moves size bytes of ranges[idx] onto dest and merges neighbors
// that then share a disk. cutWhole, or a size that covers the range, moves
// it all, including a size-0 range. A prefix or suffix uses split as the
// hash boundary when split is non-empty; an empty split asks previewCut
// for one. Returns false and leaves the world unchanged when dest already
// owns the range, the partial size is not strictly inside it, the layout
// cannot be split there, or dest lacks free space. The range cap is not
// checked; callers use canTake first. space, idx, and dest must be in range.
func (w *world) applyCut(space, idx int, kind cutKind, dest int, size int64, split []byte) bool {
	sp := &w.hashSpaces[space]
	r := sp.ranges[idx]
	from := w.diskOf(r)
	if from == dest {
		return false
	}
	// A whole move only changes the owner, so an empty range moves too.
	if kind == cutWhole || size >= r.Size {
		w.moveWhole(space, idx, dest)
		return true
	}
	if size <= 0 {
		return false
	}
	start := w.startHash(space, idx)
	var head, tail []recommendedLayout
	var moved int64
	boundary := split
	if len(split) > 0 {
		var ok bool
		if kind == cutPrefix {
			head, tail, ok = splitLayoutsAt(sp.layouts[idx], start, split, size)
		} else {
			head, tail, ok = splitLayoutsAt(sp.layouts[idx], start, split, r.Size-size)
		}
		if !ok {
			return false
		}
		moved = size
	} else {
		boundary, moved, head, tail = w.previewCut(space, idx, kind, size)
		if moved <= 0 || moved >= r.Size {
			return false
		}
	}
	if moved <= 0 || moved >= r.Size || moved > w.free(dest) {
		return false
	}
	if kind == cutPrefix {
		sp.ranges[idx].StartHash = cloneHash(boundary)
		sp.ranges[idx].Size -= moved
		sp.layouts[idx] = tail
		w.mountPoints[from].used -= moved
		w.insert(space, idx, Range{StartHash: start, EndHash: cloneHash(boundary), Size: moved}, dest, head)
	} else {
		end := cloneHash(r.EndHash)
		sp.ranges[idx].EndHash = cloneHash(boundary)
		sp.ranges[idx].Size -= moved
		sp.layouts[idx] = head
		w.mountPoints[from].used -= moved
		w.insert(space, idx+1, Range{StartHash: cloneHash(boundary), EndHash: end, Size: moved}, dest, tail)
	}
	w.mergeSpace(space)
	return true
}

// previewCut splits a range into the bytes that move (head for a prefix,
// tail for a suffix) without changing the world. Size stays attached to
// sourceRange, so a later hop cannot reassign those bytes. space and idx
// must index a range, and kind is a cut kind. want <= 0 moves nothing.
// want at or past the range size, or cutWhole, returns the whole range.
// A prefix or suffix that cannot be cut strictly inside the range returns
// a zero move.
func (w *world) previewCut(space, idx int, kind cutKind, want int64) (split []byte, moved int64, head, tail []recommendedLayout) {
	sp := &w.hashSpaces[space]
	r := sp.ranges[idx]
	start := w.startHash(space, idx)
	if want <= 0 {
		return cloneHash(start), 0, nil, cloneLayouts(sp.layouts[idx])
	}
	if want >= r.Size || kind == cutWhole {
		return cloneHash(r.EndHash), r.Size, cloneLayouts(sp.layouts[idx]), nil
	}
	if kind == cutPrefix {
		head, tail, moved = splitLayoutPrefix(sp.layouts[idx], start, want)
		if moved <= 0 || moved >= r.Size || len(head) == 0 {
			return cloneHash(start), 0, nil, nil
		}
		split = cloneHash(head[len(head)-1].endHash)
		if !pointInArc(start, r.EndHash, split) {
			return cloneHash(start), 0, nil, nil
		}
		return split, moved, head, tail
	}
	head, tail, kept := splitLayoutPrefix(sp.layouts[idx], start, r.Size-want)
	moved = r.Size - kept
	if moved <= 0 || moved >= r.Size || len(tail) == 0 {
		return cloneHash(start), 0, nil, nil
	}
	if len(head) == 0 {
		return cloneHash(start), moved, head, tail
	}
	split = cloneHash(head[len(head)-1].endHash)
	if !pointInArc(start, r.EndHash, split) {
		return cloneHash(start), 0, nil, nil
	}
	return split, moved, head, tail
}

// moveWhole assigns ranges[idx] to dest, moves its size from the old disk's
// used count to dest's, and merges. It does nothing when dest already owns
// the range. Free space, the range cap, and frozen are not checked.
// Planning callers use canTake first; reassigning an empty range does not.
// space, idx, and dest must be in range.
func (w *world) moveWhole(space, idx, dest int) {
	sp := &w.hashSpaces[space]
	from := w.diskOf(sp.ranges[idx])
	if from == dest {
		return
	}
	sz := sp.ranges[idx].Size
	sp.ranges[idx].StorageID = w.mountPoints[dest].StorageID
	w.mountPoints[from].used -= sz
	w.mountPoints[dest].used += sz
	w.mergeSpace(space)
}

// find returns the index of the range in space whose EndHash equals end,
// or -1. space must index a hash space.
func (w *world) find(space int, end []byte) int {
	for i, r := range w.hashSpaces[space].ranges {
		if hashEq(r.EndHash, end) {
			return i
		}
	}
	return -1
}

// insert places r at index i on dest and adds r.Size to dest.used.
// layouts is the piece list for the new range. When the space's layout
// slice is non-nil it must already be parallel to ranges, and layouts is
// inserted with r. This does not merge, and it does not check free space
// or the range cap. i must be a valid insert index and dest a mount index.
func (w *world) insert(space, i int, r Range, dest int, layouts []recommendedLayout) {
	sp := &w.hashSpaces[space]
	r.StorageID = w.mountPoints[dest].StorageID
	sp.ranges = slices.Insert(sp.ranges, i, r)
	if sp.layouts != nil {
		sp.layouts = slices.Insert(sp.layouts, i, layouts)
	}
	w.mountPoints[dest].used += r.Size
}

// mergeSpace joins neighbors, including across the wrap, that share a
// StorageID, and concatenates their layouts when layouts is parallel to
// ranges. Call it after an assignment that may have put neighbors on the
// same disk. Ranges must already be in circle order. space must index a
// hash space.
func (w *world) mergeSpace(space int) {
	sp := &w.hashSpaces[space]
	if len(sp.ranges) < 2 {
		return
	}
	for {
		n := len(sp.ranges)
		merged := false
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			if sp.ranges[i].StorageID != sp.ranges[j].StorageID || i == j {
				continue
			}
			sp.ranges[j].StartHash = sp.ranges[i].StartHash
			sp.ranges[j].Size += sp.ranges[i].Size
			if len(sp.layouts) == n {
				sp.layouts[j] = append(cloneLayouts(sp.layouts[i]), cloneLayouts(sp.layouts[j])...)
				sp.layouts = append(sp.layouts[:i], sp.layouts[i+1:]...)
			}
			sp.ranges = append(sp.ranges[:i], sp.ranges[i+1:]...)
			merged = true
			break
		}
		if !merged {
			return
		}
	}
}

// checkTiling requires the ranges to cover the circle exactly once: in
// EndHash order, each range starts where the one before it ends.
func checkTiling(ranges []Range) error {
	order := make([]int, len(ranges))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		return bytes.Compare(ranges[a].EndHash, ranges[b].EndHash)
	})
	for k, i := range order {
		prev := order[(k-1+len(order))%len(order)]
		if !hashEq(ranges[i].StartHash, ranges[prev].EndHash) {
			return xerrors.Errorf("range %d starts at %x, but the range before it ends at %x", i, ranges[i].StartHash, ranges[prev].EndHash)
		}
	}
	return nil
}

// layoutStart returns piece i's stored start after checking it against the
// chain: the range start for the first piece, otherwise the previous end.
func layoutStart(layouts []recommendedLayout, i int, rangeStart []byte) ([]byte, bool) {
	start := rangeStart
	if i > 0 {
		start = layouts[i-1].endHash
	}
	if !hashEq(layouts[i].startHash, start) {
		return nil, false
	}
	return layouts[i].startHash, true
}

// splitLayoutPrefix divides layouts into the first want bytes (head) and
// the rest (tail) without mutating the input. rangeStart must be the start
// of the first piece, and each piece's start must equal the previous end.
// A broken chain returns nil, nil, 0. headSize may be short of want when a
// piece cannot be cut to the remainder. want <= 0 returns the layouts as
// the tail.
func splitLayoutPrefix(layouts []recommendedLayout, rangeStart []byte, want int64) (head, tail []recommendedLayout, headSize int64) {
	if want <= 0 {
		return nil, cloneLayouts(layouts), 0
	}
	var acc int64
	for i, s := range layouts {
		start, ok := layoutStart(layouts, i, rangeStart)
		if !ok {
			return nil, nil, 0
		}
		if acc >= want {
			tail = append(tail, cloneLayout(s))
			continue
		}
		need := want - acc
		if s.size <= need {
			head = append(head, cloneLayout(s))
			acc += s.size
			continue
		}
		if need <= 0 || need >= s.size || !hashEq(s.startHash, start) {
			tail = append(tail, cloneLayouts(layouts[i:])...)
			return head, tail, acc
		}
		piece := Range{StartHash: s.startHash, EndHash: s.endHash, Size: s.size}
		split := splitHash(piece, need)
		moved := s.sizeIn(s.startHash, split)
		if moved <= 0 || moved >= s.size {
			split = splitHashBound(piece, 1, true)
			moved = s.sizeIn(s.startHash, split)
		}
		if moved <= 0 || moved >= s.size || hashEq(split, s.startHash) || hashEq(split, s.endHash) {
			tail = append(tail, cloneLayouts(layouts[i:])...)
			return head, tail, acc
		}
		left := s.piece(s.startHash, split, moved)
		right := s.piece(split, s.endHash, s.size-moved)
		head = append(head, left)
		tail = append(tail, right)
		tail = append(tail, cloneLayouts(layouts[i+1:])...)
		return head, tail, acc + left.size
	}
	return head, nil, acc
}

// layoutBytesTo returns how many bytes of layouts lie in (rangeStart, at].
// rangeStart is the first piece's start, and each piece must chain from
// it. Returns false when the chain breaks or at is not on the layouts.
func layoutBytesTo(layouts []recommendedLayout, rangeStart, at []byte) (int64, bool) {
	var acc int64
	for i, s := range layouts {
		start, ok := layoutStart(layouts, i, rangeStart)
		if !ok {
			return 0, false
		}
		if hashEq(at, s.endHash) {
			return acc + s.size, true
		}
		if pointInArc(start, s.endHash, at) {
			return acc + s.sizeIn(start, at), true
		}
		acc += s.size
	}
	return 0, false
}

// splitLayoutsAt divides layouts so the prefix has size leftSize and ends
// at `at`. rangeStart is the first piece's start. ok is false when the
// pieces do not chain or at is not exactly leftSize bytes in. The input
// is not mutated; returned pieces are copies.
func splitLayoutsAt(layouts []recommendedLayout, rangeStart, at []byte, leftSize int64) (head, tail []recommendedLayout, ok bool) {
	if leftSize <= 0 {
		return nil, cloneLayouts(layouts), hashEq(at, rangeStart)
	}
	var acc int64
	for i, s := range layouts {
		start, okStart := layoutStart(layouts, i, rangeStart)
		if !okStart {
			return nil, nil, false
		}
		if acc+s.size < leftSize {
			head = append(head, cloneLayout(s))
			acc += s.size
			continue
		}
		if acc+s.size == leftSize {
			if !hashEq(s.endHash, at) {
				return nil, nil, false
			}
			head = append(head, cloneLayout(s))
			return head, cloneLayouts(layouts[i+1:]), true
		}
		taken := leftSize - acc
		if taken <= 0 || taken >= s.size || hashEq(at, start) || hashEq(at, s.endHash) {
			return nil, nil, false
		}
		if !pointInArc(start, s.endHash, at) || s.sizeIn(start, at) != taken {
			return nil, nil, false
		}
		head = append(head, s.piece(s.startHash, at, taken))
		tail = append(tail, s.piece(at, s.endHash, s.size-taken))
		tail = append(tail, cloneLayouts(layouts[i+1:])...)
		return head, tail, true
	}
	return nil, nil, false
}

// cloneLayout copies s, including its hashes and source range.
func cloneLayout(s recommendedLayout) recommendedLayout {
	return s.piece(s.startHash, s.endHash, s.size)
}

// cloneLayouts copies every piece. An empty list returns nil.
func cloneLayouts(in []recommendedLayout) []recommendedLayout {
	if len(in) == 0 {
		return nil
	}
	out := make([]recommendedLayout, len(in))
	for i, s := range in {
		out[i] = cloneLayout(s)
	}
	return out
}
