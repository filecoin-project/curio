package hashspacesolver

import (
	"bytes"
	"math/rand"
	"testing"

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

func TestArriveStealsFairShare(t *testing.T) {
	st := mk([]int64{100, 100}, []byte{0x80}, []int64{80}, []int{0})
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.NotEmpty(t, res.Diff)
	require.Equal(t, int64(80), usedOf(out, 0)+usedOf(out, 1))
	require.Equal(t, int64(40), usedOf(out, 1))
	require.Equal(t, 1, rangeCountOf(out, 0, 0))
	require.Equal(t, 1, rangeCountOf(out, 0, 1))
}

func TestArriveNoData(t *testing.T) {
	st := State{Disks: []int64{10, 10}}
	_, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.Empty(t, res.Diff)
}

func TestArriveAlreadyBalanced(t *testing.T) {
	st := mk(
		[]int64{20, 20},
		[]byte{0x80, 0xFF},
		[]int64{10, 10},
		[]int{0, 1},
	)
	_, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.Empty(t, res.Diff)
}

func TestFullPeelsMinimum(t *testing.T) {
	st := mk(
		[]int64{25, 50},
		[]byte{0x00, 0x80},
		[]int64{5, 32},
		[]int{1, 0},
	)
	require.Error(t, Validate(st))
	out, res := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.LessOrEqual(t, usedOf(out, 0), int64(25))
	require.Equal(t, int64(7), res.BytesMoved)
	require.Len(t, res.Diff, 1)
}

func TestFullAlreadyUnderCapacity(t *testing.T) {
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
	st := mk(
		[]int64{50, 50},
		[]byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80},
		[]int64{5, 5, 5, 5, 5, 5, 5, 5},
		[]int{0, 1, 0, 1, 0, 1, 0, 1},
	)
	require.Equal(t, 4, rangeCountOf(st, 0, 0))
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
		[]int64{100, 100, 100, 100, 100},
		[]byte{0x18, 0x20, 0x38, 0x40, 0x58, 0x60, 0x78, 0x80},
		[]int64{10, 10, 10, 10, 10, 10, 10, 10},
		[]int{0, 0, 1, 1, 2, 2, 3, 3},
	)
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 4})
	require.NotEmpty(t, res.Diff)
	require.LessOrEqual(t, rangeCountOf(out, 0, 4), MAX_RANGES_PER_DISK)
	require.Greater(t, usedOf(out, 4), int64(0))
}

func TestTwoSpacesUnequalSharedCapacity(t *testing.T) {
	// A is large, B is small; shared disks must not exceed capacity.
	st := mk2(
		[]int64{100, 100},
		spaceOf([]byte{0x80}, []int64{80}, []int{0}),
		spaceOf([]byte{0x40}, []int64{20}, []int{0}),
	)
	require.NoError(t, Validate(st))
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.NotEmpty(t, res.Diff)
	require.Equal(t, int64(100), usedOf(out, 0)+usedOf(out, 1))
	require.Equal(t, int64(50), usedOf(out, 1))
	require.LessOrEqual(t, usedOf(out, 0), out.Disks[0])
	require.LessOrEqual(t, usedOf(out, 1), out.Disks[1])
}

func TestArriveStealsFromEitherSpace(t *testing.T) {
	// Disk 0 holds most of A; disk 1 holds all of B. New disk 2 should take
	// peels from whichever space fits best toward fair share.
	st := mk2(
		[]int64{100, 100, 100},
		spaceOf([]byte{0x80}, []int64{60}, []int{0}),
		spaceOf([]byte{0x40}, []int64{30}, []int{1}),
	)
	out, res := solveOK(t, st, Event{Kind: EventArrive, Disk: 2})
	require.NotEmpty(t, res.Diff)
	require.Equal(t, int64(30), usedOf(out, 2)) // fair share of 90 total
	spacesUsed := make(map[int]bool)
	for _, tr := range res.Diff {
		spacesUsed[tr.Space] = true
		require.Equal(t, 2, tr.To)
	}
	require.NotEmpty(t, spacesUsed)
}

func TestFullShedsCheapestSpace(t *testing.T) {
	// Disk 0 is over capacity; B has an exact small peel, A has a large block.
	st := mk2(
		[]int64{50, 100},
		spaceOf([]byte{0x80}, []int64{40}, []int{0}),
		spaceOf([]byte{0x00, 0x40}, []int64{5, 16}, []int{1, 0}), // 16 on disk 0 in B
	)
	// used[0] = 40+16 = 56 > 50; need to shed 6.
	require.Error(t, Validate(st))
	out, res := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.LessOrEqual(t, usedOf(out, 0), int64(50))
	require.Equal(t, int64(6), res.BytesMoved)
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
		[]int64{100, 100},
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
