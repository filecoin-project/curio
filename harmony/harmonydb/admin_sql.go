package harmonydb

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unsafe"

	"github.com/yugabyte/pgx/v5"
	"github.com/yugabyte/pgx/v5/pgconn"
)

const (
	adminQueryMaxSQLLen = 64 << 10
	adminQueryMaxRows   = 1000
	adminQueryTimeout   = 30 * time.Second
)

type adminPool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// AdminQueryResult holds tabular or command output from AdminQuery.
type AdminQueryResult struct {
	Columns      []string   `json:"Columns"`
	Rows         [][]string `json:"Rows"`
	RowsAffected int64      `json:"RowsAffected,omitempty"`
	CommandTag   string     `json:"CommandTag,omitempty"`
	Truncated    bool       `json:"Truncated,omitempty"`
}

// AdminQuery runs arbitrary SQL for trusted admin consoles.
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
	if err := validateSingleStatement(sql); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, adminQueryTimeout)
	defer cancel()

	pool, err := adminPoolFromDB(db)
	if err != nil {
		return nil, err
	}
	if isAdminReadQuery(sql) {
		return adminQueryRows(ctx, pool, sql)
	}
	return adminQueryExec(ctx, pool, sql)
}

func adminPoolFromDB(db *DB) (adminPool, error) {
	rv := reflect.ValueOf(db).Elem()
	f := rv.FieldByName("pgx")
	if !f.IsValid() || f.IsNil() {
		return nil, fmt.Errorf("database pool unavailable")
	}
	pool, ok := reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface().(adminPool)
	if !ok {
		return nil, fmt.Errorf("database pool unavailable")
	}
	return pool, nil
}

func validateSingleStatement(sql string) error {
	trimmed := strings.TrimSuffix(sql, ";")
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("multiple statements are not allowed")
	}
	return nil
}

func isAdminReadQuery(sql string) bool {
	fields := strings.Fields(strings.TrimSpace(sql))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToUpper(fields[0]) {
	case "SELECT", "WITH", "SHOW", "EXPLAIN", "TABLE", "VALUES":
		return true
	default:
		return false
	}
}

func adminQueryRows(ctx context.Context, pool adminPool, sql string) (*AdminQueryResult, error) {
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Name
	}

	out := make([][]string, 0)
	truncated := false
	for rows.Next() {
		if len(out) >= adminQueryMaxRows {
			truncated = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(values))
		for i, v := range values {
			row[i] = formatAdminCell(v)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &AdminQueryResult{
		Columns:   columns,
		Rows:      out,
		Truncated: truncated,
	}, nil
}

func adminQueryExec(ctx context.Context, pool adminPool, sql string) (*AdminQueryResult, error) {
	tag, err := pool.Exec(ctx, sql)
	if err != nil {
		return nil, err
	}
	affected := tag.RowsAffected()
	return &AdminQueryResult{
		RowsAffected: affected,
		CommandTag:   fmt.Sprintf("%d rows affected", affected),
	}, nil
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
