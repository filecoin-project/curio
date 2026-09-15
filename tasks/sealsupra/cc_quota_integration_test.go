//go:build integration

package sealsupra

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/types"
)

// No native constructor or chain request is involved. Unexpected API calls
// through the embedded nil interface fail instead of contacting a service.
type ccQuotaSQLAPI struct {
	SupraSealNodeAPI
	beforeAllocation func() error
}

func (a ccQuotaSQLAPI) StateMinerAllocated(context.Context, address.Address, types.TipSetKey) (*bitfield.BitField, error) {
	if a.beforeAllocation != nil {
		if err := a.beforeAllocation(); err != nil {
			return nil, err
		}
	}
	b := bitfield.New()
	return &b, nil
}

func ccQuotaSQLDBs(t *testing.T) [2]*harmonydb.DB {
	t.Helper()
	const prefix = "CURIO_CC_QUOTA_ITEST"
	if os.Getenv(prefix) != "1" {
		t.Skip("set CURIO_CC_QUOTA_ITEST=1 and the dedicated target variables")
	}
	get := func(key string) string {
		v := os.Getenv(prefix + "_" + key)
		require.NotEmpty(t, v, "dedicated target requires %s", key)
		return v
	}
	host, port, database, user := get("HOST"), get("PORT"), get("DATABASE"), get("USER")
	ip := net.ParseIP(host)
	require.NotNil(t, ip, "host must be a literal loopback IP")
	require.True(t, ip.IsLoopback(), "host must be loopback")
	p, err := strconv.Atoi(port)
	require.NoError(t, err)
	require.True(t, p > 0 && p < 65536, "invalid port")
	cfg := harmonydb.Config{Hosts: []string{host}, Port: port, Database: database, Username: user,
		Password: os.Getenv(prefix + "_PASSWORD"), LoadBalance: false, ReadOnly: true,
		UseTemplate: false, ITestID: harmonydb.ITestNewID(), ApplicationName: "curio-cc-quota-itest"}
	first, err := harmonydb.NewFromConfig(cfg)
	require.NoError(t, err)
	t.Cleanup(first.ITestDeleteAll)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Dynamic DDL uses pgx only for fixture setup. The operations under test
	// use the production HarmonyDB transaction and SQL constants below.
	pc, err := pgx.ParseConfig("postgresql://placeholder@127.0.0.1/placeholder?sslmode=disable&load_balance=false&fallback_to_topology_keys_only=true")
	require.NoError(t, err)
	pc.Host, pc.Port, pc.Database, pc.User, pc.Password = host, uint16(p), database, user, cfg.Password
	pc.RuntimeParams["search_path"] = "itest_" + string(cfg.ITestID)
	pc.RuntimeParams["statement_timeout"], pc.RuntimeParams["lock_timeout"] = "10000", "2000"
	conn, err := pgx.ConnectConfig(ctx, pc)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(ctx)) }()
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	sqlDir := filepath.Join(filepath.Dir(source), "..", "..", "harmony", "harmonydb", "sql")
	for _, name := range []string{"20230719-harmony.sql", "20231217-sdr-pipeline.sql", "20240802-sdr-pipeline-user-expiration.sql", "20250808-cc-scheduler.sql"} {
		ddl, err := os.ReadFile(filepath.Join(sqlDir, name))
		require.NoError(t, err)
		_, err = conn.Exec(ctx, string(ddl))
		require.NoError(t, err, name)
	}
	// Apply the actual current pipeline FK removals, excluding the migration's
	// unrelated parked-piece statements. Initial-piece cascade remains intact.
	ddl, err := os.ReadFile(filepath.Join(sqlDir, "20240507-sdr-pipeline-fk-drop.sql"))
	require.NoError(t, err)
	for statement := range strings.SplitSeq(string(ddl), ";") {
		if strings.HasPrefix(strings.TrimSpace(statement), "ALTER TABLE sectors_sdr_pipeline ") {
			_, err := conn.Exec(ctx, statement)
			require.NoError(t, err)
		}
	}
	peer := cfg
	peer.Schema, peer.ITestID = "itest_"+string(cfg.ITestID), ""
	peer.ApplicationName = "curio-cc-quota-itest-peer"
	second, err := harmonydb.NewFromConfig(peer)
	require.NoError(t, err)
	// HarmonyDB cleanup closes its pool and drops only its owned itest schema.
	// The second cleanup may report that this same schema is already absent.
	t.Cleanup(second.ITestDeleteAll)
	var version, isolation string
	require.NoError(t, first.QueryRow(ctx, `SELECT version()`).Scan(&version))
	require.NoError(t, first.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation))
	t.Logf("version=%q SQL requested=default reported=%q effective Yugabyte isolation=UNVERIFIED", version, isolation)
	return [2]*harmonydb.DB{first, second}
}

