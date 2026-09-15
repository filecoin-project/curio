package harmonytask

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Committing the interval is the last admission decision before Do. A later
// cancellation belongs to that execution and must not refund the interval.
func TestStartReservationNoCancellationGapAfterCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran, cleaned := false, false
	r := &taskStartReservation{start: func(context.Context) error { cancel(); return nil }, cancel: func() { cleaned = true }}
	done, err := runWithStartReservation(ctx, r, func() (bool, error) { ran = true; return true, nil })
	if !done || err != nil || !ran || !cleaned {
		t.Fatalf("committed start did not enter Do: %v %v", done, err)
	}
}

func TestStartReservationStandaloneCleanupPrecedesCompletion(t *testing.T) {
	for _, mode := range []string{"cancelled-before-entry", "panic-before-entry", "preparation-error"} {
		t.Run(mode, func(t *testing.T) {
			var reserved atomic.Bool
			reserved.Store(true)
			r := &taskStartReservation{start: func(context.Context) error {
				if mode == "panic-before-entry" {
					panic("synthetic preparation panic")
				}
				return errors.New("synthetic preparation error")
			}, cancel: func() { reserved.Store(false) }}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled-before-entry" {
				cancel()
			}
			type outcome struct {
				reserved, ran bool
				err           error
				panic         any
			}
			completion := make(chan outcome, 1)
			release := make(chan struct{})
			joined := make(chan struct{})
			watchdog, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			defer func() {
				close(release)
				select {
				case <-joined:
				case <-watchdog.Done():
					t.Error("test participant did not exit")
				}
			}()
			go func() {
				defer close(joined)
				defer r.cancel() // Existing outer safety defer runs after completion.
				var result outcome
				defer func() {
					result.panic = recover()
					result.reserved = reserved.Load()
					completion <- result
					select {
					case <-release:
					case <-watchdog.Done():
					}
				}()
				_, result.err = runWithStartReservation(ctx, r, func() (bool, error) { result.ran = true; return true, nil })
			}()
			select {
			case result := <-completion:
				if result.reserved {
					t.Fatal("reservation still held when completion persistence begins")
				}
				if result.ran || (mode == "cancelled-before-entry" && !errors.Is(result.err, context.Canceled)) || (mode == "panic-before-entry" && result.panic == nil) || (mode == "preparation-error" && result.err == nil) {
					t.Fatalf("unexpected outcome: %+v", result)
				}
			case <-watchdog.Done():
				t.Fatal("entry did not reach completion")
			}
		})
	}
}
