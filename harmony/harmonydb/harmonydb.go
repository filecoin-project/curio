package harmonydb

import (
	"context"
	"embed"
	"fmt"
	"math/rand"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/yugabyte/pgx/v5"
	"github.com/yugabyte/pgx/v5/pgconn"
	"github.com/yugabyte/pgx/v5/pgxpool"
	"golang.org/x/xerrors"
)

type ITestID string

// ITestNewID see ITestWithID doc
func ITestNewID() ITestID {
	return ITestID(strconv.Itoa(rand.Intn(99999)))
}

type DB struct {
	pgx       *pgxpool.Pool
	cfg       *pgxpool.Config
	schema    string
	hostnames []string
	BTFPOnce  sync.Once
	BTFP      atomic.Uintptr // BeginTransactionFramePointer
}

var logger = logging.Logger("harmonydb")

type Config struct {
	// HOSTS is a list of hostnames to nodes running YugabyteDB
	// in a cluster. Only 1 is required
	Hosts []string

	// The Yugabyte server's username with full credentials to operate on Lotus' Database. Blank for default.
	Username string

	// The password for the related username. Blank for default.
	Password string

	// The database (logical partition) within Yugabyte. Blank for default.
	Database string

	// The port to find Yugabyte. Blank for default.
	Port string

	// Load Balance the connection over multiple nodes
	LoadBalance bool
}

// NewFromConfig is a convenience function.
// In usage:
//
//	db, err := NewFromConfig(config.HarmonyDB)  // in binary init
func NewFromConfig(cfg Config) (*DB, error) {
	return New(
		cfg.Hosts,
		cfg.Username,
		cfg.Password,
		cfg.Database,
		cfg.Port,
		cfg.LoadBalance,
		"",
	)
}

func envElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

func NewFromConfigWithITestID(t *testing.T, id ITestID) (*DB, error) {
	fmt.Printf("CURIO_HARMONYDB_HOSTS: %s\n", os.Getenv("CURIO_HARMONYDB_HOSTS"))
	db, err := New(
		[]string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")},
		"yugabyte",
		"yugabyte",
		"yugabyte",
		"5433",
		false,
		id,
	)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		db.ITestDeleteAll()
	})
	return db, nil
}

// New is to be called once per binary to establish the pool.
// log() is for errors. It returns an upgraded database's connection.
// This entry point serves both production and integration tests, so it's more DI.
func New(hosts []string, username, password, database, port string, loadBalance bool, itestID ITestID) (*DB, error) {
	itest := string(itestID)

	if len(hosts) == 0 {
		return nil, xerrors.Errorf("no hosts provided")
	}

	// Debug: Log which path we're taking
	logger.Infof("Yugabyte connection config: loadBalance=%v, hosts=%v, port=%s", loadBalance, hosts, port)

	// When load balancing is disabled, use only the first host to prevent
	// Yugabyte client from discovering internal Docker IPs via topology discovery
	var connectionHost string
	if loadBalance {
		// Join all hosts with the port for load balancing
		hostPortPairs := make([]string, len(hosts))
		for i, host := range hosts {
			hostPortPairs[i] = fmt.Sprintf("%s:%s", host, port)
		}
		connectionHost = strings.Join(hostPortPairs, ",")
	} else {
		// Use only the first host when load balancing is disabled
		// This prevents topology discovery that would return internal Docker IPs
		connectionHost = fmt.Sprintf("%s:%s", hosts[0], port)
	}

	// Construct the connection string
	connString := fmt.Sprintf(
		"postgresql://%s:%s@%s/%s?sslmode=prefer",
		username,
		password,
		connectionHost,
		database,
	)

	if loadBalance {
		connString += "&load_balance=true"
	} else {
		// When load balancing is disabled, explicitly disable it
		// fallback_to_topology_keys_only=true ensures client only uses specified nodes
		// Note: Don't set topology_keys= (empty) as Yugabyte rejects empty topology_keys format
		connString += "&load_balance=false&fallback_to_topology_keys_only=true"
	}

	schema := "curio"
	if itest != "" {
		schema = "itest_" + itest
	}

	if err := ensureSchemaExists(connString, schema); err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(connString + "&search_path=" + schema)
	if err != nil {
		return nil, err
	}

	// When load balancing is disabled, restrict the pool to only use the specified host
	// This prevents Yugabyte client from discovering and connecting to internal Docker IPs
	if !loadBalance {
		// Parse port as integer
		portInt, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return nil, xerrors.Errorf("invalid port: %w", err)
		}

		// Override the connection config to use only our specified host
		cfg.ConnConfig.Host = hosts[0]
		cfg.ConnConfig.Port = uint16(portInt)

		// Note: Yugabyte-specific connection parameters (load_balance, fallback_to_topology_keys_only)
		// must be set in the connection string, not as runtime parameters.
		// The connection string already has these parameters set above.
	}

	cfg.ConnConfig.OnNotice = func(conn *pgconn.PgConn, n *pgconn.Notice) {
		logger.Debug("database notice: " + n.Message + ": " + n.Detail)
		DBMeasures.Errors.M(1)
	}

	db := DB{cfg: cfg, schema: schema, hostnames: hosts} // pgx populated in AddStatsAndConnect
	if err := db.addStatsAndConnect(); err != nil {
		return nil, err
	}

	return &db, db.upgrade()
}

