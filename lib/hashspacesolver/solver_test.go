package hashspacesolver

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func h(b byte) []byte { return []byte{b} }

// mk builds a single-space state for simple tests.
func mk(disks []int64, end []byte, size []int64, owner []int) State {
	rs := make([]Range, len(end))
	for i := range end {
		rs[i] = Range{EndHash: []byte{end[i]}, Size: size[i]}
	}
	return State{
		Disks: append([]int64(nil), disks...),
		Spaces: []Space{{
			Ranges: rs,
			Owner:  append([]int(nil), owner...),
		}},
	}
}

func mk2(disks []int64, a, b Space) State {
	return State{
		Disks:  append([]int64(nil), disks...),
		Spaces: []Space{cloneSpace(a), cloneSpace(b)},
	}
}

func spaceOf(end []byte, size []int64, owner []int) Space {
	rs := make([]Range, len(end))
	for i := range end {
		rs[i] = Range{EndHash: []byte{end[i]}, Size: size[i]}
	}
	return Space{Ranges: rs, Owner: append([]int(nil), owner...)}
}

func solveOK(t *testing.T, st State, ev Event) (State, Result) {
	t.Helper()
	res, err := Solve(st, ev)
	require.NoError(t, err)
	out, err := Apply(st, res.Diff)
	require.NoError(t, err)
	require.NoError(t, Validate(out))
	require.NoError(t, Validate(res.State))
	requireEqualState(t, res.State, out)
	require.Equal(t, res.BytesMoved, sumDiff(res.Diff))
	if ev.Kind == EventVacate {
		require.Zero(t, usedOf(out, ev.Disk))
		for s := range out.Spaces {
			require.Zero(t, rangeCountOf(out, s, ev.Disk))
		}
	}
	if ev.Kind == EventFull {
		require.LessOrEqual(t, usedOf(out, ev.Disk), out.Disks[ev.Disk])
	}
	return out, res
}

func requireEqualState(t *testing.T, a, b State) {
	t.Helper()
	require.Equal(t, a.Disks, b.Disks)
	require.Equal(t, len(a.Spaces), len(b.Spaces))
	for s := range a.Spaces {
		wa, err := newWorld(State{Disks: a.Disks, Spaces: []Space{a.Spaces[s]}})
		require.NoError(t, err)
		wb, err := newWorld(State{Disks: b.Disks, Spaces: []Space{b.Spaces[s]}})
		require.NoError(t, err)
		require.Equal(t, wa.spaces[0].owner, wb.spaces[0].owner, "space %d owners", s)
		require.Equal(t, len(wa.spaces[0].ranges), len(wb.spaces[0].ranges), "space %d ranges", s)
		for i := range wa.spaces[0].ranges {
			require.True(t, hashEq(wa.spaces[0].ranges[i].EndHash, wb.spaces[0].ranges[i].EndHash), "space %d end %d", s, i)
			require.Equal(t, wa.spaces[0].ranges[i].Size, wb.spaces[0].ranges[i].Size, "space %d size %d", s, i)
		}
	}
}

func usedOf(st State, d int) int64 {
	var u int64
	for _, sp := range st.Spaces {
		for i, r := range sp.Ranges {
			if sp.Owner[i] == d {
				u += r.Size
			}
		}
	}
	return u
}

func sumDiff(diff []Transfer) int64 {
	var n int64
	for _, t := range diff {
		n += t.Size
	}
	return n
}

func rangeCountOf(st State, space, d int) int {
	w, err := newWorld(st)
	if err != nil {
		return -1
	}
	return w.rangeCount(space, d)
}

func TestVacateReassignsEmptyArc(t *testing.T) {
	st := mk([]int64{20, 20}, []byte{0x40, 0x80}, []int64{0, 5}, []int{0, 1})
	out, res := solveOK(t, st, Event{Kind: EventVacate, Disk: 0})
	require.Zero(t, res.BytesMoved)
	require.Len(t, res.Diff, 1)
	require.Equal(t, 0, res.Diff[0].From)
	require.Equal(t, 1, res.Diff[0].To)
	require.Equal(t, int64(0), res.Diff[0].Size)
	require.Len(t, out.Spaces[0].Ranges, 1)
	require.Equal(t, []int{1}, out.Spaces[0].Owner)
	require.Equal(t, int64(5), out.Spaces[0].Ranges[0].Size)
	require.True(t, hashEq(out.Spaces[0].Ranges[0].EndHash, h(0x80)))
}

