package harmonydb

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

const (
	adminQueryMaxSQLLen = 64 << 10
	adminQueryMaxRows   = 1000
	adminQueryTimeout   = 10 * time.Minute
)

// AdminQueryResult holds tabular or command output from AdminQuery.
type AdminQueryResult struct {
	Columns      []string   `json:"Columns"`
	Rows         [][]string `json:"Rows"`
	RowsAffected int64      `json:"RowsAffected,omitempty"`
	CommandTag   string     `json:"CommandTag,omitempty"`
	Truncated    bool       `json:"Truncated,omitempty"`
}

// AdminQuery runs arbitrary SQL for trusted admin consoles.
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
	if err := validateSingleStatement(sql); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, adminQueryTimeout)
	defer cancel()

	var result *AdminQueryResult
	var err error
	if isAdminReadQuery(sql) {
		result, err = adminQueryRows(ctx, db, sql)
	} else {
		result, err = adminQueryExec(ctx, db, sql)
	}
	return result, mapAdminQueryError(err)
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

func callErr(out []reflect.Value) error {
	if out[len(out)-1].IsNil() {
		return nil
	}
	return out[len(out)-1].Interface().(error)
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

func adminQueryRows(ctx context.Context, db *DB, sql string) (*AdminQueryResult, error) {
	out, err := callDB(db, "Query", ctx, sql)
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

func adminQueryExec(ctx context.Context, db *DB, sql string) (*AdminQueryResult, error) {
	out, err := callDB(db, "Exec", ctx, sql)
	if err != nil {
		return nil, err
	}
	if err := callErr(out); err != nil {
		return nil, err
	}
	affected := int64(out[0].Interface().(int))
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
