// Package dbtest provides shared helpers that start Postgres and/or ScyllaDB
// containers via testcontainers-go for use in TestMain functions. When the
// environment variable CURIO_HARMONYDB_HOSTS is already set (e.g. by CI or
// a manually started database via `make test-dbs-up`) the containers are
// skipped and the existing databases are used instead.
//
// Each test package starts its own container(s) with dynamic port mapping so
// that multiple packages can run in parallel without port conflicts.
//
// Usage:
//
//	func TestMain(m *testing.M) { os.Exit(dbtest.WithPostgres(m)) }            // SQL only
//	func TestMain(m *testing.M) { os.Exit(dbtest.WithDatabases(m)) }           // SQL + CQL
//	func TestMain(m *testing.M) { os.Exit(dbtest.WithScyllaDB(m)) }            // CQL only
package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	pgImage     = "postgres:15"
	scyllaImage = "scylladb/scylla:latest"
)

// WithDatabases starts both a Postgres and a ScyllaDB container (unless
// CURIO_HARMONYDB_HOSTS is already set), runs the test suite, terminates
// containers, and returns the exit code from m.Run(). Use for test packages
// that need both harmonydb (SQL) and indexstore (CQL).
//
//	func TestMain(m *testing.M) { os.Exit(dbtest.WithDatabases(m)) }
func WithDatabases(m *testing.M) int {
	if os.Getenv("CURIO_HARMONYDB_HOSTS") != "" {
		return m.Run()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pgCtr, err := startPostgresContainer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		printHints()
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(pgCtr) }()

	if err := exportPostgresEnv(ctx, pgCtr); err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		return 1
	}

	scyllaCtr, err := startScyllaContainer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		printHints()
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(scyllaCtr) }()

	if err := exportScyllaEnv(ctx, scyllaCtr); err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		return 1
	}

	return m.Run()
}

// WithPostgres starts a Postgres container (unless CURIO_HARMONYDB_HOSTS is
// already set), runs the test suite, terminates the container, and returns
// the exit code. Use for test packages that only need harmonydb (SQL).
//
//	func TestMain(m *testing.M) { os.Exit(dbtest.WithPostgres(m)) }
func WithPostgres(m *testing.M) int {
	if os.Getenv("CURIO_HARMONYDB_HOSTS") != "" {
		return m.Run()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pgCtr, err := startPostgresContainer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		printHints()
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(pgCtr) }()

	if err := exportPostgresEnv(ctx, pgCtr); err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		return 1
	}

	return m.Run()
}

// WithScyllaDB starts a ScyllaDB container (unless CURIO_DB_HOST_CQL is
// already set), runs the test suite, terminates the container, and returns
// the exit code. Use for test packages that only need CQL (e.g. indexstore).
//
//	func TestMain(m *testing.M) { os.Exit(dbtest.WithScyllaDB(m)) }
func WithScyllaDB(m *testing.M) int {
	if os.Getenv("CURIO_DB_HOST_CQL") != "" || os.Getenv("CURIO_HARMONYDB_HOSTS") != "" {
		return m.Run()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	scyllaCtr, err := startScyllaContainer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		printHints()
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(scyllaCtr) }()

	if err := exportScyllaEnv(ctx, scyllaCtr); err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		return 1
	}

	return m.Run()
}

// startPostgresContainer creates and starts a Postgres:15 container with trust
// authentication, a "yugabyte" role and database (matching the harmonydb test
// config), and dynamic port mapping.
func startPostgresContainer(ctx context.Context) (testcontainers.Container, error) {
	fmt.Println("dbtest: starting Postgres via testcontainers...")

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        pgImage,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_HOST_AUTH_METHOD": "trust",
			},
			WaitingFor: wait.ForAll(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(2*time.Minute),
				wait.ForListeningPort("5432/tcp").
					WithStartupTimeout(2*time.Minute),
			),
		},
		Started: true,
	})
	if err != nil {
		if ctr != nil {
			_ = testcontainers.TerminateContainer(ctr)
		}
		return nil, fmt.Errorf("failed to start Postgres container: %w", err)
	}

	// Create the "yugabyte" role and database to match harmonydb test config.
	host, err := ctr.Host(ctx)
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, fmt.Errorf("failed to get Postgres host: %w", err)
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, fmt.Errorf("failed to get Postgres mapped port: %w", err)
	}

	dsn := fmt.Sprintf("host=%s port=%s user=postgres dbname=postgres sslmode=disable", host, port.Port())
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, fmt.Errorf("failed to connect to Postgres for setup: %w", err)
	}
	defer func() { _ = db.Close() }()

	for _, stmt := range []string{
		"CREATE ROLE yugabyte WITH LOGIN PASSWORD 'yugabyte'",
		"CREATE DATABASE yugabyte OWNER yugabyte",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			// Ignore "already exists" errors for idempotency.
			fmt.Fprintf(os.Stderr, "dbtest: postgres setup (non-fatal): %s: %v\n", stmt, err)
		}
	}

	fmt.Printf("dbtest: Postgres ready (host=%s, port=%s)\n", host, port.Port())
	return ctr, nil
}

