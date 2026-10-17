//go:build integration && !skiff

package seal

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/lotus/chain/actors/policy"
)

// This fixture executes the production SQL constants, not the chain-facing
// task. The separate Do tests cover outgoing CBOR membership with a fake API.
func newPrecommitSQLFixture(t *testing.T) (context.Context, *pgx.Conn) {
	t.Helper()
	if os.Getenv("CURIO_PRECOMMIT_ITEST") != "1" {
		t.Skip("requires CURIO_PRECOMMIT_ITEST=1 and a dedicated disposable loopback target")
	}
	const prefix = "CURIO_PRECOMMIT_ITEST_"
	host, database, user := os.Getenv(prefix+"HOST"), os.Getenv(prefix+"DATABASE"), os.Getenv(prefix+"USER")
	ip := net.ParseIP(host)
	require.True(t, ip != nil && ip.IsLoopback(), "HOST must be a literal loopback IP")
	port, err := strconv.ParseUint(os.Getenv(prefix+"PORT"), 10, 16)
	require.NoError(t, err, "PORT must be explicitly supplied")
	require.NotZero(t, port)
	require.NotEmpty(t, strings.TrimSpace(database), "DATABASE must be explicitly supplied")
	require.NotEmpty(t, strings.TrimSpace(user), "USER must be explicitly supplied")

	cfg, err := pgx.ParseConfig("postgresql://placeholder@127.0.0.1/placeholder?sslmode=disable&load_balance=false")
	require.NoError(t, err)
	cfg.Host, cfg.Port, cfg.Database, cfg.User = host, uint16(port), database, user
	cfg.Password = os.Getenv(prefix + "PASSWORD")
	cfg.Fallbacks = nil
	cfg.ConnectTimeout = 5 * time.Second
	schema := "itest_precommit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	cfg.RuntimeParams = map[string]string{"search_path": schema, "application_name": "curio-precommit-itest",
		"statement_timeout": "5000", "lock_timeout": "2000"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		require.NoError(t, conn.Close(cleanup))
	})
	_, err = conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, err := conn.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, err)
	})
	_, err = conn.Exec(ctx, precommitSQLSchema)
	require.NoError(t, err)
	var version, isolation string
	require.NoError(t, conn.QueryRow(ctx, `SELECT version()`).Scan(&version))
	require.NoError(t, conn.QueryRow(ctx, `SHOW default_transaction_isolation`).Scan(&isolation))
	t.Logf("version=%s; requested isolation=driver default; SQL default=%s; effective Yugabyte isolation=UNVERIFIED", version, isolation)
	return ctx, conn
}

func seedPrecommitSQLRows(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	_, err := conn.Exec(ctx, `INSERT INTO sectors_sdr_pipeline
		(sp_id, sector_number, reg_seal_proof, ticket_epoch, after_synth)
		VALUES (1000,1,8,100,TRUE), (1000,2,8,200,TRUE), (1000,3,8,300,TRUE),
		(1000,4,8,400,TRUE), (2000,2,8,200,TRUE), (1000,5,9,500,TRUE)`)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `UPDATE sectors_sdr_pipeline
		SET failed=TRUE, failed_reason='original', failed_reason_msg='keep evidence'
		WHERE sp_id=1000 AND sector_number=1`)
	require.NoError(t, err)
}

