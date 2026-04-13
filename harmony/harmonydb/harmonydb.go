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
	if cfg.SqlEmbedFS == nil {
		cfg.SqlEmbedFS = &upgradeFS
	}
	if cfg.DowngradeEmbedFS == nil {
		cfg.DowngradeEmbedFS = &downgradeFS
	}
	return harmonyquery.NewFromConfig(cfg)
}

func envElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

type ItestOptions struct {
	Hosts       []string
	Database    string
	Username    string
	Password    string
	Port        string
	LoadBalance bool
	ITestID     harmonyquery.ITestID
	useTemplate bool
}

type ItestOpts func(opts *ItestOptions)

func DefaultItestOptions() ItestOptions {
	return ItestOptions{
		Hosts:       []string{envElse(harmonyquery.DefaultHostEnv, "127.0.0.1")},
		Database:    "yugabyte",
		Username:    "yugabyte",
		Password:    "yugabyte",
		Port:        "5432",
		LoadBalance: false,
		useTemplate: true,
		ITestID: ITestNewID(),
	}
}

func ItestID(id harmonyquery.ITestID) ItestOpts {
	return func(opts *ItestOptions) {
		opts.ITestID = id
	}
}

func YugabyteDB(yb bool) ItestOpts {
	return func(opts *ItestOptions) {
		opts.useTemplate = !yb
		if yb {
			opts.Port = "5433"
		}
	}
}

func (c *ItestOptions) HarnomyConfig() Config {
	return Config{
		Hosts:            c.Hosts,
		Database:         c.Database,
		Username:         c.Username,
		Password:         c.Password,
		Port:             c.Port,
		LoadBalance:      c.LoadBalance,
		ITestID:          c.ITestID,
		UseTemplate:      c.useTemplate,
		SqlEmbedFS:       &upgradeFS,
		DowngradeEmbedFS: &downgradeFS,
	}
}

func NewFromConfigWithITestID(t *testing.T, opts ...ItestOpts) (*DB, error) {
	cfg := DefaultItestOptions()
	for _, opt := range opts {
		opt(&cfg)
	}

	db, err := NewFromConfig(cfg.HarnomyConfig())
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
