package storage_market

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/backpressure"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/market/mk20release"
)

const (
	mk20ReleaseITestPieceCID    = "bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pq"
	mk20ReleaseITestOptInEnv    = "CURIO_MK20_RELEASE_ITEST"
	mk20ReleaseITestHostEnv     = "CURIO_MK20_RELEASE_ITEST_HOST"
	mk20ReleaseITestPortEnv     = "CURIO_MK20_RELEASE_ITEST_PORT"
	mk20ReleaseITestDatabaseEnv = "CURIO_MK20_RELEASE_ITEST_DATABASE"
	mk20ReleaseITestUserEnv     = "CURIO_MK20_RELEASE_ITEST_USER"
	mk20ReleaseITestPasswordEnv = "CURIO_MK20_RELEASE_ITEST_PASSWORD"
)

type mk20ReleaseITestDB struct {
	primary   *harmonydb.DB
	secondary *harmonydb.DB
	target    mk20ReleaseITestTarget
}

type mk20ReleaseITestTarget struct {
	host     string
	port     string
	database string
	username string
	password string
	schema   string
}

// newMK20ReleaseITestDB opens two independent pools on one random, isolated
// schema. The dedicated target variables and literal-loopback check happen
// before HarmonyDB opens a connection, so normal Curio database variables and
// built-in integration defaults cannot select the target. ReadOnly suppresses
// historical migrations; it does not prevent normal Exec or transaction calls
// after the connection is established.
func newMK20ReleaseITestDB(t *testing.T) mk20ReleaseITestDB {
	t.Helper()
	if os.Getenv(mk20ReleaseITestOptInEnv) != "1" {
		t.Skip("set CURIO_MK20_RELEASE_ITEST=1 and the dedicated test target variables to run local Yugabyte MK20 release integration tests")
	}

	target, err := readMK20ReleaseITestTarget()
	if err != nil {
		t.Fatal(err)
	}

	opts := harmonydb.ItestOptions{
		Hosts:    []string{target.host},
		Database: target.database,
		Username: target.username,
		Password: target.password,
		Port:     target.port,
		ITestID:  harmonydb.ITestNewID(),
	}
	cfg := opts.HarmonyConfig()
	cfg.ReadOnly = true

	primary, err := harmonydb.NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("opening primary isolated Yugabyte test handle: %v", err)
	}
	var secondary *harmonydb.DB
	t.Cleanup(func() {
		if secondary != nil {
			secondary.ITestDeleteAll()
		}
		primary.ITestDeleteAll()
	})

	secondary, err = harmonydb.NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("opening secondary isolated Yugabyte test handle: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := primary.Exec(ctx, mk20ReleaseITestSchema); err != nil {
		t.Fatalf("bootstrapping focused MK20 release schema: %v", err)
	}
	target.schema = "itest_" + string(opts.ITestID)
	applyMK20ReleaseGateMigration(t, ctx, target)

	var version, defaultIsolation, transactionIsolation string
	if err := primary.QueryRow(ctx, `SELECT version()`).Scan(&version); err != nil {
		t.Fatalf("reading database version: %v", err)
	}
	if err := primary.QueryRow(ctx, `SHOW default_transaction_isolation`).Scan(&defaultIsolation); err != nil {
		t.Fatalf("reading default transaction isolation: %v", err)
	}
	if _, err := primary.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return false, tx.QueryRow(`SHOW transaction_isolation`).Scan(&transactionIsolation)
	}); err != nil {
		t.Fatalf("reading HarmonyDB transaction isolation: %v", err)
	}
	var primaryPID, secondaryPID int64
	if err := primary.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&primaryPID); err != nil {
		t.Fatalf("reading primary backend PID: %v", err)
	}
	if err := secondary.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&secondaryPID); err != nil {
		t.Fatalf("reading secondary backend PID: %v", err)
	}
	if primaryPID == secondaryPID {
		t.Fatalf("integration handles unexpectedly share backend PID %d", primaryPID)
	}
	t.Logf("database=%s", version)
	t.Logf("session default_transaction_isolation=%s", defaultIsolation)
	t.Logf("HarmonyDB default transaction requested isolation=driver default displayed transaction_isolation=%s effective Yugabyte isolation=UNVERIFIED", transactionIsolation)
	t.Logf("primary_backend=%d secondary_backend=%d", primaryPID, secondaryPID)

	return mk20ReleaseITestDB{primary: primary, secondary: secondary, target: target}
}

