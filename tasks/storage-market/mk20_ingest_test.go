package storage_market

import (
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
)

func TestValidateMK20IngestRows(t *testing.T) {
	candidate := mk20IngestCandidate{ID: "deal", SPID: 1000, AggregationIndex: 7}
	ready := mk20IngestRow{SPID: 1000, AggregationIndex: 7, Aggregated: true}

	tests := []struct {
		name    string
		rows    []mk20IngestRow
		want    bool
		wantErr bool
	}{
		{name: "missing row"},
		{name: "ready nonzero aggregate index", rows: []mk20IngestRow{ready}, want: true},
		{name: "provider changed", rows: []mk20IngestRow{{SPID: 1001, AggregationIndex: 7, Aggregated: true}}},
		{name: "aggregate index changed", rows: []mk20IngestRow{{SPID: 1000, AggregationIndex: 8, Aggregated: true}}},
		{name: "not aggregated", rows: []mk20IngestRow{{SPID: 1000, AggregationIndex: 7}}},
		{name: "complete", rows: []mk20IngestRow{{SPID: 1000, AggregationIndex: 7, Aggregated: true, Complete: true}}},
		{name: "assigned sector", rows: []mk20IngestRow{{SPID: 1000, AggregationIndex: 7, Aggregated: true, Sector: validNullInt64(10)}}},
		{name: "assigned proof", rows: []mk20IngestRow{{SPID: 1000, AggregationIndex: 7, Aggregated: true, RegSealProof: validNullInt64(8)}}},
		{name: "unexpected aggregate rows", rows: []mk20IngestRow{ready, {SPID: 1000, AggregationIndex: 8, Aggregated: true}}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateMK20IngestRows(candidate, test.rows)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("ready = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMK20IngestAttemptStopsAtFailedStage(t *testing.T) {
	testErr := errors.New("test failure")
	tests := []struct {
		name       string
		attempt    mk20IngestAttempt
		wantCalls  []string
		wantCommit bool
		wantErr    error
	}{
		{
			name: "not ready",
			attempt: mk20AttemptRecorder(nil,
				func() (bool, error) { return false, nil },
				func() (mk20SectorAssignment, error) { return mk20SectorAssignment{}, nil },
				func(mk20SectorAssignment) error { return nil }),
			wantCalls: []string{"claim"},
		},
		{
			name: "claim error",
			attempt: mk20AttemptRecorder(nil,
				func() (bool, error) { return false, testErr },
				func() (mk20SectorAssignment, error) { return mk20SectorAssignment{}, nil },
				func(mk20SectorAssignment) error { return nil }),
			wantCalls: []string{"claim"},
			wantErr:   testErr,
		},
		{
			name: "allocation error",
			attempt: mk20AttemptRecorder(nil,
				func() (bool, error) { return true, nil },
				func() (mk20SectorAssignment, error) { return mk20SectorAssignment{}, testErr },
				func(mk20SectorAssignment) error { return nil }),
			wantCalls: []string{"claim", "allocate"},
			wantErr:   testErr,
		},
		{
			name: "persistence error",
			attempt: mk20AttemptRecorder(nil,
				func() (bool, error) { return true, nil },
				func() (mk20SectorAssignment, error) { return mk20SectorAssignment{sector: 11, proof: 8}, nil },
				func(mk20SectorAssignment) error { return testErr }),
			wantCalls: []string{"claim", "allocate", "persist"},
			wantErr:   testErr,
		},
		{
			name: "success",
			attempt: mk20AttemptRecorder(nil,
				func() (bool, error) { return true, nil },
				func() (mk20SectorAssignment, error) { return mk20SectorAssignment{sector: 11, proof: 8}, nil },
				func(mk20SectorAssignment) error { return nil }),
			wantCalls:  []string{"claim", "allocate", "persist"},
			wantCommit: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			test.attempt = mk20AttemptRecorder(&calls, test.attempt.claim, test.attempt.allocate, test.attempt.persist)
			committed, err := runMK20IngestAttempt(test.attempt)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if committed != test.wantCommit {
				t.Fatalf("committed = %v, want %v", committed, test.wantCommit)
			}
			if !reflect.DeepEqual(calls, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, test.wantCalls)
			}
		})
	}
}

func TestMK20IngestRevalidatesStaleDiscoveryBeforeAllocation(t *testing.T) {
	assigned := false
	allocations := 0
	wakes := 0

	run := func() (bool, error) {
		return executeMK20Ingest(func(attempt func(mk20IngestAttempt) (bool, error)) (bool, error) {
			return attempt(mk20IngestAttempt{
				claim: func() (bool, error) { return !assigned, nil },
				allocate: func() (mk20SectorAssignment, error) {
					allocations++
					return mk20SectorAssignment{sector: abi.SectorNumber(allocations), proof: 8}, nil
				},
				persist: func(mk20SectorAssignment) error {
					if assigned {
						return errors.New("duplicate assignment")
					}
					assigned = true
					return nil
				},
			})
		}, func() { wakes++ })
	}

	first, err := run()
	if err != nil {
		t.Fatal(err)
	}
	second, err := run()
	if err != nil {
		t.Fatal(err)
	}
	if !first || second {
		t.Fatalf("commits = (%v, %v), want (true, false)", first, second)
	}
	if allocations != 1 {
		t.Fatalf("allocations = %d, want 1", allocations)
	}
	if wakes != 1 {
		t.Fatalf("wakes = %d, want 1", wakes)
	}
}

func TestMK20IngestRetryDoesNotLeakProvisionalResult(t *testing.T) {
	var persisted []abi.SectorNumber
	wakes := 0

	committed, err := executeMK20Ingest(func(attempt func(mk20IngestAttempt) (bool, error)) (bool, error) {
		firstCommitted, err := attempt(successfulMK20Attempt(101, &persisted))
		if err != nil {
			return false, err
		}
		if !firstCommitted {
			t.Fatal("first provisional attempt did not request commit")
		}

		// Model a serialization retry after the first transaction was rolled back.
		return attempt(mk20IngestAttempt{
			claim: func() (bool, error) { return false, nil },
			allocate: func() (mk20SectorAssignment, error) {
				t.Fatal("retry allocated after the row was no longer ready")
				return mk20SectorAssignment{}, nil
			},
			persist: func(mk20SectorAssignment) error {
				t.Fatal("retry persisted after the row was no longer ready")
				return nil
			},
		})
	}, func() { wakes++ })
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("provisional commit leaked from a rolled-back attempt")
	}
	if wakes != 0 {
		t.Fatalf("wakes = %d, want 0", wakes)
	}
	if !reflect.DeepEqual(persisted, []abi.SectorNumber{101}) {
		t.Fatalf("provisional persistence calls = %v, want [101]", persisted)
	}
}

