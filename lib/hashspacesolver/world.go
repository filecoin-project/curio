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

type world struct {
	disks  []int64
	ranges []Range
	owner  []int
	used   []int64
	frozen []bool
	moves  []Move
	bytes  int64
}

func newWorld(state State) (*world, error) {
	if err := checkStructure(state); err != nil {
		return nil, err
	}
	w := &world{
		disks:  append([]int64(nil), state.Disks...),
		ranges: cloneRanges(state.Ranges),
		owner:  append([]int(nil), state.Owner...),
		used:   make([]int64, len(state.Disks)),
		frozen: make([]bool, len(state.Disks)),
	}
	for i, r := range w.ranges {
		w.used[w.owner[i]] += r.Size
	}
	w.sortRanges()
	w.merge()
	return w, nil
}

func cloneRanges(in []Range) []Range {
	out := make([]Range, len(in))
	for i, r := range in {
		out[i] = Range{EndHash: cloneHash(r.EndHash), Size: r.Size}
	}
	return out
}

func (w *world) snapshot() State {
	return State{
		Disks:  append([]int64(nil), w.disks...),
		Ranges: cloneRanges(w.ranges),
		Owner:  append([]int(nil), w.owner...),
	}
}

func (w *world) plan() Plan {
	return Plan{Moves: append([]Move(nil), w.moves...), BytesMoved: w.bytes}
}

func (w *world) startHash(i int) []byte {
	return StartHash(w.ranges, i)
}

func (w *world) free(d int) int64 {
	return w.disks[d] - w.used[d]
}

