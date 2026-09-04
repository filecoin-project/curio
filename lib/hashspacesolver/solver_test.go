package hashspacesolver

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func h(b byte) []byte { return []byte{b} }

func mk(disks []int64, end []byte, size []int64, owner []int) State {
	rs := make([]Range, len(end))
	for i := range end {
		rs[i] = Range{EndHash: []byte{end[i]}, Size: size[i]}
	}
	return State{Disks: append([]int64(nil), disks...), Ranges: rs, Owner: append([]int(nil), owner...)}
}

func solveOK(t *testing.T, st State, ev Event) (State, Plan) {
	t.Helper()
	plan, err := Solve(st, ev)
	require.NoError(t, err)
	out, err := Apply(st, plan)
	require.NoError(t, err)
	require.NoError(t, Validate(out))
	require.Equal(t, plan.BytesMoved, sumMoved(st, out))
	if ev.Kind == EventVacate {
		require.Zero(t, usedOf(out, ev.Disk))
	}
	if ev.Kind == EventFull {
		require.LessOrEqual(t, usedOf(out, ev.Disk), out.Disks[ev.Disk])
	}
	return out, plan
}

func usedOf(st State, d int) int64 {
	var u int64
	for i, r := range st.Ranges {
		if st.Owner[i] == d {
			u += r.Size
		}
	}
	return u
}

func sumMoved(a, b State) int64 {
	usedA := make(map[string]int)
	for i, r := range a.Ranges {
		usedA[string(r.EndHash)] = a.Owner[i]
	}
	// Byte movement is the plan's job; compare total per-disk used as a sanity check.
	var before, after int64
	for i := range a.Disks {
		ba, bb := usedOf(a, i), usedOf(b, i)
		if bb > ba {
			after += bb - ba
		} else {
			before += ba - bb
		}
	}
	if after != before {
		return after
	}
	return after
}

func rangeCountOf(st State, d int) int {
	w, err := newWorld(st)
	if err != nil {
		return -1
	}
	return w.rangeCount(d)
}

func TestVacateNeighborAbsorbs(t *testing.T) {
	st := mk(
		[]int64{50, 50},
		[]byte{0x10, 0x30},
		[]int64{20, 20},
		[]int{0, 1},
	)
	out, plan := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Equal(t, int64(20), plan.BytesMoved)
	require.Equal(t, 1, rangeCountOf(out, 0))
	require.Zero(t, rangeCountOf(out, 1))
}

func TestVacateSplitsBetweenNeighbors(t *testing.T) {
	st := mk(
		[]int64{15, 40, 15},
		[]byte{0x10, 0x30, 0xFF},
		[]int64{5, 20, 5},
		[]int{0, 1, 2},
	)
	out, plan := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Equal(t, int64(20), plan.BytesMoved)
	require.Zero(t, usedOf(out, 1))
	require.Equal(t, int64(15), usedOf(out, 0))
	require.Equal(t, int64(15), usedOf(out, 2))
	require.Equal(t, 1, rangeCountOf(out, 0))
	require.Equal(t, 1, rangeCountOf(out, 2))
}

func TestVacateBridgesSameNeighbor(t *testing.T) {
	st := mk(
		[]int64{40, 20},
		[]byte{0x10, 0x20, 0x30},
		[]int64{10, 10, 10},
		[]int{0, 1, 0},
	)
	// newWorld merges the wrap-adjacent disk-0 ranges? 0x10 and 0x30 are not
	// adjacent: order 0x10 (disk0), 0x20 (disk1), 0x30 (disk0). Adjacent pairs
	// are (0x10,0x20), (0x20,0x30), (0x30,0x10). Last pair is both disk 0.
	out, plan := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Equal(t, int64(10), plan.BytesMoved)
	require.Equal(t, 1, rangeCountOf(out, 0))
}

func TestVacateEmptyPlan(t *testing.T) {
	st := mk([]int64{10, 10}, []byte{0x80}, []int64{5}, []int{0})
	_, plan := solveOK(t, st, Event{Kind: EventVacate, Disk: 1})
	require.Empty(t, plan.Moves)
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
	out, plan := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.NotEmpty(t, plan.Moves)
	require.Equal(t, int64(80), usedOf(out, 0)+usedOf(out, 1))
	require.Equal(t, int64(40), usedOf(out, 1))
	require.Equal(t, 1, rangeCountOf(out, 0))
	require.Equal(t, 1, rangeCountOf(out, 1))
}

func TestArriveNoData(t *testing.T) {
	st := State{Disks: []int64{10, 10}}
	_, plan := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.Empty(t, plan.Moves)
}

