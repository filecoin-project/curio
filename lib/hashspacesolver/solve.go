package hashspacesolver

import (
	"golang.org/x/xerrors"
)

// Solve computes a minimum-movement plan for a disk arriving, filling, or
// vacating. The resulting assignment keeps every active disk at or under
// capacity and at or under MAX_RANGES_PER_DISK contiguous ranges.
//
// Cost is lexicographic: fewer bytes moved, then fewer moves. Cuts are
// whole ranges or SliceSize prefixes/suffixes. Hash space is a circle.
func Solve(state State, event Event) (Plan, error) {
	w, err := newWorld(state)
	if err != nil {
		return Plan{}, err
	}
	if event.Disk < 0 || event.Disk >= len(w.disks) {
		return Plan{}, xerrors.Errorf("unknown event disk %d", event.Disk)
	}

	switch event.Kind {
	case EventArrive:
		if err := w.repair(); err != nil {
			return Plan{}, err
		}
		w.arrive(event.Disk)
	case EventFull:
		if err := w.repair(); err != nil {
			return Plan{}, err
		}
		if err := w.shed(event.Disk); err != nil {
			return Plan{}, err
		}
	case EventVacate:
		if err := w.vacate(event.Disk); err != nil {
			return Plan{}, err
		}
	default:
		return Plan{}, xerrors.Errorf("unknown event kind %d", event.Kind)
	}

	if err := w.repair(); err != nil {
		return Plan{}, err
	}
	if err := w.checkSolved(event); err != nil {
		return Plan{}, err
	}
	return w.plan(), nil
}

// Validate reports whether state is structurally sound and satisfies capacity
// and range-count limits.
func Validate(state State) error {
	w, err := newWorld(state)
	if err != nil {
		return err
	}
	return w.checkLimits()
}

// Apply returns a copy of state with plan applied.
func Apply(state State, plan Plan) (State, error) {
	w, err := newWorld(state)
	if err != nil {
		return State{}, err
	}
	for i, m := range plan.Moves {
		if err := w.replay(m); err != nil {
			return State{}, xerrors.Errorf("move %d: %w", i, err)
		}
	}
	return w.snapshot(), nil
}

func (w *world) replay(m Move) error {
	if m.To < 0 || m.To >= len(w.disks) {
		return xerrors.Errorf("unknown dest disk %d", m.To)
	}
	idx := w.find(m.EndHash)
	if idx < 0 {
		return xerrors.Errorf("no range ending at %x", m.EndHash)
	}
	from := w.owner[idx]
	if m.From != from {
		return xerrors.Errorf("range is on disk %d, not %d", from, m.From)
	}
	if m.Split == nil || hashEq(m.Split, m.EndHash) {
		w.moveWholeSilent(idx, m.To)
		return nil
	}
	if m.Size <= 0 || m.Size >= w.ranges[idx].Size {
		return xerrors.Errorf("invalid slice size %d", m.Size)
	}
	if m.Prefix {
		w.ranges[idx].Size -= m.Size
		w.used[from] -= m.Size
		w.insert(idx, Range{EndHash: cloneHash(m.Split), Size: m.Size}, m.To)
	} else {
		end := cloneHash(w.ranges[idx].EndHash)
		w.ranges[idx].EndHash = cloneHash(m.Split)
		w.ranges[idx].Size -= m.Size
		w.used[from] -= m.Size
		w.insert(idx+1, Range{EndHash: end, Size: m.Size}, m.To)
	}
	w.merge()
	return nil
}

func (w *world) moveWholeSilent(idx, dest int) {
	from := w.owner[idx]
	if from == dest {
		return
	}
	sz := w.ranges[idx].Size
	w.owner[idx] = dest
	w.used[from] -= sz
	w.used[dest] += sz
	w.merge()
}