func readMK20ReleaseGateMigration(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating MK20 release integration test source")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "harmony", "harmonydb", "sql", "20260906-mk20-release-gate.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading MK20 release gate migration %s: %v", path, err)
	}
	return string(contents)
}

func applyMK20ReleaseGateMigration(t *testing.T, ctx context.Context, target mk20ReleaseITestTarget) {
	t.Helper()
	// HarmonyDB intentionally accepts only SQL literals. Use a separate pgx
	// connection to execute the bytes read from the real migration file in the
	// same loopback-only, random integration-test schema.
	baseConfig, err := pgx.ParseConfig("postgresql://placeholder@127.0.0.1/placeholder?sslmode=disable")
	if err != nil {
		t.Fatalf("building isolated migration connection config: %v", err)
	}
	baseConfig.Host = target.host
	port, err := strconv.ParseUint(target.port, 10, 16)
	if err != nil {
		t.Fatalf("parsing isolated migration port %q: %v", target.port, err)
	}
	baseConfig.Port = uint16(port)
	baseConfig.Database = target.database
	baseConfig.User = target.username
	baseConfig.Password = target.password
	baseConfig.RuntimeParams["search_path"] = target.schema
	conn, err := pgx.ConnectConfig(ctx, baseConfig)
	if err != nil {
		t.Fatalf("opening isolated migration connection: %v", err)
	}
	defer func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Errorf("closing isolated migration connection: %v", err)
		}
	}()
	if _, err := conn.Exec(ctx, readMK20ReleaseGateMigration(t)); err != nil {
		t.Fatalf("applying MK20 release gate migration: %v", err)
	}
}

func readMK20ReleaseITestTarget() (mk20ReleaseITestTarget, error) {
	target := mk20ReleaseITestTarget{
		host:     strings.Trim(strings.TrimSpace(os.Getenv(mk20ReleaseITestHostEnv)), "[]"),
		port:     strings.TrimSpace(os.Getenv(mk20ReleaseITestPortEnv)),
		database: strings.TrimSpace(os.Getenv(mk20ReleaseITestDatabaseEnv)),
		username: strings.TrimSpace(os.Getenv(mk20ReleaseITestUserEnv)),
		password: os.Getenv(mk20ReleaseITestPasswordEnv),
	}
	if err := validateMK20ReleaseITestTarget(target); err != nil {
		return mk20ReleaseITestTarget{}, err
	}
	return target, nil
}

func validateMK20ReleaseITestTarget(target mk20ReleaseITestTarget) error {
	if ip := net.ParseIP(target.host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s must explicitly name a literal loopback IP", mk20ReleaseITestHostEnv)
	}
	port, err := strconv.ParseUint(target.port, 10, 16)
	if err != nil || port == 0 {
		return fmt.Errorf("%s must explicitly name a port from 1 through 65535", mk20ReleaseITestPortEnv)
	}
	if target.database == "" {
		return fmt.Errorf("%s must explicitly name the isolated test database", mk20ReleaseITestDatabaseEnv)
	}
	if target.username == "" {
		return fmt.Errorf("%s must explicitly name the test database user", mk20ReleaseITestUserEnv)
	}
	return nil
}

func TestMK20ReleaseITestTargetValidation(t *testing.T) {
	valid := mk20ReleaseITestTarget{host: "127.0.0.1", port: "5433", database: "yugabyte", username: "yugabyte"}
	for _, host := range []string{"127.0.0.1", "127.0.0.2", "::1"} {
		target := valid
		target.host = host
		if err := validateMK20ReleaseITestTarget(target); err != nil {
			t.Fatalf("literal loopback host %q rejected: %v", host, err)
		}
	}

	tests := []struct {
		name   string
		mutate func(*mk20ReleaseITestTarget)
	}{
		{name: "missing host", mutate: func(target *mk20ReleaseITestTarget) { target.host = "" }},
		{name: "hostname", mutate: func(target *mk20ReleaseITestTarget) { target.host = "localhost" }},
		{name: "remote IP", mutate: func(target *mk20ReleaseITestTarget) { target.host = "192.0.2.1" }},
		{name: "host list", mutate: func(target *mk20ReleaseITestTarget) { target.host = "127.0.0.1,127.0.0.2" }},
		{name: "missing port", mutate: func(target *mk20ReleaseITestTarget) { target.port = "" }},
		{name: "zero port", mutate: func(target *mk20ReleaseITestTarget) { target.port = "0" }},
		{name: "large port", mutate: func(target *mk20ReleaseITestTarget) { target.port = "65536" }},
		{name: "missing database", mutate: func(target *mk20ReleaseITestTarget) { target.database = "" }},
		{name: "missing user", mutate: func(target *mk20ReleaseITestTarget) { target.username = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target := valid
			tc.mutate(&target)
			if err := validateMK20ReleaseITestTarget(target); err == nil {
				t.Fatalf("unsafe target accepted: %+v", target)
			}
		})
	}
}

