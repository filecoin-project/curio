package harmonytask

import (
	"context"
	"errors"
	"testing"
)

type startReadinessStub struct {
	*admissionFixtureTask
	blocked bool
}

func (s *startReadinessStub) TaskStartBlocked() bool { return s.blocked }

func TestStartReadinessSkipsOnlyKnownWait(t *testing.T) {
	for _, source := range []string{workSourcePoller, workSourceRecover, workSourcePreempt} {
		t.Run(source, func(t *testing.T) {
			queries := 0
			h, impl, reservation, _ := newAdmissionFixture(t, func(context.Context, []TaskID) ([]TaskID, error) {
				queries++
				return nil, errors.New("synthetic readiness unavailable")
			})
			gate := &startReadinessStub{admissionFixtureTask: impl, blocked: true}
			h.TaskInterface = gate
			h.accept.Add([]int64{1})
			store := newMemoryAttemptStore()
			claim := func([]TaskID, int) ([]TaskID, error) { t.Fatal("readiness failure reached claim"); return nil, nil }
			release := func([]TaskID, map[TaskID]string) error { t.Fatal("unclaimed task released"); return nil }
			for range 5 {
				if h.considerWorkWithOwnership(source, []task{{ID: 1}}, eventEmitter{}, claim, release, store) {
					t.Fatal("known wait accepted")
				}
			}
			if queries != 0 || len(reservation.ids) != 0 {
				t.Fatal("known wait queried or reserved")
			}
			gate.blocked = false
			if h.considerWorkWithOwnership(source, []task{{ID: 1}}, eventEmitter{}, claim, release, store) {
				t.Fatal("readiness error accepted")
			}
			if queries != 1 {
				t.Fatalf("due admission skipped revalidation: %d", queries)
			}
			assertAdmissionStopped(t, h, impl, reservation, store)
		})
	}
}
