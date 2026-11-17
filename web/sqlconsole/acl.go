package sqlconsole

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yugabyte/pgx/v5"
	"github.com/yugabyte/pgx/v5/pgxpool"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// consoleUser is a LOGIN role used only by the GUI SQL console. It has SELECT
// on schema tables and no privileges on eth_keys. Console queries open a real
// connection as this user so SESSION AUTHORIZATION DEFAULT/RESET cannot
// escalate to the pool login role.
const consoleUser = "curio_sql_console"

const (
	aclTimeout    = 30 * time.Second
	maxConns      = 2
	passwordBytes = 32
)

var (
	poolMu sync.Mutex
	pools  = map[string]*pgxpool.Pool{} // database name -> pool
	inited = map[string]error{}         // database name -> ensure error
)

// poolFor returns a reused pool logged in as consoleUser for db.
func poolFor(ctx context.Context, db *harmonydb.DB) (*pgxpool.Pool, error) {
	dbName, err := harmonydb.DatabaseName(db)
	if err != nil {
		return nil, err
	}

	poolMu.Lock()
	defer poolMu.Unlock()

	if pool, ok := pools[dbName]; ok {
		return pool, nil
	}
	if err, ok := inited[dbName]; ok && err != nil {
		return nil, err
	}

	ensureCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ensureCtx, cancel = context.WithTimeout(ctx, aclTimeout)
		defer cancel()
	}

	pool, err := ensure(ensureCtx, db)
	inited[dbName] = err
	if err != nil {
		return nil, err
	}
	pools[dbName] = pool
	return pool, nil
}

// ensure creates/refreshes the console LOGIN user, ACLs, password, and pool.
// Caller holds poolMu.
func ensure(ctx context.Context, db *harmonydb.DB) (*pgxpool.Pool, error) {
	if err := ensureRoleAndACL(ctx, db); err != nil {
		return nil, err
	}
	password, err := ensurePassword(ctx, db)
	if err != nil {
		return nil, err
	}
	return openPool(ctx, db, password)
}

// ensureRoleAndACL creates the restricted console LOGIN user and grants.
func ensureRoleAndACL(ctx context.Context, db *harmonydb.DB) error {
	var exists bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'curio_sql_console')`).Scan(&exists); err != nil {
		return xerrors.Errorf("check SQL console user: %w", err)
	}
	if !exists {
		if db.ReadOnly() {
			return xerrors.Errorf("SQL console user %s is missing (readonly mode cannot create it)", consoleUser)
		}
		// LOGIN so the console can open its own connection. NOINHERIT so even
		// if someone later grants this user other roles, it does not pick them up.
		if _, err := db.Exec(ctx, `
			CREATE ROLE curio_sql_console
				LOGIN
				NOSUPERUSER
				NOCREATEDB
				NOCREATEROLE
				NOINHERIT`); err != nil {
			return xerrors.Errorf("create SQL console user: %w", err)
		}
	} else if !db.ReadOnly() {
		if err := harmonydb.RuntimeExec(ctx, db, `
			ALTER ROLE curio_sql_console
				LOGIN
				NOSUPERUSER
				NOCREATEDB
				NOCREATEROLE
				NOINHERIT`); err != nil {
			return xerrors.Errorf("alter SQL console user: %w", err)
		}
	}

	var schema string
	if err := db.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return xerrors.Errorf("current_schema: %w", err)
	}
	if schema == "" {
		schema = "public"
	}
	sch := pgx.Identifier{schema}.Sanitize()
	user := pgx.Identifier{consoleUser}.Sanitize()

	for _, q := range []string{
		fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", sch, user),
		fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s", sch, user),
		fmt.Sprintf("GRANT SELECT ON ALL SEQUENCES IN SCHEMA %s TO %s", sch, user),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON TABLES TO %s", sch, user),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON SEQUENCES TO %s", sch, user),
	} {
		if err := harmonydb.RuntimeExec(ctx, db, q); err != nil {
			return xerrors.Errorf("SQL console ACL grant: %w", err)
		}
	}

	for _, table := range []string{"eth_keys", "gui_sql_console_auth"} {
		var has bool
		if err := db.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = current_schema() AND table_name = $1
			)`, table).Scan(&has); err != nil {
			return xerrors.Errorf("check %s: %w", table, err)
		}
		if has {
			revoke := "REVOKE ALL ON TABLE " + pgx.Identifier{schema, table}.Sanitize() + " FROM " + user
			if err := harmonydb.RuntimeExec(ctx, db, revoke); err != nil {
				return xerrors.Errorf("revoke %s from SQL console user: %w", table, err)
			}
		}
	}

	var poolUser string
	if err := db.QueryRow(ctx, `SELECT current_user`).Scan(&poolUser); err != nil {
		return xerrors.Errorf("current_user: %w", err)
	}
	if poolUser != consoleUser {
		_ = harmonydb.RuntimeExec(ctx, db, "REVOKE "+user+" FROM "+pgx.Identifier{poolUser}.Sanitize())
	}
	return nil
}