func TestMK20ReleaseITestTargetDoesNotUseNormalDBEnvironment(t *testing.T) {
	t.Setenv("CURIO_HARMONYDB_HOSTS", "127.0.0.1")
	t.Setenv(mk20ReleaseITestHostEnv, "")
	t.Setenv(mk20ReleaseITestPortEnv, "")
	t.Setenv(mk20ReleaseITestDatabaseEnv, "")
	t.Setenv(mk20ReleaseITestUserEnv, "")

	if _, err := readMK20ReleaseITestTarget(); err == nil {
		t.Fatal("normal Curio database environment supplied an integration-test target")
	}
}

func TestMK20ReleaseDBLastSlotIsGlobalAcrossProviders(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seedActiveMK20PipelineRow(t, ctx, dbs.primary, "existing-active", 9000, false)
	first := seedOfflineMK20WaitingDeal(t, ctx, dbs.primary, 1000)
	second := seedOfflineMK20WaitingDeal(t, ctx, dbs.primary, 2000)

	type result struct {
		outcome mk20release.Outcome
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, attempt := range []struct {
		db *harmonydb.DB
		id string
	}{{dbs.primary, first}, {dbs.secondary, second}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			outcome, err := releaseMK20WaitingDeal(ctx, attempt.db, attempt.id, 2)
			results <- result{outcome: outcome, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	released, atCapacity := 0, 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		switch result.outcome {
		case mk20release.Released:
			released++
		case mk20release.AtCapacity:
			atCapacity++
		default:
			t.Fatalf("unexpected outcome %q", result.outcome)
		}
	}
	if released != 1 || atCapacity != 1 {
		t.Fatalf("released=%d atCapacity=%d, want 1 each", released, atCapacity)
	}
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 2, 1)

	var providerRows int
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(DISTINCT sp_id)
		FROM market_mk20_pipeline
		WHERE id IN ($1, $2)`, first, second).Scan(&providerRows); err != nil {
		t.Fatal(err)
	}
	if providerRows != 1 {
		t.Fatalf("released provider rows=%d, want exactly one provider to consume the global last slot", providerRows)
	}
}

func TestMK20ReleaseDBSameWaitingDealReleasedOnce(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := seedOfflineMK20WaitingDeal(t, ctx, dbs.primary, 1000)

	start := make(chan struct{})
	outcomes := make(chan mk20release.Outcome, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, db := range []*harmonydb.DB{dbs.primary, dbs.secondary} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			outcome, err := releaseMK20WaitingDeal(ctx, db, id, 10)
			if err != nil {
				errs <- err
				return
			}
			outcomes <- outcome
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(outcomes)
	for err := range errs {
		t.Fatal(err)
	}

	counts := map[mk20release.Outcome]int{}
	for outcome := range outcomes {
		counts[outcome]++
	}
	if counts[mk20release.Released] != 1 || counts[mk20release.NoLongerWaiting] != 1 {
		t.Fatalf("outcomes=%v, want one released and one no-longer-waiting", counts)
	}
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 1, 0)
	var rows int
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_pipeline WHERE id = $1`, id).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("pipeline rows for deal=%d, want 1", rows)
	}
	var duration int64
	var startEpoch sql.NullInt64
	if err := dbs.primary.QueryRow(ctx, `SELECT p.duration,
		(d.ddo_v1->'ddo'->>'start_epoch')::BIGINT
		FROM market_mk20_pipeline p
		JOIN market_mk20_deal d ON d.id = p.id
		WHERE p.id = $1`, id).Scan(&duration, &startEpoch); err != nil {
		t.Fatal(err)
	}
	if duration != 5_256_000 || startEpoch.Valid {
		t.Fatalf("release changed DDO schedule: duration=%d start_epoch=%v", duration, startEpoch)
	}
}

