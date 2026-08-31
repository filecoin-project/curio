package dbmaint

import (
	"context"
	"strings"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/mod/semver"
	"golang.org/x/xerrors"

	curiobuild "github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

var log = logging.Logger("dbanalyze")

const (
	analyzeInterval        = 24 * time.Hour
	analyzeGrowthThreshold = 0.10
	minRowDelta            = 100
	perTableAnalyzeTimeout = 10 * time.Minute
	upgradeQuietWindow     = time.Hour
)

type DBAnalyzeTask struct {
	db *harmonydb.DB
}

func NewDBAnalyzeTask(db *harmonydb.DB) *DBAnalyzeTask {
	return &DBAnalyzeTask{db: db}
}

func (d *DBAnalyzeTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	skip, err := d.skipForRollingUpgrade(ctx)
	if err != nil {
		return false, xerrors.Errorf("checking rolling upgrade: %w", err)
	}
	if skip {
		return true, nil
	}

	var schema string
	if err := d.db.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return false, xerrors.Errorf("getting current schema: %w", err)
	}

	// pg_stat_user_tables counters are empty on Yugabyte; pg_class is the catalog list.
	var rows []struct {
		TableName     string `db:"table_name"`
		RowsAtAnalyze int64  `db:"rows_at_analyze"`
		Seen          bool   `db:"seen"`
	}
	err = d.db.Select(ctx, &rows, `
		SELECT c.relname AS table_name,
		       COALESCE(a.rows_at_analyze, 0) AS rows_at_analyze,
		       a.table_name IS NOT NULL AS seen
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN table_analyze_state a ON a.table_name = c.relname
		WHERE n.nspname = $1
		  AND c.relkind = 'r'
		ORDER BY c.relname`, schema)
	if err != nil {
		return false, xerrors.Errorf("listing tables: %w", err)
	}
	if len(rows) == 0 {
		return false, xerrors.Errorf("no tables found in schema %s", schema)
	}

	analyzed := 0
	var firstErr error
	for _, r := range rows {
		if !stillOwned() {
			return false, nil
		}

		tctx, cancel := context.WithTimeout(ctx, perTableAnalyzeTimeout)
		n, err := harmonydb.AdminTableCount(tctx, d.db, schema, r.TableName)
		cancel()
		if err != nil {
			log.Warnw("COUNT(*) failed", "table", r.TableName, "error", err)
			if firstErr == nil {
				firstErr = xerrors.Errorf("COUNT(*) %s: %w", r.TableName, err)
			}
			continue
		}

		if !shouldAnalyze(n, r.Seen, r.RowsAtAnalyze) {
			continue
		}

		tctx, cancel = context.WithTimeout(ctx, perTableAnalyzeTimeout)
		err = harmonydb.AdminAnalyze(tctx, d.db, schema, r.TableName)
		cancel()
		if err != nil {
			log.Warnw("ANALYZE failed", "table", r.TableName, "error", err)
			if firstErr == nil {
				firstErr = xerrors.Errorf("ANALYZE %s: %w", r.TableName, err)
			}
			continue
		}

		_, err = d.db.Exec(ctx, `
			INSERT INTO table_analyze_state (table_name, churn_at_analyze, rows_at_analyze, last_analyzed_at, analyze_count)
			VALUES ($1, $2, $2, NOW(), 1)
			ON CONFLICT (table_name) DO UPDATE SET
			    churn_at_analyze = EXCLUDED.churn_at_analyze,
			    rows_at_analyze  = EXCLUDED.rows_at_analyze,
			    last_analyzed_at = NOW(),
			    analyze_count    = table_analyze_state.analyze_count + 1`,
			r.TableName, n)
		if err != nil {
			log.Warnw("failed to record analyze state", "table", r.TableName, "error", err)
			if firstErr == nil {
				firstErr = xerrors.Errorf("record %s: %w", r.TableName, err)
			}
			continue
		}

		analyzed++
		log.Infow("analyzed table", "table", r.TableName, "rows", n)
	}

	if _, err := d.db.Exec(ctx, `
		DELETE FROM table_analyze_state a
		WHERE NOT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1
			  AND c.relkind = 'r'
			  AND c.relname = a.table_name
		)`, schema); err != nil {
		log.Warnw("failed to prune dropped table analyze state", "error", err)
	}

	if analyzed == 0 && firstErr != nil {
		return false, xerrors.Errorf("analyzed 0 of %d tables: %w", len(rows), firstErr)
	}

	log.Infow("DB analyze pass complete", "task_id", taskID, "tables", len(rows), "analyzed", analyzed)
	return true, nil
}

func shouldAnalyze(rows int64, seen bool, prev int64) bool {
	if !seen {
		return true
	}
	if rows < prev {
		return true
	}
	delta := rows - prev
	if delta < minRowDelta {
		return false
	}
	return float64(rows) >= float64(prev)*(1+analyzeGrowthThreshold)
}

func (d *DBAnalyzeTask) skipForRollingUpgrade(ctx context.Context) (bool, error) {
	var peers []struct {
		Version     string    `db:"version"`
		StartupTime time.Time `db:"startup_time"`
		MachineID   int64     `db:"machine_id"`
	}
	err := d.db.Select(ctx, &peers, `
		SELECT machine_id, version, startup_time
		FROM harmony_machine_details
		WHERE version IS NOT NULL
		  AND startup_time > NOW() - $1::interval`, upgradeQuietWindow.String())
	if err != nil {
		return false, err
	}

	myLabel := curiobuild.ClusterMachineVersionLabel()
	mine := analyzeSemver(myLabel)
	for _, p := range peers {
		if semver.Compare(analyzeSemver(p.Version), mine) > 0 {
			log.Infow("skipping DB analyze pass, newer machine booted recently",
				"machine_id", p.MachineID,
				"their_version", p.Version,
				"my_version", myLabel,
				"startup_time", p.StartupTime)
			return true, nil
		}
	}
	return false, nil
}

// analyzeSemver turns a harmony_machine_details.version label such as
// "1.28.3 abcdef1" or "1.28.3-rc1" into a semver.Compare-able string.
func analyzeSemver(label string) string {
	v, _, _ := strings.Cut(label, " ") // drop the git hash suffix
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	return v
}

func (d *DBAnalyzeTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (d *DBAnalyzeTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: tasknames.DBAnalyze,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(analyzeInterval, d),
	}
}

func (d *DBAnalyzeTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&DBAnalyzeTask{})
var _ harmonytask.TaskInterface = &DBAnalyzeTask{}
