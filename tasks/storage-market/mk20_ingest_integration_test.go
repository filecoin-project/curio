//go:build integration

package storage_market

import (
	"context"
	"database/sql"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	mk20AssignmentITestOptInEnv    = "CURIO_MK20_ASSIGNMENT_ITEST"
	mk20AssignmentITestHostEnv     = "CURIO_MK20_ASSIGNMENT_ITEST_HOST"
	mk20AssignmentITestPortEnv     = "CURIO_MK20_ASSIGNMENT_ITEST_PORT"
	mk20AssignmentITestDBEnv       = "CURIO_MK20_ASSIGNMENT_ITEST_DATABASE"
	mk20AssignmentITestUserEnv     = "CURIO_MK20_ASSIGNMENT_ITEST_USERNAME"
	mk20AssignmentITestPasswordEnv = "CURIO_MK20_ASSIGNMENT_ITEST_PASSWORD"
)

// These tests are compiled with the integration build tag, but they connect
// only when every dedicated target variable is supplied and the host is a
// literal loopback address. They intentionally do not inherit Curio's normal
// HarmonyDB environment or use DefaultItestOptions.
func mk20AssignmentITestDB(t *testing.T) (*harmonydb.DB, harmonydb.Config) {
	t.Helper()
	if os.Getenv(mk20AssignmentITestOptInEnv) != "1" {
		t.Skipf("set %s=1 and all dedicated target variables to run", mk20AssignmentITestOptInEnv)
	}

	host := requireMK20AssignmentITestEnv(t, mk20AssignmentITestHostEnv)
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("%s must be a literal loopback IP address, got %q", mk20AssignmentITestHostEnv, host)
	}
	port := requireMK20AssignmentITestEnv(t, mk20AssignmentITestPortEnv)
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		t.Fatalf("%s must be a valid nonzero TCP port, got %q", mk20AssignmentITestPortEnv, port)
	}

	id := harmonydb.ITestNewID()
	cfg := harmonydb.Config{
		Hosts:           []string{host},
		Port:            port,
		Database:        requireMK20AssignmentITestEnv(t, mk20AssignmentITestDBEnv),
		Username:        requireMK20AssignmentITestEnv(t, mk20AssignmentITestUserEnv),
		Password:        requireMK20AssignmentITestEnv(t, mk20AssignmentITestPasswordEnv),
		LoadBalance:     false,
		ReadOnly:        true,
		SSLMode:         "disable",
		ApplicationName: "curio-mk20-assignment-itest",
		ITestID:         id,
		UseTemplate:     false,
	}
	db, err := harmonydb.NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("opening isolated MK20 assignment test schema: %v", err)
	}
	t.Cleanup(db.ITestDeleteAll)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, mk20AssignmentITestSchema); err != nil {
		t.Fatalf("creating focused MK20 assignment fixture: %v", err)
	}

	peerCfg := cfg
	peerCfg.ITestID = ""
	peerCfg.Schema = "itest_" + string(id)
	return db, peerCfg
}

func requireMK20AssignmentITestEnv(t *testing.T, name string) string {
	t.Helper()
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		t.Fatalf("%s must be set explicitly", name)
	}
	return value
}

const mk20AssignmentITestSchema = `
CREATE TABLE sectors_allocated_numbers (
    sp_id BIGINT NOT NULL PRIMARY KEY,
    allocated JSONB NOT NULL
);

CREATE TABLE market_mk20_pipeline (
    id TEXT NOT NULL,
    aggr_index BIGINT NOT NULL DEFAULT 0,
    sp_id BIGINT NOT NULL,
    aggregated BOOL NOT NULL DEFAULT FALSE,
    complete BOOL NOT NULL DEFAULT FALSE,
    sector BIGINT,
    reg_seal_proof INT,
    PRIMARY KEY (id, aggr_index)
);

CREATE TABLE open_sector_pieces (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    piece_index BIGINT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    data_url TEXT NOT NULL,
    data_raw_size BIGINT NOT NULL,
    data_delete_on_finalize BOOL NOT NULL,
    is_snap BOOL NOT NULL DEFAULT FALSE,
    PRIMARY KEY (sp_id, sector_number, piece_index)
);`