func TestMK20ReleaseDBActiveCountIncludesEveryIncompleteRow(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seedActiveMK20PipelineRow(t, ctx, dbs.primary, "unassigned", 1000, false)
	seedActiveMK20PipelineRow(t, ctx, dbs.primary, "running-task", 1000, false)
	if _, err := dbs.primary.Exec(ctx, `UPDATE market_mk20_pipeline
		SET commp_task_id = 99
		WHERE id = 'running-task'`); err != nil {
		t.Fatal(err)
	}
	seedActiveMK20PipelineRow(t, ctx, dbs.primary, "failed-sector", 2000, false)
	if _, err := dbs.primary.Exec(ctx, `UPDATE market_mk20_pipeline
		SET sector = 7
		WHERE id = 'failed-sector'`); err != nil {
		t.Fatal(err)
	}
	if _, err := dbs.primary.Exec(ctx, `INSERT INTO sectors_sdr_pipeline
		(sp_id, sector_number, failed) VALUES (2000, 7, TRUE)`); err != nil {
		t.Fatal(err)
	}
	seedActiveMK20PipelineRow(t, ctx, dbs.primary, "complete", 3000, true)

	active, err := countActiveMK20PipelineRows(ctx, dbs.primary)
	if err != nil {
		t.Fatal(err)
	}
	if active != 3 {
		t.Fatalf("active rows=%d, want all three incomplete rows", active)
	}
}

func TestMK20ReleaseDBCompleteRowReturnsCapacity(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	seedActiveMK20PipelineRow(t, ctx, dbs.primary, "capacity-holder", 1000, false)
	id := seedOfflineMK20WaitingDeal(t, ctx, dbs.primary, 2000)

	outcome, err := releaseMK20WaitingDeal(ctx, dbs.primary, id, 1)
	if err != nil || outcome != mk20release.AtCapacity {
		t.Fatalf("initial outcome=%q err=%v, want at-capacity", outcome, err)
	}
	if _, err := dbs.primary.Exec(ctx, `UPDATE market_mk20_pipeline
		SET complete = TRUE
		WHERE id = 'capacity-holder'`); err != nil {
		t.Fatal(err)
	}
	outcome, err = releaseMK20WaitingDeal(ctx, dbs.secondary, id, 1)
	if err != nil || outcome != mk20release.Released {
		t.Fatalf("post-completion outcome=%q err=%v, want released", outcome, err)
	}
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 1, 0)
}

func TestMK20ReleaseDBMissingGateFailsClosed(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := seedOfflineMK20WaitingDeal(t, ctx, dbs.primary, 1000)
	if _, err := dbs.primary.Exec(ctx, `DELETE FROM market_mk20_release_gate WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}

	outcome, err := releaseMK20WaitingDeal(ctx, dbs.secondary, id, 0)
	if err == nil || outcome != "" || !errors.Is(err, mk20release.ErrGateUnavailable) {
		t.Fatalf("missing gate outcome=%q err=%v", outcome, err)
	}
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 0, 1)
}

func TestMK20ReleaseDBGateMigrationIsIdempotent(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var rows int
	var singleton, token bool
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*), BOOL_AND(singleton), BOOL_AND(token)
		FROM market_mk20_release_gate`).Scan(&rows, &singleton, &token); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || !singleton || token {
		t.Fatalf("initial gate rows=%d singleton=%t token=%t, want one true/false singleton", rows, singleton, token)
	}

	if n, err := dbs.primary.Exec(ctx, `UPDATE market_mk20_release_gate
		SET token = TRUE
		WHERE singleton = TRUE`); err != nil || n != 1 {
		t.Fatalf("setting migration token: rows=%d err=%v", n, err)
	}
	applyMK20ReleaseGateMigration(t, ctx, dbs.target)

	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*), BOOL_AND(singleton), BOOL_AND(token)
		FROM market_mk20_release_gate`).Scan(&rows, &singleton, &token); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || !singleton || !token {
		t.Fatalf("reapplied gate rows=%d singleton=%t token=%t, want one true/true singleton", rows, singleton, token)
	}
	if _, err := dbs.primary.Exec(ctx, `INSERT INTO market_mk20_release_gate (singleton, token)
		VALUES (FALSE, FALSE)`); err == nil {
		t.Fatal("singleton CHECK constraint accepted a false key")
	}
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_release_gate`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("gate rows=%d after rejected second singleton, want 1", rows)
	}
}