func TestVacateReassignsSoleEmptyArc(t *testing.T) {
	st := mk([]int64{10, 10}, []byte{0x10}, []int64{0}, []int{0})
	out, res := solveOK(t, st, Event{Kind: EventVacate, Disk: 0})
	require.Zero(t, res.BytesMoved)
	require.Len(t, res.Diff, 1)
	require.Equal(t, int64(0), res.Diff[0].Size)
	require.Equal(t, []int{1}, out.Spaces[0].Owner)
	require.Equal(t, int64(0), out.Spaces[0].Ranges[0].Size)
}

func TestVacateNeighborAbsorbs(t *testing.T) {
	st := mk(
		[]int64{50, 50},
		[]byte{0x10, 0x30},
		[]int64{20, 20},
		[]int{0, 1},
	)
	out, res := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Equal(t, int64(20), res.BytesMoved)
	require.Equal(t, 1, rangeCountOf(out, 0, 0))
	require.Zero(t, rangeCountOf(out, 0, 1))
}

func TestVacateSplitsBetweenNeighbors(t *testing.T) {
	st := mk(
		[]int64{15, 40, 15},
		[]byte{0x10, 0x30, 0xFF},
		[]int64{5, 20, 5},
		[]int{0, 1, 2},
	)
	out, res := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Equal(t, int64(20), res.BytesMoved)
	require.Zero(t, usedOf(out, 1))
	require.Equal(t, int64(15), usedOf(out, 0))
	require.Equal(t, int64(15), usedOf(out, 2))
	require.Equal(t, 1, rangeCountOf(out, 0, 0))
	require.Equal(t, 1, rangeCountOf(out, 0, 2))
}

func TestVacateBridgesSameNeighbor(t *testing.T) {
	st := mk(
		[]int64{40, 20},
		[]byte{0x10, 0x20, 0x30},
		[]int64{10, 10, 10},
		[]int{0, 1, 0},
	)
	out, res := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Equal(t, int64(10), res.BytesMoved)
	require.Equal(t, 1, rangeCountOf(out, 0, 0))
}

func TestVacateEmptyPlan(t *testing.T) {
	st := mk([]int64{10, 10}, []byte{0x80}, []int64{5}, []int{0})
	_, res := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Empty(t, res.Diff)
}

func TestVacateInsufficientCapacity(t *testing.T) {
	st := mk(
		[]int64{10, 20},
		[]byte{0x10, 0xFF},
		[]int64{10, 20},
		[]int{0, 1},
	)
	_, err := Solve(st, Event{Kind: EventVacate, Disk: 1})
	require.Error(t, err)
}

func TestVacateOnlyDisk(t *testing.T) {
	st := mk([]int64{20}, []byte{0x80}, []int64{10}, []int{0})
	_, err := Solve(st, Event{Kind: EventVacate, Disk: 0})
	require.Error(t, err)
}

func TestArriveRelievesOverflow(t *testing.T) {
	st := mk([]int64{50, 100}, []byte{0x80}, []int64{80}, []int{0})
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.NotEmpty(t, res.Diff)
	require.Equal(t, int64(80), usedOf(out, 0)+usedOf(out, 1))
	require.Equal(t, int64(40), usedOf(out, 0))
	require.Equal(t, int64(40), usedOf(out, 1))
	require.Equal(t, 1, rangeCountOf(out, 0, 0))
	require.Equal(t, 1, rangeCountOf(out, 0, 1))
}

func TestArriveRelievesOverflowLargeDisks(t *testing.T) {
	const tib = int64(1) << 40
	st := mk([]int64{40 * tib, 100 * tib}, []byte{0x80}, []int64{80 * tib}, []int{0})
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.NotEmpty(t, res.Diff)
	require.Equal(t, 80*tib, usedOf(out, 0)+usedOf(out, 1))
	require.LessOrEqual(t, usedOf(out, 0), fillLimitOf(40*tib))
	require.LessOrEqual(t, usedOf(out, 1), fillLimitOf(100*tib))
}

func TestArriveNoData(t *testing.T) {
	st := State{Disks: []int64{10, 10}}
	_, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.Empty(t, res.Diff)
}

