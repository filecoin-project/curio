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
}

type spaceWorld struct {
	ranges []Range
	owner  []int
	spans  [][]span
}

type world struct {
	disks  []int64
	spaces []spaceWorld
	used   []int64
	frozen []bool
}

func newWorld(state State) (*world, error) {
	if err := checkStructure(state); err != nil {
		return nil, err
	}
	if len(state.Vacating) != 0 && len(state.Vacating) != len(state.Disks) {
		return nil, xerrors.Errorf("vacating length %d != %d disks", len(state.Vacating), len(state.Disks))
	}
	w := &world{
		disks:  append([]int64(nil), state.Disks...),
		spaces: make([]spaceWorld, len(state.Spaces)),
		used:   make([]int64, len(state.Disks)),
		frozen: make([]bool, len(state.Disks)),
	}
	for i, v := range state.Vacating {
		w.frozen[i] = v
	}
	for s, sp := range state.Spaces {
		w.spaces[s] = spaceWorld{
			ranges: cloneRanges(sp.Ranges),
			owner:  append([]int(nil), sp.Owner...),
		}
		w.sortSpace(s)
		w.mergeSpace(s)
		for i, r := range w.spaces[s].ranges {
			w.used[w.spaces[s].owner[i]] += r.Size
		}
		w.spaces[s].spans = make([][]span, len(w.spaces[s].ranges))
		for i, r := range w.spaces[s].ranges {
			w.spaces[s].spans[i] = []span{{
				end:    cloneHash(r.EndHash),
				size:   r.Size,
				origin: w.spaces[s].owner[i],
			}}
		}
	}
	return w, nil
}

func cloneRanges(in []Range) []Range {
	out := make([]Range, len(in))
	for i, r := range in {
		out[i] = Range{EndHash: cloneHash(r.EndHash), Size: r.Size}
	}
	return out
}

func cloneSpace(sp Space) Space {
	return Space{
		Ranges: cloneRanges(sp.Ranges),
		Owner:  append([]int(nil), sp.Owner...),
	}
}

func (w *world) snapshot() State {
	spaces := make([]Space, len(w.spaces))
	for i, sp := range w.spaces {
		spaces[i] = Space{
			Ranges: cloneRanges(sp.ranges),
			Owner:  append([]int(nil), sp.owner...),
		}
	}
	return State{
		Disks:  append([]int64(nil), w.disks...),
		Spaces: spaces,
	}
}

func (w *world) startHash(space, i int) []byte {
	return StartHash(w.spaces[space].ranges, i)
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
	for _, o := range w.spaces[space].owner {
		if o == d {
			n++
		}
	}
	return n
}