func TestMK20ReleaseDBPipelineInsertCallSiteUsesGate(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := seedOfflineMK20WaitingDeal(t, ctx, dbs.primary, 1000)
	if _, err := dbs.primary.Exec(ctx, `DELETE FROM market_mk20_release_gate WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}

	market := &CurioStorageDealMarket{
		cfg: config.DefaultCurioConfig(),
		db:  dbs.secondary,
	}
	market.insertDDODealInPipeline(ctx)

	assertMK20ReleaseCounts(t, ctx, dbs.primary, 0, 1)
	var rows int
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_pipeline WHERE id = $1`, id).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("pipeline insert call site bypassed missing gate: rows=%d", rows)
	}
}

func TestMK20ReleaseDBControlledEmptyQueueSkipsPressure(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg := config.DefaultCurioConfig()
	cfg.Ingest.MK20PipelineInsertBatch.Set(1)
	cfg.Ingest.MK20PipelineInsertMaxActive.Set(1)
	market := &CurioStorageDealMarket{
		cfg:               cfg,
		db:                dbs.secondary,
		mk20WaitingCursor: "z",
		// bp is intentionally nil: an empty queue must return before pressure.
	}

	market.insertDDODealInPipeline(ctx)

	if market.mk20WaitingCursor != "" {
		t.Fatalf("empty queue left cursor at %q, want reset", market.mk20WaitingCursor)
	}
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 0, 0)
}

