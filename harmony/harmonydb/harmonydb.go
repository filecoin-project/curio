package harmonydb

import (
	"embed"
	"os"
	"testing"
	"time"

	"github.com/curiostorage/harmonyquery"
)

// ITestNewID see ITestWithID doc
var ITestNewID = harmonyquery.ITestNewID

type DB = harmonyquery.DB
type Config = harmonyquery.Config

func init() {
	harmonyquery.DefaultHostEnv = "CURIO_HARMONYDB_HOSTS"
}

func NewFromConfig(cfg Config) (*DB, error) {
	cfg.SqlEmbedFS = &upgradeFS
	cfg.DowngradeEmbedFS = &downgradeFS
	return harmonyquery.NewFromConfig(cfg)
}

func envElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

func NewFromConfigWithITestID(t *testing.T, id harmonyquery.ITestID) (*DB, error) {
	db, err := NewFromConfig(Config{
		Hosts:            []string{envElse(harmonyquery.DefaultHostEnv, "127.0.0.1")},
		Database:         "yugabyte",
		Username:         "yugabyte",
		Password:         "yugabyte",
		Port:             envElse("CURIO_HARMONYDB_PORT", "5433"),
		LoadBalance:      false,
		ITestID:          id,
		SqlEmbedFS:       &upgradeFS,
		DowngradeEmbedFS: &downgradeFS,
	})
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		db.ITestDeleteAll()
	})
	return db, nil
}

//go:embed sql
var upgradeFS embed.FS

//go:embed downgrade
var downgradeFS embed.FS

// A function for clean, idempotent SQL upgrade testing.
var ITestUpgradeFunc = harmonyquery.ITestUpgradeFunc

const InitialSerializationErrorRetryWait = 5 * time.Second

// Query offers Next/Err/Close/Scan/Values
type Query = harmonyquery.Query

type Tx = harmonyquery.Tx
type TransactionOptions = harmonyquery.TransactionOptions

var IsErrUniqueContraint = harmonyquery.IsErrUniqueContraint
var IsErrSerialization = harmonyquery.IsErrSerialization

var OptionRetry = harmonyquery.OptionRetry
