package harmonydb

import (
	"embed"
	"os"
	"testing"

	"github.com/curiostorage/harmonydb"
)

type ITestID string

// ITestNewID see ITestWithID doc
var ITestNewID = harmonydb.ITestNewID

type DB = harmonydb.DB

type Config = harmonydb.Config

func init() {
	harmonydb.DefaultHostEnv = "CURIO_HARMONYDB_HOSTS"
}

func NewFromConfig(cfg Config) (*DB, error) {
	cfg.SqlEmbedFS = &upgadeFS
	cfg.DowngradeEmbedFS = &downgradeFS
	return harmonydb.NewFromConfig(cfg)
}

func New(hosts []string, username, password, database, port string, loadBalance bool, itestID ITestID) (*DB, error) {
	return NewFromConfig(Config{
		Hosts:            []string{envElse(harmonydb.DefaultHostEnv, "127.0.0.1")},
		Database:         database,
		Username:         username,
		Password:         password,
		Port:             port,
		LoadBalance:      loadBalance,
		SqlEmbedFS:       &upgadeFS,
		DowngradeEmbedFS: &downgradeFS,
	})
}

func envElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

func NewFromConfigWithITestID(t *testing.T, id harmonydb.ITestID) (*DB, error) {
	db, err := NewFromConfig(Config{
		Hosts:            []string{envElse(harmonydb.DefaultHostEnv, "127.0.0.1")},
		Database:         "yugabyte",
		Username:         "yugabyte",
		Password:         "yugabyte",
		Port:             "5433",
		LoadBalance:      false,
		ITestID:          id,
		SqlEmbedFS:       &upgadeFS,
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
var upgadeFS embed.FS

//go:embed downgrade
var downgradeFS embed.FS

var ITestUpgradeFunc = harmonydb.ITestUpgradeFunc
