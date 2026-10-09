package harmonytask

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

type admissionFixtureTask struct {
	startReservationStub
	filter  func(context.Context, []TaskID) ([]TaskID, error)
	doCalls atomic.Int32
}

func (s *admissionFixtureTask) FilterCandidates(ctx context.Context, ids []TaskID) ([]TaskID, error) {
	return s.filter(ctx, ids)
}

func (s *admissionFixtureTask) Do(context.Context, TaskID, func() bool) (bool, error) {
	s.doCalls.Add(1)
	return false, errors.New("candidate admission fixture must stop before Do")
}

type admissionFixtureReservation struct {
	active             bool
	ids                []TaskID
	started, cancelled int
}

func newAdmissionFixture(t *testing.T, filter func(context.Context, []TaskID) ([]TaskID, error)) (*taskTypeHandler, *admissionFixtureTask, *admissionFixtureReservation, *stubAcceptTask) {
	t.Helper()
	r := &admissionFixtureReservation{}
	accept := &stubAcceptTask{}
	impl := &admissionFixtureTask{filter: filter, startReservationStub: startReservationStub{
		TaskInterface: accept,
		reserve: func(id TaskID) (func(context.Context) error, func(), bool) {
			if r.active {
				t.Fatal("previous failed admission leaked its reservation")
			}
			r.active = true
			r.ids = append(r.ids, id)
			return func(context.Context) error { r.started++; return nil }, func() { r.active = false; r.cancelled++ }, true
		},
	}}
	h := &taskTypeHandler{
		TaskInterface:   impl,
		TaskTypeDetails: TaskTypeDetails{Name: "CandidateFixture", Max: taskhelp.Max(2), Cost: resources.Resources{Cpu: 1}},
		TaskEngine:      &TaskEngine{cfg: taskEngineConfig{ctx: context.Background(), ownerID: 7, reg: &resources.Reg{Resources: resources.Resources{Cpu: 2}}}},
		running:         runregistry.New(), accept: acceptcache.New(time.Hour), storageFailures: map[TaskID]time.Time{},
	}
	return h, impl, r, accept
}

func assertAdmissionStopped(t *testing.T, h *taskTypeHandler, impl *admissionFixtureTask, r *admissionFixtureReservation, store *memoryAttemptStore) {
	t.Helper()
	if r.active || r.started != 0 || impl.doCalls.Load() != 0 || h.Max.Active() != 0 || len(store.starts) != 0 {
		t.Fatalf("unexpected dispatch/leak: reserved=%v started=%d Do=%d active=%d recorded_starts=%v", r.active, r.started, impl.doCalls.Load(), h.Max.Active(), store.starts)
	}
}