// exportPostgresEnv retrieves the container's host and mapped port, then sets
// the environment variables that harmonydb.NewFromConfigWithITestID reads.
func exportPostgresEnv(ctx context.Context, ctr testcontainers.Container) error {
	host, err := ctr.Host(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Postgres host: %w", err)
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return fmt.Errorf("failed to get Postgres mapped port: %w", err)
	}

	for _, kv := range [][2]string{
		{"CURIO_HARMONYDB_HOSTS", host},
		{"CURIO_HARMONYDB_PORT", port.Port()},
	} {
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			return fmt.Errorf("failed to set %s: %w", kv[0], err)
		}
	}
	return nil
}

// startScyllaContainer creates and starts a ScyllaDB container with minimal
// resource usage and dynamic port mapping.
func startScyllaContainer(ctx context.Context) (testcontainers.Container, error) {
	fmt.Println("dbtest: starting ScyllaDB via testcontainers...")

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        scyllaImage,
			ExposedPorts: []string{"9042/tcp"},
			Cmd:          []string{"--smp", "1", "--memory", "512M", "--overprovisioned", "1"},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("9042/tcp").
					WithStartupTimeout(2*time.Minute),
				wait.ForLog("Starting listening for CQL clients").
					WithStartupTimeout(2*time.Minute),
			),
		},
		Started: true,
	})
	if err != nil {
		if ctr != nil {
			_ = testcontainers.TerminateContainer(ctr)
		}
		return nil, fmt.Errorf("failed to start ScyllaDB container: %w", err)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, fmt.Errorf("failed to get ScyllaDB host: %w", err)
	}
	port, err := ctr.MappedPort(ctx, "9042/tcp")
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, fmt.Errorf("failed to get ScyllaDB mapped port: %w", err)
	}

	fmt.Printf("dbtest: ScyllaDB ready (host=%s, port=%s)\n", host, port.Port())
	return ctr, nil
}

// exportScyllaEnv retrieves the container's host and mapped port, then sets
// the environment variables that indexstore tests read.
func exportScyllaEnv(ctx context.Context, ctr testcontainers.Container) error {
	host, err := ctr.Host(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ScyllaDB host: %w", err)
	}
	port, err := ctr.MappedPort(ctx, "9042/tcp")
	if err != nil {
		return fmt.Errorf("failed to get ScyllaDB mapped port: %w", err)
	}

	for _, kv := range [][2]string{
		{"CURIO_DB_HOST_CQL", host},
		{"CURIO_HARMONYDB_CQL_PORT", port.Port()},
	} {
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			return fmt.Errorf("failed to set %s: %w", kv[0], err)
		}
	}
	return nil
}

func printHints() {
	fmt.Fprintf(os.Stderr, "dbtest:\n")
	fmt.Fprintf(os.Stderr, "dbtest: Possible causes:\n")
	fmt.Fprintf(os.Stderr, "dbtest:   - Docker is not running\n")
	fmt.Fprintf(os.Stderr, "dbtest:   - Image pull failure (check network)\n")
	fmt.Fprintf(os.Stderr, "dbtest:\n")
	fmt.Fprintf(os.Stderr, "dbtest: Alternatives:\n")
	fmt.Fprintf(os.Stderr, "dbtest:   - Run 'make test-dbs-up' then 'make test'\n")
	fmt.Fprintf(os.Stderr, "dbtest:   - Set CURIO_HARMONYDB_HOSTS=127.0.0.1 to use an existing DB\n")
}