func TestMK20ReleaseDBControlledPipelineInsertCallSite(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	const malformedID = "0"
	if _, err := dbs.primary.Exec(ctx, `INSERT INTO market_mk20_pipeline_waiting (id) VALUES ($1)`, malformedID); err != nil {
		t.Fatal(err)
	}
	validIDs := []string{
		"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"01ARZ3NDEKTSV4RRFFQ69G5FAW",
		"01ARZ3NDEKTSV4RRFFQ69G5FAX",
		"01ARZ3NDEKTSV4RRFFQ69G5FAY",
	}
	for i, id := range validIDs {
		seedOfflineMK20WaitingDealWithID(t, ctx, dbs.primary, uint64(1000+i), ulid.MustParse(id))
	}

	// Exercise the real fresh sector-pressure query, including the blocked
	// result. Removing these rows must be observed by the very next pass.
	for i := 0; i < 9; i++ {
		if _, err := dbs.primary.Exec(ctx, `INSERT INTO sectors_sdr_pipeline
			(sp_id, sector_number, failed, task_id_sdr, after_sdr)
			VALUES (9000, $1, FALSE, $2, FALSE)`, i, i+1); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.DefaultCurioConfig()
	cfg.Ingest.DoSnap = false
	cfg.Ingest.MK20PipelineInsertBatch.Set(2)
	cfg.Ingest.MK20PipelineInsertMaxActive.Set(3)
	market := &CurioStorageDealMarket{
		cfg: cfg,
		db:  dbs.secondary,
		bp:  backpressure.NewCachedBackPressure(),
	}

	market.insertDDODealInPipeline(ctx)
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 0, len(validIDs)+1)
	if _, err := dbs.primary.Exec(ctx, `DELETE FROM sectors_sdr_pipeline WHERE sp_id = 9000`); err != nil {
		t.Fatal(err)
	}

	market.insertDDODealInPipeline(ctx)
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 2, len(validIDs)-1)

	market.insertDDODealInPipeline(ctx)
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 3, len(validIDs)-2)

	// An already-full pass must not release the final valid waiting deal.
	market.insertDDODealInPipeline(ctx)
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 3, len(validIDs)-2)

	if n, err := dbs.primary.Exec(ctx, `UPDATE market_mk20_pipeline
		SET complete = TRUE
		WHERE id = $1`, validIDs[0]); err != nil || n != 1 {
		t.Fatalf("returning one fixture slot: rows=%d err=%v", n, err)
	}
	market.insertDDODealInPipeline(ctx)
	assertMK20ReleaseCounts(t, ctx, dbs.primary, 3, 1)

	var releasedRows, malformedWaiting, validWaiting int
	for _, id := range validIDs {
		var pipelineRows, waitingRows int
		if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*)
			FROM market_mk20_pipeline
			WHERE id = $1`, id).Scan(&pipelineRows); err != nil {
			t.Fatal(err)
		}
		if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*)
			FROM market_mk20_pipeline_waiting
			WHERE id = $1`, id).Scan(&waitingRows); err != nil {
			t.Fatal(err)
		}
		if pipelineRows != 1 || waitingRows != 0 {
			t.Fatalf("deal %s pipeline rows=%d waiting rows=%d, want 1/0", id, pipelineRows, waitingRows)
		}
		releasedRows += pipelineRows
		validWaiting += waitingRows
	}
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*)
		FROM market_mk20_pipeline_waiting
		WHERE id = $1`, malformedID).Scan(&malformedWaiting); err != nil {
		t.Fatal(err)
	}
	if releasedRows != len(validIDs) || malformedWaiting != 1 || validWaiting != 0 {
		t.Fatalf("released rows=%d malformed waiting=%d valid waiting=%d", releasedRows, malformedWaiting, validWaiting)
	}
}

func TestMK20ReleaseDBPartialInsertRollsBackAllRows(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := seedHTTPMK20WaitingDeal(t, ctx, dbs.primary, 1000)
	parsed := ulid.MustParse(id)
	injected := errors.New("injected insert failure")

	outcome, err := mk20release.Release(ctx, dbs.primary, id, 10, func(tx *harmonydb.Tx) (mk20release.Plan, error) {
		deal, err := mk20.DealFromTX(tx, parsed)
		if err != nil {
			return mk20release.Plan{}, err
		}
		rows, err := mk20PipelineRowCost(deal)
		if err != nil {
			return mk20release.Plan{}, err
		}
		return mk20release.Plan{Rows: rows, Insert: func() error {
			if err := insertPiecesInTransaction(ctx, tx, deal); err != nil {
				return err
			}
			return injected
		}}, nil
	})
	if err == nil || outcome == mk20release.Released || !errors.Is(err, injected) {
		t.Fatalf("partial insert outcome=%q err=%v", outcome, err)
	}

	assertMK20ReleaseCounts(t, ctx, dbs.primary, 0, 1)
	var pipelineRows, downloadRows, parkedRows, referenceRows int
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_pipeline WHERE id = $1`, id).Scan(&pipelineRows); err != nil {
		t.Fatal(err)
	}
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_download_pipeline WHERE id = $1`, id).Scan(&downloadRows); err != nil {
		t.Fatal(err)
	}
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*) FROM parked_pieces`).Scan(&parkedRows); err != nil {
		t.Fatal(err)
	}
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*) FROM parked_piece_refs`).Scan(&referenceRows); err != nil {
		t.Fatal(err)
	}
	if pipelineRows != 0 || downloadRows != 0 || parkedRows != 0 || referenceRows != 0 {
		t.Fatalf("pipeline=%d download=%d parked=%d refs=%d after rollback, want 0 each", pipelineRows, downloadRows, parkedRows, referenceRows)
	}
}

func TestMK20ReleaseDBStaleSnapshotRetriesCleanly(t *testing.T) {
	dbs := newMK20ReleaseITestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	id := seedOfflineMK20WaitingDeal(t, ctx, dbs.primary, 1000)

	var attempts atomic.Int32
	snapshotReady := make(chan struct{})
	releaseFinished := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		_, err := dbs.primary.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			attempt := attempts.Add(1)
			if _, err := tx.Exec(`SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
				return false, err
			}
			var token bool
			if err := tx.QueryRow(`SELECT token FROM market_mk20_release_gate WHERE singleton = TRUE`).Scan(&token); err != nil {
				return false, err
			}
			if _, err := tx.Exec(`INSERT INTO release_retry_writes (attempt) VALUES ($1)`, attempt); err != nil {
				return false, err
			}
			if attempt == 1 {
				close(snapshotReady)
				select {
				case <-releaseFinished:
				case <-ctx.Done():
					return false, ctx.Err()
				}
			}
			_, err := tx.Exec(`UPDATE market_mk20_release_gate
				SET token = NOT token
				WHERE singleton = TRUE`)
			return err == nil, err
		}, harmonydb.OptionRetry())
		errCh <- err
	}()

	select {
	case <-snapshotReady:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	outcome, err := releaseMK20WaitingDeal(ctx, dbs.secondary, id, 10)
	if err != nil || outcome != mk20release.Released {
		t.Fatalf("competing release outcome=%q err=%v", outcome, err)
	}
	close(releaseFinished)
	if err := <-errCh; err != nil {
		t.Fatalf("retrying stale-snapshot transaction: %v", err)
	}
	if attempts.Load() < 2 {
		t.Fatalf("transaction attempts=%d, want a serialization retry", attempts.Load())
	}

	var writes, firstAttemptWrites int
	if err := dbs.primary.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE attempt = 1)
		FROM release_retry_writes`).Scan(&writes, &firstAttemptWrites); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || firstAttemptWrites != 0 {
		t.Fatalf("durable retry writes=%d first-attempt writes=%d, want one clean final-attempt row", writes, firstAttemptWrites)
	}
}

func seedOfflineMK20WaitingDeal(t *testing.T, ctx context.Context, db *harmonydb.DB, providerID uint64) string {
	return seedMK20WaitingDeal(t, ctx, db, providerID, false)
}

func seedHTTPMK20WaitingDeal(t *testing.T, ctx context.Context, db *harmonydb.DB, providerID uint64) string {
	return seedMK20WaitingDeal(t, ctx, db, providerID, true)
}

func seedMK20WaitingDeal(t *testing.T, ctx context.Context, db *harmonydb.DB, providerID uint64, useHTTP bool) string {
	t.Helper()
	return seedMK20WaitingDealWithID(t, ctx, db, providerID, useHTTP, ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader))
}

func seedOfflineMK20WaitingDealWithID(t *testing.T, ctx context.Context, db *harmonydb.DB, providerID uint64, id ulid.ULID) string {
	t.Helper()
	return seedMK20WaitingDealWithID(t, ctx, db, providerID, false, id)
}

func seedMK20WaitingDealWithID(t *testing.T, ctx context.Context, db *harmonydb.DB, providerID uint64, useHTTP bool, id ulid.ULID) string {
	t.Helper()
	provider, err := address.NewIDAddress(providerID)
	if err != nil {
		t.Fatal(err)
	}
	piece, err := cid.Parse(mk20ReleaseITestPieceCID)
	if err != nil {
		t.Fatal(err)
	}
	data := &mk20.DataSource{
		PieceCID: piece,
		Format:   mk20.PieceDataFormat{Car: &mk20.FormatCar{}},
	}
	if useHTTP {
		data.SourceHTTP = &mk20.DataSourceHTTP{URLs: []mk20.HttpUrl{{
			URL:     "https://example.invalid/piece.car",
			Headers: http.Header{"X-Test": []string{"mk20-release"}},
		}}}
	} else {
		data.SourceOffline = &mk20.DataSourceOffline{}
	}

	deal := &mk20.Deal{
		Identifier: id,
		Client:     fmt.Sprintf("client-%d", providerID),
		Data:       data,
		Products: mk20.Products{
			DDOV1: &mk20.DDOV1{
				Provider:   provider,
				Duration:   abi.ChainEpoch(5_256_000),
				StartEpoch: nil,
			},
			RetrievalV1: &mk20.RetrievalV1{},
		},
	}
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if err := deal.SaveToDB(tx); err != nil {
			return false, err
		}
		n, err := tx.Exec(`INSERT INTO market_mk20_pipeline_waiting (id) VALUES ($1)`, deal.Identifier.String())
		return n == 1 && err == nil, err
	})
	if err != nil || !committed {
		t.Fatalf("seeding waiting deal: committed=%t err=%v", committed, err)
	}
	return deal.Identifier.String()
}

func seedActiveMK20PipelineRow(t *testing.T, ctx context.Context, db *harmonydb.DB, id string, providerID int64, complete bool) {
	t.Helper()
	if _, err := db.Exec(ctx, `INSERT INTO market_mk20_pipeline (
		id, sp_id, contract, client, piece_cid_v2, piece_cid, piece_size,
		raw_size, offline, indexing, announce, duration, complete
	) VALUES ($1, $2, '', 'client', 'piece-v2', 'piece-v1', 2048,
		2032, TRUE, FALSE, FALSE, 5256000, $3)`, id, providerID, complete); err != nil {
		t.Fatal(err)
	}
}

func assertMK20ReleaseCounts(t *testing.T, ctx context.Context, db *harmonydb.DB, active, waiting int) {
	t.Helper()
	var gotActive, gotWaiting int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_pipeline WHERE complete = FALSE`).Scan(&gotActive); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_pipeline_waiting`).Scan(&gotWaiting); err != nil {
		t.Fatal(err)
	}
	if gotActive != active || gotWaiting != waiting {
		t.Fatalf("active=%d waiting=%d, want active=%d waiting=%d", gotActive, gotWaiting, active, waiting)
	}
}

// mk20ReleaseITestSchema is a focused projection of the current Curio schema.
// Production columns and primary keys used by DealFromTX,
// insertPiecesInTransaction, pressure checks, and release accounting are
// retained. release_retry_writes is a test-only transaction-retry probe.
const mk20ReleaseITestSchema = `
CREATE TABLE market_mk20_deal (
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    id TEXT PRIMARY KEY,
    client TEXT NOT NULL,
    piece_cid_v2 TEXT,
    data JSONB NOT NULL DEFAULT 'null',
    ddo_v1 JSONB NOT NULL DEFAULT 'null',
    retrieval_v1 JSONB NOT NULL DEFAULT 'null',
    pdp_v1 JSONB NOT NULL DEFAULT 'null'
);

