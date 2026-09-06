package storageingest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/yugabyte/pgx/v5/pgconn"

	"github.com/filecoin-project/go-state-types/abi"
)

func TestDrainSealProvidersEmptyIsSuccessful(t *testing.T) {
	calls := 0
	err := drainSealProvidersWith(context.Background(), []sealBatchParams{{spID: 1000}}, func(state *sealProviderDrainState) (sealProviderBatchResult, error) {
		calls++
		return sealProviderBatchResult{}, nil
	})
	if err != nil {
		t.Fatalf("empty drain failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("batch calls = %d, want 1", calls)
	}
}

func TestDrainSealProvidersUsesBoundedFairRounds(t *testing.T) {
	remaining := map[int64]int{1001: 2*sealBatchSize + 1, 1002: 2*sealBatchSize + 1}
	var order []int64
	var batchSizes []int
	err := drainSealProvidersWith(context.Background(), []sealBatchParams{{spID: 1001}, {spID: 1002}}, func(state *sealProviderDrainState) (sealProviderBatchResult, error) {
		order = append(order, state.params.spID)
		count := min(remaining[state.params.spID], sealBatchSize)
		remaining[state.params.spID] -= count
		batchSizes = append(batchSizes, count)
		return sealProviderBatchResult{
			candidates: count,
			committed:  count,
			resolved:   count,
			hasMore:    count == sealBatchSize,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := order, []int64{1001, 1002, 1001, 1002, 1001, 1002}; !reflect.DeepEqual(got, want) {
		t.Fatalf("provider order = %v, want %v", got, want)
	}
	if got, want := batchSizes, []int{sealBatchSize, sealBatchSize, sealBatchSize, sealBatchSize, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("batch sizes = %v, want %v", got, want)
	}
	for provider, count := range remaining {
		if count != 0 {
			t.Fatalf("provider %d has %d candidates left", provider, count)
		}
	}
}

func TestDrainSealProviderBatchUsesExactQuantum(t *testing.T) {
	state := sealProviderDrainState{params: sealBatchParams{spID: 1000}, failed: make(map[abi.SectorNumber]struct{})}
	gotLimit := 0
	_, err := drainSealProviderBatchWith(context.Background(), &state,
		func(_ map[abi.SectorNumber]struct{}, limit int) (sealBatchAttempt, error) {
			gotLimit = limit
			return sealBatchAttempt{}, nil
		},
		func(abi.SectorNumber) (sealSectorResult, error) {
			t.Fatal("empty batch must not use individual fallback")
			return sealSectorResult{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if sealBatchSize != 64 {
		t.Fatalf("sealBatchSize = %d, want 64", sealBatchSize)
	}
	if gotLimit != sealBatchSize {
		t.Fatalf("batch limit = %d, want sealBatchSize=%d", gotLimit, sealBatchSize)
	}
}

func TestDrainSealProvidersPartialAndExactlyFullTermination(t *testing.T) {
	tests := []struct {
		name      string
		firstSize int
		wantCalls int
	}{
		{name: "partial", firstSize: sealBatchSize - 1, wantCalls: 1},
		{name: "exactly full", firstSize: sealBatchSize, wantCalls: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var limits []int
			err := drainSealProvidersWith(context.Background(), []sealBatchParams{{spID: 1000}}, func(state *sealProviderDrainState) (sealProviderBatchResult, error) {
				return drainSealProviderBatchWith(context.Background(), state,
					func(_ map[abi.SectorNumber]struct{}, limit int) (sealBatchAttempt, error) {
						limits = append(limits, limit)
						calls++
						if calls > 1 {
							return sealBatchAttempt{}, nil
						}
						candidates := make([]abi.SectorNumber, test.firstSize)
						return sealBatchAttempt{candidates: candidates, committed: len(candidates)}, nil
					},
					func(abi.SectorNumber) (sealSectorResult, error) {
						t.Fatal("successful batch must not use individual fallback")
						return sealSectorResult{}, nil
					},
				)
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls != test.wantCalls {
				t.Fatalf("batch calls = %d, want %d", calls, test.wantCalls)
			}
			for _, limit := range limits {
				if limit != sealBatchSize {
					t.Fatalf("batch limit = %d, want %d", limit, sealBatchSize)
				}
			}
		})
	}
}

func TestDrainSealProvidersDetectsNoProgress(t *testing.T) {
	calls := 0
	err := drainSealProvidersWith(context.Background(), []sealBatchParams{{spID: 1000}}, func(state *sealProviderDrainState) (sealProviderBatchResult, error) {
		calls++
		return sealProviderBatchResult{candidates: sealBatchSize, hasMore: true}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "made no progress") {
		t.Fatalf("drain error = %v, want no-progress error", err)
	}
	if calls != 1 {
		t.Fatalf("batch calls = %d, want 1", calls)
	}
}

func TestDrainSealProviderBatchIsolatesCandidateAndContinues(t *testing.T) {
	const total = sealBatchSize + 1
	poison := abi.SectorNumber(7)
	open := make(map[abi.SectorNumber]bool, total)
	for sector := 1; sector <= total; sector++ {
		open[abi.SectorNumber(sector)] = true
	}

	state := sealProviderDrainState{params: sealBatchParams{spID: 1000}, failed: make(map[abi.SectorNumber]struct{})}
	run := func(state *sealProviderDrainState) (sealProviderBatchResult, error) {
		return drainSealProviderBatchWith(context.Background(), state,
			func(failed map[abi.SectorNumber]struct{}, limit int) (sealBatchAttempt, error) {
				candidates := nextSealTestCandidates(open, failed, limit)
				for _, sector := range candidates {
					if sector == poison {
						return sealBatchAttempt{candidates: candidates}, newSealCandidateError("poison candidate")
					}
				}
				for _, sector := range candidates {
					delete(open, sector)
				}
				return sealBatchAttempt{candidates: candidates, committed: len(candidates)}, nil
			},
			func(sector abi.SectorNumber) (sealSectorResult, error) {
				if sector == poison {
					return sealSectorResult{}, newSealCandidateError("poison sector")
				}
				delete(open, sector)
				return sealSectorResult{disposition: sealSectorMoved}, nil
			},
		)
	}

	err := drainSealProvidersWith(context.Background(), []sealBatchParams{state.params}, run)
	if err == nil || !strings.Contains(err.Error(), "poison sector") {
		t.Fatalf("drain error = %v, want isolated poison warning", err)
	}
	if len(open) != 1 || !open[poison] {
		t.Fatalf("open sectors = %v, want only poison %d", open, poison)
	}
}

func TestDrainSealProviderBatchRetainsOpenPipelineInconsistency(t *testing.T) {
	state := sealProviderDrainState{params: sealBatchParams{spID: 1000}, failed: make(map[abi.SectorNumber]struct{})}
	transfers := 0
	result, err := drainSealProviderBatchWith(context.Background(), &state,
		func(map[abi.SectorNumber]struct{}, int) (sealBatchAttempt, error) {
			return sealBatchAttempt{candidates: []abi.SectorNumber{1, 2}}, newSealCandidateError("batch conflict")
		},
		func(sector abi.SectorNumber) (sealSectorResult, error) {
			candidateState := sealSectorState{openPieces: 1, modeMatches: true, eligible: true}
			if sector == 1 {
				candidateState.pipelineExists = true
				candidateState.pipelineMatches = true
			}
			disposition, classifyErr := classifySealSectorState(state.params, sector, candidateState)
			if classifyErr != nil {
				return sealSectorResult{disposition: disposition}, classifyErr
			}
			transfers++
			return sealSectorResult{disposition: sealSectorMoved}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if transfers != 1 || result.committed != 1 {
		t.Fatalf("transfers=%d committed=%d, want one valid transfer", transfers, result.committed)
	}
	if _, excluded := state.failed[1]; !excluded {
		t.Fatal("inconsistent sector was not retained in the per-pass exclusion set")
	}
	if len(result.warnings) != 1 || !strings.Contains(result.warnings[0].Error(), "while 1 open pieces remain") {
		t.Fatalf("warnings = %v, want the retained inconsistency", result.warnings)
	}
}

func TestDrainSealProviderBatchStopsOnInfrastructureError(t *testing.T) {
	sectorCalls := 0
	state := sealProviderDrainState{params: sealBatchParams{spID: 1000}, failed: make(map[abi.SectorNumber]struct{})}
	_, err := drainSealProviderBatchWith(context.Background(), &state,
		func(map[abi.SectorNumber]struct{}, int) (sealBatchAttempt, error) {
			return sealBatchAttempt{candidates: []abi.SectorNumber{1, 2}}, errors.New("database unavailable")
		},
		func(abi.SectorNumber) (sealSectorResult, error) {
			sectorCalls++
			return sealSectorResult{}, nil
		},
	)
	if err == nil || err.Error() != "database unavailable" {
		t.Fatalf("batch error = %v, want infrastructure error", err)
	}
	if sectorCalls != 0 {
		t.Fatalf("individual fallback calls = %d, want 0", sectorCalls)
	}
}

func TestDrainSealProviderBatchStopsOnSerializationError(t *testing.T) {
	sectorCalls := 0
	state := sealProviderDrainState{params: sealBatchParams{spID: 1000}, failed: make(map[abi.SectorNumber]struct{})}
	_, err := drainSealProviderBatchWith(context.Background(), &state,
		func(map[abi.SectorNumber]struct{}, int) (sealBatchAttempt, error) {
			return sealBatchAttempt{candidates: []abi.SectorNumber{1}}, &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		},
		func(abi.SectorNumber) (sealSectorResult, error) {
			sectorCalls++
			return sealSectorResult{}, nil
		},
	)
	if err == nil {
		t.Fatal("expected serialization error")
	}
	if sectorCalls != 0 {
		t.Fatalf("individual fallback calls = %d, want 0", sectorCalls)
	}
}

func TestDrainSealProviderBatchStopsFallbackAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	state := sealProviderDrainState{params: sealBatchParams{spID: 1000}, failed: make(map[abi.SectorNumber]struct{})}
	sectorCalls := 0
	_, err := drainSealProviderBatchWith(ctx, &state,
		func(map[abi.SectorNumber]struct{}, int) (sealBatchAttempt, error) {
			return sealBatchAttempt{candidates: []abi.SectorNumber{1, 2}}, newSealCandidateError("candidate failure")
		},
		func(abi.SectorNumber) (sealSectorResult, error) {
			sectorCalls++
			cancel()
			return sealSectorResult{disposition: sealSectorMoved}, nil
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fallback error = %v, want context cancellation", err)
	}
	if sectorCalls != 1 {
		t.Fatalf("individual fallback calls = %d, want 1", sectorCalls)
	}
}

func TestClassifySealSectorState(t *testing.T) {
	params := sealBatchParams{spID: 1000}
	tests := []struct {
		name        string
		state       sealSectorState
		want        sealSectorDisposition
		wantErrPart string
	}{
		{name: "ready", state: sealSectorState{openPieces: 1, modeMatches: true, eligible: true}, want: sealSectorReady},
		{name: "not eligible", state: sealSectorState{openPieces: 1, modeMatches: true}, want: sealSectorNotEligible},
		{name: "already transferred", state: sealSectorState{pipelineExists: true, pipelineMatches: true}, want: sealSectorAlreadyTransferred},
		{name: "disappeared", state: sealSectorState{}, want: sealSectorDisappeared, wantErrPart: "disappeared"},
		{name: "non-matching transferred", state: sealSectorState{pipelineExists: true}, want: sealSectorInconsistent, wantErrPart: "non-matching"},
		{name: "open plus pipeline", state: sealSectorState{openPieces: 1, modeMatches: true, eligible: true, pipelineExists: true, pipelineMatches: true}, want: sealSectorInconsistent, wantErrPart: "while 1 open pieces remain"},
		{name: "mixed normal and snap", state: sealSectorState{openPieces: 2, eligible: true}, want: sealSectorInconsistent, wantErrPart: "mixed normal and snap"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := classifySealSectorState(params, 7, test.state)
			if got != test.want {
				t.Fatalf("disposition = %d, want %d", got, test.want)
			}
			if test.wantErrPart == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.wantErrPart != "" && (err == nil || !strings.Contains(err.Error(), test.wantErrPart)) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErrPart)
			}
		})
	}
}

func TestCandidateSelectionRequiresOneOpenSectorMode(t *testing.T) {
	if !strings.Contains(selectSealCandidatesSQL, "HAVING BOOL_AND(is_snap = $2)") {
		t.Fatal("candidate query does not reject mixed normal/Snap sectors")
	}
	beforeGrouping := strings.Split(selectSealCandidatesSQL, "GROUP BY")[0]
	if strings.Contains(beforeGrouping, "is_snap = $2") {
		t.Fatal("candidate query filters pieces by mode before validating the complete sector")
	}
}

func TestDeterministicSealCandidateErrorClassification(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{err: newSealCandidateError("inconsistent candidate"), want: true},
		{err: &pgconn.PgError{Code: "P0001"}, want: true},
		{err: &pgconn.PgError{Code: "23505"}, want: true},
		{err: &pgconn.PgError{Code: "22000"}, want: false},
		{err: &pgconn.PgError{Code: "23502"}, want: false},
		{err: &pgconn.PgError{Code: "40001"}, want: false},
		{err: &pgconn.PgError{Code: "08006"}, want: false},
		{err: context.Canceled, want: false},
		{err: errors.New("unknown"), want: false},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%T-%v", test.err, test.err), func(t *testing.T) {
			if got := isDeterministicSealCandidateError(test.err); got != test.want {
				t.Fatalf("classification = %t, want %t", got, test.want)
			}
		})
	}
}

func nextSealTestCandidates(open map[abi.SectorNumber]bool, failed map[abi.SectorNumber]struct{}, limit int) []abi.SectorNumber {
	available := make([]abi.SectorNumber, 0, len(open))
	for sector := range open {
		if _, excluded := failed[sector]; excluded {
			continue
		}
		available = append(available, sector)
	}
	sort.Slice(available, func(i, j int) bool { return available[i] < available[j] })
	return available[:min(len(available), limit)]
}
