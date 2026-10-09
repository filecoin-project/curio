//go:build integration && !skiff

package webrpcporep

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// Only an explicitly configured disposable target is accepted. The fixture
// owns a random HarmonyDB namespace; it does not run startup migrations or
// connect to Curio/chain APIs. The query and row mapper are production code.
func porepSQLFixture(t *testing.T) (context.Context, *harmonydb.DB, *pgx.Conn) {
	t.Helper()
	if os.Getenv("CURIO_POREP_PAGE_ITEST") != "1" {
		t.Skip("requires CURIO_POREP_PAGE_ITEST=1 and a dedicated loopback target")
	}
	const prefix = "CURIO_POREP_PAGE_ITEST_"
	host, port := os.Getenv(prefix+"HOST"), os.Getenv(prefix+"PORT")
	ip := net.ParseIP(host)
	require.True(t, ip != nil && ip.IsLoopback(), "HOST must be a literal loopback address")
	n, err := strconv.ParseUint(port, 10, 16)
	require.NoError(t, err)
	require.NotZero(t, n)
	database, user := os.Getenv(prefix+"DATABASE"), os.Getenv(prefix+"USER")
	require.NotEmpty(t, strings.TrimSpace(database))
	require.NotEmpty(t, strings.TrimSpace(user))
	opts := harmonydb.ItestOptions{Hosts: []string{host}, Port: port, Database: database, Username: user, Password: os.Getenv(prefix + "PASSWORD"), ITestID: harmonydb.ITestNewID()}
	cfg := opts.HarmonyConfig()
	cfg.ReadOnly, cfg.LoadBalance = true, false
	db, err := harmonydb.NewFromConfig(cfg)
	require.NoError(t, err)
	t.Cleanup(db.ITestDeleteAll)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	pcfg, err := pgx.ParseConfig("postgresql://placeholder@127.0.0.1/placeholder?sslmode=disable&load_balance=false")
	require.NoError(t, err)
	pcfg.Host, pcfg.Port, pcfg.Database, pcfg.User, pcfg.Password = host, uint16(n), database, user, opts.Password
	pcfg.Fallbacks, pcfg.ConnectTimeout = nil, 5*time.Second
	pcfg.RuntimeParams = map[string]string{"search_path": "itest_" + string(opts.ITestID), "statement_timeout": "10000", "lock_timeout": "2000"}
	conn, err := pgx.ConnectConfig(ctx, pcfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		require.NoError(t, conn.Close(c))
	})
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	for _, name := range []string{"20230719-harmony.sql", "20231217-sdr-pipeline.sql", "20240507-sdr-pipeline-fk-drop.sql", "20240617-synthetic-proofs.sql"} {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "harmony", "harmonydb", "sql", name))
		require.NoError(t, err)
		if name == "20240507-sdr-pipeline-fk-drop.sql" {
			// The tail changes unrelated parked_pieces constraints. Retain the
			// exact SDR constraint drops without inventing that unrelated table.
			projection, _, found := strings.Cut(string(b), "\nALTER TABLE parked_pieces")
			require.True(t, found)
			b = []byte(projection)
		}
		_, err = conn.Exec(ctx, string(b))
		require.NoError(t, err)
	}
	// Focused projection of the later timestamp/batching migrations. No other
	// table or batching trigger is needed by this read-only page query. The real
	// composite PK and the deliberate removal of stage-task FKs are preserved.
	_, err = conn.Exec(ctx, `ALTER TABLE sectors_sdr_pipeline ALTER COLUMN create_time TYPE timestamptz;
ALTER TABLE sectors_sdr_pipeline ADD COLUMN precommit_ready_at timestamptz;
ALTER TABLE sectors_sdr_pipeline ADD COLUMN commit_ready_at timestamptz;`)
	require.NoError(t, err)
	var version string
	require.NoError(t, conn.QueryRow(ctx, `SELECT version()`).Scan(&version))
	t.Log(version)
	return ctx, db, conn
}

