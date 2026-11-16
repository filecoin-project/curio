package sqlconsole

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	queryMaxSQLLen = 64 << 10
	queryMaxRows   = 1000
	queryTimeout   = 10 * time.Minute
)

// Result holds tabular output from Query.
type Result struct {
	Columns      []string   `json:"Columns"`
	Rows         [][]string `json:"Rows"`
	RowsAffected int64      `json:"RowsAffected,omitempty"`
	CommandTag   string     `json:"CommandTag,omitempty"`
	Truncated    bool       `json:"Truncated,omitempty"`
}

// Query runs SQL on a dedicated connection authenticated as the restricted
// console LOGIN role. That pool is ensured once per database and reused.
// Access control is DB privileges of that login, not SQL parsing.
func Query(ctx context.Context, db *harmonydb.DB, sql string) (*Result, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, fmt.Errorf("empty query")
	}
	if len(sql) > queryMaxSQLLen {
		return nil, fmt.Errorf("query too long (max %d bytes)", queryMaxSQLLen)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	pool, err := poolFor(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("SQL console ACL: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, mapQueryError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		return nil, mapQueryError(err)
	}

	result, err := queryRows(ctx, tx, sql)
	if err != nil {
		return nil, mapQueryError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapQueryError(err)
	}
	return result, nil
}

func mapQueryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("query cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("query timed out after %s", queryTimeout)
	}
	return err
}

func queryRows(ctx context.Context, tx pgx.Tx, sql string) (*Result, error) {
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	columns := make([]string, len(fds))
	for i, fd := range fds {
		columns[i] = fd.Name
	}

	out := make([][]string, 0)
	truncated := false
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= queryMaxRows {
			truncated = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(values))
		for i, v := range values {
			row[i] = formatCell(v)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &Result{
		Columns:   columns,
		Rows:      out,
		Truncated: truncated,
	}, nil
}

func formatCell(v any) string {
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