func TestCCQuotaSQLClaimsAndRollback(t *testing.T) {
	for _, invalidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("disable_after_discovery=%t", invalidate), func(t *testing.T) {
			dbs := ccQuotaSQLDBs(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := dbs[0].Exec(ctx, `INSERT INTO sectors_cc_scheduler(sp_id,to_seal,weight,duration_days) VALUES (1001,7,1,200),(1002,1,1,210),(1003,9,0,220)`)
			require.NoError(t, err)
			var claims []sectorClaim
			committed, err := dbs[0].BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				claims = nil
				if _, err := tx.Exec(`INSERT INTO harmony_task(name,posted_time,added_by) VALUES ('SDR',CURRENT_TIMESTAMP,1)`); err != nil {
					return false, err
				}
				api := ccQuotaSQLAPI{}
				if invalidate {
					// Same-transaction stale-discovery guard, not a race. Fail
					// after the first provider's allocation and debit can occur.
					api.beforeAllocation = func() error {
						_, err := tx.Exec(`UPDATE sectors_cc_scheduler SET enabled=FALSE WHERE sp_id=1002`)
						return err
					}
				}
				s := SupraSeal{api: api, spt: abi.RegisteredSealProof_StackedDrg32GiBV1_1}
				var err error
				claims, err = s.claimsFromCCScheduler(tx, 8)
				return err == nil, err
			}, harmonydb.OptionRetry())
			var rows struct{ Pipeline, Allocated, Tasks int64 }
			require.NoError(t, dbs[0].QueryRow(ctx, `SELECT (SELECT count(*) FROM sectors_sdr_pipeline),(SELECT count(*) FROM sectors_allocated_numbers),(SELECT count(*) FROM harmony_task)`).Scan(&rows.Pipeline, &rows.Allocated, &rows.Tasks))
			var first, second, unrelated int64
			require.NoError(t, dbs[0].QueryRow(ctx, `SELECT (SELECT to_seal FROM sectors_cc_scheduler WHERE sp_id=1001),(SELECT to_seal FROM sectors_cc_scheduler WHERE sp_id=1002),(SELECT to_seal FROM sectors_cc_scheduler WHERE sp_id=1003)`).Scan(&first, &second, &unrelated))
			require.EqualValues(t, 9, unrelated)
			if invalidate {
				require.ErrorContains(t, err, "CC quota changed for provider 1002")
				require.False(t, committed)
				require.Nil(t, claims)
				require.Zero(t, rows)
				require.EqualValues(t, 7, first)
				require.EqualValues(t, 1, second)
				var enabled bool
				require.NoError(t, dbs[0].QueryRow(ctx, `SELECT enabled FROM sectors_cc_scheduler WHERE sp_id=1002`).Scan(&enabled))
				require.True(t, enabled)
			} else {
				require.NoError(t, err)
				require.True(t, committed)
				require.Len(t, claims, 8)
				require.EqualValues(t, 8, rows.Pipeline)
				require.EqualValues(t, 2, rows.Allocated)
				require.EqualValues(t, 1, rows.Tasks)
				require.Zero(t, first)
				require.Zero(t, second)
			}
		})
	}
}

