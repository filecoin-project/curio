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

type spaceWorld struct {
	ranges []Range
	owner  []int
}

type world struct {
	disks  []int64
	spaces []spaceWorld
	used   []int64
	frozen []bool
	bytes  int64
	diff   []Transfer
}

func newWorld(state State) (*world, error) {
	if err := checkStructure(state); err != nil {
		return nil, err
	}
	w := &world{
		disks:  append([]int64(nil), state.Disks...),
		spaces: make([]spaceWorld, len(state.Spaces)),
		used:   make([]int64, len(state.Disks)),
		frozen: make([]bool, len(state.Disks)),
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

func (w *world) fairShare(d int) int64 {
	cap := w.totalCapacity()
	if cap == 0 {
		return 0
	}
	return w.totalUsed() * w.disks[d] / cap
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

func (w *world) applyCut(space, idx, kind, dest int, size int64) {
	sp := &w.spaces[space]
	r := sp.ranges[idx]
	from := sp.owner[idx]
	if size <= 0 {
		return
	}
	if size >= r.Size || kind == cutWhole {
		w.moveWhole(space, idx, dest)
		return
	}
	split, moved := w.cutActual(space, idx, kind, size)
	if moved <= 0 || moved >= r.Size {
		return
	}
	if w.used[dest]+moved > w.disks[dest] {
		return
	}
	start := w.startHash(space, idx)
	if kind == cutPrefix {
		w.recordTransfer(space, start, split, from, dest, moved)
		sp.ranges[idx].Size -= moved
		w.used[from] -= moved
		w.insert(space, idx, Range{EndHash: split, Size: moved}, dest)
	} else {
		end := cloneHash(r.EndHash)
		w.recordTransfer(space, split, end, from, dest, moved)
		sp.ranges[idx].EndHash = split
		sp.ranges[idx].Size = r.Size - moved
		w.used[from] -= moved
		w.insert(space, idx+1, Range{EndHash: end, Size: moved}, dest)
	}
	w.bytes += moved
	w.mergeSpace(space)
}

func (w *world) cutActual(space, idx, kind int, want int64) ([]byte, int64) {
	sp := &w.spaces[space]
	r := sp.ranges[idx]
	start := w.startHash(space, idx)
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

func (w *world) moveWhole(space, idx, dest int) {
	sp := &w.spaces[space]
	from := sp.owner[idx]
	if from == dest {
		return
	}
	sz := sp.ranges[idx].Size
	start := w.startHash(space, idx)
	end := cloneHash(sp.ranges[idx].EndHash)
	w.recordTransfer(space, start, end, from, dest, sz)
	sp.owner[idx] = dest
	w.used[from] -= sz
	w.used[dest] += sz
	w.bytes += sz
	w.mergeSpace(space)
}

func (w *world) recordTransfer(space int, start, end []byte, from, to int, size int64) {
	if size <= 0 || from == to {
		return
	}
	w.diff = append(w.diff, Transfer{
		Space:     space,
		StartHash: cloneHash(start),
		EndHash:   cloneHash(end),
		From:      from,
		To:        to,
		Size:      size,
	})
}

func (w *world) find(space int, end []byte) int {
	for i, r := range w.spaces[space].ranges {
		if hashEq(r.EndHash, end) {
			return i
		}
	}
	return -1
}

func (w *world) insert(space, i int, r Range, dest int) {
	sp := &w.spaces[space]
	sp.ranges = slices.Insert(sp.ranges, i, r)
	sp.owner = slices.Insert(sp.owner, i, dest)
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
