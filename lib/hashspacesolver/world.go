package hashspacesolver

import (
	"bytes"
	"slices"

	"golang.org/x/xerrors"
)

const (
	cutWhole = iota
	cutPrefix
	cutSuffix
)

// span is one original slice of a range. Cuts concatenate and split spans,
// but never retarget them: origin stays the disk that owned the bytes at start.
type span struct {
	end    []byte
	size   int64
	origin int
	// base is the input range this span was cut from. Sizes inside a span
	// come from base, so every cut order yields the same bytes per hash.
	base Range
}

// sizeIn returns how many of s's bytes lie in (from, to]. Both bounds must be
// on s's arc.
func (s span) sizeIn(from, to []byte) int64 {
	return sliceSize(s.base, from, to)
}

// piece returns the part of s ending at end with size bytes.
func (s span) piece(end []byte, size int64) span {
	return span{
		end:    cloneHash(end),
		size:   size,
		origin: s.origin,
		base:   cloneRange(s.base),
	}
}

// arc is one range on a circle plus the index of the mountpoint holding it.
// The embedded Range's StorageID is unused inside the solver.
type arc struct {
	Range
	disk int
}

type spaceWorld struct {
	ranges []arc
	spans  [][]span
}

type world struct {
	disks  []int64
	ids    []string
	diskOf map[string]int
	spaces []spaceWorld
	used   []int64
	frozen []bool
}

func newWorld(state State) (*world, error) {
	if err := checkStructure(state); err != nil {
		return nil, err
	}
	n := len(state.MountPoints)
	w := &world{
		disks:  make([]int64, n),
		ids:    make([]string, n),
		diskOf: make(map[string]int, n),
		spaces: make([]spaceWorld, len(state.HashSpaces)),
		used:   make([]int64, n),
		frozen: make([]bool, n),
	}
	for i, mp := range state.MountPoints {
		w.disks[i] = mp.Capacity
		w.ids[i] = mp.StorageID
		w.diskOf[mp.StorageID] = i
	}
	for s, ranges := range state.HashSpaces {
		arcs := make([]arc, len(ranges))
		for i, r := range ranges {
			arcs[i] = arc{Range: cloneRange(r), disk: w.diskOf[r.StorageID]}
		}
		w.spaces[s] = spaceWorld{ranges: arcs}
		w.sortSpace(s)
		w.spaces[s].spans = make([][]span, len(w.spaces[s].ranges))
		for i, r := range w.spaces[s].ranges {
			w.spaces[s].spans[i] = []span{{
				end:    cloneHash(r.EndHash),
				size:   r.Size,
				origin: r.disk,
				base:   cloneRange(r.Range),
			}}
		}
		w.mergeSpace(s)
		for _, r := range w.spaces[s].ranges {
			w.used[r.disk] += r.Size
		}
	}
	return w, nil
}

func cloneRange(r Range) Range {
	return Range{StartHash: cloneHash(r.StartHash), EndHash: cloneHash(r.EndHash), Size: r.Size}
}

func (w *world) snapshot() State {
	st := State{MountPoints: make([]MountPoint, len(w.disks))}
	for i, c := range w.disks {
		st.MountPoints[i] = MountPoint{Capacity: c, StorageID: w.ids[i]}
	}
	for s, sp := range w.spaces {
		if len(sp.ranges) == 0 {
			continue
		}
		ranges := make([]Range, len(sp.ranges))
		for i, a := range sp.ranges {
			ranges[i] = cloneRange(a.Range)
			ranges[i].StorageID = w.ids[a.disk]
		}
		st.HashSpaces[s] = ranges
	}
	return st
}

func (w *world) startHash(space, i int) []byte {
	return cloneHash(w.spaces[space].ranges[i].StartHash)
}

func (w *world) free(d int) int64 {
	return w.disks[d] - w.used[d]
}

func fillLimitOf(cap int64) int64 {
	if cap <= 0 {
		return 0
	}
	return cap * FILL_LIMIT_PERCENT / 100
}

func (w *world) fillLimit(d int) int64 {
	return fillLimitOf(w.disks[d])
}

func (w *world) fillHeadroom(d int) int64 {
	n := w.fillLimit(d) - w.used[d]
	if n < 0 {
		return 0
	}
	return n
}

func (w *world) overflow(d int) int64 {
	n := w.used[d] - w.fillLimit(d)
	if n < 0 {
		return 0
	}
	return n
}

func (w *world) totalUsed() int64 {
	var s int64
	for _, sp := range w.spaces {
		for _, r := range sp.ranges {
			s += r.Size
		}
	}
	return s
}

