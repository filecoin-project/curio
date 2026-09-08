//go:build integration && !skiff

package indexing

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"
)

// The only connection entry point: no normal Curio/HarmonyDB/libpq target is
// accepted. This fixture is local to indexing; it imports no other PR's tests.
func newIndexingOffsetSQLFixture(t *testing.T) (context.Context, *pgx.Conn) {
	t.Helper()
	if os.Getenv("CURIO_INDEXING_OFFSET_ITEST") != "1" {
		t.Skip("requires CURIO_INDEXING_OFFSET_ITEST=1 and an explicit disposable loopback target")
	}
	const prefix = "CURIO_INDEXING_OFFSET_ITEST_"
	host, database, user := os.Getenv(prefix+"HOST"), os.Getenv(prefix+"DATABASE"), os.Getenv(prefix+"USER")
	ip := net.ParseIP(host)
	require.True(t, ip != nil && ip.IsLoopback(), "dedicated HOST must be a literal loopback IP")
	port, err := strconv.ParseUint(os.Getenv(prefix+"PORT"), 10, 16)
	require.NoError(t, err, "dedicated PORT must be explicitly supplied")
	require.NotZero(t, port)
	require.NotEmpty(t, strings.TrimSpace(database), "dedicated DATABASE required")
	require.NotEmpty(t, strings.TrimSpace(user), "dedicated USER required")

	cfg, err := pgx.ParseConfig("postgresql://placeholder@127.0.0.1/placeholder?sslmode=disable&load_balance=false")
	require.NoError(t, err)
	cfg.Host, cfg.Port, cfg.Database, cfg.User = host, uint16(port), database, user
	cfg.Password = os.Getenv(prefix + "PASSWORD")
	cfg.Fallbacks = nil
	cfg.ConnectTimeout = 5 * time.Second
	schema := "itest_indexing_offset_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	cfg.RuntimeParams = map[string]string{
		"search_path": schema, "application_name": "curio-indexing-offset-itest",
		"statement_timeout": "5000", "lock_timeout": "2000",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		require.NoError(t, conn.Close(closeCtx))
	})
	_, err = conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, err := conn.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, err)
	})
	_, err = conn.Exec(ctx, indexingOffsetSQLSchema)
	require.NoError(t, err)
	var version, isolation string
	require.NoError(t, conn.QueryRow(ctx, `SELECT version()`).Scan(&version))
	require.NoError(t, conn.QueryRow(ctx, `SHOW default_transaction_isolation`).Scan(&isolation))
	t.Logf("version=%s schema=%s requested isolation=driver default; SQL default=%s; effective Yugabyte isolation=UNVERIFIED", version, schema, isolation)
	return ctx, conn
}

type indexingOffsetSQLRow struct {
	ID            string
	Provider      int64
	Aggregate     int64
	Offset        *int64
	Created       *time.Time
	Sealed        bool
	Indexed       bool
	Complete      bool
	PhysicalIndex bool
	Task          *int64
}

func indexingOffsetSQLRowFixture(number int) indexingOffsetSQLRow {
	created := time.Date(2000, 1, 1, 0, number, 0, 0, time.UTC)
	offset := int64(0)
	return indexingOffsetSQLRow{ID: fmt.Sprintf("%026d", number), Provider: 1000,
		Offset: &offset, Created: &created, Sealed: true}
}

