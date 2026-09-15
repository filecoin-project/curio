//go:build integration

package seal

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/types"
)

const (
	sectorAllocationITestOptIn    = "CURIO_SECTOR_ALLOC_ITEST"
	sectorAllocationITestHost     = "CURIO_SECTOR_ALLOC_ITEST_HOST"
	sectorAllocationITestPort     = "CURIO_SECTOR_ALLOC_ITEST_PORT"
	sectorAllocationITestDatabase = "CURIO_SECTOR_ALLOC_ITEST_DATABASE"
	sectorAllocationITestUser     = "CURIO_SECTOR_ALLOC_ITEST_USER"
	sectorAllocationITestPassword = "CURIO_SECTOR_ALLOC_ITEST_PASSWORD"
)

// TestAllocateSectorNumbersConcurrentFirstUse exercises the production
// transaction and allocator against two concurrent transactions. It is
// intentionally opt-in and requires an explicit loopback target; normal test
// runs cannot inherit Curio's database connection settings.
func TestAllocateSectorNumbersConcurrentFirstUse(t *testing.T) {
	dbs := newSectorAllocationIntegrationDBs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	maddr, err := address.NewIDAddress(1000)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 2
	start := make(chan struct{})
	results := make(chan abi.SectorNumber, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for _, db := range dbs {
		wg.Add(1)
		go func(db *harmonydb.DB) {
			defer wg.Done()
			<-start

			var allocated []abi.SectorNumber
			committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				var allocErr error
				allocated, allocErr = AllocateSectorNumbers(ctx, emptySectorAllocationAPI{}, tx, maddr, 1)
				return allocErr == nil, allocErr
			}, harmonydb.OptionRetry())
			if err != nil {
				errs <- err
				return
			}
			if !committed || len(allocated) != 1 {
				errs <- fmt.Errorf("allocation committed=%t count=%d", committed, len(allocated))
				return
			}
			results <- allocated[0]
		}(db)
	}

	close(start)
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatal(err)
	}

	seen := make(map[abi.SectorNumber]struct{}, workers)
	for sector := range results {
		seen[sector] = struct{}{}
	}
	if len(seen) != workers {
		t.Fatalf("allocated sector numbers = %v, want %d distinct numbers", seen, workers)
	}
}

type emptySectorAllocationAPI struct{}

func (emptySectorAllocationAPI) StateMinerAllocated(context.Context, address.Address, types.TipSetKey) (*bitfield.BitField, error) {
	empty := bitfield.New()
	return &empty, nil
}

func newSectorAllocationIntegrationDBs(t *testing.T) [2]*harmonydb.DB {
	t.Helper()
	if os.Getenv(sectorAllocationITestOptIn) != "1" {
		t.Skipf("set %s=1 and the dedicated target variables to run", sectorAllocationITestOptIn)
	}

	host := requireSectorAllocationITestEnv(t, sectorAllocationITestHost)
	port := requireSectorAllocationITestEnv(t, sectorAllocationITestPort)
	database := requireSectorAllocationITestEnv(t, sectorAllocationITestDatabase)
	username := requireSectorAllocationITestEnv(t, sectorAllocationITestUser)

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("%s must be a literal loopback IP address, got %q", sectorAllocationITestHost, host)
	}
	if ip.To4() == nil {
		host = "[" + ip.String() + "]"
	} else {
		host = ip.String()
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		t.Fatalf("%s must be a valid TCP port, got %q", sectorAllocationITestPort, port)
	}

	itestID := harmonydb.ITestNewID()
	cfg := harmonydb.Config{
		Hosts:           []string{host},
		Port:            port,
		Database:        database,
		Username:        username,
		Password:        os.Getenv(sectorAllocationITestPassword),
		LoadBalance:     false,
		ReadOnly:        true,
		UseTemplate:     false,
		ITestID:         itestID,
		ApplicationName: "curio-sector-allocation-itest",
	}
	first, err := harmonydb.NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("opening first handle to isolated sector-allocation test schema: %v", err)
	}
	t.Cleanup(first.ITestDeleteAll)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := first.Exec(ctx, `CREATE TABLE sectors_allocated_numbers (
		sp_id BIGINT NOT NULL PRIMARY KEY,
		allocated JSONB NOT NULL
	)`); err != nil {
		t.Fatalf("creating sector-allocation fixture: %v", err)
	}

	peerCfg := cfg
	peerCfg.ITestID = ""
	peerCfg.Schema = "itest_" + string(itestID)
	peerCfg.ApplicationName = "curio-sector-allocation-itest-peer"
	second, err := harmonydb.NewFromConfig(peerCfg)
	if err != nil {
		t.Fatalf("opening second handle to isolated sector-allocation test schema: %v", err)
	}

	var version, isolation string
	if err := first.QueryRow(ctx, `SELECT version()`).Scan(&version); err != nil {
		t.Fatalf("reading database version: %v", err)
	}
	if err := first.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
		t.Fatalf("reading requested transaction isolation: %v", err)
	}
	t.Logf("database_version=%q requested_transaction_isolation=%q effective_yugabyte_isolation=UNVERIFIED", version, isolation)

	return [2]*harmonydb.DB{first, second}
}

func requireSectorAllocationITestEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s must be set explicitly when %s=1", name, sectorAllocationITestOptIn)
	}
	return value
}

// The held transaction deliberately establishes the ordering. Unlike a
// simultaneous start, linked PostgreSQL blocking proves the real allocator
// reached the provider write-conflict point before the holder committed.
func TestSectorStateDBLockBlocksAllocatorUntilCommit(t *testing.T) {
	dbs := newSectorAllocationIntegrationDBs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var version string
	if err := dbs[0].QueryRow(ctx, `SELECT version()`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(version), "yugabyte") {
		t.Skip("linked PostgreSQL lock observation is not a Yugabyte observer")
	}
	maddr, err := address.NewIDAddress(1100)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		committed bool
		sectors   []abi.SectorNumber
		err       error
	}
	held := make(chan int, 1)
	entered := make(chan int, 1)
	release := make(chan struct{})
	results := make(chan outcome, 2)
	var releaseOnce sync.Once
	var participants sync.WaitGroup
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		unblock()
		cancel()
		participants.Wait()
	}()
	participants.Add(1)
	go func() {
		defer participants.Done()
		var sectors []abi.SectorNumber
		committed, err := dbs[0].BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			var err error
			sectors, err = AllocateSectorNumbers(ctx, emptySectorAllocationAPI{}, tx, maddr, 1)
			if err != nil {
				return false, err
			}
			var pid int
			if err := tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return false, err
			}
			held <- pid
			select {
			case <-release:
				return true, nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		})
		results <- outcome{committed, sectors, err}
	}()
	var holderPID int
	select {
	case holderPID = <-held:
	case result := <-results:
		t.Fatalf("holder exited before lock: %+v", result)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	participants.Add(1)
	go func() {
		defer participants.Done()
		var sectors []abi.SectorNumber
		committed, err := dbs[1].BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			var pid int
			if err := tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return false, err
			}
			entered <- pid
			var err error
			sectors, err = AllocateSectorNumbers(ctx, emptySectorAllocationAPI{}, tx, maddr, 1)
			return err == nil, err
		})
		results <- outcome{committed, sectors, err}
	}()
	var waiterPID int
	select {
	case waiterPID = <-entered:
	case result := <-results:
		t.Fatalf("contender exited before allocator: %+v", result)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	observation, stopObservation := context.WithTimeout(ctx, 3*time.Second)
	defer stopObservation()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var blocked bool
		if err := dbs[0].QueryRow(observation, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, holderPID, waiterPID).Scan(&blocked); err != nil {
			t.Fatalf("contention not established: %v", err)
		}
		if blocked {
			t.Logf("observed allocator pid=%d blocked by provider-lock holder pid=%d before release", waiterPID, holderPID)
			break
		}
		select {
		case result := <-results:
			t.Fatalf("contender completed while provider lock was held: %+v", result)
		case <-observation.Done():
			t.Fatal("contention not established before observation deadline")
		case <-tick.C:
		}
	}
	unblock()
	seen := make(map[abi.SectorNumber]bool)
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil || !result.committed || len(result.sectors) != 1 {
				t.Fatalf("allocation outcome: %+v", result)
			}
			seen[result.sectors[0]] = true
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if len(seen) != 2 {
		t.Fatalf("distinct committed sectors=%v, want two", seen)
	}
}