func (w *world) totalUsed() int64 {
	var s int64
	for _, r := range w.ranges {
		s += r.Size
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

func (w *world) fairShare(d int) int64 {
	cap := w.totalCapacity()
	if cap == 0 {
		return 0
	}
	return w.totalUsed() * w.disks[d] / cap
}

func (w *world) rangeCount(d int) int {
	n := 0
	for _, o := range w.owner {
		if o == d {
			n++
		}
	}
	return n
}

func (w *world) rangeIndexes(d int) []int {
	var out []int
	for i, o := range w.owner {
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

func (w *world) destDelta(idx, kind, dest int) int {
	n := len(w.ranges)
	if n == 0 {
		return 1
	}
	if n == 1 && kind == cutWhole {
		return 1 - w.rangeCount(dest)
	}
	prev := (idx - 1 + n) % n
	next := (idx + 1) % n
	left := n > 1 && w.owner[prev] == dest && kind != cutSuffix
	right := n > 1 && w.owner[next] == dest && kind != cutPrefix
	switch {
	case left && right:
		return -1
	case left || right:
		return 0
	default:
		return 1
	}
}

func (w *world) canAccept(dest int, size int64, delta int) bool {
	if dest < 0 || dest >= len(w.disks) || w.frozen[dest] {
		return false
	}
	if w.used[dest]+size > w.disks[dest] {
		return false
	}
	return w.rangeCount(dest)+delta <= MAX_RANGES_PER_DISK
}

func (w *world) canTake(idx, kind, dest int, size int64) bool {
	if dest == w.owner[idx] {
		return false
	}
	return w.canAccept(dest, size, w.destDelta(idx, kind, dest))
}

func (w *world) applyCut(idx, kind, dest int, size int64) {
	r := w.ranges[idx]
	from := w.owner[idx]
	if size <= 0 {
		return
	}
	if size >= r.Size || kind == cutWhole {
		w.moveWhole(idx, dest)
		return
	}
	split, moved := w.cutActual(idx, kind, size)
	if moved <= 0 || moved >= r.Size {
		return
	}
	if w.used[dest]+moved > w.disks[dest] {
		return
	}
	if kind == cutPrefix {
		w.ranges[idx].Size -= moved
		w.used[from] -= moved
		w.insert(idx, Range{EndHash: split, Size: moved}, dest)
		w.record(from, dest, r.EndHash, split, true, moved)
	} else {
		end := cloneHash(r.EndHash)
		w.ranges[idx].EndHash = split
		w.ranges[idx].Size = r.Size - moved
		w.used[from] -= moved
		w.insert(idx+1, Range{EndHash: end, Size: moved}, dest)
		w.record(from, dest, end, split, false, moved)
	}
	w.merge()
}

func (w *world) cutActual(idx, kind int, want int64) ([]byte, int64) {
	r := w.ranges[idx]
	start := w.startHash(idx)
	if want <= 0 {
		return cloneHash(start), 0
	}
	if want >= r.Size || kind == cutWhole {
		return cloneHash(r.EndHash), r.Size
	}
	var split []byte
	if kind == cutPrefix {
		split = splitHash(r, start, want)
		moved := SliceSize(r, start, split)
		if moved == 0 {
			split = splitHashMin(r, start, 1)
			moved = SliceSize(r, start, split)
		}
		return split, moved
	}
	split = splitHash(r, start, r.Size-want)
	kept := SliceSize(r, start, split)
	moved := r.Size - kept
	if moved == 0 {
		split = splitHash(r, start, r.Size-1)
		if bytes.Equal(split, start) {
			split = splitHashMin(r, start, 1)
		}
		kept = SliceSize(r, start, split)
		moved = r.Size - kept
	}
	return split, moved
}

func (w *world) moveWhole(idx, dest int) {
	from := w.owner[idx]
	if from == dest {
		return
	}
	sz := w.ranges[idx].Size
	w.owner[idx] = dest
	w.used[from] -= sz
	w.used[dest] += sz
	w.record(from, dest, w.ranges[idx].EndHash, nil, false, sz)
	w.merge()
}

func (w *world) record(from, to int, end, split []byte, prefix bool, size int64) {
	w.moves = append(w.moves, Move{
		From:    from,
		To:      to,
		EndHash: cloneHash(end),
		Split:   cloneHash(split),
		Prefix:  prefix,
		Size:    size,
	})
	w.bytes += size
}

func (w *world) find(end []byte) int {
	for i, r := range w.ranges {
		if hashEq(r.EndHash, end) {
			return i
		}
	}
	return -1
}

func (w *world) insert(i int, r Range, dest int) {
	w.ranges = slices.Insert(w.ranges, i, r)
	w.owner = slices.Insert(w.owner, i, dest)
	w.used[dest] += r.Size
}

func (w *world) sortRanges() {
	type pair struct {
		r Range
		d int
	}
	ps := make([]pair, len(w.ranges))
	for i := range w.ranges {
		ps[i] = pair{r: w.ranges[i], d: w.owner[i]}
	}
	slices.SortFunc(ps, func(a, b pair) int {
		return bytes.Compare(a.r.EndHash, b.r.EndHash)
	})
	for i, p := range ps {
		w.ranges[i] = p.r
		w.owner[i] = p.d
	}
}

func (w *world) merge() {
	if len(w.ranges) < 2 {
		return
	}
	for {
		n := len(w.ranges)
		merged := false
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			if w.owner[i] != w.owner[j] {
				continue
			}
			if i == j {
				continue
			}
			w.ranges[j].Size += w.ranges[i].Size
			w.ranges = append(w.ranges[:i], w.ranges[i+1:]...)
			w.owner = append(w.owner[:i], w.owner[i+1:]...)
			merged = true
			break
		}
		if !merged {
			return
		}
	}
}

func (w *world) neighbors(idx int) (left, right int, okL, okR bool) {
	n := len(w.ranges)
	if n < 2 {
		return 0, 0, false, false
	}
	src := w.owner[idx]
	l := w.owner[(idx-1+n)%n]
	r := w.owner[(idx+1)%n]
	if l != src && !w.frozen[l] {
		left, okL = l, true
	}
	if r != src && !w.frozen[r] {
		right, okR = r, true
	}
	return
}

func checkStructure(state State) error {
	if len(state.Owner) != len(state.Ranges) {
		return xerrors.Errorf("owner length %d != ranges length %d", len(state.Owner), len(state.Ranges))
	}
	var hlen int
	seen := make(map[string]struct{}, len(state.Ranges))
	for i, r := range state.Ranges {
		if r.Size < 0 {
			return xerrors.Errorf("range %d has negative size", i)
		}
		if len(r.EndHash) == 0 {
			return xerrors.Errorf("range %d has empty EndHash", i)
		}
		if hlen == 0 {
			hlen = len(r.EndHash)
		} else if len(r.EndHash) != hlen {
			return xerrors.Errorf("range %d EndHash length %d != %d", i, len(r.EndHash), hlen)
		}
		key := string(r.EndHash)
		if _, ok := seen[key]; ok {
			return xerrors.Errorf("duplicate EndHash at range %d", i)
		}
		seen[key] = struct{}{}
		if state.Owner[i] < 0 || state.Owner[i] >= len(state.Disks) {
			return xerrors.Errorf("range %d owner %d out of range", i, state.Owner[i])
		}
	}
	for i, sz := range state.Disks {
		if sz < 0 {
			return xerrors.Errorf("disk %d has negative size", i)
		}
	}
	return nil
}