func TestArriveIdleWhenUnderFillLimit(t *testing.T) {
	st := mk(
		[]int64{20, 20},
		[]byte{0x80, 0xFF},
		[]int64{10, 10},
		[]int{0, 1},
	)
	_, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.Empty(t, res.Diff)
}

func TestArriveClusterOverFillLimit(t *testing.T) {
	// Cluster is already ~90% full. The new disk can only take up to its
	// own 80% fill limit; remaining disks stay above 80%.
	st := mk(
		[]int64{100, 100, 10},
		[]byte{0x80, 0xFF},
		[]int64{90, 90},
		[]int{0, 1},
	)
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 2})
	require.NotEmpty(t, res.Diff)
	require.LessOrEqual(t, usedOf(out, 2), fillLimitOf(10))
	require.Greater(t, usedOf(out, 0), fillLimitOf(100))
	require.Greater(t, usedOf(out, 1), fillLimitOf(100))
}

func TestFullStopsWhenDestsAtFillLimit(t *testing.T) {
	st := mk(
		[]int64{100, 100},
		[]byte{0x80, 0xFF},
		[]int64{90, 80},
		[]int{0, 1},
	)
	out, res := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.Empty(t, res.Diff)
	require.Equal(t, int64(90), usedOf(out, 0))
}

func TestFullPeelsMinimum(t *testing.T) {
	st := mk(
		[]int64{25, 50},
		[]byte{0x00, 0x80},
		[]int64{5, 22},
		[]int{1, 0},
	)
	require.NoError(t, Validate(st))
	out, res := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.LessOrEqual(t, usedOf(out, 0), fillLimitOf(25))
	require.GreaterOrEqual(t, res.BytesMoved, int64(2))
	require.Len(t, res.Diff, 1)
}

func TestFullAlreadyUnderFillLimit(t *testing.T) {
	st := mk(
		[]int64{20, 20},
		[]byte{0x80, 0xFF},
		[]int64{10, 10},
		[]int{0, 1},
	)
	_, res := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.Empty(t, res.Diff)
}

func TestFullCannotShed(t *testing.T) {
	st := mk(
		[]int64{5, 10},
		[]byte{0x80, 0xFF},
		[]int64{20, 10},
		[]int{0, 1},
	)
	_, err := Solve(st, Event{Kind: EventFull, Disk: 0})
	require.Error(t, err)
}

func TestRepairTooManyRanges(t *testing.T) {
	end := make([]byte, 18)
	size := make([]int64, 18)
	owner := make([]int, 18)
	for i := range end {
		end[i] = byte((i + 1) * 10)
		size[i] = 5
		owner[i] = i % 2
	}
	st := mk([]int64{200, 200}, end, size, owner)
	require.Equal(t, 9, rangeCountOf(st, 0, 0))
	require.Error(t, Validate(st))
	out, _ := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.LessOrEqual(t, rangeCountOf(out, 0, 0), MAX_RANGES_PER_DISK)
	require.LessOrEqual(t, rangeCountOf(out, 0, 1), MAX_RANGES_PER_DISK)
}

func TestUnknownEventDisk(t *testing.T) {
	st := State{Disks: []int64{10}}
	_, err := Solve(st, Event{Kind: EventArrive, Disk: 3})
	require.Error(t, err)
}

func TestUnknownEventKind(t *testing.T) {
	st := State{Disks: []int64{10}}
	_, err := Solve(st, Event{Kind: 0, Disk: 0})
	require.Error(t, err)
}

func TestStructuralErrors(t *testing.T) {
	t.Run("owner length", func(t *testing.T) {
		st := State{Disks: []int64{10}, Spaces: []Space{{Ranges: []Range{{EndHash: h(1), Size: 1}}}}}
		_, err := Solve(st, Event{Kind: EventFull, Disk: 0})
		require.Error(t, err)
	})
	t.Run("negative disk", func(t *testing.T) {
		st := State{Disks: []int64{-1}}
		_, err := Solve(st, Event{Kind: EventFull, Disk: 0})
		require.Error(t, err)
	})
	t.Run("duplicate end hash", func(t *testing.T) {
		st := mk([]int64{10}, []byte{0x10, 0x10}, []int64{1, 1}, []int{0, 0})
		_, err := Solve(st, Event{Kind: EventFull, Disk: 0})
		require.Error(t, err)
	})
}

