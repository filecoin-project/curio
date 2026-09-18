package storage_market

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/backpressure"
	"github.com/filecoin-project/curio/market/mk20release"
)

// Read these six SELECTs from production source, rather than keeping a second
// SQL implementation in the measurement recipe. Source files must be present
// beside the compiled binary's recorded source path when the operator runs it.
func mk20ReleaseCostQueries(t *testing.T) []string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	var queries []string
	for _, item := range []struct{ path, function, contains string }{
		{"mk20_release.go", "hasMK20WaitingDeals", "SELECT EXISTS"},
		{"mk20_release.go", "selectMK20WaitingCandidates", "FROM market_mk20_pipeline_waiting"},
		{"mk20_release.go", "countActiveMK20PipelineRows", "COUNT(*)"},
		{"../../market/backpressure/backpressure.go", "checkMK20Backpressure", "WITH pipeline_data"},
		{"../../market/backpressure/backpressure.go", "checkSectorBackpressure", "WITH BufferedSDR"},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(source), item.path), nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != item.function {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if ok && lit.Kind == token.STRING {
					value, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					if strings.Contains(value, item.contains) {
						queries = append(queries, value)
					}
				}
				return true
			})
		}
	}
	require.Len(t, queries, 6, "production query shape changed; review the finite recipe")
	return queries
}

func TestMK20ReleaseCostQuerySelection(t *testing.T) {
	queries := mk20ReleaseCostQueries(t)
	require.Contains(t, queries[0], "SELECT EXISTS")
	require.Contains(t, queries[1], "LIMIT $1")
	require.Contains(t, queries[2], "id > $1")
	require.Contains(t, queries[2], "LIMIT $2")
	require.Contains(t, queries[3], "complete = FALSE")
	require.Contains(t, queries[4], "WITH pipeline_data")
	require.Contains(t, queries[5], "WITH BufferedSDR")
}