func (w *world) rangeIndexes(space, d int) []int {
	var out []int
	for i, o := range w.spaces[space].owner {
		if o == d {
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
	left := n > 1 && sp.owner[prev] == dest && kind != cutSuffix
	right := n > 1 && sp.owner[next] == dest && kind != cutPrefix
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
	if dest == w.spaces[space].owner[idx] {
		return false
	}
	return w.canAccept(space, dest, size, w.destDelta(space, idx, kind, dest))
}

func (w *world) applyCut(space, idx, kind, dest int, size int64, split []byte) bool {
	sp := &w.spaces[space]
	r := sp.ranges[idx]
	from := sp.owner[idx]
	if size <= 0 || from == dest {
		return false
	}
	if size >= r.Size || kind == cutWhole {
		w.moveWhole(space, idx, dest)
		return true
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
		sp.ranges[idx].Size -= moved
		sp.spans[idx] = tail
		w.used[from] -= moved
		w.insert(space, idx, Range{EndHash: cloneHash(boundary), Size: moved}, dest, head)
	} else {
		end := cloneHash(r.EndHash)
		sp.ranges[idx].EndHash = cloneHash(boundary)
		sp.ranges[idx].Size -= moved
		sp.spans[idx] = head
		w.used[from] -= moved
		w.insert(space, idx+1, Range{EndHash: end, Size: moved}, dest, tail)
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
	from := sp.owner[idx]
	if from == dest {
		return
	}
	sz := sp.ranges[idx].Size
	sp.owner[idx] = dest
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
				if spn.size >= 0 && spn.origin != sp.owner[i] {
					out = append(out, Transfer{
						Space:     s,
						StartHash: cloneHash(prev),
						EndHash:   cloneHash(spn.end),
						From:      spn.origin,
						To:        sp.owner[i],
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
	sp.ranges = slices.Insert(sp.ranges, i, r)
	sp.owner = slices.Insert(sp.owner, i, dest)
	if sp.spans != nil {
		sp.spans = slices.Insert(sp.spans, i, spans)
	}
	w.used[dest] += r.Size
}

func (w *world) sortSpace(space int) {
	sp := &w.spaces[space]
	type pair struct {
		r Range
		d int
	}
	ps := make([]pair, len(sp.ranges))
	for i := range sp.ranges {
		ps[i] = pair{r: sp.ranges[i], d: sp.owner[i]}
	}
	slices.SortFunc(ps, func(a, b pair) int {
		return bytes.Compare(a.r.EndHash, b.r.EndHash)
	})
	for i, p := range ps {
		sp.ranges[i] = p.r
		sp.owner[i] = p.d
	}
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
			if sp.owner[i] != sp.owner[j] || i == j {
				continue
			}
			sp.ranges[j].Size += sp.ranges[i].Size
			if len(sp.spans) == n {
				sp.spans[j] = append(cloneSpans(sp.spans[i]), cloneSpans(sp.spans[j])...)
				sp.spans = append(sp.spans[:i], sp.spans[i+1:]...)
			}
			sp.ranges = append(sp.ranges[:i], sp.ranges[i+1:]...)
			sp.owner = append(sp.owner[:i], sp.owner[i+1:]...)
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
	src := sp.owner[idx]
	l := sp.owner[(idx-1+n)%n]
	r := sp.owner[(idx+1)%n]
	if l != src && !w.frozen[l] {
		left, okL = l, true
	}
	if r != src && !w.frozen[r] {
		right, okR = r, true
	}
	return
}

func checkStructure(state State) error {
	for i, sz := range state.Disks {
		if sz < 0 {
			return xerrors.Errorf("disk %d has negative size", i)
		}
	}
	for s, sp := range state.Spaces {
		if len(sp.Owner) != len(sp.Ranges) {
			return xerrors.Errorf("space %d: owner length %d != ranges length %d", s, len(sp.Owner), len(sp.Ranges))
		}
		var hlen int
		seen := make(map[string]struct{}, len(sp.Ranges))
		for i, r := range sp.Ranges {
			if r.Size < 0 {
				return xerrors.Errorf("space %d range %d has negative size", s, i)
			}
			if len(r.EndHash) == 0 {
				return xerrors.Errorf("space %d range %d has empty EndHash", s, i)
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
			if sp.Owner[i] < 0 || sp.Owner[i] >= len(state.Disks) {
				return xerrors.Errorf("space %d range %d owner %d out of range", s, i, sp.Owner[i])
			}
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
	r := Range{EndHash: s.end, Size: s.size}
	split := splitHash(r, start, want)
	moved := SliceSize(r, start, split)
	if moved <= 0 || moved >= s.size {
		split = splitHashMin(r, start, 1)
		moved = SliceSize(r, start, split)
	}
	if moved <= 0 || moved >= s.size || hashEq(split, start) || hashEq(split, s.end) {
		return span{}, span{}, false
	}
	left = span{end: split, size: moved, origin: s.origin}
	right = span{end: cloneHash(s.end), size: s.size - moved, origin: s.origin}
	return left, right, true
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
		head = append(head, span{end: cloneHash(at), size: taken, origin: s.origin})
		tail = append(tail, span{end: cloneHash(s.end), size: s.size - taken, origin: s.origin})
		tail = append(tail, cloneSpans(spans[i+1:])...)
		return head, tail, true
	}
	return nil, nil, false
}

func cloneSpan(s span) span {
	return span{end: cloneHash(s.end), size: s.size, origin: s.origin}
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
