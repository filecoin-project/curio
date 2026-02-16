// Package dbtest provides a shared helper that starts a YugabyteDB container
// via testcontainers-go for use in TestMain functions.  When the environment
// variable CURIO_HARMONYDB_HOSTS is already set (e.g. by CI or a manually
// started database) the container is skipped and the existing database is used
// instead.
//
// The container uses dynamic port mapping so that multiple packages can each
// start their own YugabyteDB without port conflicts when `go test ./...` runs
// packages in parallel.
//
// Two optimizations are applied to speed up schema migrations in tests:
//
//  1. Tablet count is reduced to 1 per tserver (--yb_num_shards_per_tserver=1,
//     --ysql_num_shards_per_tserver=1) instead of the default based on CPU
//     count.  This drastically reduces the overhead of CREATE TABLE/INDEX.
//
//  2. A colocated database ("curio_test") is created after startup.  Colocated
//     databases store all tables in a single tablet, which eliminates per-table
//     tablet creation overhead entirely.  Tests connect to this database via
//     the CURIO_HARMONYDB_DB environment variable.
package dbtest

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/yugabytedb"
	"github.com/testcontainers/testcontainers-go/wait"
)

const ybImage = "yugabytedb/yugabyte:2024.1.2.0-b77"

// colocatedDBName is the name of the colocated database created for tests.
const colocatedDBName = "curio_test"

