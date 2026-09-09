package sealsupra

import (
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPlanCCAllocationsQuotaAndRemainder(t *testing.T) {
	tests := []struct {
		name      string
		quotas    []int64
		weights   []int64
		requested int64
		want      []int64
	}{
		{"last provider quota", []int64{100, 1}, []int64{1, 1}, 64, []int64{63, 1}},
		{"small remainder", []int64{10, 10, 1}, []int64{1, 1, 1}, 2, []int64{1, 1, 0}},
		{"weighted rounding", []int64{20, 20, 20}, []int64{3, 2, 1}, 10, []int64{6, 3, 1}},
		{"exact weighted shares", []int64{20, 20, 20}, []int64{3, 2, 1}, 12, []int64{6, 4, 2}},
		{"zero quota", []int64{0, 3, 8}, []int64{9, 3, 1}, 7, []int64{0, 3, 4}},
		{"negative old quota", []int64{-2, 8}, []int64{3, 1}, 4, []int64{0, 4}},
		{"exact exhaustion", []int64{1, 2, 3}, []int64{3, 2, 1}, 6, []int64{1, 2, 3}},
		{"zero request", []int64{1}, []int64{1}, 0, []int64{0}},
		{"large weights", []int64{128, 128}, []int64{math.MaxInt64 / 2, math.MaxInt64 / 2}, 128, []int64{64, 64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			schedules := make([]ccSchedule, len(tt.quotas))
			for i := range schedules {
				schedules[i] = ccSchedule{SpID: int64(1001 + i), ToSeal: tt.quotas[i], Weight: tt.weights[i], DurationDays: 200}
			}
			before := append([]ccSchedule(nil), schedules...)
			allocations, err := planCCAllocations(schedules, tt.requested)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]int64, len(schedules))
			var total int64
			for _, allocation := range allocations {
				i := int(allocation.schedule.SpID - 1001)
				if allocation.count <= 0 || allocation.count > schedules[i].ToSeal || got[i] != 0 {
					t.Fatalf("invalid or duplicate allocation: %+v", allocation)
				}
				got[i] = allocation.count
				total += allocation.count
			}
			if !reflect.DeepEqual(got, tt.want) || total != tt.requested {
				t.Fatalf("counts = %v (total %d), want %v (total %d)", got, total, tt.want, tt.requested)
			}
			if !reflect.DeepEqual(schedules, before) {
				t.Fatal("planner mutated schedules")
			}
		})
	}
}

func TestPlanCCAllocationsFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		schedules []ccSchedule
		requested int64
	}{
		{"missing schedules", nil, 1},
		{"negative request", []ccSchedule{{Weight: 1, ToSeal: 3}}, -1},
		{"zero weight", []ccSchedule{{Weight: 0, ToSeal: 3}}, 1},
		{"negative weight", []ccSchedule{{Weight: -1, ToSeal: 3}}, 1},
		{"weight sum overflow", []ccSchedule{{Weight: math.MaxInt64, ToSeal: 3}, {Weight: 1, ToSeal: 3}}, 1},
		{"insufficient quota", []ccSchedule{{Weight: 1, ToSeal: 1}, {Weight: 1, ToSeal: 1}}, 3},
		{"exhausted quotas", []ccSchedule{{Weight: 1}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := planCCAllocations(tt.schedules, tt.requested)
			if err == nil || got != nil {
				t.Fatalf("allocation = %+v, error = %v; want no partial plan and an error", got, err)
			}
		})
	}
}

func TestDebitCCQuotaChecksRowsAndPropagatesErrors(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, tt := range []struct {
		name string
		rows int
		err  error
	}{
		{"success", 1, nil},
		{"stale quota or disabled provider", 0, nil},
		{"unexpected row count", 2, nil},
		{"database error", 0, dbErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			err := debitCCQuota(ccAllocation{schedule: ccSchedule{SpID: 1001}, count: 3}, func(count, spID int64) (int, error) {
				calls++
				if count != 3 || spID != 1001 {
					t.Fatalf("incorrect debit scope: count=%d provider=%d", count, spID)
				}
				return tt.rows, tt.err
			})
			if calls != 1 || (err == nil) != (tt.rows == 1 && tt.err == nil) {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("lost database error: %v", err)
			}
		})
	}
}

// This checks SQL/call-site shape, not database row effects or concurrency.
func TestCCQuotaProductionCallSite(t *testing.T) {
	for _, predicate := range []string{"to_seal = to_seal - $1", "sp_id = $2", "enabled = TRUE", "to_seal >= $1"} {
		if !strings.Contains(ccSchedulerDebitSQL, predicate) {
			t.Fatalf("missing debit predicate %q", predicate)
		}
	}
	source, err := os.ReadFile("task_supraseal.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{
		"allocations, err := planCCAllocations(enabledSchedules, toSeal)",
		"ORDER BY weight DESC, sp_id ASC",
		"err = debitCCQuota(allocation, func(count, spID int64) (int, error)",
		"return tx.Exec(ccSchedulerDebitSQL, count, spID)",
		"return false, xerrors.Errorf(\"getting CC scheduler claims: %w\", err)",
	} {
		if !strings.Contains(string(source), call) {
			t.Fatalf("production call-site missing %q", call)
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