func seedIndexingOffsetSQLRow(t *testing.T, ctx context.Context, conn *pgx.Conn, mk20 bool, row indexingOffsetSQLRow) {
	t.Helper()
	if mk20 {
		_, err := conn.Exec(ctx, `INSERT INTO market_mk20_pipeline
			(id, sp_id, aggr_index, sector_offset, indexing_created_at, sealed, indexed, complete, indexing, indexing_task_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, row.ID, row.Provider, row.Aggregate,
			row.Offset, row.Created, row.Sealed, row.Indexed, row.Complete, row.PhysicalIndex, row.Task)
		require.NoError(t, err)
	} else {
		_, err := conn.Exec(ctx, `INSERT INTO market_mk12_deal_pipeline
			(uuid, sp_id, sector_offset, indexing_created_at, sealed, indexed, complete, should_index, indexing_task_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, row.ID, row.Provider,
			row.Offset, row.Created, row.Sealed, row.Indexed, row.Complete, row.PhysicalIndex, row.Task)
		require.NoError(t, err)
	}
}

func indexingOffsetSQLSnapshot(t *testing.T, ctx context.Context, conn *pgx.Conn, mk20 bool) []indexingOffsetSQLRow {
	t.Helper()
	query := `SELECT uuid, sp_id, 0::bigint, sector_offset, indexing_created_at, sealed, indexed, complete, should_index, indexing_task_id
		FROM market_mk12_deal_pipeline ORDER BY uuid`
	if mk20 {
		query = `SELECT id, sp_id, aggr_index, sector_offset, indexing_created_at, sealed, indexed, complete, indexing, indexing_task_id
			FROM market_mk20_pipeline ORDER BY id, aggr_index`
	}
	rows, err := conn.Query(ctx, query)
	require.NoError(t, err)
	result, err := pgx.CollectRows(rows, pgx.RowToStructByPos[indexingOffsetSQLRow])
	require.NoError(t, err)
	return result
}

// Execute the exact combined candidate/conditional-assignment constant consumed
// by IndexingTask.schedule. Only task creation/commit plumbing is test-owned;
// this does not replace or reproduce the scheduler's selection algorithm.
func assignIndexingOffsetSQL(t *testing.T, ctx context.Context, conn *pgx.Conn, mk20, rollback bool) (int64, int64) {
	t.Helper()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	require.NoError(t, err)
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		err := tx.Rollback(cleanupCtx)
		if err != nil && err != pgx.ErrTxClosed {
			t.Errorf("rollback: %v", err)
		}
	}()
	var isolation string
	require.NoError(t, tx.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation))
	t.Logf("assignment SQL transaction_isolation=%s; effective Yugabyte isolation=UNVERIFIED", isolation)
	var taskID int64
	require.NoError(t, tx.QueryRow(ctx, `INSERT INTO harmony_task (name) VALUES ('Indexing') RETURNING id`).Scan(&taskID))
	query := indexingMK12AssignSQL
	if mk20 {
		query = indexingMK20AssignSQL
	}
	tag, err := tx.Exec(ctx, query, taskID)
	require.NoError(t, err)
	n := tag.RowsAffected()
	require.LessOrEqual(t, n, int64(1))
	if n == 1 && !rollback {
		require.NoError(t, tx.Commit(ctx))
	}
	return taskID, n
}

func requireIndexingOffsetSQLTaskCount(t *testing.T, ctx context.Context, conn *pgx.Conn, count int) {
	t.Helper()
	var got int
	require.NoError(t, conn.QueryRow(ctx, `SELECT COUNT(*) FROM harmony_task`).Scan(&got))
	require.Equal(t, count, got)
}

func TestIndexingOffsetSQLReadiness(t *testing.T) {
	for _, market := range []string{"MK12", "MK20"} {
		t.Run(market, func(t *testing.T) {
			mk20 := market == "MK20"
			ctx, conn := newIndexingOffsetSQLFixture(t)
			older := indexingOffsetSQLRowFixture(1)
			older.Offset = nil
			zero := indexingOffsetSQLRowFixture(2) // Metadata-only: must still be assigned.
			positive := indexingOffsetSQLRowFixture(3)
			offset := int64(512)
			positive.Offset, positive.PhysicalIndex = &offset, true
			peer := indexingOffsetSQLRowFixture(4)
			peer.Provider = 2000
			if mk20 {
				peer.ID, peer.Aggregate = zero.ID, 7
			}
			for _, row := range []indexingOffsetSQLRow{older, zero, positive, peer} {
				seedIndexingOffsetSQLRow(t, ctx, conn, mk20, row)
			}
			for i, state := range []string{"assigned", "complete", "unsealed", "indexed", "no timestamp"} {
				row := indexingOffsetSQLRowFixture(i + 10)
				row.Created = older.Created // Older exclusions cannot consume LIMIT 1.
				switch state {
				case "assigned":
					var task int64
					require.NoError(t, conn.QueryRow(ctx, `INSERT INTO harmony_task (name) VALUES ('Indexing') RETURNING id`).Scan(&task))
					row.Task = &task
				case "complete":
					row.Complete, row.Indexed = true, true
				case "unsealed":
					row.Sealed = false
				case "indexed":
					row.Indexed = true
				case "no timestamp":
					row.Created = nil
				}
				seedIndexingOffsetSQLRow(t, ctx, conn, mk20, row)
			}
			otherMarket := indexingOffsetSQLRowFixture(99)
			seedIndexingOffsetSQLRow(t, ctx, conn, !mk20, otherMarket)
			untouched := indexingOffsetSQLSnapshot(t, ctx, conn, !mk20)
			for _, target := range []indexingOffsetSQLRow{zero, positive} {
				want := indexingOffsetSQLSnapshot(t, ctx, conn, mk20)
				taskID, n := assignIndexingOffsetSQL(t, ctx, conn, mk20, false)
				require.EqualValues(t, 1, n, "older NULL row must not block the later eligible row")
				for i := range want {
					if want[i].ID == target.ID && want[i].Aggregate == target.Aggregate {
						want[i].Task = &taskID
					}
				}
				require.Equal(t, want, indexingOffsetSQLSnapshot(t, ctx, conn, mk20), "only the exact selected identity may change")
				require.Equal(t, untouched, indexingOffsetSQLSnapshot(t, ctx, conn, !mk20))
			}
			requireIndexingOffsetSQLTaskCount(t, ctx, conn, 3) // Existing task plus two new assignments.
		})
	}
}