CREATE TABLE market_mk20_pipeline (
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    id TEXT NOT NULL,
    sp_id BIGINT NOT NULL,
    contract TEXT NOT NULL,
    client TEXT NOT NULL,
    piece_cid_v2 TEXT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    raw_size BIGINT NOT NULL,
    offline BOOLEAN NOT NULL,
    url TEXT DEFAULT NULL,
    indexing BOOLEAN NOT NULL,
    announce BOOLEAN NOT NULL,
    allocation_id BIGINT DEFAULT NULL,
    duration BIGINT NOT NULL,
    piece_aggregation INT NOT NULL DEFAULT 0,
    started BOOLEAN DEFAULT FALSE,
    downloaded BOOLEAN DEFAULT FALSE,
    commp_task_id BIGINT DEFAULT NULL,
    after_commp BOOLEAN DEFAULT FALSE,
    deal_aggregation INT NOT NULL DEFAULT 0,
    aggr_index BIGINT DEFAULT 0,
    agg_task_id BIGINT DEFAULT NULL,
    aggregated BOOLEAN DEFAULT FALSE,
    sector BIGINT DEFAULT NULL,
    reg_seal_proof INT DEFAULT NULL,
    sector_offset BIGINT DEFAULT NULL,
    sealed BOOLEAN DEFAULT FALSE,
    indexing_created_at TIMESTAMPTZ DEFAULT NULL,
    indexing_task_id BIGINT DEFAULT NULL,
    indexed BOOLEAN DEFAULT FALSE,
    complete BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (id, aggr_index)
);