func TestSolveDoesNotMutateInput(t *testing.T) {
	st := mk([]int64{30, 30}, []byte{0x80}, []int64{40}, []int{0})
	before := usedOf(st, 0)
	end := append([]byte(nil), st.Spaces[0].Ranges[0].EndHash...)
	_, _ = solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.Equal(t, before, usedOf(st, 0))
	require.Equal(t, end, st.Spaces[0].Ranges[0].EndHash)
	require.Equal(t, 0, st.Spaces[0].Owner[0])
}

func TestDeterministic(t *testing.T) {
	st := mk(
		[]int64{80, 80, 80},
		[]byte{0x20, 0x40, 0xFF},
		[]int64{20, 20, 10},
		[]int{0, 1, 2},
	)
	p1, err := Solve(st, Event{Kind: EventVacate, Disk: 2})
	require.NoError(t, err)
	p2, err := Solve(st, Event{Kind: EventVacate, Disk: 2})
	require.NoError(t, err)
	require.Equal(t, p1.BytesMoved, p2.BytesMoved)
	require.Equal(t, len(p1.Diff), len(p2.Diff))
	for i := range p1.Diff {
		require.Equal(t, p1.Diff[i].From, p2.Diff[i].From)
		require.Equal(t, p1.Diff[i].To, p2.Diff[i].To)
		require.Equal(t, p1.Diff[i].Size, p2.Diff[i].Size)
		require.True(t, bytes.Equal(p1.Diff[i].StartHash, p2.Diff[i].StartHash))
		require.True(t, bytes.Equal(p1.Diff[i].EndHash, p2.Diff[i].EndHash))
	}
}

func TestArriveAtMostThreeRanges(t *testing.T) {
	st := mk(
		[]int64{8, 8, 8, 8, 400},
		[]byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80},
		[]int64{10, 10, 10, 10, 10, 10, 10, 10},
		[]int{0, 1, 2, 3, 0, 1, 2, 3},
	)
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 4})
	require.NotEmpty(t, res.Diff)
	require.LessOrEqual(t, rangeCountOf(out, 0, 4), MAX_RANGES_PER_DISK)
	for d := 0; d < 4; d++ {
		require.LessOrEqual(t, usedOf(out, d), fillLimitOf(out.Disks[d]), "disk %d", d)
	}
	require.GreaterOrEqual(t, usedOf(out, 4), int64(56))
}

func TestTwoSpacesUnequalSharedCapacity(t *testing.T) {
	// A is large, B is small; shared disks must not exceed capacity.
	st := mk2(
		[]int64{70, 100},
		spaceOf([]byte{0x80}, []int64{80}, []int{0}),
		spaceOf([]byte{0x40}, []int64{20}, []int{0}),
	)
	require.Error(t, Validate(st))
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.NotEmpty(t, res.Diff)
	require.Equal(t, int64(100), usedOf(out, 0)+usedOf(out, 1))
	require.Equal(t, fillLimitOf(70), usedOf(out, 0))
	require.Equal(t, int64(44), usedOf(out, 1))
}

func TestArriveStealsFromEitherSpace(t *testing.T) {
	// Disk 0 is above the fill limit in A; disk 1 is under. The new disk
	// only takes the overflow, from whichever space that overflow lives in.
	st := mk2(
		[]int64{50, 100, 100},
		spaceOf([]byte{0x80}, []int64{60}, []int{0}),
		spaceOf([]byte{0x40}, []int64{30}, []int{1}),
	)
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 2})
	require.NotEmpty(t, res.Diff)
	require.Equal(t, int64(20), usedOf(out, 2))
	require.Equal(t, fillLimitOf(50), usedOf(out, 0))
	for _, tr := range res.Diff {
		require.Equal(t, 2, tr.To)
		require.Equal(t, 0, tr.Space)
	}
}

func TestFullShedsCheapestSpace(t *testing.T) {
	// Disk 0 is over capacity; B has an exact small peel, A has a large block.
	st := mk2(
		[]int64{50, 100},
		spaceOf([]byte{0x80}, []int64{30}, []int{0}),
		spaceOf([]byte{0x00, 0x40}, []int64{5, 15}, []int{1, 0}),
	)
	// used[0] = 30+15 = 45; fill limit 40; need to shed 5.
	require.NoError(t, Validate(st))
	out, res := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.LessOrEqual(t, usedOf(out, 0), fillLimitOf(50))
	require.GreaterOrEqual(t, res.BytesMoved, int64(5))
}