func TestMK20AssignmentDBConcurrentTransactionsAssignOnce(t *testing.T) {
	db, peerCfg := mk20AssignmentITestDB(t)
	peerDB, err := harmonydb.NewFromConfig(peerCfg)
	if err != nil {
		t.Fatalf("opening second MK20 assignment database handle: %v", err)
	}
	// HarmonyDB does not expose a non-destructive Close method. The owner
	// handle's registered cleanup drops the isolated schema; this peer handle
	// remains scoped to the short-lived integration-test process.
	dbs := []*harmonydb.DB{db, peerDB}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	candidate := mk20IngestCandidate{ID: "01JTESTMK20ASSIGNMENT00000000", SPID: 2000, AggregationIndex: 7}
	insertMK20AssignmentITestCandidate(t, ctx, db, candidate)

	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var allocations atomic.Int64
	var wakes atomic.Int64
	var wg sync.WaitGroup
	for _, transactionDB := range dbs {
		wg.Add(1)
		go func(transactionDB *harmonydb.DB) {
			defer wg.Done()
			<-start
			committed, err := ingestMK20Candidate(ctx, transactionDB, candidate, func(tx *harmonydb.Tx) (mk20SectorAssignment, error) {
				sector := abi.SectorNumber(allocations.Add(1))
				_, err := tx.Exec(`INSERT INTO open_sector_pieces (
					sp_id, sector_number, piece_index, piece_cid, piece_size,
					data_url, data_raw_size, data_delete_on_finalize, is_snap
				) VALUES ($1, $2, 0, 'piece', 2048, 'pieceref:test', 2032, FALSE, FALSE)`,
					candidate.SPID, sector)
				if err != nil {
					return mk20SectorAssignment{}, err
				}
				return mk20SectorAssignment{sector: sector, proof: abi.RegisteredSealProof_StackedDrg2KiBV1_1}, nil
			}, func() { wakes.Add(1) })
			if err != nil {
				errs <- err
				return
			}
			results <- committed
		}(transactionDB)
	}
	close(start)
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Fatal(err)
	}
	commits := 0
	for committed := range results {
		if committed {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("committed assignments = %d, want 1", commits)
	}
	if allocations.Load() != 1 {
		t.Fatalf("allocator calls = %d, want 1", allocations.Load())
	}
	if wakes.Load() != 1 {
		t.Fatalf("wake calls = %d, want 1", wakes.Load())
	}

	var openPieces int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM open_sector_pieces WHERE sp_id = $1`, candidate.SPID).Scan(&openPieces); err != nil {
		t.Fatal(err)
	}
	if openPieces != 1 {
		t.Fatalf("open-sector pieces = %d, want 1", openPieces)
	}
	var sector sql.NullInt64
	if err := db.QueryRow(ctx, `SELECT sector FROM market_mk20_pipeline WHERE id = $1 AND aggr_index = $2`,
		candidate.ID, candidate.AggregationIndex).Scan(&sector); err != nil {
		t.Fatal(err)
	}
	if !sector.Valid {
		t.Fatal("pipeline assignment was not persisted")
	}
}

func TestMK20AssignmentDBPersistenceFailureRollsBackAllocation(t *testing.T) {
	db, _ := mk20AssignmentITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	candidate := mk20IngestCandidate{ID: "01JTESTMK20ROLLBACK000000000", SPID: 2001, AggregationIndex: 9}
	insertMK20AssignmentITestCandidate(t, ctx, db, candidate)

	var wakes atomic.Int64
	committed, err := ingestMK20Candidate(ctx, db, candidate, func(tx *harmonydb.Tx) (mk20SectorAssignment, error) {
		if _, err := tx.Exec(`INSERT INTO open_sector_pieces (
			sp_id, sector_number, piece_index, piece_cid, piece_size,
			data_url, data_raw_size, data_delete_on_finalize, is_snap
		) VALUES ($1, 42, 0, 'piece', 2048, 'pieceref:test', 2032, FALSE, FALSE)`, candidate.SPID); err != nil {
			return mk20SectorAssignment{}, err
		}
		if _, err := tx.Exec(`UPDATE market_mk20_pipeline SET complete = TRUE
			WHERE id = $1 AND aggr_index = $2`, candidate.ID, candidate.AggregationIndex); err != nil {
			return mk20SectorAssignment{}, err
		}
		return mk20SectorAssignment{sector: 42, proof: abi.RegisteredSealProof_StackedDrg2KiBV1_1}, nil
	}, func() { wakes.Add(1) })
	if err == nil {
		t.Fatal("expected conditional persistence failure")
	}
	if committed {
		t.Fatal("failed transaction reported a committed assignment")
	}
	if wakes.Load() != 0 {
		t.Fatalf("wake calls = %d, want 0", wakes.Load())
	}

	var openPieces int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM open_sector_pieces WHERE sp_id = $1`, candidate.SPID).Scan(&openPieces); err != nil {
		t.Fatal(err)
	}
	if openPieces != 0 {
		t.Fatalf("rolled-back open-sector pieces = %d, want 0", openPieces)
	}
	var complete bool
	var sector sql.NullInt64
	if err := db.QueryRow(ctx, `SELECT complete, sector FROM market_mk20_pipeline
		WHERE id = $1 AND aggr_index = $2`, candidate.ID, candidate.AggregationIndex).Scan(&complete, &sector); err != nil {
		t.Fatal(err)
	}
	if complete || sector.Valid {
		t.Fatalf("pipeline row after rollback: complete=%v sector=%v", complete, sector)
	}
}

func insertMK20AssignmentITestCandidate(t *testing.T, ctx context.Context, db *harmonydb.DB, candidate mk20IngestCandidate) {
	t.Helper()
	if _, err := db.Exec(ctx, `INSERT INTO market_mk20_pipeline
		(id, aggr_index, sp_id, aggregated) VALUES ($1, $2, $3, TRUE)`,
		candidate.ID, candidate.AggregationIndex, candidate.SPID); err != nil {
		t.Fatal(err)
	}
}

