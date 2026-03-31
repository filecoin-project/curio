package helpers

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func LoadBaseConfigFromDB(ctx context.Context, db *harmonydb.DB) (*config.CurioConfig, error) {
	cfg := config.DefaultCurioConfig()

	var baseText string
	if err := db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText); err != nil {
		return nil, xerrors.Errorf("load base config row: %w", err)
	}

	if _, err := deps.LoadConfigWithUpgrades(baseText, cfg); err != nil {
		return nil, xerrors.Errorf("decode base config: %w", err)
	}

	return cfg, nil
}

func EncodeCurioConfigForDB(cfg *config.CurioConfig) (string, error) {
	cb, err := config.ConfigUpdate(
		cfg,
		config.DefaultCurioConfig(),
		config.Commented(true),
		config.DefaultKeepUncommented(),
		config.NoEnv(),
	)
	if err != nil {
		return "", xerrors.Errorf("encode config update: %w", err)
	}
	return string(cb), nil
}

func UpsertBaseConfig(ctx context.Context, db *harmonydb.DB, cfg *config.CurioConfig) error {
	encoded, err := EncodeCurioConfigForDB(cfg)
	if err != nil {
		return err
	}

	_, err = db.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, "base", encoded)
	if err != nil {
		return xerrors.Errorf("upsert base config: %w", err)
	}

	return nil
}

func UpdateBaseConfigWithTimestamp(ctx context.Context, db *harmonydb.DB, cfg *config.CurioConfig) error {
	encoded, err := EncodeCurioConfigForDB(cfg)
	if err != nil {
		return err
	}

	_, err = db.Exec(ctx, `UPDATE harmony_config SET config = $1, timestamp = NOW() WHERE title = 'base'`, encoded)
	if err != nil {
		return xerrors.Errorf("update base config timestamp: %w", err)
	}

	return nil
}

func SetBaseConfigWithDefaults(t *testing.T, ctx context.Context, db *harmonydb.DB) (*config.CurioConfig, error) {
	baseCfg, err := LoadBaseConfigFromDB(ctx, db)
	require.NoError(t, err)

	baseCfg.Subsystems.EnableDealMarket = true
	baseCfg.Subsystems.EnableCommP = true
	baseCfg.HTTP.Enable = true
	baseCfg.HTTP.DelegateTLS = true
	baseCfg.HTTP.DomainName = "localhost"
	baseCfg.HTTP.ListenAddress = FreeListenAddr(t)
	baseCfg.HTTP.DenylistServers = config.NewDynamic([]string{})
	baseCfg.Batching.PreCommit.Timeout = time.Second
	baseCfg.Batching.Commit.Timeout = time.Second
	baseCfg.Ingest.MaxDealWaitTime.Set(2 * time.Second)
	baseCfg.Market.StorageMarketConfig.MK12.PublishMsgPeriod = time.Second
	baseCfg.Market.StorageMarketConfig.MK12.ExpectedPoRepSealDuration = 2 * time.Minute
	baseCfg.Market.StorageMarketConfig.MK20.ExpectedPoRepSealDuration = 2 * time.Minute
	baseCfg.Market.StorageMarketConfig.IPNI.Disable = true

	err = UpsertBaseConfig(ctx, db, baseCfg)
	require.NoError(t, err)

	return baseCfg, nil
}

func nullInt64(v sql.NullInt64) string {
	if !v.Valid {
		return "NULL"
	}
	return strconv.FormatInt(v.Int64, 10)
}

func nullString(v sql.NullString) string {
	if !v.Valid {
		return "NULL"
	}
	return v.String
}

func stringOrNull(v []byte) string {
	if len(v) == 0 {
		return "NULL"
	}
	return string(v)
}

func Int64PtrOrNA(ptr *int64) string {
	if ptr == nil {
		return "NA"
	}
	return strconv.FormatInt(*ptr, 10)
}

func TimePtrOrNA(v *time.Time) string {
	if v == nil {
		return "NA"
	}
	return v.Format(time.RFC3339)
}

func NullInt64OrNA(v sql.NullInt64) string {
	if !v.Valid {
		return "NA"
	}
	return strconv.FormatInt(v.Int64, 10)
}