func TestVacateBothSpaces(t *testing.T) {
	st := mk2(
		[]int64{80, 40, 80},
		spaceOf([]byte{0x20, 0x40}, []int64{10, 20}, []int{0, 1}),
		spaceOf([]byte{0x30, 0x60}, []int64{10, 15}, []int{2, 1}),
	)
	out, res := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Zero(t, usedOf(out, 1))
	require.Equal(t, int64(35), res.BytesMoved)
	require.LessOrEqual(t, rangeCountOf(out, 0, 0), MAX_RANGES_PER_DISK)
	require.LessOrEqual(t, rangeCountOf(out, 1, 0), MAX_RANGES_PER_DISK)
	require.LessOrEqual(t, rangeCountOf(out, 0, 2), MAX_RANGES_PER_DISK)
	require.LessOrEqual(t, rangeCountOf(out, 1, 2), MAX_RANGES_PER_DISK)
}

func TestDiffDisjointPerSpace(t *testing.T) {
	st := mk2(
		[]int64{90, 100},
		spaceOf([]byte{0x80}, []int64{80}, []int{0}),
		spaceOf([]byte{0x40}, []int64{20}, []int{0}),
	)
	_, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	for s := 0; s < 2; s++ {
		var segs []Transfer
		for _, tr := range res.Diff {
			if tr.Space == s {
				segs = append(segs, tr)
			}
		}
		for i := 0; i < len(segs); i++ {
			for j := i + 1; j < len(segs); j++ {
				require.False(t, intervalsOverlap(segs[i], segs[j]), "overlapping diffs in space %d", s)
			}
		}
	}
}

func intervalsOverlap(a, b Transfer) bool {
	// Two half-open arcs overlap if either endpoint of one lies in the other.
	return (pointInArc(a.StartHash, a.EndHash, b.EndHash) && !hashEq(b.EndHash, a.StartHash)) ||
		(pointInArc(b.StartHash, b.EndHash, a.EndHash) && !hashEq(a.EndHash, b.StartHash))
}

func TestUnsplittablePrefixDoesNotLoop(t *testing.T) {
	// Each disk-0 range covers a single hash step, so a partial cut cannot
	// be represented. Disk 1 has free bytes but not enough for a whole range.
	// Repair must stop instead of retrying the same prefix forever.
	st := mk([]int64{300, 80, 30},
		[]byte{0x10, 0x11, 0x20, 0x21, 0x30, 0x31, 0x40, 0x41},
		[]int64{10, 50, 10, 50, 10, 50, 11, 50},
		[]int{1, 0, 1, 0, 1, 0, 1, 0},
	)
	done := make(chan error, 1)
	go func() {
		_, err := Solve(st, Event{Kind: EventFull, Disk: 0})
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("splitOntoOthers retried a cut that moved nothing")
	}
}

func TestEachByteMovesOnce(t *testing.T) {
	// Disk 0 is over the range cap. Repair absorbs one of its ranges onto
	// disk 1, which puts disk 1 over the fill limit so arrive steals onward.
	// The stolen slice must be one move from the original owner, not a second hop.
	end := make([]byte, 18)
	size := make([]int64, 18)
	owner := make([]int, 18)
	for i := range end {
		end[i] = byte((i + 1) * 10)
		if i%2 == 0 {
			size[i] = 10
			owner[i] = 0
		} else {
			size[i] = 8
			owner[i] = 1
		}
	}
	st := mk([]int64{200, 200, 400}, end, size, owner)
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 2})
	require.NotEmpty(t, res.Diff)
	for i := range res.Diff {
		for j := i + 1; j < len(res.Diff); j++ {
			if res.Diff[i].Space != res.Diff[j].Space {
				continue
			}
			require.False(t, intervalsOverlap(res.Diff[i], res.Diff[j]) || intervalsOverlap(res.Diff[j], res.Diff[i]))
		}
	}
	rev := append([]Transfer(nil), res.Diff...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	revOut, err := Apply(st, rev)
	require.NoError(t, err)
	requireEqualState(t, out, revOut)
}