CREATE TABLE market_mk20_pipeline_waiting (
    id TEXT PRIMARY KEY
);

CREATE TABLE market_mk20_download_pipeline (
    id TEXT NOT NULL,
    product TEXT NOT NULL,
    piece_cid_v2 TEXT NOT NULL,
    ref_ids BIGINT[] NOT NULL,
    PRIMARY KEY (id, product, piece_cid_v2)
);

CREATE TABLE parked_pieces (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    piece_cid TEXT NOT NULL,
    piece_padded_size BIGINT NOT NULL,
    piece_raw_size BIGINT NOT NULL,
    complete BOOLEAN NOT NULL DEFAULT FALSE,
    task_id BIGINT,
    cleanup_task_id BIGINT,
    long_term BOOLEAN NOT NULL DEFAULT FALSE,
    skip BOOLEAN NOT NULL DEFAULT FALSE,
    ref_count INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX parked_pieces_active_piece_key
    ON parked_pieces (piece_cid, piece_padded_size, long_term)
    WHERE cleanup_task_id IS NULL;

CREATE TABLE parked_piece_refs (
    ref_id BIGSERIAL PRIMARY KEY,
    piece_id BIGINT NOT NULL REFERENCES parked_pieces(id) ON DELETE CASCADE,
    data_url TEXT,
    data_headers JSONB NOT NULL DEFAULT '{}',
    long_term BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE sectors_sdr_pipeline (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    failed BOOLEAN NOT NULL DEFAULT FALSE,
	task_id_sdr BIGINT,
	after_sdr BOOLEAN NOT NULL DEFAULT FALSE,
	task_id_tree_r BIGINT,
	after_tree_r BOOLEAN NOT NULL DEFAULT FALSE,
	task_id_porep BIGINT,
	after_porep BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (sp_id, sector_number)
);

CREATE TABLE harmony_task (
    id BIGINT PRIMARY KEY,
    owner_id BIGINT
);

CREATE TABLE open_sector_pieces (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    piece_index INT NOT NULL,
    PRIMARY KEY (sp_id, sector_number, piece_index)
);

CREATE TABLE sectors_sdr_initial_pieces (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    piece_index INT NOT NULL,
    PRIMARY KEY (sp_id, sector_number, piece_index)
);

CREATE TABLE release_retry_writes (
    attempt INT NOT NULL
);
`
