package harmonydb

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/yugabyte/pgx/v5"
)

const adminSQLMaxLen = 64 << 10

// ybAnalyzeGUCUnavailable is set once SET yb_make_next_ddl_statement_nonincrementing
// fails. The backing DB does not change for the life of a process.
var ybAnalyzeGUCUnavailable atomic.Bool

// AdminAnalyze runs ANALYZE on schema.table. On the same connection it first
// sets yb_make_next_ddl_statement_nonincrementing so Yugabyte does not bump
// the catalog version (which invalidates every live YSQL backend).
// The GUC is Yugabyte-only and SUSET; a missing or denied SET is ignored so
// ANALYZE still runs (local Postgres itests, older YB, non-superuser).
func AdminAnalyze(ctx context.Context, db *DB, schema, table string) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}
	if schema == "" || table == "" {
		return fmt.Errorf("schema and table are required")
	}

	analyzeSQL := "ANALYZE " + pgx.Identifier{schema, table}.Sanitize()
	if len(analyzeSQL) > adminSQLMaxLen {
		return fmt.Errorf("query too long (max %d bytes)", adminSQLMaxLen)
	}

	if ybAnalyzeGUCUnavailable.Load() {
		_, err := db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
			return execAnalyze(tx, analyzeSQL)
		})
		return mapAdminSQLError(err)
	}

	// The YB GUC must be set on the same connection as ANALYZE. A failed SET
	// (Postgres itest, older YB, non-superuser) aborts that transaction, so
	// ANALYZE is retried in a new one and the miss is remembered.
	setFailed := false
	_, err := db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
		if _, setErr := tx.Exec(`SET yb_make_next_ddl_statement_nonincrementing = on`); setErr != nil {
			ybAnalyzeGUCUnavailable.Store(true)
			setFailed = true
			return false, setErr
		}
		return execAnalyze(tx, analyzeSQL)
	})
	if setFailed {
		_, err = db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
			return execAnalyze(tx, analyzeSQL)
		})
	}
	return mapAdminSQLError(err)
}

func execAnalyze(tx *Tx, analyzeSQL string) (bool, error) {
	out, err := callTx(tx, "Exec", analyzeSQL)
	if err != nil {
		return false, err
	}
	if err := callErr(out); err != nil {
		return false, err
	}
	return true, nil
}

// AdminTableCount returns SELECT COUNT(*) FROM schema.table.
// Dynamic identifiers cannot be passed through HarmonyQuery's compile-time SQL
// strings, so this uses the same reflect path as AdminAnalyze.
func AdminTableCount(ctx context.Context, db *DB, schema, table string) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database not configured")
	}
	if schema == "" || table == "" {
		return 0, fmt.Errorf("schema and table are required")
	}

	countSQL := "SELECT COUNT(*) FROM " + pgx.Identifier{schema, table}.Sanitize()
	if len(countSQL) > adminSQLMaxLen {
		return 0, fmt.Errorf("query too long (max %d bytes)", adminSQLMaxLen)
	}

	out, err := callDB(db, "Query", ctx, countSQL)
	if err != nil {
		return 0, err
	}
	if err := callErr(out); err != nil {
		return 0, mapAdminSQLError(err)
	}
	q, ok := out[0].Interface().(*Query)
	if !ok || q == nil {
		return 0, fmt.Errorf("database query unavailable")
	}
	defer q.Close()

	if !q.Next() {
		if err := q.Err(); err != nil {
			return 0, mapAdminSQLError(err)
		}
		return 0, fmt.Errorf("COUNT(*) returned no rows")
	}

	var n int64
	if err := q.Scan(&n); err != nil {
		return 0, mapAdminSQLError(err)
	}
	return n, nil
}

func mapAdminSQLError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("query cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("query timed out")
	}
	return err
}