func TestMergeTransfersFixpoint(t *testing.T) {
	got := mergeTransfers([]Transfer{
		{Space: 0, From: 0, To: 1, StartHash: h(0x10), EndHash: h(0x20), Size: 1},
		{Space: 0, From: 0, To: 1, StartHash: h(0x30), EndHash: h(0x40), Size: 1},
		{Space: 0, From: 0, To: 1, StartHash: h(0x20), EndHash: h(0x30), Size: 1},
		{Space: 1, From: 0, To: 1, StartHash: h(0x00), EndHash: h(0x10), Size: 4},
	})
	require.Len(t, got, 2)
	var combined, other Transfer
	for _, tr := range got {
		if tr.Space == 0 {
			combined = tr
		} else {
			other = tr
		}
	}
	require.Equal(t, 0, combined.From)
	require.Equal(t, 1, combined.To)
	require.Equal(t, int64(3), combined.Size)
	require.True(t, hashEq(combined.StartHash, h(0x10)))
	require.True(t, hashEq(combined.EndHash, h(0x40)))
	require.Equal(t, int64(4), other.Size)
	require.True(t, hashEq(other.StartHash, h(0x00)))
	require.True(t, hashEq(other.EndHash, h(0x10)))
}

func TestSuffixRecutCanExceedCheckedSize(t *testing.T) {
	// Suffix of 1 byte from a 5-byte range rounds to 2. Cutting again from
	// that 2 rounds to 3, which is more than the size already checked.
	st := mk([]int64{20, 20}, []byte{0x10, 0x20}, []int64{1, 5}, []int{1, 0})
	w, err := newWorld(st)
	require.NoError(t, err)
	idx := 1
	split, moved, _, _ := w.previewCut(0, idx, cutSuffix, 1)
	require.Equal(t, int64(2), moved)
	_, again, _, _ := w.previewCut(0, idx, cutSuffix, moved)
	require.Equal(t, int64(3), again)
	before := w.used[1]
	require.True(t, w.applyCut(0, idx, cutSuffix, 1, moved, split))
	require.Equal(t, before+moved, w.used[1])
}

func TestApplyMovesMiddleOfRange(t *testing.T) {
	// Disk 1 holds the arc before disk 0, so the leftover prefix and suffix
	// of disk 0 do not meet and merge. The transfer is inside (0x40, 0xc0].
	st := mk([]int64{200, 200}, []byte{0x40, 0xc0}, []int64{40, 80}, []int{1, 0})
	src := 1
	r := st.Spaces[0].Ranges[src]
	start := StartHash(st.Spaces[0].Ranges, src)
	midStart, midEnd := h(0x60), h(0xa0)
	require.False(t, hashEq(midStart, start))
	require.False(t, hashEq(midEnd, r.EndHash))
	left := SliceSize(r, start, midStart)
	mid := SliceSize(r, start, midEnd) - left
	require.Positive(t, left)
	require.Positive(t, mid)
	require.Less(t, left+mid, r.Size)

	out, err := Apply(st, []Transfer{{
		Space: 0, From: 0, To: 1,
		StartHash: midStart, EndHash: midEnd, Size: mid,
	}})
	require.NoError(t, err)
	require.NoError(t, Validate(out))
	require.Equal(t, r.Size-mid, usedOf(out, 0))
	require.Equal(t, int64(40)+mid, usedOf(out, 1))

	sp := out.Spaces[0]
	require.Equal(t, []int{1, 0, 1, 0}, sp.Owner)
	require.True(t, hashEq(sp.Ranges[0].EndHash, h(0x40)))
	require.Equal(t, int64(40), sp.Ranges[0].Size)
	require.True(t, hashEq(sp.Ranges[1].EndHash, midStart))
	require.Equal(t, left, sp.Ranges[1].Size)
	require.True(t, hashEq(sp.Ranges[2].EndHash, midEnd))
	require.Equal(t, mid, sp.Ranges[2].Size)
	require.True(t, hashEq(sp.Ranges[3].EndHash, h(0xc0)))
	require.Equal(t, r.Size-left-mid, sp.Ranges[3].Size)
}