func TestMK20IngestRetryUsesFinalAuthoritativeAssignment(t *testing.T) {
	var persisted []abi.SectorNumber
	wakes := 0

	committed, err := executeMK20Ingest(func(attempt func(mk20IngestAttempt) (bool, error)) (bool, error) {
		firstCommitted, err := attempt(successfulMK20Attempt(101, &persisted))
		if err != nil {
			return false, err
		}
		if !firstCommitted {
			t.Fatal("first provisional attempt did not request commit")
		}
		return attempt(successfulMK20Attempt(202, &persisted))
	}, func() { wakes++ })
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("final retry did not commit")
	}
	if !reflect.DeepEqual(persisted, []abi.SectorNumber{101, 202}) {
		t.Fatalf("persistence calls = %v, want [101 202]", persisted)
	}
	if wakes != 1 {
		t.Fatalf("wakes = %d, want 1", wakes)
	}
}

func TestMK20IngestSQLProtectsCompositeAssignment(t *testing.T) {
	for _, fragment := range []string{
		"MIN(aggr_index) AS aggr_index",
		"aggregated = TRUE",
		"complete = FALSE",
		"sector IS NULL",
		"reg_seal_proof IS NULL",
	} {
		if !strings.Contains(selectMK20IngestCandidatesSQL, fragment) {
			t.Errorf("candidate SQL is missing %q", fragment)
		}
	}

	for _, fragment := range []string{
		"WHERE id = $1",
		"ORDER BY aggr_index",
		"FOR UPDATE",
	} {
		if !strings.Contains(lockMK20IngestRowsSQL, fragment) {
			t.Errorf("claim SQL is missing %q", fragment)
		}
	}

	for _, fragment := range []string{
		"id = $3",
		"aggr_index = $4",
		"sp_id = $5",
		"aggregated = TRUE",
		"complete = FALSE",
		"sector IS NULL",
		"reg_seal_proof IS NULL",
	} {
		if !strings.Contains(persistMK20IngestAssignmentSQL, fragment) {
			t.Errorf("persistence SQL is missing %q", fragment)
		}
	}
}

func TestProcessMK20DealIngestionUsesAtomicCandidatePath(t *testing.T) {
	source, err := os.ReadFile("mk20.go")
	if err != nil {
		t.Fatal(err)
	}

	start := strings.Index(string(source), "func (d *CurioStorageDealMarket) processMK20DealIngestion")
	end := strings.Index(string(source), "func (d *CurioStorageDealMarket) migratePieceCIDV2")
	if start < 0 || end <= start {
		t.Fatal("could not locate MK20 ingestion production function")
	}
	body := string(source[start:end])
	if !strings.Contains(body, "ingestMK20Candidate(") {
		t.Fatal("production ingestion does not use the atomic candidate transaction path")
	}
	if strings.Contains(body, "d.db.BeginTransaction(") {
		t.Fatal("production ingestion bypasses the tested candidate transaction helper")
	}
}

func mk20AttemptRecorder(calls *[]string, claim func() (bool, error), allocate func() (mk20SectorAssignment, error), persist func(mk20SectorAssignment) error) mk20IngestAttempt {
	return mk20IngestAttempt{
		claim: func() (bool, error) {
			if calls != nil {
				*calls = append(*calls, "claim")
			}
			return claim()
		},
		allocate: func() (mk20SectorAssignment, error) {
			if calls != nil {
				*calls = append(*calls, "allocate")
			}
			return allocate()
		},
		persist: func(assignment mk20SectorAssignment) error {
			if calls != nil {
				*calls = append(*calls, "persist")
			}
			return persist(assignment)
		},
	}
}

func successfulMK20Attempt(sector abi.SectorNumber, persisted *[]abi.SectorNumber) mk20IngestAttempt {
	return mk20IngestAttempt{
		claim: func() (bool, error) { return true, nil },
		allocate: func() (mk20SectorAssignment, error) {
			return mk20SectorAssignment{sector: sector, proof: 8}, nil
		},
		persist: func(assignment mk20SectorAssignment) error {
			*persisted = append(*persisted, assignment.sector)
			return nil
		},
	}
}

func validNullInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}
