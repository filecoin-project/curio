package harmonydb

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
)

// sqlConsoleUser is a LOGIN role used only by AdminQuery. It has SELECT on
// schema tables and no privileges on eth_keys. Curio's pool login user is
// never granted this role; the console assumes it via SET SESSION AUTHORIZATION.
const sqlConsoleUser = "curio_sql_console"

const sqlConsoleACLTimeout = 30 * time.Second

var (
	sqlConsoleACLOnce sync.Once
	sqlConsoleACLErr  error
)

// ensureSQLConsoleACLOnce creates/refreshes the SQL console user on first use.
func ensureSQLConsoleACLOnce(db *DB) error {
	sqlConsoleACLOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), sqlConsoleACLTimeout)
		defer cancel()
		sqlConsoleACLErr = ensureSQLConsoleACL(ctx, db)
	})
	return sqlConsoleACLErr
}

// ensureSQLConsoleACL creates the restricted console LOGIN user and its ACLs.
// Does not grant that user to Curio's shared pool role.
func ensureSQLConsoleACL(ctx context.Context, db *DB) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	var exists bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'curio_sql_console')`).Scan(&exists); err != nil {
		return xerrors.Errorf("check SQL console user: %w", err)
	}
	if !exists {
		if db.ReadOnly() {
			return xerrors.Errorf("SQL console user %s is missing (readonly mode cannot create it)", sqlConsoleUser)
		}
		// LOGIN so SET SESSION AUTHORIZATION can target it. NOINHERIT so even
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
		// Upgrade older NOLOGIN installs and keep privilege flags tight.
		if err := execRuntime(ctx, db, `
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
	user := pgx.Identifier{sqlConsoleUser}.Sanitize()

	for _, q := range []string{
		fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", sch, user),
		fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s", sch, user),
		fmt.Sprintf("GRANT SELECT ON ALL SEQUENCES IN SCHEMA %s TO %s", sch, user),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON TABLES TO %s", sch, user),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON SEQUENCES TO %s", sch, user),
	} {
		if err := execRuntime(ctx, db, q); err != nil {
			return xerrors.Errorf("SQL console ACL grant: %w", err)
		}
	}

	var hasEthKeys bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = 'eth_keys'
		)`).Scan(&hasEthKeys); err != nil {
		return xerrors.Errorf("check eth_keys: %w", err)
	}
	if hasEthKeys {
		revoke := "REVOKE ALL ON TABLE " + pgx.Identifier{schema, "eth_keys"}.Sanitize() + " FROM " + user
		if err := execRuntime(ctx, db, revoke); err != nil {
			return xerrors.Errorf("revoke eth_keys from SQL console user: %w", err)
		}
	}

	// Drop any prior membership of the pool login user in curio_sql_console so
	// console privileges are not inherited by normal Curio queries.
	var poolUser string
	if err := db.QueryRow(ctx, `SELECT current_user`).Scan(&poolUser); err != nil {
		return xerrors.Errorf("current_user: %w", err)
	}
	if poolUser != sqlConsoleUser {
		_ = execRuntime(ctx, db, "REVOKE "+user+" FROM "+pgx.Identifier{poolUser}.Sanitize())
	}
	return nil
}

// execRuntime runs a runtime-built SQL string via the AdminQuery reflect path.
func execRuntime(ctx context.Context, db *DB, sql string) error {
	out, err := callDB(db, "Exec", ctx, sql)
	if err != nil {
		return err
	}
	return callErr(out)
}