func TestApplyMiddleCutOrderIndependent(t *testing.T) {
	// Disk 1's two arcs merge across the wrap. Moving its 10-byte tail onto
	// disk 0 first glues dense and sparse data into one range; the later
	// middle cut must still be sized from the 90-byte arc, not a uniform
	// share of 100. (0x40, 0x50] holds 22 and (0x50, 0x60] holds 45-22=23.
	st := mk([]int64{200, 200, 200}, []byte{0x40, 0x80, 0xff}, []int64{10, 90, 50}, []int{1, 0, 1})
	t1 := Transfer{Space: 0, From: 1, To: 0, StartHash: h(0xff), EndHash: h(0x40), Size: 10}
	t2 := Transfer{Space: 0, From: 0, To: 2, StartHash: h(0x50), EndHash: h(0x60), Size: 23}
	want := mk([]int64{200, 200, 200}, []byte{0x50, 0x60, 0x80, 0xff}, []int64{32, 23, 45, 50}, []int{0, 2, 0, 1})

	for _, diff := range [][]Transfer{{t1, t2}, {t2, t1}} {
		out, err := Apply(st, diff)
		require.NoError(t, err)
		require.NoError(t, Validate(out))
		requireEqualState(t, want, out)
	}
}

func TestApplyRejectsMiddleSizeMismatch(t *testing.T) {
	st := mk([]int64{200, 200, 200}, []byte{0x40, 0x80, 0xff}, []int64{10, 90, 50}, []int{1, 0, 1})
	_, err := Apply(st, []Transfer{
		{Space: 0, From: 1, To: 0, StartHash: h(0xff), EndHash: h(0x40), Size: 10},
		{Space: 0, From: 0, To: 2, StartHash: h(0x50), EndHash: h(0x60), Size: 30},
	})
	require.Error(t, err)
}

func TestApplyRejectsUnknownRange(t *testing.T) {
	st := mk([]int64{20, 20}, []byte{0x80}, []int64{5}, []int{0})
	_, err := Apply(st, []Transfer{{Space: 0, From: 0, To: 1, StartHash: h(0x00), EndHash: h(0x01), Size: 5}})
	require.Error(t, err)
}

func TestRandomClusterEvents(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 64; i++ {
		st := randomValidState(rng)
		require.NoError(t, Validate(st), "iter %d seed state", i)
		st, ev := randomEvent(rng, st)
		res, err := Solve(st, ev)
		if err != nil {
			if ev.Kind == EventVacate || ev.Kind == EventFull {
				continue
			}
			t.Fatalf("iter %d: unexpected solve error: %v", i, err)
		}
		out, err := Apply(st, res.Diff)
		require.NoError(t, err, "iter %d", i)
		require.NoError(t, Validate(out), "iter %d", i)
		requireEqualState(t, res.State, out)
		shuffled := append([]Transfer(nil), res.Diff...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		shufOut, err := Apply(st, shuffled)
		require.NoError(t, err, "iter %d shuffled", i)
		requireEqualState(t, res.State, shufOut)
		if ev.Kind == EventVacate {
			require.Zero(t, usedOf(out, ev.Disk), "iter %d", i)
		}
	}
}

func randomValidState(rng *rand.Rand) State {
	nDisks := 2 + rng.Intn(3)
	disks := make([]int64, nDisks)
	for i := range disks {
		disks[i] = int64(120 + rng.Intn(80))
	}
	nSpaces := 1 + rng.Intn(2)
	spaces := make([]Space, nSpaces)
	used := make([]int64, nDisks)
	for s := 0; s < nSpaces; s++ {
		nRanges := nDisks + rng.Intn(3)
		ranges := make([]Range, nRanges)
		owner := make([]int, nRanges)
		di := 0
		for i := 0; i < nRanges; i++ {
			sz := int64(6 + rng.Intn(10))
			for di < nDisks-1 && used[di]+sz > disks[di]/3 {
				di++
			}
			ranges[i] = Range{EndHash: []byte{byte((s+1)*40 + (i+1)*11)}, Size: sz}
			owner[i] = di % nDisks
			used[owner[i]] += sz
		}
		spaces[s] = Space{Ranges: ranges, Owner: owner}
	}
	return State{Disks: disks, Spaces: spaces}
}

func randomEvent(rng *rand.Rand, st State) (State, Event) {
	switch rng.Intn(3) {
	case 0:
		st.Disks = append(append([]int64(nil), st.Disks...), 60+int64(rng.Intn(40)))
		return st, Event{Kind: EventArrive, Disk: len(st.Disks) - 1}
	case 1:
		return st, Event{Kind: EventFull, Disk: rng.Intn(len(st.Disks))}
	default:
		return st, Event{Kind: EventVacate, Disk: rng.Intn(len(st.Disks))}
	}
}