func precommitSQLSnapshot(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	t.Helper()
	var snapshot string
	require.NoError(t, conn.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY sp_id,sector_number)::text
		FROM sectors_sdr_pipeline p`).Scan(&snapshot))
	return snapshot
}

func rollbackPrecommitSQL(t *testing.T, tx pgx.Tx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
		t.Errorf("rolling back fixture transaction: %v", err)
	}
}

func TestPrecommitSQLCandidatesAndAssignment(t *testing.T) {
	ctx, conn := newPrecommitSQLFixture(t)
	seedPrecommitSQLRows(t, ctx, conn)
	rows, err := conn.Query(ctx, PRECOMMIT_BATCH_CANDIDATES_SQL, policy.MaxPreCommitRandomnessLookback, 2)
	require.NoError(t, err)
	candidates, err := pgx.CollectRows(rows, pgx.RowToStructByName[BatchRow])
	require.NoError(t, err)
	require.Len(t, candidates, 5)
	// Failed sector 1 must not consume a position before ROW_NUMBER groups 2+3.
	require.EqualValues(t, 2, candidates[0].SectorNumber)
	require.EqualValues(t, 3, candidates[1].SectorNumber)
	require.EqualValues(t, 0, candidates[0].BatchIndex)
	require.EqualValues(t, 0, candidates[1].BatchIndex)
	require.EqualValues(t, 4, candidates[2].SectorNumber)
	require.EqualValues(t, 1, candidates[2].BatchIndex)

	// Separate statements permit a real stale-discovery change. No concurrency
	// claim is inferred from this conditional-assignment row-effect test.
	_, err = conn.Exec(ctx, `UPDATE sectors_sdr_pipeline SET failed=TRUE WHERE sp_id=1000 AND sector_number=2`)
	require.NoError(t, err)
	tag, err := conn.Exec(ctx, PRECOMMIT_BATCH_ASSIGN_SQL, int64(77), int64(1000), 8, []int64{1, 2, 3, 5})
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	var assigned []int64
	rows, err = conn.Query(ctx, `SELECT sector_number FROM sectors_sdr_pipeline WHERE task_id_precommit_msg=77`)
	require.NoError(t, err)
	assigned, err = pgx.CollectRows(rows, pgx.RowTo[int64])
	require.NoError(t, err)
	require.Equal(t, []int64{3}, assigned)
	before := precommitSQLSnapshot(t, ctx, conn)
	tag, err = conn.Exec(ctx, PRECOMMIT_BATCH_ASSIGN_SQL, int64(78), int64(1000), 8, []int64{1, 2, 3})
	require.NoError(t, err)
	require.Zero(t, tag.RowsAffected())
	require.Equal(t, before, precommitSQLSnapshot(t, ctx, conn))
}

func TestPrecommitSQLDetachAndCIDMembership(t *testing.T) {
	ctx, conn := newPrecommitSQLFixture(t)
	seedPrecommitSQLRows(t, ctx, conn)
	_, err := conn.Exec(ctx, `UPDATE sectors_sdr_pipeline SET task_id_precommit_msg=77;
		UPDATE sectors_sdr_pipeline SET task_id_precommit_msg=88 WHERE sp_id=1000 AND sector_number=4`)
	require.NoError(t, err)
	before := precommitSQLSnapshot(t, ctx, conn)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	require.NoError(t, err)
	defer rollbackPrecommitSQL(t, tx)
	tag, err := tx.Exec(ctx, SUBMIT_PRECOMMIT_DETACH_FAILED_SECTOR_SQL, int64(77), int64(1000), int64(1))
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	tag, err = tx.Exec(ctx, SUBMIT_PRECOMMIT_SET_MESSAGE_CID_SQL, "test-message", int64(77), int64(1000), []int64{1, 2, 4})
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	require.NoError(t, tx.Rollback(ctx))
	require.Equal(t, before, precommitSQLSnapshot(t, ctx, conn), "rollback must preserve assignment, failure evidence, and CID state")

	for _, identity := range [][3]int64{{88, 1000, 1}, {77, 2000, 1}, {77, 1000, 2}} {
		tag, err = conn.Exec(ctx, SUBMIT_PRECOMMIT_DETACH_FAILED_SECTOR_SQL, identity[0], identity[1], identity[2])
		require.NoError(t, err)
		require.Zero(t, tag.RowsAffected())
	}
	tag, err = conn.Exec(ctx, SUBMIT_PRECOMMIT_DETACH_FAILED_SECTOR_SQL, int64(77), int64(1000), int64(1))
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	tag, err = conn.Exec(ctx, SUBMIT_PRECOMMIT_SET_MESSAGE_CID_SQL, "test-message", int64(77), int64(1000), []int64{1, 2, 4})
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	var failed bool
	var reason, message string
	require.NoError(t, conn.QueryRow(ctx, `SELECT failed,failed_reason,failed_reason_msg
		FROM sectors_sdr_pipeline WHERE sp_id=1000 AND sector_number=1`).Scan(&failed, &reason, &message))
	require.True(t, failed)
	require.Equal(t, "original", reason)
	require.Equal(t, "keep evidence", message)
	var count int
	require.NoError(t, conn.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline
		WHERE precommit_msg_cid IS NOT NULL`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, conn.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline
		WHERE task_id_precommit_msg=77 AND ((sp_id=2000 AND sector_number=2) OR (sp_id=1000 AND sector_number IN (3,5)))`).Scan(&count))
	require.Equal(t, 3, count, "unselected sectors and another provider retain their task")
}

// Projection of 20231217-sdr-pipeline plus 20240402 DDO, 20240507 task-FK
// removal, 20240522 TIMESTAMPTZ, 20240617 synth, 20240802 user duration, and
// 20241210 batching migrations. The initial-piece composite FK/cascade and
// both primary keys are retained. No full historical migration is replayed.
const precommitSQLSchema = `
CREATE TABLE sectors_sdr_pipeline (
 sp_id BIGINT NOT NULL, sector_number BIGINT NOT NULL, reg_seal_proof INT NOT NULL,
 ticket_epoch BIGINT, user_sector_duration_epochs BIGINT,
 tree_r_cid TEXT, tree_d_cid TEXT, after_synth BOOLEAN NOT NULL DEFAULT FALSE,
 precommit_ready_at TIMESTAMPTZ, task_id_precommit_msg BIGINT,
 precommit_msg_cid TEXT, after_precommit_msg BOOLEAN NOT NULL DEFAULT FALSE,
 failed BOOLEAN NOT NULL DEFAULT FALSE, failed_at TIMESTAMPTZ,
 failed_reason VARCHAR(20) NOT NULL DEFAULT '', failed_reason_msg TEXT NOT NULL DEFAULT '',
 PRIMARY KEY (sp_id,sector_number)
);
CREATE TABLE sectors_sdr_initial_pieces (
 sp_id BIGINT NOT NULL, sector_number BIGINT NOT NULL, piece_index BIGINT NOT NULL,
 piece_cid TEXT NOT NULL, piece_size BIGINT NOT NULL,
 f05_deal_start_epoch BIGINT, f05_deal_end_epoch BIGINT,
 direct_start_epoch BIGINT, direct_end_epoch BIGINT,
 PRIMARY KEY (sp_id,sector_number,piece_index),
 FOREIGN KEY (sp_id,sector_number) REFERENCES sectors_sdr_pipeline (sp_id,sector_number) ON DELETE CASCADE
);`
