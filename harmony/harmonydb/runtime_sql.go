package harmonydb

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"unsafe"

	"github.com/yugabyte/pgx/v5/pgxpool"
)

// RuntimeExec runs a SQL string that cannot be a compile-time literal
// (HarmonyQuery's Exec only accepts rawStringOnly constants).
func RuntimeExec(ctx context.Context, db *DB, sql string) error {
	out, err := callDB(db, "Exec", ctx, sql)
	if err != nil {
		return err
	}
	return callErr(out)
}

// DatabaseName returns the pool's configured database name.
func DatabaseName(db *DB) (string, error) {
	cfg, err := poolConfig(db)
	if err != nil {
		return "", err
	}
	name := cfg.ConnConfig.Database
	if name == "" {
		return "", fmt.Errorf("database name unavailable")
	}
	return name, nil
}

// ClonePoolConfig returns a copy of db's pool config with a different login.
// Callers set MaxConns and other pool limits themselves before opening.
func ClonePoolConfig(db *DB, username, password string) (*pgxpool.Config, error) {
	src, err := poolConfig(db)
	if err != nil {
		return nil, err
	}
	cfg := src.Copy()
	cfg.ConnConfig.User = username
	cfg.ConnConfig.Password = password
	cfg.AfterConnect = nil
	cfg.BeforeAcquire = nil
	cfg.AfterRelease = nil
	cfg.BeforeClose = nil
	if cfg.ConnConfig != nil {
		cfg.ConnConfig.Tracer = nil
	}
	return cfg, nil
}

// poolConfig reads the unexported harmonyquery pool config from db.
func poolConfig(db *DB) (*pgxpool.Config, error) {
	if db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	v := reflect.ValueOf(db).Elem().FieldByName("cfg")
	if !v.IsValid() || v.IsNil() {
		return nil, fmt.Errorf("database pool config unavailable")
	}
	src := *(**pgxpool.Config)(unsafe.Pointer(v.UnsafeAddr()))
	if src == nil || src.ConnConfig == nil {
		return nil, fmt.Errorf("database pool config unavailable")
	}
	return src, nil
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