func porepSQLSelect(db *harmonydb.DB) func(context.Context, interface{}, string, ...interface{}) error {
	return func(ctx context.Context, out interface{}, _ string, args ...interface{}) error {
		return db.Select(ctx, out, porepPageQuery, args...)
	}
}

func TestPoRepPageSQL(t *testing.T) {
	ctx, db, conn := porepSQLFixture(t)
	load := func(req PoRepPageRequest) *PoRepPage {
		t.Helper()
		p, err := loadPoRepPage(ctx, req, porepSQLSelect(db))
		require.NoError(t, err)
		return p
	}
	empty := load(PoRepPageRequest{})
	require.Empty(t, empty.Sectors)
	require.Zero(t, empty.Total)
	require.False(t, empty.ObservedAt.IsZero())
	_, err := db.Exec(ctx, `INSERT INTO harmony_machines(id,host_and_port,cpu,ram,gpu) VALUES(101,'worker.example:12300',8,1024,0);
INSERT INTO harmony_task(id,posted_time,owner_id,added_by,name) VALUES(1,CURRENT_TIMESTAMP,101,101,'SDR'),(2,CURRENT_TIMESTAMP,NULL,101,'SDR');
INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof) SELECT 1000,n,8 FROM generate_series(1,205) n;
UPDATE sectors_sdr_pipeline SET task_id_sdr=1 WHERE sector_number=3;
UPDATE sectors_sdr_pipeline SET task_id_sdr=2 WHERE sector_number=4;
UPDATE sectors_sdr_pipeline SET task_id_sdr=999,task_id_tree_c=998 WHERE sector_number=5;
UPDATE sectors_sdr_pipeline SET failed=true,failed_reason='synthetic' WHERE sector_number=1;
UPDATE sectors_sdr_pipeline SET after_sdr=true,after_synth=true,precommit_ready_at=CURRENT_TIMESTAMP WHERE sector_number=2;
UPDATE sectors_sdr_pipeline SET commit_ready_at=CURRENT_TIMESTAMP WHERE sector_number=3;`)
	require.NoError(t, err)
	page := load(PoRepPageRequest{})
	require.Len(t, page.Sectors, 100)
	require.Equal(t, int64(205), page.Total)
	require.Equal(t, int64(205), page.Matching)
	require.Equal(t, int64(1), page.WaitingForPrecommit)
	require.Equal(t, int64(1), page.WaitingForCommit)
	for i, row := range page.Sectors {
		require.Equal(t, int64(i+1), row.SectorNumber)
		require.Equal(t, int64(1000), row.SpID)
		require.Nil(t, row.ChainSector)
		require.Nil(t, row.AfterSeed)
	}
	require.True(t, page.Sectors[2].SDROwned)
	require.True(t, page.Sectors[2].StartedSDR)
	require.False(t, page.Sectors[3].SDROwned)
	require.Equal(t, []int64{999, 998}, page.Sectors[4].MissingTasks)
	require.Nil(t, page.Sectors[4].SeedEpoch)
	require.False(t, page.Sectors[0].TaskSDR.Valid)
	next := load(PoRepPageRequest{Offset: 100})
	require.Len(t, next.Sectors, 100)
	require.Equal(t, int64(101), next.Sectors[0].SectorNumber)
	last := load(PoRepPageRequest{Offset: 200})
	require.Len(t, last.Sectors, 5)
	beyond := load(PoRepPageRequest{Offset: 999})
	require.Empty(t, beyond.Sectors)
	require.Equal(t, int64(205), beyond.Total)
	hidden := load(PoRepPageRequest{HidePendingSDR: true})
	require.Len(t, hidden.Sectors, 3)
	require.Equal(t, int64(3), hidden.Matching)
	require.Equal(t, int64(205), hidden.Total)
	for i, row := range hidden.Sectors {
		require.Equal(t, int64(i+1), row.SectorNumber)
	}
	_, err = loadPoRepPage(ctx, PoRepPageRequest{Offset: -1}, porepSQLSelect(db))
	require.ErrorContains(t, err, "negative")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	result, err := loadPoRepPage(cancelled, PoRepPageRequest{}, porepSQLSelect(db))
	require.Error(t, err)
	require.Nil(t, result)
	// Deliberately hold only our fixture table. A real statement_timeout must
	// remain an error, not an apparently successful empty page. No contention
	// safety claim is made by this bounded failure-path check.
	_, err = conn.Exec(ctx, `BEGIN; LOCK TABLE sectors_sdr_pipeline IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, err := conn.Exec(cleanup, `ROLLBACK`)
		require.NoError(t, err)
	}()
	_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		_, err := tx.Exec(`SET LOCAL statement_timeout='100ms'`)
		if err != nil {
			return false, err
		}
		p, err := loadPoRepPage(ctx, PoRepPageRequest{}, func(_ context.Context, out interface{}, _ string, args ...interface{}) error {
			return tx.Select(out, porepPageQuery, args...)
		})
		require.Nil(t, p)
		require.ErrorContains(t, err, "statement timeout")
		return false, err
	})
	require.Error(t, err)
}

func TestPoRepPageSQLBoundedCost(t *testing.T) {
	ctx, db, conn := porepSQLFixture(t)
	// Exactly one finite metadata-only population. Every fourth row is owned,
	// every fifth has passed SDR, and every seventeenth failed (overlap allowed).
	_, err := db.Exec(ctx, `INSERT INTO harmony_machines(id,host_and_port,cpu,ram,gpu)
SELECT n,'worker-'||n||'.example:12300',8,1024,0 FROM generate_series(1,44)n;
INSERT INTO harmony_task(id,posted_time,owner_id,added_by,name)
SELECT n,CURRENT_TIMESTAMP,1+(n%44),1,'SDR' FROM generate_series(1,44)n;
INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof,task_id_sdr,after_sdr,failed)
SELECT 1000+(n%3),n,8,CASE WHEN n%4=0 THEN 1+((n/4)%44) ELSE NULL END,n%5=0,n%17=0 FROM generate_series(1,31477)n;
ANALYZE sectors_sdr_pipeline; ANALYZE harmony_task;`)
	require.NoError(t, err)
	var matching int64
	var owners int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(DISTINCT t.owner_id) FROM sectors_sdr_pipeline p JOIN harmony_task t ON t.id=p.task_id_sdr`).Scan(&owners))
	require.Equal(t, 44, owners)
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM sectors_sdr_pipeline WHERE failed OR after_sdr OR task_id_sdr IS NOT NULL`).Scan(&matching))
	for _, hide := range []bool{false, true} {
		for sample := 0; sample < 3; sample++ {
			start := time.Now()
			p, err := loadPoRepPage(ctx, PoRepPageRequest{HidePendingSDR: hide}, porepSQLSelect(db))
			require.NoError(t, err)
			require.Len(t, p.Sectors, 100)
			require.Equal(t, int64(31477), p.Total)
			if hide {
				require.Equal(t, matching, p.Matching)
			} else {
				require.Equal(t, p.Total, p.Matching)
			}
			t.Logf("hide=%v sample=%d wall=%s returned=%d total=%d matching=%d retry_count=NOT_MEASURED", hide, sample, time.Since(start), len(p.Sectors), p.Total, p.Matching)
		}
		rows, err := conn.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+porepPageQuery, hide, POREP_PAGE_SIZE, 0)
		require.NoError(t, err)
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			t.Log(line)
		}
		require.NoError(t, rows.Err())
		rows.Close()
	}
	rows, err := conn.Query(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname=current_schema() AND tablename IN ('sectors_sdr_pipeline','harmony_task','harmony_machines') ORDER BY tablename,indexname`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var index string
		require.NoError(t, rows.Scan(&index))
		t.Log(index)
	}
	require.NoError(t, rows.Err())
}
