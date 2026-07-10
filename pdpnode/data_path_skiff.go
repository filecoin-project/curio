//go:build skiff

package pdpnode

import (
	"os"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/deps/config"
)

const defaultSkiffDataPath = "/data"

func resolveSkiffDataPath(cctx *cli.Context, cfg *config.CurioConfig) string {
	if cctx.IsSet("data") {
		return cctx.String("data")
	}
	if v := os.Getenv("DATA_STORAGE"); v != "" {
		return v
	}
	if v := os.Getenv("SKIFF_DATA"); v != "" {
		return v
	}
	if v := os.Getenv("CURIO_DATA"); v != "" {
		return v
	}
	if cfg != nil && cfg.Subsystems.DataPath != "" {
		return cfg.Subsystems.DataPath
	}
	return defaultSkiffDataPath
}
