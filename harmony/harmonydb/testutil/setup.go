package testutil

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const maxRetries = 5

const (
	templateSchemaID harmonydb.ITestID = "template"
	testDBName       string            = "curio_itest"
)

var (
	templateOnce  sync.Once
	templateErr   error
	baseConnCfg   connConfig
	createDBMutex sync.Mutex
)

type connConfig struct {
	host     string
	port     string
	username string
	password string
	baseDB   string
}

// SetupTestDB prepares a reusable template schema once, then rapidly clones it
// for every test invocation using CREATE TABLE ... (LIKE ... INCLUDING ALL).
// YugabyteDB doesn't support custom database templates, so we use schema-based
// isolation within a single shared test database.
// It returns an ITestID that can be passed to harmonydb.NewFromConfigWithITestID.
func SetupTestDB(t *testing.T) harmonydb.ITestID {
	t.Helper()

	templateOnce.Do(func() {
		baseConnCfg = loadConnConfig()
		templateErr = prepareTemplateSchema()
	})
	if templateErr != nil {
		t.Fatalf("preparing template schema: %v", templateErr)
	}

	id := harmonydb.ITestNewID()
	if err := cloneTemplateSchema(id); err != nil {
		t.Fatalf("cloning template schema: %v", err)
	}

	harmonydb.RegisterITestDatabase(id, testDBName)
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

// prepareTemplateSchema creates the shared test database (if needed) and
// applies all migrations to a template schema that will be cloned for each test.
// Retries on YugabyteDB serialization errors.
func prepareTemplateSchema() error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1))*200*time.Millisecond + time.Duration(rand.Intn(100))*time.Millisecond
			time.Sleep(backoff)
		}

		lastErr = doPrepareTemplateSchema()
		if lastErr == nil {
			return nil
		}

		if !isSerializationError(lastErr) {
			return lastErr
		}
	}
	return fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

func doPrepareTemplateSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create the shared test database if it doesn't exist
	adminConn, err := pgx.Connect(ctx, baseConnCfg.connString(baseConnCfg.baseDB))
	if err != nil {
		return fmt.Errorf("connecting to admin database: %w", err)
	}

	err = func() error {
		// Check if database exists
		createDBMutex.Lock()
		defer createDBMutex.Unlock()
		var exists bool
		err = adminConn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", testDBName).Scan(&exists)
		if err != nil {
			_ = adminConn.Close(ctx)
			return fmt.Errorf("checking if test database exists: %w", err)
		}

		if !exists {
			_, err := adminConn.Exec(ctx, "CREATE DATABASE "+quoteIdentifier(testDBName))
			// Ignore "already exists" errors (race condition with parallel tests or previous runs)
			if err != nil && !strings.Contains(err.Error(), "already exists") {
				_ = adminConn.Close(ctx)
				return fmt.Errorf("creating test database: %w", err)
			}
		}
		_ = adminConn.Close(ctx)

		// Connect to the test database and drop old template schema if it exists
		testConn, err := pgx.Connect(ctx, baseConnCfg.connString(testDBName))
		if err != nil {
			return fmt.Errorf("connecting to test database: %w", err)
		}
		templateSchema := fmt.Sprintf("itest_%s", templateSchemaID)
		_, _ = testConn.Exec(ctx, "DROP SCHEMA IF EXISTS "+quoteIdentifier(templateSchema)+" CASCADE")
		_ = testConn.Close(ctx)

		// Use harmonydb.New to create the template schema and apply all migrations
		db, err := harmonydb.New([]string{baseConnCfg.host}, baseConnCfg.username, baseConnCfg.password, testDBName, baseConnCfg.port, false, templateSchemaID)
		if err != nil {
			return fmt.Errorf("initializing template schema: %w", err)
		}
		db.Close()
		return nil
	}()
	if err != nil {
		return err
	}

	return nil
}

// cloneTemplateSchema creates a new schema for the test by copying all table
// structures and data from the template schema. This includes seed data that
// was inserted during migrations (e.g., harmony_config entries).
// Retries on YugabyteDB serialization errors (40001) which can occur with concurrent cloning.
func cloneTemplateSchema(id harmonydb.ITestID) error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			backoff := time.Duration(1<<uint(attempt-1))*100*time.Millisecond + time.Duration(rand.Intn(100))*time.Millisecond
			time.Sleep(backoff)
		}

		lastErr = doCloneTemplateSchema(id)
		if lastErr == nil {
			return nil
		}

		// Retry on serialization errors (SQLSTATE 40001)
		if !isSerializationError(lastErr) {
			return lastErr
		}
	}
	return fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

func doCloneTemplateSchema(id harmonydb.ITestID) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	conn, err := pgx.Connect(ctx, baseConnCfg.connString(testDBName))
	if err != nil {
		return fmt.Errorf("connecting to test database: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	templateSchema := fmt.Sprintf("itest_%s", templateSchemaID)
	newSchema := fmt.Sprintf("itest_%s", id)

	// Drop schema if it exists from a previous failed attempt
	_, _ = conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+quoteIdentifier(newSchema)+" CASCADE")

	// Create the new schema
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoteIdentifier(newSchema)); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	// Get all tables from template schema
	rows, err := conn.Query(ctx, `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
	`, templateSchema)
	if err != nil {
		return fmt.Errorf("querying template tables: %w", err)
	}

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			rows.Close()
			return fmt.Errorf("scanning table name: %w", err)
		}
		tables = append(tables, tableName)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating template tables: %w", err)
	}

	// Clone each table structure
	for _, table := range tables {
		createSQL := fmt.Sprintf(
			"CREATE TABLE %s.%s (LIKE %s.%s INCLUDING ALL)",
			quoteIdentifier(newSchema), quoteIdentifier(table),
			quoteIdentifier(templateSchema), quoteIdentifier(table),
		)
		if _, err := conn.Exec(ctx, createSQL); err != nil {
			return fmt.Errorf("cloning table %s: %w", table, err)
		}
	}

	// Copy data from all tables (includes migration tracking in 'base' and seed data from migrations)
	for _, table := range tables {
		_, err = conn.Exec(ctx, fmt.Sprintf(
			"INSERT INTO %s.%s SELECT * FROM %s.%s",
			quoteIdentifier(newSchema), quoteIdentifier(table),
			quoteIdentifier(templateSchema), quoteIdentifier(table),
		))
		if err != nil {
			return fmt.Errorf("copying data for table %s: %w", table, err)
		}
	}

	return nil
}

// isSerializationError checks if the error is a YugabyteDB serialization failure (40001)
func isSerializationError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "40001") ||
		strings.Contains(errStr, "serialization") ||
		strings.Contains(errStr, "Restart read required")
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
