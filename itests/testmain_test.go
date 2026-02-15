package itests

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/yugabytedb"
	"github.com/testcontainers/testcontainers-go/wait"
)

const ybImage = "yugabytedb/yugabyte:2024.1.2.0-b77"

func TestMain(m *testing.M) {
	// If CURIO_HARMONYDB_HOSTS is already set, use the external DB (CI or
	// manual docker-run workflow). Run tests directly without starting a
	// container.
	if os.Getenv("CURIO_HARMONYDB_HOSTS") != "" {
		os.Exit(m.Run())
	}

	// No external DB configured — start YugabyteDB via testcontainers.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("itests: no CURIO_HARMONYDB_HOSTS set, starting YugabyteDB via testcontainers...")

	ctr, err := yugabytedb.Run(ctx, ybImage,
		// Bind container ports to fixed host ports so that existing test
		// code (which hardcodes 5433 and 9042) works without changes.
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.PortBindings = nat.PortMap{
				"5433/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "5433"}},
				"9042/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "9042"}},
			}
		}),
		// Remove YSQL_PASSWORD so YugabyteDB uses trust authentication
		// (same as the default `docker run` in CI). The testcontainers
		// YugabyteDB module sets YSQL_PASSWORD=yugabyte by default which
		// enables md5 auth, but harmonyquery's ensureSchemaExists has a
		// bug where it sends a masked password in the connection string.
		testcontainers.WithConfigModifier(func(cfg *container.Config) {
			filtered := cfg.Env[:0]
			for _, e := range cfg.Env {
				if e != "YSQL_PASSWORD=yugabyte" {
					filtered = append(filtered, e)
				}
			}
			cfg.Env = filtered
		}),
		// Override wait strategy with a generous deadline — YugabyteDB
		// can take 30-60s to become fully ready.
		testcontainers.WithWaitStrategyAndDeadline(3*time.Minute,
			wait.ForLog("YugabyteDB Started").WithOccurrence(1),
			wait.ForLog("Data placement constraint successfully verified").WithOccurrence(1),
			wait.ForListeningPort("5433/tcp"),
			wait.ForListeningPort("9042/tcp"),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "itests: failed to start YugabyteDB container: %v\n", err)
		fmt.Fprintf(os.Stderr, "itests:\n")
		fmt.Fprintf(os.Stderr, "itests: Possible causes:\n")
		fmt.Fprintf(os.Stderr, "itests:   - Docker is not running\n")
		fmt.Fprintf(os.Stderr, "itests:   - Ports 5433 or 9042 are already in use (another YugabyteDB?)\n")
		fmt.Fprintf(os.Stderr, "itests:     If you already have YugabyteDB running, set CURIO_HARMONYDB_HOSTS=127.0.0.1\n")
		fmt.Fprintf(os.Stderr, "itests:\n")
		if ctr != nil {
			_ = testcontainers.TerminateContainer(ctr)
		}
		os.Exit(1)
	}

	fmt.Println("itests: YugabyteDB container started successfully (YSQL=127.0.0.1:5433, YCQL=127.0.0.1:9042)")

	// Set the environment variable so that harmonydb.NewFromConfigWithITestID
	// and indexstore test helpers pick up the container's address.
	os.Setenv("CURIO_HARMONYDB_HOSTS", "127.0.0.1")

	code := m.Run()

	fmt.Println("itests: stopping YugabyteDB container...")
	_ = testcontainers.TerminateContainer(ctr)

	os.Exit(code)
}
