package hashspacesolver

import (
	"bytes"
	"math"
	"math/big"
	"slices"

	"golang.org/x/xerrors"
)

// Solve computes a minimum-movement target for a disk arriving, filling, or
// vacating. The resulting assignment keeps every active disk at or under
// capacity and at or under MAX_RANGES_PER_DISK contiguous ranges per space.
//
// Cost is lexicographic: fewer bytes moved, then fewer moves. Cuts may come
// from any space. Hash space within each Space is a circle.
//
// Result.State is the finished layout. Result.Diff moves each byte at most
// once, from the disk that owned it at the start to the disk that holds it
// at the end. The list is order-independent.
func Solve(state State, event Event) (Result, error) {
	w, err := newWorld(state)
	if err != nil {
		return Result{}, err
	}
	if event.Disk < 0 || event.Disk >= len(w.disks) {
		return Result{}, xerrors.Errorf("unknown event disk %d", event.Disk)
	}

	switch event.Kind {
	case EventArrive:
		if err := w.repair(); err != nil {
			return Result{}, err
		}
		w.arrive(event.Disk)
	case EventFull:
		if err := w.repair(); err != nil {
			return Result{}, err
		}
		if err := w.shed(event.Disk); err != nil {
			return Result{}, err
		}
	case EventVacate:
		if err := w.vacate(event.Disk); err != nil {
			return Result{}, err
		}
	case EventAbsorb:
		if err := w.repair(); err != nil {
			return Result{}, err
		}
		w.absorb(event.Disk)
	case EventBalance:
		if err := w.repair(); err != nil {
			return Result{}, err
		}
		w.balance()
	default:
		return Result{}, xerrors.Errorf("unknown event kind %d", event.Kind)
	}

	if err := w.repair(); err != nil {
		return Result{}, err
	}
	if err := w.checkSolved(event); err != nil {
		return Result{}, err
	}

	diff := mergeTransfers(w.placedTransfers())
	var bytes int64
	for _, t := range diff {
		bytes += t.Size
	}
	return Result{State: w.snapshot(), Diff: diff, BytesMoved: bytes}, nil
}

// Validate reports whether state is structurally sound and satisfies capacity
// and per-space range-count limits.
func Validate(state State) error {
	w, err := newWorld(state)
	if err != nil {
		return err
	}
	return w.checkLimits()
}

// Apply returns a copy of state with all transfers applied. Transfers may be
// applied in any order when they are disjoint within a space.
func Apply(state State, diff []Transfer) (State, error) {
	w, err := newWorld(state)
	if err != nil {
		return State{}, err
	}
	for i, t := range diff {
		if err := w.applyTransfer(t); err != nil {
			return State{}, xerrors.Errorf("transfer %d: %w", i, err)
		}
	}
	return w.snapshot(), nil
}

