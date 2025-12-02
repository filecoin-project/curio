package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	templateSchemaID harmonydb.ITestID = "template"
	templateDBName   string            = "curio_itest_template"
	testDBPrefix     string            = "curio_itest"
)

var (
	templateOnce sync.Once
	templateErr  error
	baseConnCfg  connConfig
)

type connConfig struct {
	host     string
	port     string
	username string
	password string
	baseDB   string
}

// SetupTestDB prepares a reusable template database once, then rapidly clones it
// for every test invocation using PostgreSQL's template mechanism. It returns
// an ITestID that can be passed to harmonydb.NewFromConfigWithITestID.
func SetupTestDB(t *testing.T) harmonydb.ITestID {
	t.Helper()

	templateOnce.Do(func() {
		baseConnCfg = loadConnConfig()
		templateErr = prepareTemplateDatabase()
	})
	if templateErr != nil {
		t.Fatalf("preparing template database: %v", templateErr)
	}

	id := harmonydb.ITestNewID()
	dbName := fmt.Sprintf("%s_%s", testDBPrefix, string(id))
	if err := cloneTemplateDatabase(id, dbName); err != nil {
		t.Fatalf("cloning template database: %v", err)
	}

	harmonydb.RegisterITestDatabase(id, dbName)
	return id
}

func loadConnConfig() connConfig {
	return connConfig{
		host:     firstNonEmpty(splitFirst(os.Getenv("CURIO_HARMONYDB_HOSTS")), os.Getenv("CURIO_DB_HOST"), "127.0.0.1"),
		port:     firstNonEmpty(os.Getenv("CURIO_HARMONYDB_PORT"), os.Getenv("CURIO_DB_PORT"), "5433"),
		username: firstNonEmpty(os.Getenv("CURIO_HARMONYDB_USERNAME"), os.Getenv("CURIO_DB_USER"), "yugabyte"),
		password: firstNonEmpty(os.Getenv("CURIO_HARMONYDB_PASSWORD"), os.Getenv("CURIO_DB_PASSWORD"), "yugabyte"),
		baseDB:   firstNonEmpty(os.Getenv("CURIO_HARMONYDB_NAME"), os.Getenv("CURIO_DB_NAME"), "yugabyte"),
	}
}

func prepareTemplateDatabase() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminConn, err := pgx.Connect(ctx, baseConnCfg.connString(baseConnCfg.baseDB))
	if err != nil {
		return fmt.Errorf("connecting to yugabyte admin database: %w", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	if err := dropDatabaseIfExists(ctx, adminConn, templateDBName); err != nil {
		return fmt.Errorf("dropping existing template database: %w", err)
	}

	if _, err := adminConn.Exec(ctx, "CREATE DATABASE "+quoteIdentifier(templateDBName)+" WITH TEMPLATE template1"); err != nil {
		return fmt.Errorf("creating template database: %w", err)
	}

	db, err := harmonydb.New([]string{baseConnCfg.host}, baseConnCfg.username, baseConnCfg.password, templateDBName, baseConnCfg.port, false, templateSchemaID)
	if err != nil {
		return fmt.Errorf("initializing template schema: %w", err)
	}
	db.Close()

	return nil
}

func cloneTemplateDatabase(id harmonydb.ITestID, targetDB string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	adminConn, err := pgx.Connect(ctx, baseConnCfg.connString(baseConnCfg.baseDB))
	if err != nil {
		return fmt.Errorf("connecting to yugabyte admin database: %w", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	if err := dropDatabaseIfExists(ctx, adminConn, targetDB); err != nil {
		return fmt.Errorf("dropping target database: %w", err)
	}

	if _, err := adminConn.Exec(ctx, "CREATE DATABASE "+quoteIdentifier(targetDB)+" WITH TEMPLATE "+quoteIdentifier(templateDBName)); err != nil {
		return fmt.Errorf("creating cloned database: %w", err)
	}

	cloneConn, err := pgx.Connect(ctx, baseConnCfg.connString(targetDB))
	if err != nil {
		return fmt.Errorf("connecting to cloned database: %w", err)
	}
	defer func() { _ = cloneConn.Close(ctx) }()

	oldSchema := fmt.Sprintf("itest_%s", templateSchemaID)
	newSchema := fmt.Sprintf("itest_%s", id)
	if _, err := cloneConn.Exec(ctx, "ALTER SCHEMA "+quoteIdentifier(oldSchema)+" RENAME TO "+quoteIdentifier(newSchema)); err != nil {
		return fmt.Errorf("renaming cloned schema: %w", err)
	}

	return nil
}

func dropDatabaseIfExists(ctx context.Context, conn *pgx.Conn, name string) error {
	_, _ = conn.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
	_, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(name))
	return err
}

func (c connConfig) connString(database string) string {
	u := url.URL{
		Scheme:   "postgresql",
		Host:     fmt.Sprintf("%s:%s", c.host, c.port),
		Path:     "/" + database,
		RawQuery: "sslmode=disable",
	}
	if c.password == "" {
		u.User = url.User(c.username)
	} else {
		u.User = url.UserPassword(c.username, c.password)
	}
	return u.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func splitFirst(hosts string) string {
	if hosts == "" {
		return ""
	}
	for _, part := range strings.Split(hosts, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			return part
		}
	}
	return ""
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
