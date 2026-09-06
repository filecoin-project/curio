package sealsupra

import "testing"

func TestPlanCCAllocationsPreservesExistingRemainderBehavior(t *testing.T) {
	schedules := []ccSchedule{
		{SpID: 1001, ToSeal: 10, Weight: 1},
		{SpID: 1002, ToSeal: 10, Weight: 1},
		{SpID: 1003, ToSeal: 1, Weight: 1},
	}

	allocations := planCCAllocations(schedules, 2)
	if len(allocations) != 1 {
		t.Fatalf("allocation count = %d, want 1", len(allocations))
	}
	if allocations[0].schedule.SpID != 1003 || allocations[0].count != 2 {
		t.Fatalf("allocation = provider %d count %d, want provider 1003 count 2", allocations[0].schedule.SpID, allocations[0].count)
	}
}

func TestPlanCCAllocationsPreservesWeightedCounts(t *testing.T) {
	schedules := []ccSchedule{
		{SpID: 1003, ToSeal: 20, Weight: 3},
		{SpID: 1001, ToSeal: 20, Weight: 2},
		{SpID: 1002, ToSeal: 20, Weight: 1},
	}

	allocations := planCCAllocations(schedules, 10)
	wantProviders := []int64{1003, 1001, 1002}
	wantCounts := []int64{5, 3, 2}
	if len(allocations) != len(wantProviders) {
		t.Fatalf("allocation count = %d, want %d", len(allocations), len(wantProviders))
	}
	for i := range allocations {
		if allocations[i].schedule.SpID != wantProviders[i] || allocations[i].count != wantCounts[i] {
			t.Fatalf("allocation %d = provider %d count %d, want provider %d count %d", i, allocations[i].schedule.SpID, allocations[i].count, wantProviders[i], wantCounts[i])
		}
	}
}

func TestCCProviderLockOrderIsStableAndDoesNotChangeScheduleOrder(t *testing.T) {
	allocations := []ccAllocation{
		{schedule: ccSchedule{SpID: 1003}, count: 5},
		{schedule: ccSchedule{SpID: 1001}, count: 3},
		{schedule: ccSchedule{SpID: 1002}, count: 2},
	}

	got := ccProviderLockOrder(allocations)
	want := []int64{1001, 1002, 1003}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lock order = %v, want %v", got, want)
		}
	}

	for i, wantProvider := range []int64{1003, 1001, 1002} {
		if allocations[i].schedule.SpID != wantProvider {
			t.Fatalf("scheduling order changed to %v", allocations)
		}
	}
}