func TestIndexingOffsetSQLAllNullThenReady(t *testing.T) {
	for _, market := range []string{"MK12", "MK20"} {
		t.Run(market, func(t *testing.T) {
			mk20 := market == "MK20"
			ctx, conn := newIndexingOffsetSQLFixture(t)
			for i := 1; i <= 2; i++ {
				row := indexingOffsetSQLRowFixture(i)
				row.Offset = nil
				seedIndexingOffsetSQLRow(t, ctx, conn, mk20, row)
			}
			before := indexingOffsetSQLSnapshot(t, ctx, conn, mk20)
			_, n := assignIndexingOffsetSQL(t, ctx, conn, mk20, false)
			require.Zero(t, n)
			require.Equal(t, before, indexingOffsetSQLSnapshot(t, ctx, conn, mk20))
			requireIndexingOffsetSQLTaskCount(t, ctx, conn, 0)
			// A later, separate scheduling statement observes producer progress.
			id := indexingOffsetSQLRowFixture(2).ID
			if mk20 {
				_, err := conn.Exec(ctx, `UPDATE market_mk20_pipeline SET sector_offset = 0 WHERE id = $1 AND sp_id = 1000 AND aggr_index = 0`, id)
				require.NoError(t, err)
			} else {
				_, err := conn.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET sector_offset = 0 WHERE uuid = $1 AND sp_id = 1000`, id)
				require.NoError(t, err)
			}
			task, n := assignIndexingOffsetSQL(t, ctx, conn, mk20, false)
			require.EqualValues(t, 1, n)
			after := indexingOffsetSQLSnapshot(t, ctx, conn, mk20)
			require.Nil(t, after[0].Task)
			require.Equal(t, &task, after[1].Task)
			requireIndexingOffsetSQLTaskCount(t, ctx, conn, 1)
		})
	}
}

func TestIndexingOffsetSQLAssignmentRollback(t *testing.T) {
	for _, market := range []string{"MK12", "MK20"} {
		t.Run(market, func(t *testing.T) {
			mk20 := market == "MK20"
			ctx, conn := newIndexingOffsetSQLFixture(t)
			seedIndexingOffsetSQLRow(t, ctx, conn, mk20, indexingOffsetSQLRowFixture(1))
			before := indexingOffsetSQLSnapshot(t, ctx, conn, mk20)
			_, n := assignIndexingOffsetSQL(t, ctx, conn, mk20, true)
			require.EqualValues(t, 1, n)
			require.Equal(t, before, indexingOffsetSQLSnapshot(t, ctx, conn, mk20))
			requireIndexingOffsetSQLTaskCount(t, ctx, conn, 0)
			_, n = assignIndexingOffsetSQL(t, ctx, conn, mk20, false)
			require.EqualValues(t, 1, n)
			assigned := indexingOffsetSQLSnapshot(t, ctx, conn, mk20)
			_, n = assignIndexingOffsetSQL(t, ctx, conn, mk20, false)
			require.Zero(t, n)
			require.Equal(t, assigned, indexingOffsetSQLSnapshot(t, ctx, conn, mk20))
			requireIndexingOffsetSQLTaskCount(t, ctx, conn, 1)
		})
	}
}

// Projection of 20240731-market-migration.sql and 20250505-market-mk20.sql:
// actual identity constraints, nullable BIGINT offsets/task IDs, TIMESTAMPTZ
// readiness time, and stage/metadata flags. Neither assignment column has an
// FK to harmony_task. Unused payload fields and unrelated tables are omitted.
// harmony_task's SERIAL identity/name types come from 20230719-harmony.sql;
// the test owns minimal task-create/rollback plumbing, not a running engine.
const indexingOffsetSQLSchema = `
CREATE TABLE harmony_task (id SERIAL PRIMARY KEY NOT NULL, name VARCHAR(16) NOT NULL);
CREATE TABLE market_mk12_deal_pipeline (
    uuid TEXT NOT NULL UNIQUE, sp_id BIGINT NOT NULL,
    sector_offset BIGINT DEFAULT NULL, indexing_created_at TIMESTAMPTZ,
    sealed BOOLEAN DEFAULT FALSE, indexed BOOLEAN DEFAULT FALSE,
    indexing_task_id BIGINT DEFAULT NULL, should_index BOOLEAN DEFAULT FALSE,
    complete BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE TABLE market_mk20_pipeline (
    id TEXT NOT NULL, aggr_index BIGINT DEFAULT 0, sp_id BIGINT NOT NULL,
    sector_offset BIGINT DEFAULT NULL, indexing_created_at TIMESTAMPTZ DEFAULT NULL,
    sealed BOOLEAN DEFAULT FALSE, indexed BOOLEAN DEFAULT FALSE,
    indexing_task_id BIGINT DEFAULT NULL, indexing BOOLEAN NOT NULL,
    complete BOOLEAN NOT NULL DEFAULT FALSE, PRIMARY KEY (id, aggr_index)
);`
