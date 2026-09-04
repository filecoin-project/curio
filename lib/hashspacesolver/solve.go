package hashspacesolver

import (
	"bytes"

	"golang.org/x/xerrors"
)

// Solve computes a minimum-movement target for a disk arriving, filling, or
// vacating. The resulting assignment keeps every active disk at or under
// capacity and at or under MAX_RANGES_PER_DISK contiguous ranges per space.
//
// Cost is lexicographic: fewer bytes moved, then fewer moves. Cuts may come
// from any space. Hash space within each Space is a circle.
//
// Result.State is the finished layout. Result.Diff is an order-independent
// list of absolute interval transfers for delayed application.
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
	default:
		return Result{}, xerrors.Errorf("unknown event kind %d", event.Kind)
	}

	if err := w.repair(); err != nil {
		return Result{}, err
	}
	if err := w.checkSolved(event); err != nil {
		return Result{}, err
	}

	diff := mergeTransfers(w.diff)
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
	if t.Size <= 0 {
		return nil
	}
	sp := &w.spaces[t.Space]
	idx := -1
	for i, r := range sp.ranges {
		start := StartHash(sp.ranges, i)
		if coversInterval(start, r.EndHash, t.StartHash, t.EndHash) {
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
	start := StartHash(sp.ranges, idx)
	if hashEq(t.StartHash, start) && hashEq(t.EndHash, r.EndHash) {
		if t.Size != r.Size && t.Size > 0 {
			// whole-range move; size should match but tolerate after merges
		}
		w.moveWholeSilent(t.Space, idx, t.To)
		return nil
	}
	if hashEq(t.StartHash, start) {
		if t.Size >= r.Size {
			w.moveWholeSilent(t.Space, idx, t.To)
			return nil
		}
		w.splitMovePrefix(t.Space, idx, t.EndHash, t.Size, t.To)
		return nil
	}
	if hashEq(t.EndHash, r.EndHash) {
		if t.Size >= r.Size {
			w.moveWholeSilent(t.Space, idx, t.To)
			return nil
		}
		w.splitMoveSuffix(t.Space, idx, t.StartHash, t.Size, t.To)
		return nil
	}
	return xerrors.Errorf("transfer must be a prefix, suffix, or whole of one range")
}

func coversInterval(rStart, rEnd, tStart, tEnd []byte) bool {
	// Full-circle transfer: start == end means the whole circle.
	if hashEq(tStart, tEnd) {
		return hashEq(rStart, rEnd) && hashEq(rStart, tStart)
	}
	okStart := hashEq(tStart, rStart) || pointInArc(rStart, rEnd, tStart)
	okEnd := hashEq(tEnd, rEnd) || pointInArc(rStart, rEnd, tEnd)
	return okStart && okEnd
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

func (w *world) splitMovePrefix(space, idx int, split []byte, size int64, dest int) {
	sp := &w.spaces[space]
	from := sp.owner[idx]
	r := sp.ranges[idx]
	if size >= r.Size {
		w.moveWholeSilent(space, idx, dest)
		return
	}
	sp.ranges[idx].Size -= size
	w.used[from] -= size
	w.insert(space, idx, Range{EndHash: cloneHash(split), Size: size}, dest)
	w.mergeSpace(space)
}

func (w *world) splitMoveSuffix(space, idx int, split []byte, size int64, dest int) {
	sp := &w.spaces[space]
	from := sp.owner[idx]
	r := sp.ranges[idx]
	if size >= r.Size {
		w.moveWholeSilent(space, idx, dest)
		return
	}
	end := cloneHash(r.EndHash)
	sp.ranges[idx].EndHash = cloneHash(split)
	sp.ranges[idx].Size -= size
	w.used[from] -= size
	w.insert(space, idx+1, Range{EndHash: end, Size: size}, dest)
	w.mergeSpace(space)
}

func mergeTransfers(in []Transfer) []Transfer {
	if len(in) == 0 {
		return nil
	}
	out := make([]Transfer, 0, len(in))
	for _, t := range in {
		if t.Size <= 0 || t.From == t.To {
			continue
		}
		merged := false
		for i := range out {
			o := &out[i]
			if o.Space != t.Space || o.From != t.From || o.To != t.To {
				continue
			}
			if hashEq(o.EndHash, t.StartHash) {
				o.EndHash = cloneHash(t.EndHash)
				o.Size += t.Size
				merged = true
				break
			}
			if hashEq(t.EndHash, o.StartHash) {
				o.StartHash = cloneHash(t.StartHash)
				o.Size += t.Size
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, Transfer{
				Space:     t.Space,
				StartHash: cloneHash(t.StartHash),
				EndHash:   cloneHash(t.EndHash),
				From:      t.From,
				To:        t.To,
				Size:      t.Size,
			})
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
		if w.used[event.Disk] != 0 {
			return xerrors.Errorf("disk %d was not emptied", event.Disk)
		}
	case EventFull:
		if w.used[event.Disk] > w.disks[event.Disk] {
			return xerrors.Errorf("disk %d still over capacity", event.Disk)
		}
	}
	return nil
}

type candidate struct {
	space, idx, kind, dest int
	size                   int64
	dlt                    int
	over                   int64
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
		w.applyCut(best.space, best.idx, best.kind, best.dest, best.size)
		return true
	}
	smallest := idxs[0]
	for _, idx := range idxs[1:] {
		if w.spaces[space].ranges[idx].Size < w.spaces[space].ranges[smallest].Size {
			smallest = idx
		}
	}
	return w.splitOntoOthers(space, smallest, src)
}

func (w *world) arrive(newDisk int) {
	target := w.fairShare(newDisk)
	if target <= 0 || w.used[newDisk] >= target {
		return
	}
	totalRanges := 0
	for _, sp := range w.spaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges+4; guard++ {
		need := target - w.used[newDisk]
		if need <= 0 {
			return
		}
		cut, ok := w.bestSteal(newDisk, need)
		if !ok {
			return
		}
		w.applyCut(cut.space, cut.idx, cut.kind, newDisk, cut.size)
	}
}

func (w *world) bestSteal(newDisk int, need int64) (candidate, bool) {
	var best *candidate
	for _, src := range w.activeDisks() {
		if src == newDisk {
			continue
		}
		excess := w.used[src] - w.fairShare(src)
		if excess <= 0 {
			continue
		}
		for s := range w.spaces {
			for _, idx := range w.rangeIndexes(s, src) {
				for _, c := range w.sizedCuts(s, idx, need, excess, newDisk) {
					c.over = excess
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
		if kind != cutWhole {
			_, size = w.cutActual(space, idx, kind, size)
			if size <= 0 || size >= sz {
				return
			}
		}
		if !w.canTake(space, idx, kind, dest, size) {
			return
		}
		out = append(out, candidate{
			space: space,
			idx:   idx,
			kind:  kind,
			dest:  dest,
			size:  size,
			dlt:   w.destDelta(space, idx, kind, dest),
		})
	}
	add(cutWhole, sz)
	want := need
	if limit > 0 && limit < want {
		want = limit
	}
	if destFree := w.free(dest); destFree < want {
		want = destFree
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
		return xerrors.Errorf("cannot shed from vacated disk %d", full)
	}
	totalRanges := 0
	for _, sp := range w.spaces {
		totalRanges += len(sp.ranges)
	}
	for guard := 0; guard < totalRanges+4; guard++ {
		need := w.used[full] - w.disks[full]
		if need <= 0 {
			return nil
		}
		cut, ok := w.bestShed(full, need)
		if !ok {
			if w.makeRoom(full) {
				continue
			}
			return xerrors.Errorf("cannot shed %d bytes from disk %d", need, full)
		}
		w.applyCut(cut.space, cut.idx, cut.kind, cut.dest, cut.size)
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
					if best == nil || betterShed(c, *best, need) {
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

func betterShed(a, b candidate, need int64) bool {
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
}

func (w *world) vacate(id int) error {
	w.frozen[id] = true
	if w.used[id] == 0 {
		return nil
	}
	if w.totalCapacity() < w.totalUsed() {
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
	if w.used[id] != 0 {
		return xerrors.Errorf("failed to vacate disk %d", id)
	}
	return nil
}

func (w *world) placeRange(space, idx, src int) bool {
	sp := &w.spaces[space]
	if idx < 0 || idx >= len(sp.ranges) || sp.owner[idx] != src {
		return true
	}
	sz := sp.ranges[idx].Size
	left, right, okL, okR := w.neighbors(space, idx)

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
			w.applyCut(space, idx, cutPrefix, left, pref)
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
		w.applyCut(best.space, best.idx, best.kind, best.dest, best.size)
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
	w.applyCut(cut.space, cut.idx, cut.kind, cut.dest, cut.size)
	return true
}