// This finite operator-only sample reuses the isolated release fixture. It is
// not a throughput benchmark or a sealing/completion test. Retry counts must be
// obtained from the owned sessions' server statement logs as described in the
// recipe; successful calls alone do not establish zero retries.
func TestMK20ReleaseDBBoundedCostSample(t *testing.T) {
	if os.Getenv("CURIO_MK20_RELEASE_COST_ITEST") != "1" {
		t.Skip("additional CURIO_MK20_RELEASE_COST_ITEST=1 opt-in required for the finite cost sample")
	}
	for _, baseline := range []int{2216, 4096} {
		if !t.Run(fmt.Sprintf("active_%d", baseline), func(t *testing.T) {
			dbs := newMK20ReleaseITestDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			conn := openMK20ReleaseITestConnection(t, ctx, dbs.target)
			defer func() {
				if err := conn.Close(context.Background()); err != nil {
					t.Errorf("closing isolated measurement connection: %v", err)
				}
			}()

			// One real offline deal supplies the JSON shape; SQL clones synthetic
			// identities only. No file, downloader, poller loop, or sealer runs.
			template := seedOfflineMK20WaitingDeal(t, ctx, dbs.primary, 1000)
			_, err := conn.Exec(ctx, `INSERT INTO market_mk20_deal
				(id, client, piece_cid_v2, data, ddo_v1, retrieval_v1, pdp_v1)
				SELECT lpad(g::text, 26, '0'), client, piece_cid_v2, data, ddo_v1, retrieval_v1, pdp_v1
				FROM market_mk20_deal CROSS JOIN generate_series(1, 32107) g WHERE id = $1`, template)
			require.NoError(t, err)
			_, err = conn.Exec(ctx, `INSERT INTO market_mk20_pipeline_waiting
				SELECT id FROM market_mk20_deal WHERE id <> $1`, template)
			require.NoError(t, err)
			_, err = conn.Exec(ctx, `INSERT INTO market_mk20_pipeline
				(id, sp_id, contract, client, piece_cid_v2, piece_cid, piece_size, raw_size,
				 offline, indexing, announce, duration, complete)
				SELECT 'baseline-' || g, 2000, '', 'fixture', 'fixture-v2', 'fixture-v1', 2048, 2032,
				 TRUE, FALSE, FALSE, 1000000, g > $1 FROM generate_series(1, $1::integer + 8192) g`, baseline)
			require.NoError(t, err)
			assertMK20ReleaseCounts(t, ctx, dbs.primary, baseline, 32108)
			for _, table := range []string{"market_mk20_pipeline_waiting", "market_mk20_pipeline", "market_mk20_deal", "parked_pieces", "harmony_task", "sectors_sdr_pipeline", "open_sector_pieces", "sectors_sdr_initial_pieces"} {
				_, err = conn.Exec(ctx, "ANALYZE "+pgx.Identifier{table}.Sanitize())
				require.NoError(t, err)
			}
			rows, err := conn.Query(ctx, `SELECT tablename, indexdef FROM pg_indexes WHERE schemaname = $1 ORDER BY tablename, indexname`, dbs.target.schema)
			require.NoError(t, err)
			indexes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ Table, Definition string }])
			require.NoError(t, err)
			t.Logf("schema=%s active=%d complete=8192 waiting=32108 indexes=%+v", dbs.target.schema, baseline, indexes)
			var version string
			require.NoError(t, conn.QueryRow(ctx, `SELECT version()`).Scan(&version))
			analyze := "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "
			if strings.Contains(strings.ToLower(version), "yugabyte") || strings.Contains(version, "-YB-") {
				analyze = "EXPLAIN (ANALYZE, DIST, FORMAT JSON) "
			}
			args := [][]any{nil, {64}, {fmt.Sprintf("%026d", 16054), 64}, nil, nil, nil}
			for i, query := range mk20ReleaseCostQueries(t) {
				for _, prefix := range []string{"EXPLAIN (FORMAT JSON) ", analyze} {
					started := time.Now()
					tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
					require.NoError(t, err)
					var plan string
					err = tx.QueryRow(ctx, prefix+query, args[i]...).Scan(&plan)
					rollbackErr := tx.Rollback(ctx)
					require.NoError(t, err)
					require.NoError(t, rollbackErr)
					require.LessOrEqual(t, len(plan), 64*1024, "stop rather than emit unbounded plan output")
					t.Logf("phase=plan_%d mode=%s wall=%s binds=%v sql=%s plan=%s", i, prefix, time.Since(started), args[i], query, plan)
				}
			}

			cfg := config.DefaultCurioConfig()
			cfg.Ingest.DoSnap = false
			bp, err := backpressure.NewCachedBackPressure().Val()
			require.NoError(t, err)
			passCtx, stopPass := context.WithTimeout(ctx, mk20ReleasePassTimeout)
			defer stopPass()
			started := time.Now()
			t.Logf("phase=release_begin utc=%s schema=%s", started.UTC().Format(time.RFC3339Nano), dbs.target.schema)
			calls, wakes := 0, 0
			result, err := runMK20ReleasePass(passCtx, "", mk20ReleasePolicy{batch: 4, maxActive: int64(baseline + 5)}, mk20ReleasePassDeps{
				waiting: func(ctx context.Context) (bool, error) {
					begin := time.Now()
					value, err := hasMK20WaitingDeals(ctx, dbs.primary)
					t.Logf("phase=waiting_exists wall=%s exists=%t err=%v", time.Since(begin), value, err)
					return value, err
				},
				pressure: func(ctx context.Context) (bool, error) {
					begin := time.Now()
					value, err := bp.MK20ReleasePressure(ctx, &cfg.Ingest, dbs.primary)
					t.Logf("phase=fresh_pressure wall=%s pressure=%t err=%v", time.Since(begin), value, err)
					return value, err
				},
				active: func(ctx context.Context) (int64, error) {
					begin := time.Now()
					value, err := countActiveMK20PipelineRows(ctx, dbs.primary)
					t.Logf("phase=active_count wall=%s count=%d err=%v", time.Since(begin), value, err)
					return value, err
				},
				candidates: func(ctx context.Context, cursor string, limit int) ([]string, error) {
					begin := time.Now()
					value, err := selectMK20WaitingCandidates(ctx, dbs.primary, cursor, limit)
					t.Logf("phase=candidates wall=%s rows=%d err=%v", time.Since(begin), len(value), err)
					return value, err
				},
				release: func(ctx context.Context, id string, cap int64) (mk20release.Outcome, error) {
					calls++
					if calls > 4 {
						stopPass()
						return "", fmt.Errorf("cost sample exceeded four pass release calls")
					}
					begin := time.Now()
					out, err := releaseMK20WaitingDeal(ctx, dbs.primary, id, cap)
					t.Logf("phase=pass_release call=%d wall=%s outcome=%s err=%v", calls, time.Since(begin), out, err)
					if err != nil {
						stopPass()
					}
					return out, err
				},
				wakeDealPoller: func() { wakes++ },
			})
			stopPass()
			t.Logf("phase=one_pass wall=%s calls=%d result=%+v", time.Since(started), calls, result)
			require.NoError(t, err)
			require.Equal(t, 4, result.released)
			require.Equal(t, 1, wakes)
			assertMK20ReleaseCounts(t, ctx, dbs.primary, baseline+4, 32104)

			// Independent handles share a final slot. Starting together is a
			// cost sample, not proof that a SQL lock wait actually occurred.
			raceCtx, stopRace := context.WithTimeout(ctx, mk20ReleasePassTimeout)
			var wg sync.WaitGroup
			defer func() { stopRace(); wg.Wait() }()
			start := make(chan struct{})
			outcomes := make(chan mk20release.Outcome, 2)
			for i, db := range []*harmonydb.DB{dbs.primary, dbs.secondary} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					select {
					case <-start:
					case <-raceCtx.Done():
						return
					}
					begin := time.Now()
					out, err := releaseMK20WaitingDeal(raceCtx, db, fmt.Sprintf("%026d", i+5), int64(baseline+5))
					t.Logf("phase=independent_release handle=%d wall=%s outcome=%s err=%v", i, time.Since(begin), out, err)
					if err != nil {
						t.Errorf("independent release: %v", err)
					}
					outcomes <- out
				}()
			}
			close(start)
			wg.Wait()
			stopRace()
			close(outcomes)
			var got []mk20release.Outcome
			for out := range outcomes {
				got = append(got, out)
			}
			require.ElementsMatch(t, []mk20release.Outcome{mk20release.Released, mk20release.AtCapacity}, got)
			assertMK20ReleaseCounts(t, ctx, dbs.primary, baseline+5, 32103)
			t.Logf("phase=release_end utc=%s schema=%s", time.Now().UTC().Format(time.RFC3339Nano), dbs.target.schema)
			t.Log("retry counts=REQUIRE_OPERATOR_STATEMENT_LOG_COUNT; no inference of zero retries from success")
		}) {
			return
		} // Do not repeat a failed sample on a larger fixture.
	}
}