// ensurePassword loads or creates the shared console password and applies it
// to the role. Stored in-DB so every Curio node uses the same login.
func ensurePassword(ctx context.Context, db *harmonydb.DB) (string, error) {
	if !db.ReadOnly() {
		if err := harmonydb.RuntimeExec(ctx, db, `
			CREATE TABLE IF NOT EXISTS gui_sql_console_auth (
				singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
				password TEXT NOT NULL
			)`); err != nil {
			return "", xerrors.Errorf("create gui_sql_console_auth: %w", err)
		}
		user := pgx.Identifier{consoleUser}.Sanitize()
		_ = harmonydb.RuntimeExec(ctx, db, "REVOKE ALL ON TABLE gui_sql_console_auth FROM "+user)
		_ = harmonydb.RuntimeExec(ctx, db, "REVOKE ALL ON TABLE gui_sql_console_auth FROM PUBLIC")
	}

	var password string
	err := db.QueryRow(ctx, `SELECT password FROM gui_sql_console_auth WHERE singleton`).Scan(&password)
	if err != nil {
		if db.ReadOnly() {
			return "", xerrors.Errorf("SQL console password missing (readonly mode cannot create it): %w", err)
		}
		password, err = generatePassword()
		if err != nil {
			return "", err
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO gui_sql_console_auth (singleton, password) VALUES (TRUE, $1)
			ON CONFLICT (singleton) DO NOTHING`, password); err != nil {
			return "", xerrors.Errorf("store SQL console password: %w", err)
		}
		if err := db.QueryRow(ctx, `SELECT password FROM gui_sql_console_auth WHERE singleton`).Scan(&password); err != nil {
			return "", xerrors.Errorf("load SQL console password: %w", err)
		}
	}

	if db.ReadOnly() {
		return password, nil
	}
	alter := "ALTER ROLE " + pgx.Identifier{consoleUser}.Sanitize() + " PASSWORD " + quoteSQLLiteral(password)
	if err := harmonydb.RuntimeExec(ctx, db, alter); err != nil {
		return "", xerrors.Errorf("set SQL console password: %w", err)
	}
	return password, nil
}

// generatePassword returns a random hex password for the console role.
func generatePassword() (string, error) {
	b := make([]byte, passwordBytes)
	if _, err := rand.Read(b); err != nil {
		return "", xerrors.Errorf("generate SQL console password: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// quoteSQLLiteral returns a single-quoted SQL string literal.
func quoteSQLLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// openPool opens a small pool as consoleUser using db's endpoints.
func openPool(ctx context.Context, db *harmonydb.DB, password string) (*pgxpool.Pool, error) {
	cfg, err := harmonydb.ClonePoolConfig(db, consoleUser, password)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = 0

	connectCtx, cancel := context.WithTimeout(ctx, aclTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(connectCtx, cfg)
	if err != nil {
		return nil, xerrors.Errorf("connect as SQL console user: %w", err)
	}
	var user string
	if err := pool.QueryRow(connectCtx, `SELECT current_user`).Scan(&user); err != nil {
		pool.Close()
		return nil, xerrors.Errorf("SQL console identity check: %w", err)
	}
	if user != consoleUser {
		pool.Close()
		return nil, xerrors.Errorf("SQL console connected as %q, want %q", user, consoleUser)
	}
	return pool, nil
}
