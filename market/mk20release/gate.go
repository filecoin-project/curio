// Package mk20release provides the transaction-scoped gate used to move MK20
// DDO deals from the waiting queue into the pipeline.
package mk20release

import (
	"errors"
	"fmt"
)

// ErrGateUnavailable means that the singleton release-gate row could not be
// acquired. Callers must fail closed; falling back to an ungated release would
// invalidate the cross-process active-row limit.
var ErrGateUnavailable = errors.New("MK20 release gate is unavailable")

// Outcome describes the result of one whole-deal release attempt.
type Outcome string

const (
	// Released means that the pipeline rows and waiting-row deletion committed.
	Released Outcome = "released"
	// AtCapacity means that the configured active-row cap has no free slots.
	AtCapacity Outcome = "at_capacity"
	// DoesNotFit means that this deal is valid but does not fit the slots
	// currently available under the configured cap.
	DoesNotFit Outcome = "does_not_fit"
	// TooLarge means that this whole deal costs more rows than the configured
	// cap could ever accommodate.
	TooLarge Outcome = "too_large"
	// NoLongerWaiting means that another release already removed the candidate.
	NoLongerWaiting Outcome = "no_longer_waiting"
)

// Plan describes one atomic deal release. Rows is the exact number of
// market_mk20_pipeline rows that Insert will create for the deal ID. Insert is
// run in the same transaction used for the gate and all postconditions.
//
// Prepare and Insert can be called again when HarmonyDB retries a serialization
// failure. They must perform only retry-safe database work and must not perform
// external side effects.
type Plan struct {
	Rows   int64
	Insert func() error
}

// transaction deliberately exposes only the operations needed to enforce the
// release invariant. The production implementation wraps harmonydb.Tx; model
// implementations exercise orchestration but do not prove database isolation.
type transaction interface {
	lock() error
	waiting(string) (bool, error)
	active() (int64, error)
	dealRows(string) (int64, error)
	removeWaiting(string) (int, error)
}

type beginTransaction func(func(transaction) (bool, error)) (bool, error)

// release runs stage inside a retry-capable transaction runner. Keeping this
// orchestration separate makes the important retry rule explicit: outcome is
// reset every time the transaction callback is re-executed, and Released is
// never returned until the runner confirms that the transaction committed.
func release(id string, maxActive int64, begin beginTransaction, prepare func(transaction) (Plan, error)) (Outcome, error) {
	if id == "" || maxActive < 0 || begin == nil || prepare == nil {
		return "", fmt.Errorf("invalid MK20 release arguments")
	}

	var result Outcome
	committed, err := begin(func(tx transaction) (bool, error) {
		// HarmonyDB OptionRetry can execute this callback more than once.
		result = ""
		outcome, err := stage(tx, id, maxActive, func() (Plan, error) {
			return prepare(tx)
		})
		if err != nil {
			return false, err
		}
		result = outcome
		return outcome == Released, nil
	})
	if err != nil {
		return "", fmt.Errorf("MK20 release transaction: %w", err)
	}
	if committed != (result == Released) {
		return "", fmt.Errorf("MK20 release outcome %q does not match transaction commit=%t", result, committed)
	}
	return result, nil
}

// stage performs one provisional release inside ONE transaction. maxActive=0
// disables the active-row cap, but does not disable the singleton gate.
func stage(tx transaction, id string, maxActive int64, prepare func() (Plan, error)) (Outcome, error) {
	if tx == nil || id == "" || maxActive < 0 || prepare == nil {
		return "", fmt.Errorf("MK20 release requires a transaction, deal ID, non-negative cap and prepare callback")
	}

	// This real row write must be the first authoritative database operation.
	// It serializes participating release transactions across Curio processes.
	if err := tx.lock(); err != nil {
		return "", fmt.Errorf("locking MK20 release gate: %w", err)
	}

	waiting, err := tx.waiting(id)
	if err != nil {
		return "", fmt.Errorf("checking MK20 waiting row: %w", err)
	}
	if !waiting {
		return NoLongerWaiting, nil
	}

	// Waiting and pipeline rows for the same deal must never coexist on the
	// normal release path. Preserve the anomalous state for diagnosis.
	prior, err := tx.dealRows(id)
	if err != nil {
		return "", fmt.Errorf("checking existing MK20 deal rows: %w", err)
	}
	if prior != 0 {
		return "", fmt.Errorf("MK20 deal %s is both waiting and already in pipeline (%d rows)", id, prior)
	}

	var active int64
	if maxActive > 0 {
		active, err = tx.active()
		if err != nil {
			return "", fmt.Errorf("counting incomplete MK20 rows: %w", err)
		}
		if active < 0 {
			return "", fmt.Errorf("negative incomplete MK20 row count: %d", active)
		}
		if active >= maxActive {
			return AtCapacity, nil
		}
	}

	plan, err := prepare()
	if err != nil {
		return "", fmt.Errorf("preparing MK20 deal %s: %w", id, err)
	}
	if plan.Rows <= 0 || plan.Insert == nil {
		return "", fmt.Errorf("MK20 deal %s has an invalid release plan", id)
	}

	if maxActive > 0 {
		if plan.Rows > maxActive {
			return TooLarge, nil
		}
		// Subtraction is overflow-safe even when the configured cap is close
		// to math.MaxInt64.
		if plan.Rows > maxActive-active {
			return DoesNotFit, nil
		}
	}

	if err := plan.Insert(); err != nil {
		return "", fmt.Errorf("inserting MK20 deal %s: %w", id, err)
	}

	inserted, err := tx.dealRows(id)
	if err != nil {
		return "", fmt.Errorf("checking inserted MK20 deal rows: %w", err)
	}
	if inserted != plan.Rows {
		return "", fmt.Errorf("MK20 deal %s inserted %d pipeline rows, expected %d", id, inserted, plan.Rows)
	}

	if maxActive > 0 {
		after, err := tx.active()
		if err != nil {
			return "", fmt.Errorf("checking MK20 release cap: %w", err)
		}
		if after < 0 || after > maxActive {
			return "", fmt.Errorf("MK20 release cap invariant failed: active=%d cap=%d", after, maxActive)
		}
	}

	n, err := tx.removeWaiting(id)
	if err != nil {
		return "", fmt.Errorf("removing MK20 waiting row: %w", err)
	}
	if n != 1 {
		return "", fmt.Errorf("MK20 deal %s removed %d waiting rows, expected one", id, n)
	}

	return Released, nil
}
