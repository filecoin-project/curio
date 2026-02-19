package harmonytask

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// sharedTestDB is a single DB connection shared across all tests in this package.
// Migrations run once; each test cleans rows between runs via getDB().
var sharedTestDB *harmonydb.DB
var sharedTestID = harmonydb.ITestNewID()

func TestMain(m *testing.M) {
	// Drop all itest_* schemas from prior runs (including crashed ones that
	// never got ITestDeleteAll). This prevents YugabyteDB tablet exhaustion.
	dropAllItestSchemas()

	var err error
	sharedTestDB, err = harmonydb.NewFromConfig(harmonydb.Config{
		Hosts:    []string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")},
		Database: "yugabyte",
		Username: "yugabyte",
		Password: "yugabyte",
		Port:     "5433",
		ITestID:  sharedTestID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "itest: cannot connect to DB: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	// Clean up our own schema (same thing dropAllItestSchemas does, but immediate).
	sharedTestDB.ITestDeleteAll()
	os.Exit(code)
}

func envElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

// dropAllItestSchemas drops every itest_* schema in the database.
func dropAllItestSchemas() {
	host := envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")
	connStr := fmt.Sprintf("postgresql://yugabyte:yugabyte@%s:5433/yugabyte?sslmode=disable", host)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "itest cleanup: cannot connect to DB (skipping): %v\n", err)
		return
	}
	defer func() { _ = conn.Close(ctx) }()

	rows, err := conn.Query(ctx, `SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'itest_%'`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "itest cleanup: cannot list schemas: %v\n", err)
		return
	}

	var schemas []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			schemas = append(schemas, s)
		}
	}
	rows.Close()

	if len(schemas) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "itest cleanup: dropping %d stale schemas...\n", len(schemas))

	for _, s := range schemas {
		dropCtx, dropCancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := conn.Exec(dropCtx, "DROP SCHEMA "+s+" CASCADE")
		dropCancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "itest cleanup: failed to drop %s: %v\n", s, err)
		}
	}

	fmt.Fprintf(os.Stderr, "itest cleanup: done, waiting for tablet reclaim...\n")
	time.Sleep(3 * time.Second)
}
