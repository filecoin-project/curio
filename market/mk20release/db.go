package mk20release

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// Release atomically moves one waiting deal through the MK20 release gate.
// maxActive is a global cap on incomplete pipeline ROWS; zero disables the cap
// while retaining cross-process serialization. A negative value is invalid.
//
// prepare must read the deal through the supplied transaction and return its
// exact whole-deal row cost. Plan.Insert must use that same transaction. The
// waiting-row deletion is owned by Release. All callback writes are rolled back
// on deferral or error, and Released is returned only after commit succeeds.
func Release(ctx context.Context, db *harmonydb.DB, id string, maxActive int64, prepare func(*harmonydb.Tx) (Plan, error)) (Outcome, error) {
	if db == nil || prepare == nil {
		return "", fmt.Errorf("invalid MK20 release arguments")
	}

	return release(id, maxActive, func(callback func(transaction) (bool, error)) (bool, error) {
		return db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			return callback(dbTransaction{tx: tx})
		}, harmonydb.OptionRetry())
	}, func(tx transaction) (Plan, error) {
		return prepare(tx.(dbTransaction).tx)
	})
}

type dbTransaction struct {
	tx *harmonydb.Tx
}

func (d dbTransaction) lock() error {
	n, err := d.tx.Exec(`UPDATE market_mk20_release_gate
		SET token = NOT token
		WHERE singleton = TRUE`)
	if err != nil {
		return fmt.Errorf("%w: updating singleton row: %w", ErrGateUnavailable, err)
	}
	if n != 1 {
		return fmt.Errorf("%w: expected one singleton row, updated %d", ErrGateUnavailable, n)
	}
	return nil
}

func (d dbTransaction) waiting(id string) (bool, error) {
	var exists bool
	err := d.tx.QueryRow(`SELECT EXISTS (
		SELECT 1
		FROM market_mk20_pipeline_waiting
		WHERE id = $1
	)`, id).Scan(&exists)
	return exists, err
}

func (d dbTransaction) active() (int64, error) {
	var count int64
	err := d.tx.QueryRow(`SELECT COUNT(*)
		FROM market_mk20_pipeline
		WHERE complete = FALSE`).Scan(&count)
	return count, err
}

func (d dbTransaction) dealRows(id string) (int64, error) {
	var count int64
	err := d.tx.QueryRow(`SELECT COUNT(*)
		FROM market_mk20_pipeline
		WHERE id = $1`, id).Scan(&count)
	return count, err
}

func (d dbTransaction) removeWaiting(id string) (int, error) {
	return d.tx.Exec(`DELETE FROM market_mk20_pipeline_waiting WHERE id = $1`, id)
}
