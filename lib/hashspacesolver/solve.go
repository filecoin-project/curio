package hashspacesolver

import (
	"bytes"
	"math"
	"math/big"
	"slices"

	"golang.org/x/xerrors"
)

// Solve plans one event: arrive, fill, vacate, absorb, or balance. The
// finished assignment keeps every active disk's FreeSpace at or above zero
// and at or under MAX_RANGES_PER_DISK ranges per space. Planning is greedy.
//
// A space may loop: FF00 - AABB includes AAAA, not BBBB.
// Cuts may come from either hash space.
//
// Result.State is the finished layout. Result.Diff moves each byte at most
// once, from the disk that owned it at the start to the disk that holds it
// at the end. The list is order-independent.
func Solve(state State, event Event) (Result, error) {
	w, err := newWorld(state)
	if err != nil {
		return Result{}, err
	}
	var disk int
	if event.Kind != EventBalance {
		var ok bool
		disk, ok = w.mountOf[event.StorageID]
		if !ok {
			return Result{}, xerrors.Errorf("unknown event StorageID %q", event.StorageID)
		}
	}

	switch event.Kind {
	case EventArrive, EventAbsorb:
		if err := w.repair(); err != nil {
			return Result{}, err
		}
		// Steal from disks above the fill limit until newDisk reaches its
		// rebalance target or no legal cut remains.
		if w.targetHeadroom(disk) > 0 {
			var sources []int
			for _, d := range w.activeDisks() {
				if d != disk && w.overflow(d) > 0 {
					sources = append(sources, d)
				}
			}
			totalRanges := 0
			for _, sp := range w.hashSpaces {
				totalRanges += len(sp.ranges)
			}
			for guard := 0; guard < totalRanges*MAX_RANGES_PER_DISK+len(w.mountPoints)+8; guard++ {
				cut, okCut := w.pickSteal(disk, sources, true)
				if !okCut {
					cut, okCut = w.pickSteal(disk, sources, false)
				}
				if !okCut {
					break
				}
				w.applyCut(cut.space, cut.idx, cut.kind, disk, cut.size, cut.split)
			}
		}
	case EventFull:
		if err := w.repair(); err != nil {
			return Result{}, err
		}
		if w.mountPoints[disk].frozen {
			return Result{}, xerrors.Errorf("cannot shed from vacated mountpoint %s", w.mountPoints[disk].StorageID)
		}
		// Stopping with no legal cut is not an error, so the disk may stay
		// above its rebalance target. A disk already at or under the fill
		// limit is left as it is.
		if w.overflow(disk) > 0 {
			totalRanges := 0
			for _, sp := range w.hashSpaces {
				totalRanges += len(sp.ranges)
			}
			for guard := 0; guard < totalRanges+4; guard++ {
				need := w.excess(disk)
				if need <= 0 {
					break
				}
				cut, okCut := w.bestShed(disk, need)
				if !okCut {
					if w.makeRoom(disk) {
						continue
					}
					break
				}
				w.applyCut(cut.space, cut.idx, cut.kind, cut.dest, cut.size, cut.split)
			}
		}
	case EventBalance:
		if err := w.repair(); err != nil {
			return Result{}, err
		}
		w.balance()
	case EventVacate:
		w.mountPoints[disk].frozen = true
		for s := range w.hashSpaces {
			for {
				idx := -1
				sp := &w.hashSpaces[s]
				for ri, r := range sp.ranges {
					if w.diskOf(r) == disk && r.Size == 0 {
						idx = ri
						break
					}
				}
				if idx < 0 {
					break
				}
				// A circle of one range goes to any other active disk.
				// Otherwise the next owner, unless that is the source, then
				// the previous owner. No bytes move.
				n := len(sp.ranges)
				dest, found := 0, false
				if n == 1 {
					for _, d := range w.activeDisks() {
						if d != disk {
							dest, found = d, true
							break
						}
					}
				} else {
					next := w.diskOf(sp.ranges[(idx+1)%n])
					if next != disk {
						dest, found = next, true
					} else if prev := w.diskOf(sp.ranges[(idx-1+n)%n]); prev != disk {
						dest, found = prev, true
					}
				}
				if !found {
					return Result{}, xerrors.Errorf("cannot reassign empty range on mountpoint %s", w.mountPoints[disk].StorageID)
				}
				w.moveWhole(s, idx, dest)
			}
		}
		if w.mountPoints[disk].used != 0 {
			var used int64
			for _, sp := range w.hashSpaces {
				for _, r := range sp.ranges {
					used += r.Size
				}
			}
			var capacity int64
			for _, mp := range w.mountPoints {
				if !mp.frozen {
					capacity += mp.capacity
				}
			}
			if capacity < used {
				return Result{}, xerrors.Errorf("not enough remaining capacity to vacate mountpoint %s", w.mountPoints[disk].StorageID)
			}
			totalRanges := 0
			for _, sp := range w.hashSpaces {
				totalRanges += len(sp.ranges)
			}
			emptied := false
			for guard := 0; guard < totalRanges+8; guard++ {
				bestSpace, bestIdx := -1, -1
				var bestSize int64 = -1
				for s := range w.hashSpaces {
					for _, idx := range w.rangeIndexes(s, disk) {
						sz := w.hashSpaces[s].ranges[idx].Size
						if sz > bestSize || (sz == bestSize && (s < bestSpace || (s == bestSpace && idx < bestIdx))) {
							bestSpace, bestIdx, bestSize = s, idx, sz
						}
					}
				}
				if bestSpace < 0 {
					emptied = true
					break
				}
				if w.placeRange(bestSpace, bestIdx, disk) {
					continue
				}
				if w.makeRoom(disk) && w.placeRange(bestSpace, bestIdx, disk) {
					continue
				}
				return Result{}, xerrors.Errorf("cannot place range on mountpoint %s elsewhere", w.mountPoints[disk].StorageID)
			}
			if !emptied && (w.mountPoints[disk].used != 0 || w.ownsRange(disk)) {
				return Result{}, xerrors.Errorf("failed to vacate mountpoint %s", w.mountPoints[disk].StorageID)
			}
		}
	default:
		return Result{}, xerrors.Errorf("unknown event kind %d", event.Kind)
	}

	if err := w.repair(); err != nil {
		return Result{}, err
	}
	if err := w.checkLimits(); err != nil {
		return Result{}, err
	}
	if event.Kind == EventVacate && (w.mountPoints[disk].used != 0 || w.ownsRange(disk)) {
		return Result{}, xerrors.Errorf("mountpoint %s was not emptied", w.mountPoints[disk].StorageID)
	}

	var moved []Transfer
	for s, sp := range w.hashSpaces {
		for i := range sp.ranges {
			if len(sp.layouts[i]) == 0 {
				continue
			}
			prev := w.startHash(s, i)
			for _, lay := range sp.layouts[i] {
				if !hashEq(lay.startHash, prev) {
					return Result{}, xerrors.Errorf("space %d range %d layout starts at %x, previous piece ends at %x", s, i, lay.startHash, prev)
				}
				if lay.size >= 0 && lay.mountPointIdx != w.diskOf(sp.ranges[i]) {
					moved = append(moved, Transfer{
						Space:     s,
						StartHash: cloneHash(lay.startHash),
						EndHash:   cloneHash(lay.endHash),
						From:      w.mountPoints[lay.mountPointIdx].StorageID,
						To:        sp.ranges[i].StorageID,
						Size:      lay.size,
					})
				}
				prev = lay.endHash
			}
			if !hashEq(prev, sp.ranges[i].EndHash) {
				return Result{}, xerrors.Errorf("space %d range %d layout ends at %x, range ends at %x", s, i, prev, sp.ranges[i].EndHash)
			}
		}
	}
	diff := mergeTransfers(moved)
	var bytes int64
	for _, t := range diff {
		bytes += t.Size
	}
	return Result{State: w.snapshot(), Diff: diff, BytesMoved: bytes}, nil
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

// fuller reports whether a's used/cap fraction is greater than b's.
// A non-positive capacity is not fuller. The products are compared exactly,
// so a used*cap that does not fit in int64 still orders correctly.
func fuller(usedA, capA, usedB, capB int64) bool {
	if capA <= 0 || capB <= 0 {
		return false
	}
	left := new(big.Int).Mul(big.NewInt(usedA), big.NewInt(capB))
	right := new(big.Int).Mul(big.NewInt(usedB), big.NewInt(capA))
	return left.Cmp(right) > 0
}

// balance moves half the widest fill gap onto the emptier disk. A gap under
// SPREAD_POINTS moves nothing, and a frozen disk is neither end. The
// destination is not filled past its rebalance target.
func (w *world) balance() {
	hi, lo := -1, -1
	var budget int64
	for _, a := range w.activeDisks() {
		capA := w.mountPoints[a].capacity
		usedA := w.mountPoints[a].used
		if capA <= 0 || usedA <= 0 {
			continue
		}
		for _, b := range w.activeDisks() {
			capB := w.mountPoints[b].capacity
			if a == b || capB <= 0 {
				continue
			}
			usedB := w.mountPoints[b].used
			if !fuller(usedA, capA, usedB, capB) {
				continue
			}
			n := BalanceBytes(usedA, capA, usedB, capB)
			if n > budget {
				hi, lo, budget = a, b, n
			}
		}
	}
	if budget <= 0 {
		return
	}
	totalRanges := 0
	for _, sp := range w.hashSpaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges*MAX_RANGES_PER_DISK+len(w.mountPoints)+8; guard++ {
		if budget <= 0 {
			return
		}
		cut, ok := w.bestTake(hi, lo, budget)
		if !ok || !w.applyCut(cut.space, cut.idx, cut.kind, lo, cut.size, cut.split) {
			return
		}
		budget -= cut.size
	}
}

// bestTake is the preferred cut of at most budget bytes from src onto dest.
// Returns false when no cut fits. src and dest must be mount indexes.
func (w *world) bestTake(src, dest int, budget int64) (candidate, bool) {
	var best *candidate
	for s := range w.hashSpaces {
		for _, idx := range w.rangeIndexes(s, src) {
			for _, c := range w.sizedCuts(s, idx, budget, budget, dest) {
				c.dest = dest
				// Prefer a cut that adds no range, then a larger cut, then a
				// smaller range-count change, then a lower space, idx, and kind.
				keep := best == nil
				if !keep {
					a, b := c, *best
					aAbs, bAbs := a.dlt <= 0, b.dlt <= 0
					if aAbs != bAbs {
						keep = aAbs
					} else if a.size != b.size {
						keep = a.size > b.size
					} else if a.dlt != b.dlt {
						keep = a.dlt < b.dlt
					} else if a.space != b.space {
						keep = a.space < b.space
					} else if a.idx != b.idx {
						keep = a.idx < b.idx
					} else {
						keep = a.kind < b.kind
					}
				}
				if keep {
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
}

// Validate reports whether state is structurally sound and each disk is
// within the per-space range cap.
func Validate(state State) error {
	w, err := newWorld(state)
	if err != nil {
		return err
	}
	return w.checkLimits()
}

// Apply returns a copy of state with all transfers applied. Transfers may be
// applied in any order when they are disjoint within a space. A negative
// size is skipped. An empty transfer, or one that covers a whole range,
// moves that range. Anything else must be a prefix, a suffix, or a strict
// middle of one range, and Size must match the layout bytes in the interval.
// Free space and the range cap are not checked.
func Apply(state State, diff []Transfer) (State, error) {
	w, err := newWorld(state)
	if err != nil {
		return State{}, err
	}
	for i, t := range diff {
		if t.Space < 0 || t.Space >= len(w.hashSpaces) {
			return State{}, xerrors.Errorf("transfer %d: unknown space %d", i, t.Space)
		}
		to, ok := w.mountOf[t.To]
		if !ok {
			return State{}, xerrors.Errorf("transfer %d: unknown dest StorageID %q", i, t.To)
		}
		if t.Size < 0 {
			continue
		}
		sp := &w.hashSpaces[t.Space]
		idx := -1
		for ri, r := range sp.ranges {
			inside := false
			if hashEq(t.StartHash, t.EndHash) {
				inside = hashEq(r.StartHash, r.EndHash) && hashEq(r.StartHash, t.StartHash)
			} else {
				okStart := hashEq(t.StartHash, r.StartHash) || pointInArc(r.StartHash, r.EndHash, t.StartHash)
				okEnd := hashEq(t.EndHash, r.EndHash) || pointInArc(r.StartHash, r.EndHash, t.EndHash)
				inside = okStart && okEnd
			}
			if inside {
				idx = ri
				break
			}
		}
		if idx < 0 {
			return State{}, xerrors.Errorf("transfer %d: no range covering (%x, %x]", i, t.StartHash, t.EndHash)
		}
		r := sp.ranges[idx]
		if t.From != "" && r.StorageID != t.From {
			return State{}, xerrors.Errorf("transfer %d: interval owned by %s, want %s", i, r.StorageID, t.From)
		}
		start := r.StartHash
		if t.Size == 0 {
			if !hashEq(t.StartHash, start) || !hashEq(t.EndHash, r.EndHash) {
				return State{}, xerrors.Errorf("transfer %d: empty transfer must cover a whole range", i)
			}
			w.moveWholeSilent(t.Space, idx, to)
			continue
		}
		if hashEq(t.StartHash, start) && hashEq(t.EndHash, r.EndHash) {
			w.moveWholeSilent(t.Space, idx, to)
			continue
		}
		if hashEq(t.StartHash, start) {
			from := w.diskOf(sp.ranges[idx])
			if t.Size >= sp.ranges[idx].Size {
				w.moveWholeSilent(t.Space, idx, to)
				continue
			}
			bound := w.startHash(t.Space, idx)
			head, tail, okSplit := splitLayoutsAt(sp.layouts[idx], bound, t.EndHash, t.Size)
			if !okSplit {
				return State{}, xerrors.Errorf("transfer %d: prefix split (%x, %x] size %d", i, bound, t.EndHash, t.Size)
			}
			sp.ranges[idx].StartHash = cloneHash(t.EndHash)
			sp.ranges[idx].Size -= t.Size
			sp.layouts[idx] = tail
			w.mountPoints[from].used -= t.Size
			w.insert(t.Space, idx, Range{StartHash: bound, EndHash: cloneHash(t.EndHash), Size: t.Size}, to, head)
			w.mergeSpace(t.Space)
			continue
		}
		if hashEq(t.EndHash, r.EndHash) {
			from := w.diskOf(sp.ranges[idx])
			rr := sp.ranges[idx]
			if t.Size >= rr.Size {
				w.moveWholeSilent(t.Space, idx, to)
				continue
			}
			bound := w.startHash(t.Space, idx)
			kept := rr.Size - t.Size
			head, tail, okSplit := splitLayoutsAt(sp.layouts[idx], bound, t.StartHash, kept)
			if !okSplit {
				return State{}, xerrors.Errorf("transfer %d: suffix split (%x, %x] size %d", i, t.StartHash, rr.EndHash, t.Size)
			}
			end := cloneHash(rr.EndHash)
			sp.ranges[idx].EndHash = cloneHash(t.StartHash)
			sp.ranges[idx].Size = kept
			sp.layouts[idx] = head
			w.mountPoints[from].used -= t.Size
			w.insert(t.Space, idx+1, Range{StartHash: cloneHash(t.StartHash), EndHash: end, Size: t.Size}, to, tail)
			w.mergeSpace(t.Space)
			continue
		}
		left, okL := layoutBytesTo(sp.layouts[idx], start, t.StartHash)
		endOff, okR := layoutBytesTo(sp.layouts[idx], start, t.EndHash)
		if !okL || !okR {
			return State{}, xerrors.Errorf("transfer %d: transfer (%x, %x] is not inside one range", i, t.StartHash, t.EndHash)
		}
		if endOff-left != t.Size {
			return State{}, xerrors.Errorf("transfer %d: transfer (%x, %x] size %d, range holds %d there", i, t.StartHash, t.EndHash, t.Size, endOff-left)
		}
		if left <= 0 || left+t.Size >= r.Size {
			return State{}, xerrors.Errorf("transfer %d: transfer must be a prefix, suffix, or whole of one range", i)
		}
		from := w.diskOf(sp.ranges[idx])
		rr := sp.ranges[idx]
		right := rr.Size - left - t.Size
		if left <= 0 || t.Size <= 0 || right <= 0 {
			return State{}, xerrors.Errorf("transfer %d: middle split left %d mid %d right %d", i, left, t.Size, right)
		}
		bound := w.startHash(t.Space, idx)
		head, rest, okSplit := splitLayoutsAt(sp.layouts[idx], bound, t.StartHash, left)
		if !okSplit {
			return State{}, xerrors.Errorf("transfer %d: middle split at %x", i, t.StartHash)
		}
		midLayouts, tail, okSplit := splitLayoutsAt(rest, t.StartHash, t.EndHash, t.Size)
		if !okSplit {
			return State{}, xerrors.Errorf("transfer %d: middle split at %x", i, t.EndHash)
		}
		sp.ranges[idx].StartHash = cloneHash(t.EndHash)
		sp.ranges[idx].Size = right
		sp.layouts[idx] = tail
		w.mountPoints[from].used -= t.Size
		w.insert(t.Space, idx, Range{StartHash: cloneHash(t.StartHash), EndHash: cloneHash(t.EndHash), Size: t.Size}, to, midLayouts)
		sp.ranges = slices.Insert(sp.ranges, idx, Range{StartHash: bound, EndHash: cloneHash(t.StartHash), Size: left, StorageID: w.mountPoints[from].StorageID})
		sp.layouts = slices.Insert(sp.layouts, idx, head)
		w.mergeSpace(t.Space)
	}
	return w.snapshot(), nil
}

// pointInArc reports whether p lies strictly after start and at or before
// end, wrapping when the arc crosses the top of the circle. On a full
// circle (start == end) every point except start is inside. The hashes
// are compared as raw bytes, so they should share a length.
func pointInArc(start, end, p []byte) bool {
	if hashEq(start, end) {
		return !hashEq(p, start)
	}
	if bytes.Compare(start, end) < 0 {
		return bytes.Compare(start, p) < 0 && bytes.Compare(p, end) <= 0
	}
	return bytes.Compare(start, p) < 0 || bytes.Compare(p, end) <= 0
}

// moveWholeSilent assigns ranges[idx] to dest the same way moveWhole does.
// Apply uses it for a transfer that already covers a whole range, so the
// diff is not rebuilt here. Free space, the range cap, and frozen are not
// checked. space, idx, and dest must be in range.
func (w *world) moveWholeSilent(space, idx, dest int) {
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

// mergeTransfers drops a transfer with a negative size or with From equal
// to To, then joins any remaining pair that shares space, From, and To and
// meets at a hash. Call it on the transfers collected from the resulting
// layout. Input order does not matter.
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
				a, b := out[i], out[j]
				if a.Size == 0 || b.Size == 0 || a.Space != b.Space || a.From != b.From || a.To != b.To {
					continue
				}
				switch {
				case hashEq(a.EndHash, b.StartHash):
					a.EndHash = cloneHash(b.EndHash)
					a.Size += b.Size
				case hashEq(b.EndHash, a.StartHash):
					a.StartHash = cloneHash(b.StartHash)
					a.Size += b.Size
				default:
					continue
				}
				out[i] = a
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

// checkLimits reports whether every active disk holds at most
// MAX_RANGES_PER_DISK ranges in each space. Frozen disks are skipped.
// Free space cannot go negative: capacity is the drive's FreeSpace plus
// the hash-space bytes already on it, and a cut never places more than
// free. The world must already be a valid tiling.
func (w *world) checkLimits() error {
	for _, d := range w.activeDisks() {
		for s := range w.hashSpaces {
			if rc := w.rangeCount(s, d); rc > MAX_RANGES_PER_DISK {
				return xerrors.Errorf("mountpoint %s space %d holds %d ranges, max %d", w.mountPoints[d].StorageID, s, rc, MAX_RANGES_PER_DISK)
			}
		}
	}
	return nil
}

// candidate is one proposed cut. idx is the range in space, kind is
// cutWhole, cutPrefix, or cutSuffix, and dest is the mount that would
// receive size bytes. dlt is dest's range-count change from destDelta.
// over is the source excess used when ranking a steal. split is the hash
// boundary for a partial cut and is empty for a whole move.
type candidate struct {
	space, idx, dest int
	kind             cutKind
	size             int64
	dlt              int
	over             int64
	split            []byte
}

// repair moves ranges off active disks that exceed MAX_RANGES_PER_DISK
// until each is within the cap. It returns an error when a pass cannot
// lower a count. Call it before handling an arrive or a full disk, and
// again before the end check. Frozen disks are left alone.
func (w *world) repair() error {
	totalRanges := 0
	for _, sp := range w.hashSpaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges+len(w.mountPoints)+8; guard++ {
		type overKey struct{ space, disk int }
		var over []overKey
		for _, d := range w.activeDisks() {
			for s := range w.hashSpaces {
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

// donateRange moves one range in space off src so src's range count drops.
// It prefers the smallest range an active disk can take whole, and
// otherwise splits the smallest range. Returns false when src is already
// within MAX_RANGES_PER_DISK or no legal cut exists. space must index a
// hash space and src a mount index.
func (w *world) donateRange(space, src int) bool {
	idxs := w.rangeIndexes(space, src)
	if len(idxs) <= MAX_RANGES_PER_DISK {
		return false
	}
	var best *candidate
	for _, idx := range idxs {
		sz := w.hashSpaces[space].ranges[idx].Size
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
		if w.hashSpaces[space].ranges[idx].Size < w.hashSpaces[space].ranges[smallest].Size {
			smallest = idx
		}
	}
	return w.splitOntoOthers(space, smallest, src)
}

// pickSteal searches sources for a cut onto newDisk of at most the
// source's excess and newDisk's remaining room under the rebalance target.
// When absorbOnly is set, a cut that would add a range is skipped.
// Returns false when newDisk has no such room or no cut qualifies.
// newDisk and each source must be mount indexes.
func (w *world) pickSteal(newDisk int, sources []int, absorbOnly bool) (candidate, bool) {
	need := w.targetHeadroom(newDisk)
	if need <= 0 {
		return candidate{}, false
	}
	var best *candidate
	for _, src := range sources {
		over := w.excess(src)
		if over <= 0 {
			continue
		}
		for s := range w.hashSpaces {
			for _, idx := range w.rangeIndexes(s, src) {
				for _, c := range w.sizedCuts(s, idx, need, over, newDisk) {
					if absorbOnly && c.dlt > 0 {
						continue
					}
					c.over = over
					c.dest = newDisk
					// Prefer a cut that fits need, then one that leaves the source
					// at or above its rebalance target, then one that adds no range.
					// Among fits, the larger cut wins; otherwise the smaller. Ties
					// prefer more source excess, a smaller range-count change, then
					// a lower space, idx, and kind.
					keep := best == nil
					if !keep {
						a, b := c, *best
						aFit, bFit := a.size <= need, b.size <= need
						if aFit != bFit {
							keep = aFit
						} else if aKeep, bKeep := a.size <= a.over, b.size <= b.over; aKeep != bKeep {
							keep = aKeep
						} else if aAbs, bAbs := a.dlt <= 0, b.dlt <= 0; aAbs != bAbs {
							keep = aAbs
						} else if a.size != b.size {
							if aFit {
								keep = a.size > b.size
							} else {
								keep = a.size < b.size
							}
						} else if a.over != b.over {
							keep = a.over > b.over
						} else if a.dlt != b.dlt {
							keep = a.dlt < b.dlt
						} else if a.space != b.space {
							keep = a.space < b.space
						} else if a.idx != b.idx {
							keep = a.idx < b.idx
						} else {
							keep = a.kind < b.kind
						}
					}
					if keep {
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

// sizedCuts lists whole, prefix, and suffix cuts of ranges[idx] onto dest
// that fit in need, in limit, and in dest's room under the rebalance
// target. A whole cut is included when limit is non-positive or the range
// is no larger than limit. An empty range yields nothing. space and idx
// must index a range and dest a mount index. The world is not modified.
func (w *world) sizedCuts(space, idx int, need, limit int64, dest int) []candidate {
	sz := w.hashSpaces[space].ranges[idx].Size
	if sz <= 0 {
		return nil
	}
	var out []candidate
	add := func(kind cutKind, size int64) {
		if size <= 0 || size > sz {
			return
		}
		var split []byte
		if kind != cutWhole {
			split, size, _, _ = w.previewCut(space, idx, kind, size)
			if size <= 0 || size >= sz {
				return
			}
		}
		if !w.canTake(space, idx, kind, dest, size) {
			return
		}
		if size > w.targetHeadroom(dest) {
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
	if destHead := w.targetHeadroom(dest); destHead < want {
		want = destHead
	}
	if want > 0 && want < sz {
		add(cutPrefix, want)
		add(cutSuffix, want)
	}
	return out
}

// bestShed is the preferred cut of about need bytes off full onto any
// other active disk. Returns false when no cut is legal. full must be a
// mount index. need is how far full sits above its rebalance target;
// makeRoom passes 1 to free any legal byte.
func (w *world) bestShed(full int, need int64) (candidate, bool) {
	var best *candidate
	for s := range w.hashSpaces {
		for _, idx := range w.rangeIndexes(s, full) {
			for _, dest := range w.activeDisks() {
				if dest == full {
					continue
				}
				for _, c := range w.sizedCuts(s, idx, need, w.hashSpaces[s].ranges[idx].Size, dest) {
					// Prefer a cut that covers need. Among covers, the smaller
					// cut wins; otherwise the larger. Ties prefer a smaller
					// range-count change, then a lower dest, space, and idx.
					keep := best == nil
					if !keep {
						a, b := c, *best
						aCov, bCov := a.size >= need, b.size >= need
						if aCov != bCov {
							keep = aCov
						} else if a.size != b.size {
							if aCov {
								keep = a.size < b.size
							} else {
								keep = a.size > b.size
							}
						} else if a.dlt != b.dlt {
							keep = a.dlt < b.dlt
						} else if a.dest != b.dest {
							keep = a.dest < b.dest
						} else if a.space != b.space {
							keep = a.space < b.space
						} else {
							keep = a.idx < b.idx
						}
					}
					if keep {
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

// placeRange moves ranges[idx] off src. It prefers a whole move onto a
// neighbor, then any active disk that can take the range, then a split
// between two different neighbors, then splitOntoOthers. A neighbor is the
// adjacent disk when it is not src and not frozen. Returns true when the
// range is already off src or the move landed, and false when no placement
// works. An out-of-range idx counts as already placed. space must index a
// hash space and src a mount index.
func (w *world) placeRange(space, idx, src int) bool {
	sp := &w.hashSpaces[space]
	if idx < 0 || idx >= len(sp.ranges) || w.diskOf(sp.ranges[idx]) != src {
		return true
	}
	sz := sp.ranges[idx].Size
	var left, right int
	var okL, okR bool
	if n := len(sp.ranges); n >= 2 {
		l := w.diskOf(sp.ranges[(idx-1+n)%n])
		rdisk := w.diskOf(sp.ranges[(idx+1)%n])
		if l != src && !w.mountPoints[l].frozen {
			left, okL = l, true
		}
		if rdisk != src && !w.mountPoints[rdisk].frozen {
			right, okR = rdisk, true
		}
	}

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
			if idx >= 0 && w.diskOf(sp.ranges[idx]) == src && w.canTake(space, idx, cutWhole, right, sp.ranges[idx].Size) {
				w.moveWhole(space, idx, right)
				return true
			}
		}
	}

	return w.splitOntoOthers(space, idx, src)
}

// splitOntoOthers cuts the range that still ends at its original EndHash
// off src until src no longer owns that end. Each step takes as much as
// some other active disk can hold, preferring a cut that does not add a
// range and then a larger cut. Returns false when a step cannot be
// applied, and true when idx is stale or the end is already off src.
// space must index a hash space and src a mount index.
func (w *world) splitOntoOthers(space, idx, src int) bool {
	sp := &w.hashSpaces[space]
	if idx < 0 || idx >= len(sp.ranges) || w.diskOf(sp.ranges[idx]) != src {
		return true
	}
	end := cloneHash(sp.ranges[idx].EndHash)
	for w.find(space, end) >= 0 && w.diskOf(sp.ranges[w.find(space, end)]) == src {
		idx = w.find(space, end)
		remain := sp.ranges[idx].Size
		var best *candidate
		for _, dest := range w.activeDisks() {
			if dest == src {
				continue
			}
			take := min(remain, w.free(dest))
			for _, kind := range []cutKind{cutPrefix, cutWhole} {
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
	return w.find(space, end) < 0 || w.diskOf(sp.ranges[w.find(space, end)]) != src
}

// makeRoom frees a fragment slot or a little space on some active disk
// other than avoid, so a later placement can proceed. It first donates a
// range from a disk already at MAX_RANGES_PER_DISK. Otherwise it sheds
// one byte from the fullest other disk that holds data. Returns whether
// the world changed. avoid must be a mount index.
func (w *world) makeRoom(avoid int) bool {
	for _, src := range w.activeDisks() {
		if src == avoid {
			continue
		}
		for s := range w.hashSpaces {
			if w.rangeCount(s, src) >= MAX_RANGES_PER_DISK && w.donateRange(s, src) {
				return true
			}
		}
	}
	fullest := -1
	for _, src := range w.activeDisks() {
		if src == avoid || w.mountPoints[src].used == 0 {
			continue
		}
		if fullest < 0 || w.mountPoints[src].used > w.mountPoints[fullest].used || (w.mountPoints[src].used == w.mountPoints[fullest].used && src < fullest) {
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