func (w *world) totalCapacity() int64 {
	var s int64
	for i, cap := range w.disks {
		if !w.frozen[i] {
			s += cap
		}
	}
	return s
}

func (w *world) ownsRange(disk int) bool {
	for s := range w.spaces {
		if w.rangeCount(s, disk) > 0 {
			return true
		}
	}
	return false
}

func (w *world) rangeCount(space, d int) int {
	n := 0
	for _, r := range w.spaces[space].ranges {
		if r.disk == d {
			n++
		}
	}
	return n
}

func (w *world) rangeIndexes(space, d int) []int {
	var out []int
	for i, r := range w.spaces[space].ranges {
		if r.disk == d {
			out = append(out, i)
		}
	}
	return out
}

func (w *world) activeDisks() []int {
	out := make([]int, 0, len(w.disks))
	for i := range w.disks {
		if !w.frozen[i] {
			out = append(out, i)
		}
	}
	return out
}

func (w *world) destDelta(space, idx, kind, dest int) int {
	sp := &w.spaces[space]
	n := len(sp.ranges)
	if n == 0 {
		return 1
	}
	if n == 1 && kind == cutWhole {
		return 1 - w.rangeCount(space, dest)
	}
	prev := (idx - 1 + n) % n
	next := (idx + 1) % n
	left := n > 1 && sp.ranges[prev].disk == dest && kind != cutSuffix
	right := n > 1 && sp.ranges[next].disk == dest && kind != cutPrefix
	switch {
	case left && right:
		return -1
	case left || right:
		return 0
	default:
		return 1
	}
}

func (w *world) canAccept(space, dest int, size int64, delta int) bool {
	if dest < 0 || dest >= len(w.disks) || w.frozen[dest] {
		return false
	}
	if w.used[dest]+size > w.disks[dest] {
		return false
	}
	return w.rangeCount(space, dest)+delta <= MAX_RANGES_PER_DISK
}

func (w *world) canTake(space, idx, kind, dest int, size int64) bool {
	if dest == w.spaces[space].ranges[idx].disk {
		return false
	}
	return w.canAccept(space, dest, size, w.destDelta(space, idx, kind, dest))
}

