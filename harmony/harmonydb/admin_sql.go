package harmonydb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yugabyte/pgx/v5"
)

const (
	adminQueryMaxSQLLen = 64 << 10
	adminQueryMaxRows   = 1000
	adminQueryTimeout   = 10 * time.Minute
)

// ybAnalyzeGUCUnavailable is set once SET yb_make_next_ddl_statement_nonincrementing
// fails. The backing DB does not change for the life of a process.
var ybAnalyzeGUCUnavailable atomic.Bool

// AdminQueryResult holds tabular or command output from AdminQuery.
type AdminQueryResult struct {
	Columns      []string   `json:"Columns"`
	Rows         [][]string `json:"Rows"`
	RowsAffected int64      `json:"RowsAffected,omitempty"`
	CommandTag   string     `json:"CommandTag,omitempty"`
	Truncated    bool       `json:"Truncated,omitempty"`
}

// AdminQuery runs SQL for the admin GUI console as curio_sql_console
// (SELECT-only, no eth_keys). The role is ensured once on first console query.
// Access control is the DB role, not SQL parsing.
//
// HarmonyQuery's Query/Exec only accept compile-time string literals
// (rawStringOnly). Admin consoles and ANALYZE need a runtime string, so we
// call those public methods via reflect instead of reaching into the
// unexported pool.
func AdminQuery(ctx context.Context, db *DB, sql string) (*AdminQueryResult, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, fmt.Errorf("empty query")
	}
	if len(sql) > adminQueryMaxSQLLen {
		return nil, fmt.Errorf("query too long (max %d bytes)", adminQueryMaxSQLLen)
	}
	if err := ensureSQLConsoleACLOnce(db); err != nil {
		return nil, fmt.Errorf("SQL console ACL: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, adminQueryTimeout)
	defer cancel()

	var result *AdminQueryResult
	_, err := db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
		if err := applyAdminConsoleSession(tx); err != nil {
			return false, err
		}
		var qErr error
		result, qErr = adminQueryRowsTx(ctx, tx, sql)
		return qErr == nil, qErr
	})
	return result, mapAdminQueryError(err)
}

// applyAdminConsoleSession switches this transaction only to the limited SQL
// console role. ensureSQLConsoleACLOnce must have succeeded first. Does not
// change the pool login role used by the rest of Curio.
func applyAdminConsoleSession(tx *Tx) error {
	if _, err := tx.Exec(`SET LOCAL ROLE curio_sql_console`); err != nil {
		return fmt.Errorf("SQL console ACL role unavailable: %w", err)
	}
	if _, err := tx.Exec(`SET LOCAL lock_timeout = '5s'`); err != nil {
		return err
	}
	return nil
}

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
	if len(analyzeSQL) > adminQueryMaxSQLLen {
		return fmt.Errorf("query too long (max %d bytes)", adminQueryMaxSQLLen)
	}

	if ybAnalyzeGUCUnavailable.Load() {
		_, err := db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
			return execAnalyze(tx, analyzeSQL)
		})
		return mapAdminQueryError(err)
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
	return mapAdminQueryError(err)
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
	if len(countSQL) > adminQueryMaxSQLLen {
		return 0, fmt.Errorf("query too long (max %d bytes)", adminQueryMaxSQLLen)
	}

	out, err := callDB(db, "Query", ctx, countSQL)
	if err != nil {
		return 0, err
	}
	if err := callErr(out); err != nil {
		return 0, mapAdminQueryError(err)
	}
	q, ok := out[0].Interface().(*Query)
	if !ok || q == nil {
		return 0, fmt.Errorf("database query unavailable")
	}
	defer q.Close()

	if !q.Next() {
		if err := q.Err(); err != nil {
			return 0, mapAdminQueryError(err)
		}
		return 0, fmt.Errorf("COUNT(*) returned no rows")
	}

	var n int64
	if err := q.Scan(&n); err != nil {
		return 0, mapAdminQueryError(err)
	}
	return n, nil
}

func mapAdminQueryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("query cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("query timed out after %s", adminQueryTimeout)
	}
	return err
}

// callDB invokes DB.Query or DB.Exec with a runtime SQL string.
func callDB(db *DB, method string, ctx context.Context, sql string) ([]reflect.Value, error) {
	m := reflect.ValueOf(db).MethodByName(method)
	if !m.IsValid() {
		return nil, fmt.Errorf("database %s unavailable", strings.ToLower(method))
	}
	sqlArg := reflect.ValueOf(sql).Convert(m.Type().In(1))
	return m.Call([]reflect.Value{reflect.ValueOf(ctx), sqlArg}), nil
}

func callTx(tx *Tx, method, sql string) ([]reflect.Value, error) {
	m := reflect.ValueOf(tx).MethodByName(method)
	if !m.IsValid() {
		return nil, fmt.Errorf("database %s unavailable", strings.ToLower(method))
	}
	sqlArg := reflect.ValueOf(sql).Convert(m.Type().In(0))
	return m.Call([]reflect.Value{sqlArg}), nil
}

func callErr(out []reflect.Value) error {
	if out[len(out)-1].IsNil() {
		return nil
	}
	return out[len(out)-1].Interface().(error)
}

func adminQueryRowsTx(ctx context.Context, tx *Tx, sql string) (*AdminQueryResult, error) {
	out, err := callTx(tx, "Query", sql)
	if err != nil {
		return nil, err
	}
	if err := callErr(out); err != nil {
		return nil, err
	}
	q := out[0].Interface().(*Query)
	defer q.Close()

	columns, err := queryColumnNames(q)
	if err != nil {
		return nil, err
	}

	rows := make([][]string, 0)
	truncated := false
	for q.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(rows) >= adminQueryMaxRows {
			truncated = true
			break
		}
		values, err := q.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(values))
		for i, v := range values {
			row[i] = formatAdminCell(v)
		}
		rows = append(rows, row)
	}
	if err := q.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &AdminQueryResult{
		Columns:   columns,
		Rows:      rows,
		Truncated: truncated,
	}, nil
}

func queryColumnNames(q *Query) ([]string, error) {
	if q.Qry == nil {
		return nil, fmt.Errorf("database query unavailable")
	}
	m := reflect.ValueOf(q.Qry).MethodByName("FieldDescriptions")
	if !m.IsValid() {
		return nil, fmt.Errorf("database query unavailable")
	}
	fds := m.Call(nil)[0]
	names := make([]string, fds.Len())
	for i := 0; i < fds.Len(); i++ {
		name := fds.Index(i).FieldByName("Name")
		if !name.IsValid() {
			return nil, fmt.Errorf("database query unavailable")
		}
		names[i] = name.String()
	}
	return names, nil
}

func formatAdminCell(v any) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case []byte:
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(v)
	}
}