func (w *world) applyTransfer(t Transfer) error {
	if t.Space < 0 || t.Space >= len(w.spaces) {
		return xerrors.Errorf("unknown space %d", t.Space)
	}
	if t.To < 0 || t.To >= len(w.disks) {
		return xerrors.Errorf("unknown dest disk %d", t.To)
	}
	if t.Size < 0 {
		return nil
	}
	sp := &w.spaces[t.Space]
	idx := -1
	for i, r := range sp.ranges {
		start := r.StartHash
		if /* coversInterval */ func(rStart, rEnd, tStart, tEnd []byte) bool {
			// Full-circle transfer: start == end means the whole circle.
			if hashEq(tStart, tEnd) {
				return hashEq(rStart, rEnd) && hashEq(rStart, tStart)
			}
			okStart := hashEq(tStart, rStart) || pointInArc(rStart, rEnd, tStart)
			okEnd := hashEq(tEnd, rEnd) || pointInArc(rStart, rEnd, tEnd)
			return okStart && okEnd
		}(start, r.EndHash, t.StartHash, t.EndHash) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return xerrors.Errorf("no range covering (%x, %x]", t.StartHash, t.EndHash)
	}
	from := sp.owner[idx]
	if t.From >= 0 && from != t.From {
		return xerrors.Errorf("interval owned by %d, want %d", from, t.From)
	}
	r := sp.ranges[idx]
	start := r.StartHash
	if t.Size == 0 {
		if !hashEq(t.StartHash, start) || !hashEq(t.EndHash, r.EndHash) {
			return xerrors.Errorf("empty transfer must cover a whole range")
		}
		w.moveWholeSilent(t.Space, idx, t.To)
		return nil
	}
	if hashEq(t.StartHash, start) && hashEq(t.EndHash, r.EndHash) {
		w.moveWholeSilent(t.Space, idx, t.To)
		return nil
	}
	if hashEq(t.StartHash, start) {
		if t.Size >= r.Size {
			w.moveWholeSilent(t.Space, idx, t.To)
			return nil
		}
		return /* world.splitMovePrefix */ func(space, idx int, split []byte, size int64, dest int) error {
			sp := &w.spaces[space]
			from := sp.owner[idx]
			r := sp.ranges[idx]
			if size >= r.Size {
				w.moveWholeSilent(space, idx, dest)
				return nil
			}
			start := w.startHash(space, idx)
			head, tail, ok := splitSpansAt(sp.spans[idx], start, split, size)
			if !ok {
				return xerrors.Errorf("prefix split (%x, %x] size %d", start, split, size)
			}
			sp.ranges[idx].StartHash = cloneHash(split)
			sp.ranges[idx].Size -= size
			sp.spans[idx] = tail
			w.used[from] -= size
			w.insert(space, idx, Range{StartHash: start, EndHash: cloneHash(split), Size: size}, dest, head)
			w.mergeSpace(space)
			return nil
		}(t.Space, idx, t.EndHash, t.Size, t.To)
	}
	if hashEq(t.EndHash, r.EndHash) {
		if t.Size >= r.Size {
			w.moveWholeSilent(t.Space, idx, t.To)
			return nil
		}
		return /* world.splitMoveSuffix */ func(space, idx int, split []byte, size int64, dest int) error {
			sp := &w.spaces[space]
			from := sp.owner[idx]
			r := sp.ranges[idx]
			if size >= r.Size {
				w.moveWholeSilent(space, idx, dest)
				return nil
			}
			start := w.startHash(space, idx)
			kept := r.Size - size
			head, tail, ok := splitSpansAt(sp.spans[idx], start, split, kept)
			if !ok {
				return xerrors.Errorf("suffix split (%x, %x] size %d", split, r.EndHash, size)
			}
			end := cloneHash(r.EndHash)
			sp.ranges[idx].EndHash = cloneHash(split)
			sp.ranges[idx].Size = kept
			sp.spans[idx] = head
			w.used[from] -= size
			w.insert(space, idx+1, Range{StartHash: cloneHash(split), EndHash: end, Size: size}, dest, tail)
			w.mergeSpace(space)
			return nil
		}(t.Space, idx, t.StartHash, t.Size, t.To)
	}
	left, okL := spanBytesTo(sp.spans[idx], start, t.StartHash)
	end, okR := spanBytesTo(sp.spans[idx], start, t.EndHash)
	if !okL || !okR {
		return xerrors.Errorf("transfer (%x, %x] is not inside one range", t.StartHash, t.EndHash)
	}
	if end-left != t.Size {
		return xerrors.Errorf("transfer (%x, %x] size %d, range holds %d there", t.StartHash, t.EndHash, t.Size, end-left)
	}
	if left <= 0 || left+t.Size >= r.Size {
		return xerrors.Errorf("transfer must be a prefix, suffix, or whole of one range")
	}
	return /* world.splitMoveMiddle */ func(space, idx int, midStart, midEnd []byte, left, mid int64, dest int) error {
		sp := &w.spaces[space]
		from := sp.owner[idx]
		r := sp.ranges[idx]
		right := r.Size - left - mid
		if left <= 0 || mid <= 0 || right <= 0 {
			return xerrors.Errorf("middle split left %d mid %d right %d", left, mid, right)
		}
		start := w.startHash(space, idx)
		head, rest, ok := splitSpansAt(sp.spans[idx], start, midStart, left)
		if !ok {
			return xerrors.Errorf("middle split at %x", midStart)
		}
		midSpans, tail, ok := splitSpansAt(rest, midStart, midEnd, mid)
		if !ok {
			return xerrors.Errorf("middle split at %x", midEnd)
		}
		sp.ranges[idx].StartHash = cloneHash(midEnd)
		sp.ranges[idx].Size = right
		sp.spans[idx] = tail
		w.used[from] -= mid
		w.insert(space, idx, Range{StartHash: cloneHash(midStart), EndHash: cloneHash(midEnd), Size: mid}, dest, midSpans)
		sp.ranges = slices.Insert(sp.ranges, idx, Range{StartHash: start, EndHash: cloneHash(midStart), Size: left})
		sp.owner = slices.Insert(sp.owner, idx, from)
		sp.spans = slices.Insert(sp.spans, idx, head)
		w.mergeSpace(space)
		return nil
	}(t.Space, idx, t.StartHash, t.EndHash, left, t.Size, t.To)
}