func (w *world) applyCut(space, idx, kind, dest int, size int64, split []byte) bool {
	sp := &w.spaces[space]
	r := sp.ranges[idx]
	from := r.disk
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
	var head, tail []span
	var moved int64
	boundary := split
	if len(split) > 0 {
		var ok bool
		if kind == cutPrefix {
			head, tail, ok = splitSpansAt(sp.spans[idx], start, split, size)
		} else {
			head, tail, ok = splitSpansAt(sp.spans[idx], start, split, r.Size-size)
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
	if moved <= 0 || moved >= r.Size || w.used[dest]+moved > w.disks[dest] {
		return false
	}
	if kind == cutPrefix {
		sp.ranges[idx].StartHash = cloneHash(boundary)
		sp.ranges[idx].Size -= moved
		sp.spans[idx] = tail
		w.used[from] -= moved
		w.insert(space, idx, Range{StartHash: start, EndHash: cloneHash(boundary), Size: moved}, dest, head)
	} else {
		end := cloneHash(r.EndHash)
		sp.ranges[idx].EndHash = cloneHash(boundary)
		sp.ranges[idx].Size -= moved
		sp.spans[idx] = head
		w.used[from] -= moved
		w.insert(space, idx+1, Range{StartHash: cloneHash(boundary), EndHash: end, Size: moved}, dest, tail)
	}
	w.mergeSpace(space)
	return true
}

func (w *world) cutActual(space, idx, kind int, want int64) ([]byte, int64) {
	split, moved, _, _ := w.previewCut(space, idx, kind, want)
	return split, moved
}

// previewCut splits a range into the bytes that move (head for a prefix,
// tail for a suffix) without changing the world. Size stays attached to the
// original span, so a later hop cannot reassign those bytes.
func (w *world) previewCut(space, idx, kind int, want int64) (split []byte, moved int64, head, tail []span) {
	sp := &w.spaces[space]
	r := sp.ranges[idx]
	start := w.startHash(space, idx)
	if want <= 0 {
		return cloneHash(start), 0, nil, cloneSpans(sp.spans[idx])
	}
	if want >= r.Size || kind == cutWhole {
		return cloneHash(r.EndHash), r.Size, cloneSpans(sp.spans[idx]), nil
	}
	if kind == cutPrefix {
		head, tail, moved = splitSpanPrefix(sp.spans[idx], start, want)
		if moved <= 0 || moved >= r.Size || len(head) == 0 {
			return cloneHash(start), 0, nil, nil
		}
		return cloneHash(head[len(head)-1].end), moved, head, tail
	}
	head, tail, kept := splitSpanPrefix(sp.spans[idx], start, r.Size-want)
	moved = r.Size - kept
	if moved <= 0 || moved >= r.Size || len(tail) == 0 {
		return cloneHash(start), 0, nil, nil
	}
	if len(head) == 0 {
		return cloneHash(start), moved, head, tail
	}
	return cloneHash(head[len(head)-1].end), moved, head, tail
}

func (w *world) moveWhole(space, idx, dest int) {
	sp := &w.spaces[space]
	from := sp.ranges[idx].disk
	if from == dest {
		return
	}
	sz := sp.ranges[idx].Size
	sp.ranges[idx].disk = dest
	w.used[from] -= sz
	w.used[dest] += sz
	w.mergeSpace(space)
}

// placedTransfers is the net of the solve: each original span that changed
// disks becomes one source-to-destination move. Spans that ended where they
// started are omitted.
func (w *world) placedTransfers() []Transfer {
	var out []Transfer
	for s, sp := range w.spaces {
		for i := range sp.ranges {
			if len(sp.spans[i]) == 0 {
				continue
			}
			prev := w.startHash(s, i)
			for _, spn := range sp.spans[i] {
				if spn.size >= 0 && spn.origin != sp.ranges[i].disk {
					out = append(out, Transfer{
						Space:     s,
						StartHash: cloneHash(prev),
						EndHash:   cloneHash(spn.end),
						From:      w.ids[spn.origin],
						To:        w.ids[sp.ranges[i].disk],
						Size:      spn.size,
					})
				}
				prev = spn.end
			}
		}
	}
	return out
}

func (w *world) find(space int, end []byte) int {
	for i, r := range w.spaces[space].ranges {
		if hashEq(r.EndHash, end) {
			return i
		}
	}
	return -1
}

func (w *world) insert(space, i int, r Range, dest int, spans []span) {
	sp := &w.spaces[space]
	sp.ranges = slices.Insert(sp.ranges, i, arc{Range: r, disk: dest})
	if sp.spans != nil {
		sp.spans = slices.Insert(sp.spans, i, spans)
	}
	w.used[dest] += r.Size
}

func (w *world) sortSpace(space int) {
	slices.SortFunc(w.spaces[space].ranges, func(a, b arc) int {
		return bytes.Compare(a.EndHash, b.EndHash)
	})
}

func (w *world) mergeSpace(space int) {
	sp := &w.spaces[space]
	if len(sp.ranges) < 2 {
		return
	}
	for {
		n := len(sp.ranges)
		merged := false
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			if sp.ranges[i].disk != sp.ranges[j].disk || i == j {
				continue
			}
			sp.ranges[j].StartHash = sp.ranges[i].StartHash
			sp.ranges[j].Size += sp.ranges[i].Size
			if len(sp.spans) == n {
				sp.spans[j] = append(cloneSpans(sp.spans[i]), cloneSpans(sp.spans[j])...)
				sp.spans = append(sp.spans[:i], sp.spans[i+1:]...)
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

func (w *world) neighbors(space, idx int) (left, right int, okL, okR bool) {
	sp := &w.spaces[space]
	n := len(sp.ranges)
	if n < 2 {
		return 0, 0, false, false
	}
	src := sp.ranges[idx].disk
	l := sp.ranges[(idx-1+n)%n].disk
	r := sp.ranges[(idx+1)%n].disk
	if l != src && !w.frozen[l] {
		left, okL = l, true
	}
	if r != src && !w.frozen[r] {
		right, okR = r, true
	}
	return
}

func checkStructure(state State) error {
	ids := make(map[string]struct{}, len(state.MountPoints))
	for i, mp := range state.MountPoints {
		if mp.StorageID == "" {
			return xerrors.Errorf("mountpoint %d has empty StorageID", i)
		}
		if _, ok := ids[mp.StorageID]; ok {
			return xerrors.Errorf("duplicate mountpoint StorageID %s", mp.StorageID)
		}
		ids[mp.StorageID] = struct{}{}
		if mp.Capacity < 0 {
			return xerrors.Errorf("mountpoint %s has negative capacity", mp.StorageID)
		}
	}
	for s, ranges := range state.HashSpaces {
		var hlen int
		seen := make(map[string]struct{}, len(ranges))
		for i, r := range ranges {
			if err := checkRange(r); err != nil {
				return xerrors.Errorf("space %d range %d: %w", s, i, err)
			}
			if hlen == 0 {
				hlen = len(r.EndHash)
			} else if len(r.EndHash) != hlen {
				return xerrors.Errorf("space %d range %d EndHash length %d != %d", s, i, len(r.EndHash), hlen)
			}
			key := string(r.EndHash)
			if _, ok := seen[key]; ok {
				return xerrors.Errorf("space %d: duplicate EndHash at range %d", s, i)
			}
			seen[key] = struct{}{}
			if _, ok := ids[r.StorageID]; !ok {
				return xerrors.Errorf("space %d range %d: unknown StorageID %q", s, i, r.StorageID)
			}
		}
		if err := checkTiling(ranges); err != nil {
			return xerrors.Errorf("space %d: %w", s, err)
		}
	}
	return nil
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

func splitSpanPrefix(spans []span, rangeStart []byte, want int64) (head, tail []span, headSize int64) {
	if want <= 0 {
		return nil, cloneSpans(spans), 0
	}
	var acc int64
	for i, s := range spans {
		start := rangeStart
		if i > 0 {
			start = spans[i-1].end
		}
		if acc >= want {
			tail = append(tail, cloneSpan(s))
			continue
		}
		need := want - acc
		if s.size <= need {
			head = append(head, cloneSpan(s))
			acc += s.size
			continue
		}
		left, right, ok := cutOneSpan(s, start, need)
		if !ok {
			tail = append(tail, cloneSpans(spans[i:])...)
			return head, tail, acc
		}
		head = append(head, left)
		tail = append(tail, right)
		tail = append(tail, cloneSpans(spans[i+1:])...)
		return head, tail, acc + left.size
	}
	return head, nil, acc
}

func cutOneSpan(s span, start []byte, want int64) (left, right span, ok bool) {
	if want <= 0 || want >= s.size {
		return span{}, span{}, false
	}
	r := Range{StartHash: start, EndHash: s.end, Size: s.size}
	split := splitHash(r, want)
	moved := s.sizeIn(start, split)
	if moved <= 0 || moved >= s.size {
		split = splitHashMin(r, 1)
		moved = s.sizeIn(start, split)
	}
	if moved <= 0 || moved >= s.size || hashEq(split, start) || hashEq(split, s.end) {
		return span{}, span{}, false
	}
	return s.piece(split, moved), s.piece(s.end, s.size-moved), true
}

// spanBytesTo returns how many bytes of spans lie in (rangeStart, at].
func spanBytesTo(spans []span, rangeStart, at []byte) (int64, bool) {
	var acc int64
	for i, s := range spans {
		start := rangeStart
		if i > 0 {
			start = spans[i-1].end
		}
		if hashEq(at, s.end) {
			return acc + s.size, true
		}
		if pointInArc(start, s.end, at) {
			return acc + s.sizeIn(start, at), true
		}
		acc += s.size
	}
	return 0, false
}

// splitSpansAt divides spans so the prefix has size leftSize and ends at `at`.
func splitSpansAt(spans []span, rangeStart, at []byte, leftSize int64) (head, tail []span, ok bool) {
	if leftSize <= 0 {
		return nil, cloneSpans(spans), hashEq(at, rangeStart)
	}
	var acc int64
	for i, s := range spans {
		start := rangeStart
		if i > 0 {
			start = spans[i-1].end
		}
		if acc+s.size < leftSize {
			head = append(head, cloneSpan(s))
			acc += s.size
			continue
		}
		if acc+s.size == leftSize {
			if !hashEq(s.end, at) {
				return nil, nil, false
			}
			head = append(head, cloneSpan(s))
			return head, cloneSpans(spans[i+1:]), true
		}
		taken := leftSize - acc
		if taken <= 0 || taken >= s.size || hashEq(at, start) || hashEq(at, s.end) {
			return nil, nil, false
		}
		if !pointInArc(start, s.end, at) || s.sizeIn(start, at) != taken {
			return nil, nil, false
		}
		head = append(head, s.piece(at, taken))
		tail = append(tail, s.piece(s.end, s.size-taken))
		tail = append(tail, cloneSpans(spans[i+1:])...)
		return head, tail, true
	}
	return nil, nil, false
}

func cloneSpan(s span) span {
	return s.piece(s.end, s.size)
}

func cloneSpans(in []span) []span {
	if len(in) == 0 {
		return nil
	}
	out := make([]span, len(in))
	for i, s := range in {
		out[i] = cloneSpan(s)
	}
	return out
}
