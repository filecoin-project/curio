//go:build skiff

package pdpnode

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	adminAddrLocal  = "127.0.0.1:4701"
	adminAddrDocker = "0.0.0.0:4701"
)

func skiffDockerMode() bool {
	return os.Getenv("SKIFF_DOCKER") != ""
}

// generateStorageRPCSecret creates a new random base64-encoded 32-byte secret.
func generateStorageRPCSecret() string {
	sk, err := io.ReadAll(io.LimitReader(rand.Reader, 32))
	if err != nil {
		panic(xerrors.Errorf("generating storage rpc secret: %w", err))
	}
	return base64.StdEncoding.EncodeToString(sk)
}

func defaultSkiffBaseConfig() *config.CurioConfig {
	cfg := config.DefaultCurioConfig()
	cfg.Subsystems.EnablePDP = true
	cfg.Subsystems.EnableWebGui = true
	cfg.Subsystems.GuiAddress = adminAddrLocal
	cfg.HTTP.Enable = false

	if skiffDockerMode() {
		cfg.Subsystems.GuiAddress = adminAddrDocker
		cfg.HTTP.Enable = true
		cfg.HTTP.DelegateTLS = true
		cfg.HTTP.ListenAddress = "0.0.0.0:80"
		cfg.HTTP.DomainName = os.Getenv("SKIFF_HTTP_DOMAIN")
		if cfg.HTTP.DomainName == "" {
			cfg.HTTP.DomainName = "localhost"
		}
	}

	cfg.Apis.StorageRPCSecret = generateStorageRPCSecret()
	return cfg
}

func applySkiffDefaults(cfg *config.CurioConfig) {
	cfg.Subsystems.EnablePDP = true
	if !cfg.Subsystems.EnableWebGui {
		cfg.Subsystems.EnableWebGui = true
	}
	if skiffDockerMode() {
		if cfg.Subsystems.GuiAddress == "" || cfg.Subsystems.GuiAddress == adminAddrLocal {
			cfg.Subsystems.GuiAddress = adminAddrDocker
		}
	} else if cfg.Subsystems.GuiAddress == "" || cfg.Subsystems.GuiAddress == adminAddrDocker {
		cfg.Subsystems.GuiAddress = adminAddrLocal
	}
	if cfg.Apis.StorageRPCSecret == "" {
		cfg.Apis.StorageRPCSecret = generateStorageRPCSecret()
	}
}

func configToTOML(cfg *config.CurioConfig) (string, error) {
	cb, err := config.ConfigUpdate(cfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	if err != nil {
		return "", xerrors.Errorf("serializing config: %w", err)
	}
	return string(cb), nil
}

func writeConfigLayer(ctx context.Context, db *harmonydb.DB, title, text string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO harmony_config (title, config) VALUES ($1, $2)
		ON CONFLICT (title) DO UPDATE SET config = EXCLUDED.config
	`, title, text)
	return err
}

func loadConfigLayer(ctx context.Context, db *harmonydb.DB, title string) (string, error) {
	var text string
	err := db.QueryRow(ctx, `SELECT config FROM harmony_config WHERE title = $1`, title).Scan(&text)
	if err != nil {
		return "", err
	}
	return text, nil
}

func layerExists(ctx context.Context, db *harmonydb.DB, title string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM harmony_config WHERE title = $1 AND LENGTH(config) > 0)`, title).Scan(&exists)
	return exists, err
}

func mergeConfigLayers(baseText string, overlayTexts ...string) (*config.CurioConfig, error) {
	cfg := config.DefaultCurioConfig()
	layers := []config.ConfigText{{Title: "base", Config: baseText}}
	for _, text := range overlayTexts {
		layers = append(layers, config.ConfigText{Title: "overlay", Config: text})
	}
	if err := config.ApplyLayers(context.Background(), cfg, layers, config.FixTOML); err != nil {
		return nil, err
	}
	applySkiffDefaults(cfg)
	return cfg, nil
}

func ensureSkiffBaseLayer(ctx context.Context, db *harmonydb.DB) error {
	hasBase, err := layerExists(ctx, db, "base")
	if err != nil {
		return xerrors.Errorf("checking base layer: %w", err)
	}
	if hasBase {
		return nil
	}

	var cfg *config.CurioConfig

	hasPDP, err := layerExists(ctx, db, "pdp")
	if err != nil {
		return xerrors.Errorf("checking pdp layer: %w", err)
	}
	if hasPDP {
		pdpText, err := loadConfigLayer(ctx, db, "pdp")
		if err != nil {
			return xerrors.Errorf("loading pdp layer: %w", err)
		}
		baseText, err := configToTOML(defaultSkiffBaseConfig())
		if err != nil {
			return err
		}
		cfg, err = mergeConfigLayers(baseText, pdpText)
		if err != nil {
			return xerrors.Errorf("merging pdp into new base: %w", err)
		}
		log.Info("created skiff base layer from defaults merged with existing pdp layer")
	} else {
		cfg = defaultSkiffBaseConfig()
		log.Info("created skiff base configuration layer")
	}

	text, err := configToTOML(cfg)
	if err != nil {
		return err
	}
	if err := writeConfigLayer(ctx, db, "base", text); err != nil {
		return xerrors.Errorf("inserting skiff base layer: %w", err)
	}
	return nil
}