func pointInArc(start, end, p []byte) bool {
	if hashEq(start, end) {
		return !hashEq(p, start)
	}
	if bytes.Compare(start, end) < 0 {
		return bytes.Compare(start, p) < 0 && bytes.Compare(p, end) <= 0
	}
	return bytes.Compare(start, p) < 0 || bytes.Compare(p, end) <= 0
}

func (w *world) moveWholeSilent(space, idx, dest int) {
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

func mergeTransfers(in []Transfer) []Transfer {
	if len(in) == 0 {
		return nil
	}
	out := make([]Transfer, 0, len(in))
	for _, t := range in {
		if t.Size < 0 || t.From == t.To {
			continue
		}
		out = append(out, Transfer{
			Space:     t.Space,
			StartHash: cloneHash(t.StartHash),
			EndHash:   cloneHash(t.EndHash),
			From:      t.From,
			To:        t.To,
			Size:      t.Size,
		})
	}
	for {
		progress := false
		for i := 0; i < len(out); i++ {
			for j := i + 1; j < len(out); j++ {
				joined, ok := /* joinAdjacent */ func(a, b Transfer) (Transfer, bool) {
					if a.Size == 0 || b.Size == 0 {
						return Transfer{}, false
					}
					if a.Space != b.Space || a.From != b.From || a.To != b.To {
						return Transfer{}, false
					}
					if hashEq(a.EndHash, b.StartHash) {
						a.EndHash = cloneHash(b.EndHash)
						a.Size += b.Size
						return a, true
					}
					if hashEq(b.EndHash, a.StartHash) {
						a.StartHash = cloneHash(b.StartHash)
						a.Size += b.Size
						return a, true
					}
					return Transfer{}, false
				}(out[i], out[j])
				if !ok {
					continue
				}
				out[i] = joined
				out = append(out[:j], out[j+1:]...)
				progress = true
				break
			}
			if progress {
				break
			}
		}
		if !progress {
			break
		}
	}
	return out
}

func (w *world) checkLimits() error {
	for _, d := range w.activeDisks() {
		if w.used[d] > w.disks[d] {
			return xerrors.Errorf("disk %d used %d exceeds size %d", d, w.used[d], w.disks[d])
		}
		for s := range w.spaces {
			if rc := w.rangeCount(s, d); rc > MAX_RANGES_PER_DISK {
				return xerrors.Errorf("disk %d space %d holds %d ranges, max %d", d, s, rc, MAX_RANGES_PER_DISK)
			}
		}
	}
	return nil
}

func (w *world) checkSolved(event Event) error {
	if err := w.checkLimits(); err != nil {
		return err
	}
	switch event.Kind {
	case EventVacate:
		if w.used[event.Disk] != 0 || w.ownsRange(event.Disk) {
			return xerrors.Errorf("disk %d was not emptied", event.Disk)
		}
	}
	return nil
}

type candidate struct {
	space, idx, kind, dest int
	size                   int64
	dlt                    int
	over                   int64
	split                  []byte
}

func (w *world) repair() error {
	totalRanges := 0
	for _, sp := range w.spaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges+len(w.disks)+8; guard++ {
		type overKey struct{ space, disk int }
		var over []overKey
		for _, d := range w.activeDisks() {
			for s := range w.spaces {
				if w.rangeCount(s, d) > MAX_RANGES_PER_DISK {
					over = append(over, overKey{s, d})
				}
			}
		}
		if len(over) == 0 {
			return nil
		}
		progress := false
		for _, o := range over {
			if w.donateRange(o.space, o.disk) {
				progress = true
				break
			}
		}
		if !progress {
			return xerrors.New("range repair did not converge")
		}
	}
	return xerrors.New("range repair did not converge")
}

func (w *world) donateRange(space, src int) bool {
	idxs := w.rangeIndexes(space, src)
	if len(idxs) <= MAX_RANGES_PER_DISK {
		return false
	}
	var best *candidate
	for _, idx := range idxs {
		sz := w.spaces[space].ranges[idx].Size
		for _, dest := range w.activeDisks() {
			if !w.canTake(space, idx, cutWhole, dest, sz) {
				continue
			}
			c := candidate{space: space, idx: idx, kind: cutWhole, dest: dest, size: sz, dlt: w.destDelta(space, idx, cutWhole, dest)}
			if best == nil || c.size < best.size || (c.size == best.size && (c.dlt < best.dlt || (c.dlt == best.dlt && (c.dest < best.dest || (c.dest == best.dest && c.space < best.space))))) {
				cp := c
				best = &cp
			}
		}
	}
	if best != nil {
		return w.applyCut(best.space, best.idx, best.kind, best.dest, best.size, best.split)
	}
	smallest := idxs[0]
	for _, idx := range idxs[1:] {
		if w.spaces[space].ranges[idx].Size < w.spaces[space].ranges[smallest].Size {
			smallest = idx
		}
	}
	return w.splitOntoOthers(space, smallest, src)
}

// absorb pulls overflow onto dest. Disks at or under the fill limit are left
// where they are; the destination is not filled up to match them.
func (w *world) absorb(dest int) {
	w.arrive(dest)
}

// BalanceBytes is the number of bytes to move from the fuller disk onto the
// emptier one so the emptier gains half their fill-percentage gap. Fill is
// the fraction used/cap on each disk, and half that gap times the emptier
// capacity is the byte count. It is zero when the gap is below SPREAD_POINTS.
func BalanceBytes(usedHi, capHi, usedLo, capLo int64) int64 {
	if capHi <= 0 || capLo <= 0 || usedHi <= 0 || usedLo < 0 {
		return 0
	}
	hi := float64(usedHi) / float64(capHi)
	lo := float64(usedLo) / float64(capLo)
	gap := hi - lo
	// A ratio of two int64s can sit an ulp under an exact percentage.
	if gap*100 < float64(SPREAD_POINTS)-1e-9 {
		return 0
	}
	nFloat := gap / 2 * float64(capLo)
	if nFloat <= 0 {
		return 0
	}
	n := usedHi
	if nFloat < float64(math.MaxInt64) && nFloat < float64(usedHi) {
		n = int64(math.Round(nFloat))
	}
	if free := capLo - usedLo; free < n {
		n = free
	}
	if n < 0 {
		return 0
	}
	return n
}

// balance moves half the widest fill gap onto the emptier disk. A gap under
// SPREAD_POINTS moves nothing, and a vacating disk is neither end.
func (w *world) balance() {
	hi, lo, budget := /* world.balancePair */ func() (hi, lo int, budget int64) {
		hi, lo = -1, -1
		for _, a := range w.activeDisks() {
			if w.disks[a] <= 0 {
				continue
			}
			for _, b := range w.activeDisks() {
				if a == b || w.disks[b] <= 0 {
					continue
				}
				if ! /* fuller */ func(usedA, capA, usedB, capB int64) bool {
					if capA <= 0 || capB <= 0 {
						return false
					}
					left := new(big.Int).Mul(big.NewInt(usedA), big.NewInt(capB))
					right := new(big.Int).Mul(big.NewInt(usedB), big.NewInt(capA))
					return left.Cmp(right) > 0
				}(w.used[a], w.disks[a], w.used[b], w.disks[b]) {
					continue
				}
				n := BalanceBytes(w.used[a], w.disks[a], w.used[b], w.disks[b])
				if n > budget {
					hi, lo, budget = a, b, n
				}
			}
		}
		return hi, lo, budget
	}()
	if budget <= 0 {
		return
	}
	totalRanges := 0
	for _, sp := range w.spaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges*MAX_RANGES_PER_DISK+len(w.disks)+8; guard++ {
		if budget <= 0 {
			return
		}
		cut, ok := /* world.bestTake */ func(src, dest int, budget int64) (candidate, bool) {
			var best *candidate
			for s := range w.spaces {
				for _, idx := range w.rangeIndexes(s, src) {
					for _, c := range w.sizedCuts(s, idx, budget, budget, dest) {
						c.dest = dest
						if best == nil || betterSteal(c, *best, budget) {
							cp := c
							best = &cp
						}
					}
				}
			}
			if best == nil {
				return candidate{}, false
			}
			return *best, true
		}(hi, lo, budget)
		if !ok {
			return
		}
		if !w.applyCut(cut.space, cut.idx, cut.kind, lo, cut.size, cut.split) {
			return
		}
		budget -= cut.size
	}
}

func (w *world) arrive(newDisk int) {
	if w.fillHeadroom(newDisk) <= 0 {
		return
	}
	totalRanges := 0
	for _, sp := range w.spaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges*MAX_RANGES_PER_DISK+len(w.disks)+8; guard++ {
		if w.overflow(newDisk) == 0 && ! /* world.anyOverflow */ func(except int) bool {
			for _, d := range w.activeDisks() {
				if d != except && w.overflow(d) > 0 {
					return true
				}
			}
			return false
		}(newDisk) {
			return
		}
		cut, ok := /* world.bestSteal */ func(newDisk int) (candidate, bool) {
			if c, ok := w.pickSteal(newDisk, true); ok {
				return c, true
			}
			return w.pickSteal(newDisk, false)
		}(newDisk)
		if !ok {
			return
		}
		w.applyCut(cut.space, cut.idx, cut.kind, newDisk, cut.size, cut.split)
	}
}

func (w *world) pickSteal(newDisk int, absorbOnly bool) (candidate, bool) {
	need := w.fillHeadroom(newDisk)
	if need <= 0 {
		return candidate{}, false
	}
	var best *candidate
	for _, src := range w.activeDisks() {
		if src == newDisk {
			continue
		}
		over := w.overflow(src)
		if over <= 0 {
			continue
		}
		for s := range w.spaces {
			for _, idx := range w.rangeIndexes(s, src) {
				for _, c := range w.sizedCuts(s, idx, need, over, newDisk) {
					if absorbOnly && c.dlt > 0 {
						continue
					}
					c.over = over
					c.dest = newDisk
					if best == nil || betterSteal(c, *best, need) {
						cp := c
						best = &cp
					}
				}
			}
		}
	}
	if best == nil {
		return candidate{}, false
	}
	return *best, true
}

func (w *world) sizedCuts(space, idx int, need, limit int64, dest int) []candidate {
	sz := w.spaces[space].ranges[idx].Size
	if sz <= 0 {
		return nil
	}
	var out []candidate
	add := func(kind int, size int64) {
		if size <= 0 || size > sz {
			return
		}
		var split []byte
		if kind != cutWhole {
			split, size = /* world.cutActual */ func(space, idx, kind int, want int64) ([]byte, int64) {
				split, moved, _, _ := w.previewCut(space, idx, kind, want)
				return split, moved
			}(space, idx, kind, size)
			if size <= 0 || size >= sz {
				return
			}
		}
		if !w.canTake(space, idx, kind, dest, size) {
			return
		}
		if size > w.fillHeadroom(dest) {
			return
		}
		out = append(out, candidate{
			space: space,
			idx:   idx,
			kind:  kind,
			dest:  dest,
			size:  size,
			dlt:   w.destDelta(space, idx, kind, dest),
			split: split,
		})
	}
	if limit <= 0 || sz <= limit {
		add(cutWhole, sz)
	}
	want := need
	if limit > 0 && limit < want {
		want = limit
	}
	if destHead := w.fillHeadroom(dest); destHead < want {
		want = destHead
	}
	if want > 0 && want < sz {
		add(cutPrefix, want)
		add(cutSuffix, want)
	}
	return out
}

func betterSteal(a, b candidate, need int64) bool {
	aFit, bFit := a.size <= need, b.size <= need
	if aFit != bFit {
		return aFit
	}
	aKeep, bKeep := a.size <= a.over, b.size <= b.over
	if aKeep != bKeep {
		return aKeep
	}
	aAbs, bAbs := a.dlt <= 0, b.dlt <= 0
	if aAbs != bAbs {
		return aAbs
	}
	if a.size != b.size {
		if aFit {
			return a.size > b.size
		}
		return a.size < b.size
	}
	if a.over != b.over {
		return a.over > b.over
	}
	if a.dlt != b.dlt {
		return a.dlt < b.dlt
	}
	if a.space != b.space {
		return a.space < b.space
	}
	if a.idx != b.idx {
		return a.idx < b.idx
	}
	return a.kind < b.kind
}

func (w *world) shed(full int) error {
	if w.frozen[full] {
		return nil
	}
	totalRanges := 0
	for _, sp := range w.spaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges+4; guard++ {
		need := w.overflow(full)
		if need <= 0 {
			return nil
		}
		cut, ok := w.bestShed(full, need)
		if !ok {
			if w.makeRoom(full) {
				continue
			}
			if w.used[full] > w.disks[full] {
				return xerrors.Errorf("cannot shed %d bytes from disk %d", need, full)
			}
			return nil
		}
		w.applyCut(cut.space, cut.idx, cut.kind, cut.dest, cut.size, cut.split)
	}
	if w.used[full] > w.disks[full] {
		return xerrors.Errorf("cannot shed enough from disk %d", full)
	}
	return nil
}

func (w *world) bestShed(full int, need int64) (candidate, bool) {
	var best *candidate
	for s := range w.spaces {
		for _, idx := range w.rangeIndexes(s, full) {
			for _, dest := range w.activeDisks() {
				if dest == full {
					continue
				}
				for _, c := range w.sizedCuts(s, idx, need, w.spaces[s].ranges[idx].Size, dest) {
					if best == nil || /* betterShed */ func(a, b candidate, need int64) bool {
						aCov, bCov := a.size >= need, b.size >= need
						if aCov != bCov {
							return aCov
						}
						if a.size != b.size {
							if aCov {
								return a.size < b.size
							}
							return a.size > b.size
						}
						if a.dlt != b.dlt {
							return a.dlt < b.dlt
						}
						if a.dest != b.dest {
							return a.dest < b.dest
						}
						if a.space != b.space {
							return a.space < b.space
						}
						return a.idx < b.idx
					}(c, *best, need) {
						cp := c
						best = &cp
					}
				}
			}
		}
	}
	if best == nil {
		return candidate{}, false
	}
	return *best, true
}

func (w *world) vacate(id int) error {
	w.frozen[id] = true
	if err := /* world.reassignEmptyRanges */ func(id int) error {
		for s := range w.spaces {
			for {
				idx := -1
				sp := &w.spaces[s]
				for i, o := range sp.owner {
					if o == id && sp.ranges[i].Size == 0 {
						idx = i
						break
					}
				}
				if idx < 0 {
					break
				}
				dest, ok := /* world.emptyNeighbor */ func(space, idx, src int) (int, bool) {
					sp := &w.spaces[space]
					n := len(sp.ranges)
					if n == 1 {
						for _, dest := range w.activeDisks() {
							if dest != src {
								return dest, true
							}
						}
						return 0, false
					}
					next := sp.owner[(idx+1)%n]
					if next != src && !w.frozen[next] {
						return next, true
					}
					prev := sp.owner[(idx-1+n)%n]
					if prev != src && !w.frozen[prev] {
						return prev, true
					}
					for _, dest := range w.activeDisks() {
						if dest != src {
							return dest, true
						}
					}
					return 0, false
				}(s, idx, id)
				if !ok {
					return xerrors.Errorf("cannot reassign empty range on disk %d", id)
				}
				w.moveWhole(s, idx, dest)
			}
		}
		return nil
	}(id); err != nil {
		return err
	}
	if w.used[id] == 0 {
		return nil
	}
	if /* world.totalCapacity */ func() int64 {
		var s int64
		for i, cap := range w.disks {
			if !w.frozen[i] {
				s += cap
			}
		}
		return s
	}() < /* world.totalUsed */ func() int64 {
		var s int64
		for _, sp := range w.spaces {
			for _, r := range sp.ranges {
				s += r.Size
			}
		}
		return s
	}() {
		return xerrors.Errorf("not enough remaining capacity to vacate disk %d", id)
	}

	totalRanges := 0
	for _, sp := range w.spaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges+8; guard++ {
		bestSpace, bestIdx := -1, -1
		var bestSize int64 = -1
		for s := range w.spaces {
			for _, idx := range w.rangeIndexes(s, id) {
				sz := w.spaces[s].ranges[idx].Size
				if sz > bestSize || (sz == bestSize && (s < bestSpace || (s == bestSpace && idx < bestIdx))) {
					bestSpace, bestIdx, bestSize = s, idx, sz
				}
			}
		}
		if bestSpace < 0 {
			return nil
		}
		if w.placeRange(bestSpace, bestIdx, id) {
			continue
		}
		if w.makeRoom(id) && w.placeRange(bestSpace, bestIdx, id) {
			continue
		}
		return xerrors.Errorf("cannot place range on disk %d elsewhere", id)
	}
	if w.used[id] != 0 || w.ownsRange(id) {
		return xerrors.Errorf("failed to vacate disk %d", id)
	}
	return nil
}

// reassignEmptyRanges gives each size-0 arc on id to an adjacent owner.
// No bytes move; merge drops the vacated disk's boundary.

func (w *world) placeRange(space, idx, src int) bool {
	sp := &w.spaces[space]
	if idx < 0 || idx >= len(sp.ranges) || sp.owner[idx] != src {
		return true
	}
	sz := sp.ranges[idx].Size
	left, right, okL, okR := /* world.neighbors */ func(space, idx int) (left, right int, okL, okR bool) {
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
	}(space, idx)

	if okL && okR && left == right && w.canTake(space, idx, cutWhole, left, sz) {
		w.moveWhole(space, idx, left)
		return true
	}

	var best *candidate
	consider := func(dest int) {
		if !w.canTake(space, idx, cutWhole, dest, sz) {
			return
		}
		c := candidate{space: space, idx: idx, kind: cutWhole, dest: dest, size: sz, dlt: w.destDelta(space, idx, cutWhole, dest), over: w.free(dest)}
		if best == nil || c.dlt < best.dlt || (c.dlt == best.dlt && (c.over > best.over || (c.over == best.over && c.dest < best.dest))) {
			cp := c
			best = &cp
		}
	}
	if okL {
		consider(left)
	}
	if okR {
		consider(right)
	}
	for _, dest := range w.activeDisks() {
		consider(dest)
	}
	if best != nil {
		w.moveWhole(space, idx, best.dest)
		return true
	}

	if okL && okR && left != right {
		end := cloneHash(sp.ranges[idx].EndHash)
		pref := min(w.free(left), sz)
		if pref > 0 && w.canTake(space, idx, cutPrefix, left, pref) {
			if pref >= sz {
				w.moveWhole(space, idx, left)
				return true
			}
			w.applyCut(space, idx, cutPrefix, left, pref, nil)
			idx = w.find(space, end)
			if idx >= 0 && sp.owner[idx] == src && w.canTake(space, idx, cutWhole, right, sp.ranges[idx].Size) {
				w.moveWhole(space, idx, right)
				return true
			}
		}
	}

	return w.splitOntoOthers(space, idx, src)
}

func (w *world) splitOntoOthers(space, idx, src int) bool {
	sp := &w.spaces[space]
	if idx < 0 || idx >= len(sp.ranges) || sp.owner[idx] != src {
		return true
	}
	end := cloneHash(sp.ranges[idx].EndHash)
	for w.find(space, end) >= 0 && sp.owner[w.find(space, end)] == src {
		idx = w.find(space, end)
		remain := sp.ranges[idx].Size
		var best *candidate
		for _, dest := range w.activeDisks() {
			if dest == src {
				continue
			}
			take := min(remain, w.free(dest))
			for _, kind := range []int{cutPrefix, cutWhole} {
				sz := take
				if kind == cutWhole {
					sz = remain
				}
				if sz <= 0 || !w.canTake(space, idx, kind, dest, sz) {
					continue
				}
				c := candidate{space: space, idx: idx, kind: kind, dest: dest, size: sz, dlt: w.destDelta(space, idx, kind, dest)}
				if best == nil || c.dlt < best.dlt || (c.dlt == best.dlt && (c.size > best.size || (c.size == best.size && c.dest < best.dest))) {
					cp := c
					best = &cp
				}
			}
		}
		if best == nil {
			return false
		}
		if !w.applyCut(best.space, best.idx, best.kind, best.dest, best.size, best.split) {
			return false
		}
	}
	return w.find(space, end) < 0 || sp.owner[w.find(space, end)] != src
}

func (w *world) makeRoom(avoid int) bool {
	for _, src := range w.activeDisks() {
		if src == avoid {
			continue
		}
		for s := range w.spaces {
			if w.rangeCount(s, src) >= MAX_RANGES_PER_DISK && w.donateRange(s, src) {
				return true
			}
		}
	}
	fullest := -1
	for _, src := range w.activeDisks() {
		if src == avoid || w.used[src] == 0 {
			continue
		}
		if fullest < 0 || w.used[src] > w.used[fullest] || (w.used[src] == w.used[fullest] && src < fullest) {
			fullest = src
		}
	}
	if fullest < 0 {
		return false
	}
	cut, ok := w.bestShed(fullest, 1)
	if !ok {
		return false
	}
	return w.applyCut(cut.space, cut.idx, cut.kind, cut.dest, cut.size, cut.split)
}