// StartYugabyte starts a YugabyteDB container (unless CURIO_HARMONYDB_HOSTS is
// already set), runs the test suite, terminates the container, and returns the
// exit code from m.Run().  Callers should use it as:
//
//	func TestMain(m *testing.M) { os.Exit(dbtest.StartYugabyte(m)) }
func StartYugabyte(m *testing.M) int {
	// If the env var is already set, an external database is available.
	if os.Getenv("CURIO_HARMONYDB_HOSTS") != "" {
		return m.Run()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("dbtest: no CURIO_HARMONYDB_HOSTS set, starting YugabyteDB via testcontainers...")

	ctr, err := yugabytedb.Run(ctx, ybImage,
		// Do NOT bind to fixed host ports — let Docker pick free ports so
		// that multiple packages can each run their own container in
		// parallel without conflicts.

		// Reduce tablet count to 1 per tserver for both YCQL and YSQL.
		// YugabyteDB normally creates multiple tablets per table based on
		// CPU count, which makes CREATE TABLE very slow. With 1 shard
		// per tserver, each DDL statement creates just one tablet.
		// See: https://docs.yugabyte.com/v2024.1/best-practices-operations/administration/#settings-for-ci-and-cd-integration-tests
		testcontainers.CustomizeRequest(testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Cmd: []string{
					"--tserver_flags=yb_num_shards_per_tserver=1,ysql_num_shards_per_tserver=1",
				},
			},
		}),
		// Strip all YSQL_*/YCQL_* env vars so YugabyteDB starts with
		// default trust authentication (no passwords, same as the bare
		// `docker run` used historically in CI).  The testcontainers
		// YugabyteDB module sets user/password env vars by default, which
		// causes yugabyted to enable md5/PasswordAuthenticator.
		testcontainers.WithConfigModifier(func(cfg *container.Config) {
			filtered := cfg.Env[:0]
			for _, e := range cfg.Env {
				if !strings.HasPrefix(e, "YSQL_") && !strings.HasPrefix(e, "YCQL_") {
					filtered = append(filtered, e)
				}
			}
			cfg.Env = filtered
		}),
		// Generous deadline — YugabyteDB can take 30-60s to become ready.
		testcontainers.WithWaitStrategyAndDeadline(3*time.Minute,
			wait.ForLog("YugabyteDB Started").WithOccurrence(1),
			wait.ForLog("Data placement constraint successfully verified").WithOccurrence(1),
			wait.ForListeningPort("5433/tcp"),
			wait.ForListeningPort("9042/tcp"),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: failed to start YugabyteDB container: %v\n", err)
		fmt.Fprintf(os.Stderr, "dbtest:\n")
		fmt.Fprintf(os.Stderr, "dbtest: Possible causes:\n")
		fmt.Fprintf(os.Stderr, "dbtest:   - Docker is not running\n")
		fmt.Fprintf(os.Stderr, "dbtest:   - Image pull failure (check network)\n")
		fmt.Fprintf(os.Stderr, "dbtest:\n")
		fmt.Fprintf(os.Stderr, "dbtest: To skip the container and use an existing DB, set CURIO_HARMONYDB_HOSTS=127.0.0.1\n")
		if ctr != nil {
			_ = testcontainers.TerminateContainer(ctr)
		}
		return 1
	}

	// Retrieve dynamically mapped host and ports.
	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: failed to get container host: %v\n", err)
		_ = testcontainers.TerminateContainer(ctr)
		return 1
	}

	ysqlPort, err := ctr.MappedPort(ctx, "5433/tcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: failed to get YSQL mapped port: %v\n", err)
		_ = testcontainers.TerminateContainer(ctr)
		return 1
	}

	ycqlPort, err := ctr.MappedPort(ctx, "9042/tcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: failed to get YCQL mapped port: %v\n", err)
		_ = testcontainers.TerminateContainer(ctr)
		return 1
	}

	fmt.Printf("dbtest: YugabyteDB ready (YSQL=%s:%s, YCQL=%s:%s)\n",
		host, ysqlPort.Port(), host, ycqlPort.Port())

	// Create a colocated database for tests.  In a colocated database all
	// tables share a single tablet, which eliminates the per-table tablet
	// creation overhead that makes schema migrations slow in YugabyteDB.
	// yugabyted binds YSQL to the container's assigned IP (not 0.0.0.0
	// or 127.0.0.1), so we need the container IP for the exec command.
	containerIP, err := ctr.ContainerIP(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: failed to get container IP: %v\n", err)
		_ = testcontainers.TerminateContainer(ctr)
		return 1
	}

	exitCode, execReader, err := ctr.Exec(ctx, []string{
		"ysqlsh", "-h", containerIP, "-p", "5433", "-U", "yugabyte",
		"-c", fmt.Sprintf("CREATE DATABASE %s WITH COLOCATION = true", colocatedDBName),
	})
	if err != nil || exitCode != 0 {
		var execOutput string
		if execReader != nil {
			if b, readErr := io.ReadAll(execReader); readErr == nil {
				execOutput = string(b)
			}
		}
		fmt.Fprintf(os.Stderr, "dbtest: failed to create colocated database %q (exit=%d): %v\nOutput: %s\n",
			colocatedDBName, exitCode, err, execOutput)
		_ = testcontainers.TerminateContainer(ctr)
		return 1
	}
	fmt.Printf("dbtest: created colocated database %q\n", colocatedDBName)

	// Publish connection info via environment variables so that
	// harmonydb.NewFromConfigWithITestID (reads CURIO_HARMONYDB_HOSTS,
	// CURIO_HARMONYDB_PORT, and CURIO_HARMONYDB_DB) and indexstore tests
	// (reads CURIO_HARMONYDB_HOSTS and CURIO_HARMONYDB_CQL_PORT) can find
	// the container.
	for _, kv := range [][2]string{
		{"CURIO_HARMONYDB_HOSTS", host},
		{"CURIO_HARMONYDB_PORT", ysqlPort.Port()},
		{"CURIO_HARMONYDB_CQL_PORT", ycqlPort.Port()},
		{"CURIO_HARMONYDB_DB", colocatedDBName},
	} {
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			fmt.Fprintf(os.Stderr, "dbtest: failed to set %s: %v\n", kv[0], err)
			_ = testcontainers.TerminateContainer(ctr)
			return 1
		}
	}

	code := m.Run()

	fmt.Println("dbtest: stopping YugabyteDB container...")
	_ = testcontainers.TerminateContainer(ctr)

	return code
}