// The first production attempt holds its provider and deal locks inside the
// real allocation callback. The second production attempt must visibly wait,
// then reread the committed assignment and skip without calling its allocator.
func TestMK20AssignmentDBWaitsForCommittedProviderState(t *testing.T) {
	db, peerCfg := mk20AssignmentITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var version, isolation string
	if err := db.QueryRow(ctx, `SELECT version()`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(version), "yugabyte") {
		t.Skip("linked PostgreSQL lock observation is not a Yugabyte observer")
	}
	if err := db.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
		t.Fatal(err)
	}
	t.Logf("database_version=%q requested isolation=driver default; SQL reported isolation=%q; effective Yugabyte isolation=UNVERIFIED", version, isolation)
	peerCfg.ApplicationName = "assignment-" + peerCfg.Schema
	peerDB, err := harmonydb.NewFromConfig(peerCfg)
	if err != nil {
		t.Fatal(err)
	}
	candidate := mk20IngestCandidate{ID: "01JTESTMK20HELDASSIGN0000000", SPID: 2100, AggregationIndex: 7}
	insertMK20AssignmentITestCandidate(t, ctx, db, candidate)
	type outcome struct {
		first     bool
		committed bool
		err       error
	}
	held := make(chan int, 1)
	release := make(chan struct{})
	results := make(chan outcome, 2)
	var releaseOnce sync.Once
	var participants sync.WaitGroup
	var allocations, wakes atomic.Int64
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		unblock()
		cancel()
		participants.Wait()
	}()
	participants.Add(1)
	go func() {
		defer participants.Done()
		committed, err := ingestMK20Candidate(ctx, db, candidate, func(tx *harmonydb.Tx) (mk20SectorAssignment, error) {
			allocations.Add(1)
			if _, err := tx.Exec(`INSERT INTO open_sector_pieces
				(sp_id, sector_number, piece_index, piece_cid, piece_size, data_url, data_raw_size, data_delete_on_finalize)
				VALUES ($1, 42, 0, 'piece', 2048, 'pieceref:test', 2032, FALSE)`, candidate.SPID); err != nil {
				return mk20SectorAssignment{}, err
			}
			var pid int
			if err := tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return mk20SectorAssignment{}, err
			}
			held <- pid
			select {
			case <-release:
				return mk20SectorAssignment{sector: 42, proof: abi.RegisteredSealProof_StackedDrg2KiBV1_1}, nil
			case <-ctx.Done():
				return mk20SectorAssignment{}, ctx.Err()
			}
		}, func() { wakes.Add(1) })
		results <- outcome{true, committed, err}
	}()
	var holderPID int
	select {
	case holderPID = <-held:
	case result := <-results:
		t.Fatalf("holder exited before allocation barrier: %+v", result)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	participants.Add(1)
	go func() {
		defer participants.Done()
		committed, err := ingestMK20Candidate(ctx, peerDB, candidate, func(*harmonydb.Tx) (mk20SectorAssignment, error) {
			allocations.Add(1)
			return mk20SectorAssignment{sector: 99, proof: abi.RegisteredSealProof_StackedDrg2KiBV1_1}, nil
		}, func() { wakes.Add(1) })
		results <- outcome{false, committed, err}
	}()
	observation, stopObservation := context.WithTimeout(ctx, 3*time.Second)
	defer stopObservation()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var blocked bool
		if err := db.QueryRow(observation, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND application_name = $1
			  AND $2::int = ANY(pg_blocking_pids(pid)))`, peerCfg.ApplicationName, holderPID).Scan(&blocked); err != nil {
			t.Fatalf("contention not established: %v", err)
		}
		if blocked {
			t.Logf("observed production assignment contender blocked by holder pid=%d before release", holderPID)
			break
		}
		select {
		case result := <-results:
			t.Fatalf("assignment contender completed while holder was unreleased: %+v", result)
		case <-observation.Done():
			t.Fatal("contention not established before observation deadline")
		case <-tick.C:
		}
	}
	unblock()
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil || result.committed != result.first {
				t.Fatalf("assignment outcome: %+v", result)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if allocations.Load() != 1 || wakes.Load() != 1 {
		t.Fatalf("allocations=%d wakes=%d, want one committed assignment", allocations.Load(), wakes.Load())
	}
	var sector int64
	if err := db.QueryRow(ctx, `SELECT sector FROM market_mk20_pipeline
		WHERE id = $1 AND aggr_index = $2 AND sp_id = $3`, candidate.ID, candidate.AggregationIndex, candidate.SPID).Scan(&sector); err != nil {
		t.Fatal(err)
	}
	if sector != 42 {
		t.Fatalf("committed assignment=%d, want 42", sector)
	}
	var pieces int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM open_sector_pieces WHERE sp_id = $1`, candidate.SPID).Scan(&pieces); err != nil {
		t.Fatal(err)
	}
	if pieces != 1 {
		t.Fatalf("committed open pieces=%d, want one", pieces)
	}
}