func TestCCQuotaSQLConditionalDebit(t *testing.T) {
	dbs := ccQuotaSQLDBs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := dbs[0].Exec(ctx, `INSERT INTO sectors_cc_scheduler(sp_id,to_seal,enabled) VALUES (1001,3,TRUE),(1002,3,FALSE),(1003,3,TRUE)`)
	require.NoError(t, err)
	for _, tc := range []struct{ provider, count int64 }{{1001, 4}, {1002, 1}, {9999, 1}} {
		committed, err := dbs[0].BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			err := debitCCQuota(ccAllocation{schedule: ccSchedule{SpID: tc.provider}, count: tc.count}, func(count, provider int64) (int, error) { return tx.Exec(ccSchedulerDebitSQL, count, provider) })
			return err == nil, err
		})
		require.ErrorContains(t, err, "updated 0 rows")
		require.False(t, committed)
	}
	var unchanged int64
	require.NoError(t, dbs[0].QueryRow(ctx, `SELECT count(*) FROM sectors_cc_scheduler WHERE to_seal=3`).Scan(&unchanged))
	require.EqualValues(t, 3, unchanged)
}

func TestCCQuotaSQLConcurrentDebit(t *testing.T) {
	dbs := ccQuotaSQLDBs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var version string
	require.NoError(t, dbs[0].QueryRow(ctx, `SELECT version()`).Scan(&version))
	if strings.Contains(strings.ToLower(version), "yugabyte") {
		t.Skip("linked PostgreSQL blocking observation is not a Yugabyte observer")
	}
	_, err := dbs[0].Exec(ctx, `INSERT INTO sectors_cc_scheduler(sp_id,to_seal) VALUES (1001,3),(1002,9)`)
	require.NoError(t, err)
	type result struct {
		owner, committed bool
		err              error
	}
	held, entered := make(chan int, 1), make(chan int, 1)
	release, outcomes := make(chan struct{}), make(chan result, 2)
	var once sync.Once
	var participants sync.WaitGroup
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); cancel(); participants.Wait() }()
	start := func(owner bool, db *harmonydb.DB) {
		participants.Add(1)
		go func() {
			defer participants.Done()
			committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				var pid int
				if err := tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return false, err
				}
				if !owner {
					entered <- pid
				}
				err := debitCCQuota(ccAllocation{schedule: ccSchedule{SpID: 1001}, count: 3}, func(count, provider int64) (int, error) { return tx.Exec(ccSchedulerDebitSQL, count, provider) })
				if err != nil {
					return false, err
				}
				if owner {
					held <- pid
					select {
					case <-release:
					case <-ctx.Done():
						return false, ctx.Err()
					}
				}
				return true, nil
			})
			outcomes <- result{owner, committed, err}
		}()
	}
	start(true, dbs[0])
	var holder, waiter int
	select {
	case holder = <-held:
	case r := <-outcomes:
		t.Fatalf("holder failed: %+v", r)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	start(false, dbs[1])
	select {
	case waiter = <-entered:
	case r := <-outcomes:
		t.Fatalf("contender failed: %+v", r)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	observation, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var blocked bool
		err := dbs[0].QueryRow(observation, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, holder, waiter).Scan(&blocked)
		require.NoError(t, err, "contention not established")
		if blocked {
			t.Log("observed quota UPDATE blocked by holder before commit")
			break
		}
		select {
		case r := <-outcomes:
			t.Fatalf("contender finished before holder release: %+v", r)
		case <-observation.Done():
			t.Fatal("contention not established")
		case <-tick.C:
		}
	}
	unblock()
	for range 2 {
		select {
		case r := <-outcomes:
			if r.owner {
				require.NoError(t, r.err)
				require.True(t, r.committed)
			} else {
				require.ErrorContains(t, r.err, "updated 0 rows")
				require.False(t, r.committed)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var remaining, unrelated int64
	require.NoError(t, dbs[0].QueryRow(ctx, `SELECT (SELECT to_seal FROM sectors_cc_scheduler WHERE sp_id=1001),(SELECT to_seal FROM sectors_cc_scheduler WHERE sp_id=1002)`).Scan(&remaining, &unrelated))
	require.Zero(t, remaining)
	require.EqualValues(t, 9, unrelated)
}
