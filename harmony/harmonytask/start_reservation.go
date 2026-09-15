package harmonytask

import "context"

// A read-only, process-local fast refusal. A false result is NOT permission to
// start: eligibility, claim and reservation are still checked normally. Do not
// perform I/O, reserve a token or move the phase deadline in this method.
type taskStartReadiness interface {
	TaskStartBlocked() bool
}

// taskStartReserver is optional. CanAccept may be cached or called speculatively;
// this hook runs only after ordinary eligibility/resource checks, before claim.
// A nil start/cancel pair preserves the unpaced batch path. Otherwise the
// reservation covers one task, and start is called immediately before Do.
// start must be short and non-I/O: it participates in the atomic decision
// between entering Do and cancelling a pending admission.
type taskStartReserver interface {
	ReserveTaskStart(TaskID) (start func(context.Context) error, cancel func(), allowed bool)
}

type taskStartReservation struct {
	start  func(context.Context) error
	cancel func()
}

func reserveTaskStart(impl TaskInterface, ids []TaskID) ([]TaskID, *taskStartReservation) {
	gate, ok := impl.(taskStartReserver)
	if !ok || len(ids) == 0 {
		return ids, nil
	}
	start, cancel, allowed := gate.ReserveTaskStart(ids[0])
	if !allowed {
		return nil, nil
	}
	if start == nil && cancel == nil {
		return ids, nil
	}
	if start == nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil
	}
	return ids[:1], &taskStartReservation{start: start, cancel: cancel}
}

func runWithStartReservation(ctx context.Context, reservation *taskStartReservation, run func() (bool, error)) (bool, error) {
	return withStartReservationCleanup(reservation, func() (bool, error) {
		if reservation != nil {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if err := reservation.start(ctx); err != nil {
				return false, err
			}
		}
		return run()
	})
}

// Release before completion persistence, which can retry indefinitely. The
// callback owns the single entry decision; cleanup never refunds a committed start.
func withStartReservationCleanup(reservation *taskStartReservation, run func() (bool, error)) (bool, error) {
	if reservation != nil {
		defer reservation.cancel()
	}
	return run()
}