func (w *world) checkLimits() error {
	for _, d := range w.activeDisks() {
		if w.used[d] > w.disks[d] {
			return xerrors.Errorf("disk %d used %d exceeds size %d", d, w.used[d], w.disks[d])
		}
		if rc := w.rangeCount(d); rc > MAX_RANGES_PER_DISK {
			return xerrors.Errorf("disk %d holds %d ranges, max %d", d, rc, MAX_RANGES_PER_DISK)
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
	idx, kind, dest int
	size            int64
	dlt             int
	over            int64
}

func (w *world) repair() error {
	for guard := 0; guard < len(w.ranges)+len(w.disks)+8; guard++ {
		var over []int
		for _, d := range w.activeDisks() {
			if w.rangeCount(d) > MAX_RANGES_PER_DISK {
				over = append(over, d)
			}
		}
		if len(over) == 0 {
			return nil
		}
		progress := false
		for _, d := range over {
			if w.donateRange(d) {
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

func (w *world) donateRange(src int) bool {
	idxs := w.rangeIndexes(src)
	if len(idxs) <= MAX_RANGES_PER_DISK {
		return false
	}
	var best *candidate
	for _, idx := range idxs {
		sz := w.ranges[idx].Size
		for _, dest := range w.activeDisks() {
			if !w.canTake(idx, cutWhole, dest, sz) {
				continue
			}
			c := candidate{idx: idx, kind: cutWhole, dest: dest, size: sz, dlt: w.destDelta(idx, cutWhole, dest)}
			if best == nil || c.size < best.size || (c.size == best.size && (c.dlt < best.dlt || (c.dlt == best.dlt && c.dest < best.dest))) {
				cp := c
				best = &cp
			}
		}
	}
	if best != nil {
		w.applyCut(best.idx, best.kind, best.dest, best.size)
		return true
	}
	smallest := idxs[0]
	for _, idx := range idxs[1:] {
		if w.ranges[idx].Size < w.ranges[smallest].Size {
			smallest = idx
		}
	}
	return w.splitOntoOthers(smallest, src)
}

func (w *world) arrive(newDisk int) {
	target := w.fairShare(newDisk)
	if target <= 0 || w.used[newDisk] >= target {
		return
	}
	for guard := 0; guard < len(w.ranges)+4; guard++ {
		need := target - w.used[newDisk]
		if need <= 0 {
			return
		}
		cut, ok := w.bestSteal(newDisk, need)
		if !ok {
			return
		}
		w.applyCut(cut.idx, cut.kind, newDisk, cut.size)
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
		for _, idx := range w.rangeIndexes(src) {
			for _, c := range w.sizedCuts(idx, need, excess, newDisk) {
				c.over = excess
				c.dest = newDisk
				if best == nil || betterSteal(c, *best, need) {
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

func (w *world) sizedCuts(idx int, need, limit int64, dest int) []candidate {
	sz := w.ranges[idx].Size
	if sz <= 0 {
		return nil
	}
	var out []candidate
	add := func(kind int, size int64) {
		if size <= 0 || size > sz {
			return
		}
		if kind != cutWhole {
			_, size = w.cutActual(idx, kind, size)
			if size <= 0 || size >= sz {
				return
			}
		}
		if !w.canTake(idx, kind, dest, size) {
			return
		}
		out = append(out, candidate{
			idx:  idx,
			kind: kind,
			dest: dest,
			size: size,
			dlt:  w.destDelta(idx, kind, dest),
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
	if a.idx != b.idx {
		return a.idx < b.idx
	}
	return a.kind < b.kind
}

func (w *world) shed(full int) error {
	if w.frozen[full] {
		return xerrors.Errorf("cannot shed from vacated disk %d", full)
	}
	for guard := 0; guard < len(w.ranges)+4; guard++ {
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
		w.applyCut(cut.idx, cut.kind, cut.dest, cut.size)
	}
	if w.used[full] > w.disks[full] {
		return xerrors.Errorf("cannot shed enough from disk %d", full)
	}
	return nil
}

func (w *world) bestShed(full int, need int64) (candidate, bool) {
	var best *candidate
	for _, idx := range w.rangeIndexes(full) {
		for _, dest := range w.activeDisks() {
			if dest == full {
				continue
			}
			for _, c := range w.sizedCuts(idx, need, w.ranges[idx].Size, dest) {
				if best == nil || betterShed(c, *best, need) {
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

	for guard := 0; guard < len(w.ranges)+8; guard++ {
		idxs := w.rangeIndexes(id)
		if len(idxs) == 0 {
			return nil
		}
		best := idxs[0]
		for _, idx := range idxs[1:] {
			if w.ranges[idx].Size > w.ranges[best].Size || (w.ranges[idx].Size == w.ranges[best].Size && idx < best) {
				best = idx
			}
		}
		if w.placeRange(best, id) {
			continue
		}
		if w.makeRoom(id) && w.placeRange(best, id) {
			continue
		}
		return xerrors.Errorf("cannot place range on disk %d elsewhere", id)
	}
	if w.used[id] != 0 {
		return xerrors.Errorf("failed to vacate disk %d", id)
	}
	return nil
}

func (w *world) placeRange(idx, src int) bool {
	if idx < 0 || idx >= len(w.ranges) || w.owner[idx] != src {
		return true
	}
	sz := w.ranges[idx].Size
	left, right, okL, okR := w.neighbors(idx)

	if okL && okR && left == right && w.canTake(idx, cutWhole, left, sz) {
		w.moveWhole(idx, left)
		return true
	}

	var best *candidate
	consider := func(dest int) {
		if !w.canTake(idx, cutWhole, dest, sz) {
			return
		}
		c := candidate{idx: idx, kind: cutWhole, dest: dest, size: sz, dlt: w.destDelta(idx, cutWhole, dest), over: w.free(dest)}
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
		w.moveWhole(idx, best.dest)
		return true
	}

	if okL && okR && left != right {
		end := cloneHash(w.ranges[idx].EndHash)
		pref := min(w.free(left), sz)
		if pref > 0 && w.canTake(idx, cutPrefix, left, pref) {
			if pref >= sz {
				w.moveWhole(idx, left)
				return true
			}
			w.applyCut(idx, cutPrefix, left, pref)
			idx = w.find(end)
			if idx >= 0 && w.owner[idx] == src && w.canTake(idx, cutWhole, right, w.ranges[idx].Size) {
				w.moveWhole(idx, right)
				return true
			}
		}
	}

	return w.splitOntoOthers(idx, src)
}

func (w *world) splitOntoOthers(idx, src int) bool {
	if idx < 0 || idx >= len(w.ranges) || w.owner[idx] != src {
		return true
	}
	end := cloneHash(w.ranges[idx].EndHash)
	for w.find(end) >= 0 && w.owner[w.find(end)] == src {
		idx = w.find(end)
		remain := w.ranges[idx].Size
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
				if sz <= 0 || !w.canTake(idx, kind, dest, sz) {
					continue
				}
				c := candidate{idx: idx, kind: kind, dest: dest, size: sz, dlt: w.destDelta(idx, kind, dest)}
				if best == nil || c.dlt < best.dlt || (c.dlt == best.dlt && (c.size > best.size || (c.size == best.size && c.dest < best.dest))) {
					cp := c
					best = &cp
				}
			}
		}
		if best == nil {
			return false
		}
		w.applyCut(best.idx, best.kind, best.dest, best.size)
	}
	return w.find(end) < 0 || w.owner[w.find(end)] != src
}

func (w *world) makeRoom(avoid int) bool {
	for _, src := range w.activeDisks() {
		if src != avoid && w.rangeCount(src) >= MAX_RANGES_PER_DISK && w.donateRange(src) {
			return true
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
	w.applyCut(cut.idx, cut.kind, cut.dest, cut.size)
	return true
}