type tracer struct {
}

type ctxkey string

const SQL_START = ctxkey("sqlStart")
const SQL_STRING = ctxkey("sqlString")

func (t tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(context.WithValue(ctx, SQL_START, time.Now()), SQL_STRING, data.SQL)
}

var slowQueryThreshold = 5000 * time.Millisecond

func (t tracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	DBMeasures.Hits.M(1)
	ms := time.Since(ctx.Value(SQL_START).(time.Time)).Milliseconds()
	DBMeasures.TotalWait.M(ms)
	DBMeasures.Waits.Observe(float64(ms))
	if data.Err != nil {
		DBMeasures.Errors.M(1)
	}
	if ms > slowQueryThreshold.Milliseconds() {
		logger.Warnw("Slow SQL run",
			"query", ctx.Value(SQL_STRING).(string),
			"err", data.Err,
			"rowCt", data.CommandTag.RowsAffected(),
			"milliseconds", ms)
		return
	}
	logger.Debugw("SQL run",
		"query", ctx.Value(SQL_STRING).(string),
		"err", data.Err,
		"rowCt", data.CommandTag.RowsAffected(),
		"milliseconds", ms)
}

func (db *DB) GetRoutableIP() (string, error) {
	tx, err := db.pgx.Begin(context.Background())
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	local := tx.Conn().PgConn().Conn().LocalAddr()
	addr, ok := local.(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("could not get local addr from %v", addr)
	}
	return addr.IP.String(), nil
}

// addStatsAndConnect connects a prometheus logger. Be sure to run this before using the DB.
func (db *DB) addStatsAndConnect() error {

	db.cfg.ConnConfig.Tracer = tracer{}

	hostnameToIndex := map[string]float64{}
	for i, h := range db.hostnames {
		hostnameToIndex[h] = float64(i)
	}
	db.cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		s := db.pgx.Stat()
		DBMeasures.OpenConnections.M(int64(s.TotalConns()))
		DBMeasures.WhichHost.Observe(hostnameToIndex[c.Config().Host])

		//FUTURE place for any connection seasoning
		return nil
	}

	// Timeout the first connection so we know if the DB is down.
	ctx, ctxClose := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	defer ctxClose()
	var err error
	db.pgx, err = pgxpool.NewWithConfig(ctx, db.cfg)
	if err != nil {
		logger.Error(fmt.Sprintf("Unable to connect to database: %v\n", err))
		return err
	}
	return nil
}

// ITestDeleteAll will delete everything created for "this" integration test.
// This must be called at the end of each integration test.
func (db *DB) ITestDeleteAll() {
	if !strings.HasPrefix(db.schema, "itest_") {
		fmt.Println("Warning: this should never be called on anything but an itest schema.")
		return
	}
	defer db.pgx.Close()
	_, err := db.pgx.Exec(context.Background(), "DROP SCHEMA "+db.schema+" CASCADE")
	if err != nil {
		fmt.Println("warning: unclean itest shutdown: cannot delete schema: " + err.Error())
		return
	}
}