func TestCandidateAdmissionSkipsInvalidHeadsAndRevalidatesPositiveCache(t *testing.T) {
	for _, prefix := range []int{1, 8} {
		for _, cached := range []bool{false, true} {
			good := TaskID(prefix + 1)
			tasks := make([]task, prefix+1)
			ids := make([]TaskID, prefix+1)
			for i := range tasks {
				ids[i] = TaskID(i + 1)
				tasks[i] = task{ID: ids[i]}
			}
			filterCalls := 0
			h, impl, reservation, accept := newAdmissionFixture(t, func(ctx context.Context, got []TaskID) ([]TaskID, error) {
				filterCalls++
				if !reflect.DeepEqual(got, ids) {
					t.Fatalf("filter did not see every candidate: %v", got)
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > candidateFilterTimeout {
					t.Fatal("filter context is unbounded")
				}
				return []TaskID{good}, nil
			})
			if cached {
				h.accept.Add(toInt64s(ids))
			}
			store := newMemoryAttemptStore()
			claims := 0
			ok := h.considerWorkWithOwnership(workSourcePoller, tasks, eventEmitter{},
				func(got []TaskID, _ int) ([]TaskID, error) {
					claims++
					if !reflect.DeepEqual(got, []TaskID{good}) {
						t.Fatalf("invalid head reached claim: %v", got)
					}
					// Stop at the real handler's ownership boundary. This is not
					// a successful native/Do execution test.
					return nil, nil
				}, func([]TaskID, map[TaskID]string) error {
					t.Fatal("lost claim must not release another owner")
					return nil
				}, store)
			if ok || claims != 1 || filterCalls != 1 || !reflect.DeepEqual(reservation.ids, []TaskID{good}) || reservation.cancelled != 1 || len(store.tokens) != 0 {
				t.Fatalf("prefix=%d cached=%v ok=%v claims=%d filters=%d reservations=%v cancels=%d", prefix, cached, ok, claims, filterCalls, reservation.ids, reservation.cancelled)
			}
			if cached && accept.canAcceptCalls.Load() != 0 {
				t.Fatal("ordinary acceptance cache unexpectedly bypassed after readiness revalidation")
			}
			assertAdmissionStopped(t, h, impl, reservation, store)
		}
	}
}

func TestCandidateAdmissionTransientFilterFailureDoesNotConsumeState(t *testing.T) {
	for _, failure := range []error{errors.New("synthetic reference query error"), context.Canceled, context.DeadlineExceeded} {
		filterCalls := 0
		h, impl, reservation, accept := newAdmissionFixture(t, func(context.Context, []TaskID) ([]TaskID, error) {
			filterCalls++
			if filterCalls == 1 {
				return nil, failure
			}
			return []TaskID{2}, nil
		})
		h.accept.Add([]int64{1, 2})
		store := newMemoryAttemptStore()
		claims, releases := 0, 0
		claim := func(ids []TaskID, _ int) ([]TaskID, error) {
			claims++
			if !reflect.DeepEqual(ids, []TaskID{2}) {
				t.Fatalf("wrong next claim: %v", ids)
			}
			return nil, nil
		}
		release := func([]TaskID, map[TaskID]string) error { releases++; return nil }
		if h.considerWorkWithOwnership(workSourcePoller, []task{{ID: 1}, {ID: 2}}, eventEmitter{}, claim, release, store) {
			t.Fatal("transient lookup failure accepted work")
		}
		if claims != 0 || releases != 0 || len(reservation.ids) != 0 || len(h.storageFailures) != 0 || len(store.tokens) != 0 {
			t.Fatal("lookup failure mutated ownership/reservation/cooldown state")
		}
		assertAdmissionStopped(t, h, impl, reservation, store)
		if h.considerWorkWithOwnership(workSourcePoller, []task{{ID: 1}, {ID: 2}}, eventEmitter{}, claim, release, store) {
			t.Fatal("fixture claim should stop dispatch")
		}
		if claims != 1 || releases != 0 || filterCalls != 2 || reservation.cancelled != 1 || accept.canAcceptCalls.Load() != 0 {
			t.Fatalf("lookup retry lost cached work or state: claims=%d releases=%d filters=%d cancels=%d accept_calls=%d", claims, releases, filterCalls, reservation.cancelled, accept.canAcceptCalls.Load())
		}
		assertAdmissionStopped(t, h, impl, reservation, store)
	}
}

func TestCandidateAdmissionMissingReferenceIsNotPermanentExclusion(t *testing.T) {
	ready := false
	h, impl, reservation, _ := newAdmissionFixture(t, func(context.Context, []TaskID) ([]TaskID, error) {
		if ready {
			return []TaskID{1}, nil
		}
		return nil, nil
	})
	h.accept.Add([]int64{1})
	store := newMemoryAttemptStore()
	claims := 0
	claim := func(ids []TaskID, _ int) ([]TaskID, error) { claims++; return nil, nil }
	release := func([]TaskID, map[TaskID]string) error { t.Fatal("no ownership was acquired"); return nil }
	if h.considerWorkWithOwnership(workSourcePoller, []task{{ID: 1}}, eventEmitter{}, claim, release, store) {
		t.Fatal("missing reference admitted")
	}
	if claims != 0 || len(reservation.ids) != 0 || len(h.storageFailures) != 0 {
		t.Fatal("missing reference mutated admission state")
	}
	ready = true
	if h.considerWorkWithOwnership(workSourcePoller, []task{{ID: 1}}, eventEmitter{}, claim, release, store) {
		t.Fatal("fixture claim should stop dispatch")
	}
	if claims != 1 || reservation.cancelled != 1 {
		t.Fatal("restored reference remained excluded or leaked reservation")
	}
	assertAdmissionStopped(t, h, impl, reservation, store)
}

type admissionFixtureStorage struct {
	claim func(int) (func() error, error)
}

func (*admissionFixtureStorage) HasCapacity() bool                    { return true }
func (s *admissionFixtureStorage) Claim(id int) (func() error, error) { return s.claim(id) }

func TestCandidateAdmissionReferenceVanishesBeforeStorageClaim(t *testing.T) {
	for _, mode := range []string{"matching-attempt", "different-owner", "same-owner-new-attempt", "already-started", "recovery"} {
		t.Run(mode, func(t *testing.T) {
			linked := map[TaskID]bool{1: true, 2: true}
			h, impl, reservation, _ := newAdmissionFixture(t, func(_ context.Context, ids []TaskID) ([]TaskID, error) {
				var out []TaskID
				for _, id := range ids {
					if linked[id] {
						out = append(out, id)
					}
				}
				return out, nil
			})
			store := newMemoryAttemptStore()
			owner, token, started, retries := 7, "", false, 3
			storageCalls, claims, releaseCalls := 0, 0, 0
			h.Cost.Storage = &admissionFixtureStorage{claim: func(id int) (func() error, error) {
				storageCalls++
				if id != 1 || store.tokens[1] == "" {
					t.Fatal("storage claim was not preceded by matching attempt preparation")
				}
				token = store.tokens[1]
				linked[1] = false // reference disappeared after advisory filtering
				switch mode {
				case "different-owner":
					owner = 8
				case "same-owner-new-attempt":
					token = "replacement-attempt"
				case "already-started":
					started = true
				}
				return nil, errors.New("synthetic expected 1 sector ref, got 0")
			}}
			claim := func(ids []TaskID, _ int) ([]TaskID, error) {
				claims++
				if !reflect.DeepEqual(ids, []TaskID{1}) {
					t.Fatalf("wrong initial claim: %v", ids)
				}
				return ids, nil
			}
			release := func(ids []TaskID, tokens map[TaskID]string) error {
				if !reflect.DeepEqual(ids, []TaskID{1}) || tokens[1] != store.tokens[1] {
					t.Fatal("handler released wrong task/attempt")
				}
				return releasePreparedTaskOwnership(ids, tokens, 7, func(_ context.Context, failed []int64, attempts []string, expectedOwner int) (int, error) {
					releaseCalls++
					if len(failed) != 1 || failed[0] != 1 || len(attempts) != 1 {
						t.Fatal("release batch was malformed")
					}
					// The exact production SQL is pinned in storage_claim_release_test.
					if owner == expectedOwner && token == attempts[0] && !started {
						owner = 0
						return 1, nil
					}
					return 0, nil
				})
			}
			source := workSourcePoller
			if mode == "recovery" {
				source = workSourceRecover
			}
			if !h.considerWorkWithOwnership(source, []task{{ID: 1}, {ID: 2}}, eventEmitter{}, claim, release, store) {
				t.Fatal("expected pending admission before storage revalidation")
			}
			settleAdmissions(t, h)
			wantClaims := 1
			wantOwner := 7
			if mode == "matching-attempt" || mode == "recovery" {
				wantOwner = 0
			}
			if mode == "different-owner" {
				wantOwner = 8
			}
			if claims != wantClaims || storageCalls != 1 || releaseCalls != 1 || owner != wantOwner || retries != 3 || reservation.cancelled != 1 {
				t.Fatalf("claims=%d storage=%d release=%d owner=%d retries=%d cancels=%d", claims, storageCalls, releaseCalls, owner, retries, reservation.cancelled)
			}
			if mode == "same-owner-new-attempt" && token != "replacement-attempt" {
				t.Fatal("stale release changed the new attempt")
			}
			assertAdmissionStopped(t, h, impl, reservation, store)
			// On the next normal scheduling opportunity the following valid task
			// reaches claim, without waiting out a committed pacing interval.
			followingClaims := 0
			if h.considerWorkWithOwnership(workSourcePoller, []task{{ID: 1}, {ID: 2}}, eventEmitter{},
				func(ids []TaskID, _ int) ([]TaskID, error) {
					followingClaims++
					if !reflect.DeepEqual(ids, []TaskID{2}) {
						t.Fatalf("invalid head blocked following work: %v", ids)
					}
					return nil, nil
				}, release, store) {
				t.Fatal("fixture claim should stop dispatch")
			}
			if followingClaims != 1 || reservation.cancelled != 2 || !reflect.DeepEqual(reservation.ids, []TaskID{1, 2}) || releaseCalls != 1 {
				t.Fatal("failed claim consumed pacing or prevented following work")
			}
			assertAdmissionStopped(t, h, impl, reservation, store)
		})
	}
}
