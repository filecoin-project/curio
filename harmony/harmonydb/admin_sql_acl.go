package harmonydb

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
)

// sqlConsoleRole is the NOLOGIN role used for AdminQuery. Created on first
// console query with SELECT on schema tables and no privileges on eth_keys.
const sqlConsoleRole = "curio_sql_console"

const sqlConsoleACLTimeout = 30 * time.Second

var (
	sqlConsoleACLOnce sync.Once
	sqlConsoleACLErr  error
)

// ensureSQLConsoleACLOnce creates/refreshes the SQL console role on first use.
func ensureSQLConsoleACLOnce(db *DB) error {
	sqlConsoleACLOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), sqlConsoleACLTimeout)
		defer cancel()
		sqlConsoleACLErr = ensureSQLConsoleACL(ctx, db)
	})
	return sqlConsoleACLErr
}

// ensureSQLConsoleACL creates/refreshes the SQL console role ACLs.
// Safe to call repeatedly: grants are re-applied and eth_keys stays revoked.
func ensureSQLConsoleACL(ctx context.Context, db *DB) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}

	var exists bool
	if err := db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'curio_sql_console')`).Scan(&exists); err != nil {
		return xerrors.Errorf("check SQL console role: %w", err)
	}
	if !exists {
		if db.ReadOnly() {
			return xerrors.Errorf("SQL console role %s is missing (readonly mode cannot create it)", sqlConsoleRole)
		}
		if _, err := db.Exec(ctx, `
			CREATE ROLE curio_sql_console NOLOGIN NOSUPERUSER`); err != nil {
			return xerrors.Errorf("create SQL console role: %w", err)
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
	role := pgx.Identifier{sqlConsoleRole}.Sanitize()

	for _, q := range []string{
		fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", sch, role),
		fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s", sch, role),
		fmt.Sprintf("GRANT SELECT ON ALL SEQUENCES IN SCHEMA %s TO %s", sch, role),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON TABLES TO %s", sch, role),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON SEQUENCES TO %s", sch, role),
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
		revoke := "REVOKE ALL ON TABLE " + pgx.Identifier{schema, "eth_keys"}.Sanitize() + " FROM " + role
		if err := execRuntime(ctx, db, revoke); err != nil {
			return xerrors.Errorf("revoke eth_keys from SQL console role: %w", err)
		}
	}

	// Membership is required for non-superusers to SET ROLE. This does not
	// strip privileges from Curio's login role; AdminQuery still restricts
	// only via SET LOCAL ROLE inside that transaction.
	var user string
	if err := db.QueryRow(ctx, `SELECT current_user`).Scan(&user); err != nil {
		return xerrors.Errorf("current_user: %w", err)
	}

	// Allow this user to `SET ROLE curio_sql_console``
	member := "GRANT " + role + " TO " + pgx.Identifier{user}.Sanitize()
	if err := execRuntime(ctx, db, member); err != nil {
		return xerrors.Errorf("grant SQL console role membership: %w", err)
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