func TestArriveAlreadyBalanced(t *testing.T) {
	st := mk(
		[]int64{20, 20},
		[]byte{0x80, 0xFF},
		[]int64{10, 10},
		[]int{0, 1},
	)
	_, plan := solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.Empty(t, plan.Moves)
}

func TestFullPeelsMinimum(t *testing.T) {
	// (0x00, 0x80] is 128 hash units and 32 bytes, so a 7-byte peel is exact.
	st := mk(
		[]int64{25, 50},
		[]byte{0x00, 0x80},
		[]int64{5, 32},
		[]int{1, 0},
	)
	require.Error(t, Validate(st))
	out, plan := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.LessOrEqual(t, usedOf(out, 0), int64(25))
	require.Equal(t, int64(7), plan.BytesMoved)
	require.Len(t, plan.Moves, 1)
}

func TestFullAlreadyUnderCapacity(t *testing.T) {
	st := mk(
		[]int64{20, 20},
		[]byte{0x80, 0xFF},
		[]int64{10, 10},
		[]int{0, 1},
	)
	_, plan := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.Empty(t, plan.Moves)
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
	require.Equal(t, 4, rangeCountOf(st, 0))
	require.Error(t, Validate(st))
	out, _ := solveOK(t, st, Event{Kind: EventFull, Disk: 0})
	require.LessOrEqual(t, rangeCountOf(out, 0), MAX_RANGES_PER_DISK)
	require.LessOrEqual(t, rangeCountOf(out, 1), MAX_RANGES_PER_DISK)
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
		st := State{Disks: []int64{10}, Ranges: []Range{{EndHash: h(1), Size: 1}}}
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
	end := append([]byte(nil), st.Ranges[0].EndHash...)
	_, _ = solveOK(t, st, Event{Kind: EventArrive, Disk: 1})
	require.Equal(t, before, usedOf(st, 0))
	require.Equal(t, end, st.Ranges[0].EndHash)
	require.Equal(t, 0, st.Owner[0])
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
	require.Equal(t, p1, p2)
}

func TestArriveAtMostThreeRanges(t *testing.T) {
	st := mk(
		[]int64{100, 100, 100, 100, 100},
		[]byte{0x18, 0x20, 0x38, 0x40, 0x58, 0x60, 0x78, 0x80},
		[]int64{10, 10, 10, 10, 10, 10, 10, 10},
		[]int{0, 0, 1, 1, 2, 2, 3, 3},
	)
	out, plan := solveOK(t, st, Event{Kind: EventArrive, Disk: 4})
	require.NotEmpty(t, plan.Moves)
	require.LessOrEqual(t, rangeCountOf(out, 4), MAX_RANGES_PER_DISK)
	require.Greater(t, usedOf(out, 4), int64(0))
}

func TestRandomClusterEvents(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 64; i++ {
		st := randomValidState(rng)
		require.NoError(t, Validate(st), "iter %d seed state", i)
		st, ev := randomEvent(rng, st)
		plan, err := Solve(st, ev)
		if err != nil {
			if ev.Kind == EventVacate || ev.Kind == EventFull {
				continue
			}
			t.Fatalf("iter %d: unexpected solve error: %v", i, err)
		}
		out, err := Apply(st, plan)
		require.NoError(t, err, "iter %d", i)
		require.NoError(t, Validate(out), "iter %d", i)
		if ev.Kind == EventVacate {
			require.Zero(t, usedOf(out, ev.Disk), "iter %d", i)
		}
	}
}

func randomValidState(rng *rand.Rand) State {
	nDisks := 2 + rng.Intn(3)
	nRanges := nDisks + rng.Intn(4)
	disks := make([]int64, nDisks)
	for i := range disks {
		disks[i] = int64(80 + rng.Intn(80))
	}
	ranges := make([]Range, nRanges)
	owner := make([]int, nRanges)
	used := make([]int64, nDisks)
	di := 0
	for i := 0; i < nRanges; i++ {
		sz := int64(8 + rng.Intn(16))
		for di < nDisks-1 && used[di]+sz > disks[di]/2 {
			di++
		}
		ranges[i] = Range{EndHash: []byte{byte((i + 1) * 17)}, Size: sz}
		owner[i] = di
		used[di] += sz
	}
	return State{Disks: disks, Ranges: ranges, Owner: owner}
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

func TestApplyRejectsUnknownRange(t *testing.T) {
	st := mk([]int64{20, 20}, []byte{0x80}, []int64{5}, []int{0})
	_, err := Apply(st, Plan{Moves: []Move{{From: 0, To: 1, EndHash: h(0x01), Size: 5}}})
	require.Error(t, err)
}