var schemaREString = "^[A-Za-z0-9_]+$"
var schemaRE = regexp.MustCompile(schemaREString)

func ensureSchemaExists(connString, schema string) error {
	// FUTURE allow using fallback DBs for start-up.
	ctx, cncl := context.WithDeadline(context.Background(), time.Now().Add(3*time.Second))
	p, err := pgx.Connect(ctx, connString)
	defer cncl()
	if err != nil {
		return xerrors.Errorf("unable to connect to db: %s, err: %v", connString, err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	if len(schema) < 5 || !schemaRE.MatchString(schema) {
		return xerrors.New("schema must be of the form " + schemaREString + "\n Got: " + schema)
	}

	_, err = backoffForSerializationError(func() (pgconn.CommandTag, error) {
		return p.Exec(context.Background(), "CREATE SCHEMA IF NOT EXISTS "+schema)
	})

	return err
}

//go:embed sql
var upgadeFS embed.FS

//go:embed downgrade
var downgradeFS embed.FS

var ITestUpgradeFunc func(*pgxpool.Pool, string, string)

// DowngradeTo downgrades the database schema to a previous date (when an upgrade was applied).
// Note: these dates (YYYYMMDD) are not the SQL date but the date the user did an upgrade.
func (db *DB) DowngradeTo(ctx context.Context, dateNum int) error {
	// Is the date good?
	if dateNum < 2000_01_01 || dateNum > 2099_12_31 {
		return xerrors.Errorf("invalid date: %d", dateNum)
	}
	// Ensure all SQL files after that date have a corresponding downgrade file
	var toDowngrade []string
	err := db.Select(ctx, &toDowngrade, "SELECT entry FROM base WHERE applied >= TO_DATE($1, 'YYYYMMDD') ORDER by entry DESC", strconv.Itoa(dateNum))
	if err != nil {
		return xerrors.Errorf("cannot select to downgrade: %w", err)
	}
	// Ensure all SQL files after that date have a corresponding downgrade file
	m := map[string]string{}
	downgrades, err := downgradeFS.ReadDir("downgrade")
	if err != nil {
		return xerrors.Errorf("cannot read downgrade directory: %w", err)
	}
	for _, downgrade := range downgrades {
		m[downgrade.Name()[:8]] = "downgrade/" + downgrade.Name()
	}

	allGood := true
	for _, file := range toDowngrade {
		file = strings.TrimSpace(file)
		downgradeFile, ok := m[file[:8]]
		if !ok {
			allGood = false
			logger.Errorf("cannot find downgrade file for %s", file)
			f, err := findFileStartingWith(upgadeFS, file[:8])
			if err != nil {
				logger.Errorf("cannot find file starting with %s that relates to downgrade-needed value: %w", file[:8], err)
				continue
			}
			logger.Errorf("Original file needing downgrade: %s", file[:8], f)
			continue
		}
		if _, err := downgradeFS.ReadFile(downgradeFile); err != nil {
			allGood = false
			logger.Errorf("cannot find/read downgrade file for %s. Err: %w", file, err)
		}
	}
	if !allGood {
		return xerrors.New("cannot downgrade to date: some downgrade files are missing")
	}
	for _, file := range toDowngrade {
		if err := applySqlFile(db, downgradeFS, m[file[:8]]); err != nil {
			return xerrors.Errorf("cannot apply downgrade file for %s. Err: %w", file, err)
		}
		_, err := db.Exec(context.Background(), "DELETE FROM base WHERE entry = $1", file[:8])
		if err != nil {
			return xerrors.Errorf("cannot delete from base for downgrade: %w", err)
		}
	}
	return nil
}
func (db *DB) upgrade() error {
	// Does the version table exist? if not, make it.
	// NOTE: This cannot change except via the next sql file.
	_, err := db.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS base (
		id SERIAL PRIMARY KEY,
		entry CHAR(12),
		applied TIMESTAMP DEFAULT current_timestamp
	)`)
	if err != nil {
		logger.Error("Upgrade failed.")
		return xerrors.Errorf("Cannot create base table %w", err)
	}

	// __Run scripts in order.__

	landed := map[string]bool{}
	{
		var landedEntries []struct{ Entry string }
		err = db.Select(context.Background(), &landedEntries, "SELECT entry FROM base")
		if err != nil {
			logger.Error("Cannot read entries: " + err.Error())
			return xerrors.Errorf("cannot read entries: %w", err)
		}
		for _, l := range landedEntries {
			landed[l.Entry[:8]] = true
		}
	}
	dir, err := upgadeFS.ReadDir("sql")
	if err != nil {
		logger.Error("Cannot read fs entries: " + err.Error())
		return err
	}
	sort.Slice(dir, func(i, j int) bool { return dir[i].Name() < dir[j].Name() })

	if len(dir) == 0 {
		logger.Error("No sql files found.")
	}
	for _, e := range dir {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			logger.Debug("Must have only SQL files here, found: " + name)
			continue
		}
		if landed[name[:8]] {
			logger.Debug("DB Schema " + name + " already applied.")
			continue
		}
		file, err := upgadeFS.ReadFile("sql/" + name)
		if err != nil {
			logger.Error("weird embed file read err")
			return err
		}

		logger.Infow("Upgrading", "file", name, "size", len(file))
		if err := applySqlFile(db, upgadeFS, "sql/"+name); err != nil {
			logger.Error("Cannot apply sql file: " + err.Error())
			return err
		}

		// Mark Completed.
		_, err = db.Exec(context.Background(), "INSERT INTO base (entry) VALUES ($1)", name[:8])
		if err != nil {
			logger.Error("Cannot update base: " + err.Error())
			return xerrors.Errorf("cannot insert into base: %w", err)
		}
	}
	return nil
}

func parseSQLStatements(sqlContent string) []string {
	var statements []string
	var currentStatement strings.Builder

	lines := strings.Split(sqlContent, "\n")
	var inFunction bool

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "--") || strings.HasPrefix(trimmedLine, "#") {
			// Skip empty lines and comments.
			continue
		}

		// Detect function blocks starting or ending.
		if strings.Contains(trimmedLine, "$$") {
			inFunction = !inFunction
		}

		// Add the line to the current statement.
		currentStatement.WriteString(line + "\n")

		// If we're not in a function and the line ends with a semicolon, or we just closed a function block.
		if (!inFunction && strings.HasSuffix(trimmedLine, ";")) || (strings.Contains(trimmedLine, "$$") && !inFunction) {
			statements = append(statements, currentStatement.String())
			currentStatement.Reset()
		}
	}

	// Add any remaining statement not followed by a semicolon (should not happen in well-formed SQL but just in case).
	if currentStatement.Len() > 0 {
		statements = append(statements, currentStatement.String())
	}

	return statements
}

func applySqlFile(db *DB, fs embed.FS, path string) error {
	file, err := fs.ReadFile(path)
	if err != nil {
		return err
	}

	var megaSQL strings.Builder
	for _, statement := range parseSQLStatements(string(file)) {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		if !strings.HasSuffix(trimmed, ";") {
			megaSQL.WriteString(statement)
			megaSQL.WriteString(";")
		} else {
			megaSQL.WriteString(statement)
		}
	}

	if megaSQL.Len() == 0 {
		return nil
	}

	_, err = db.Exec(context.Background(), rawStringOnly(megaSQL.String()))
	if err != nil {
		return xerrors.Errorf("cannot apply sql file: %w", err)
	}

	if ITestUpgradeFunc != nil {
		ITestUpgradeFunc(db.pgx, path[:8], megaSQL.String())
	}

	return err
}

func findFileStartingWith(fs embed.FS, prefix string) (string, error) {
	entries, err := fs.ReadDir("sql")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			return entry.Name(), nil
		}
	}
	return "", xerrors.New("file not found")
}
