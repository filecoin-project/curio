//go:build skiff

package pdpnode

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/deps/config"
)

func skiffStorageRoot(cctx *cli.Context, cfg *config.CurioConfig, _ string) string {
	return resolveSkiffDataPath(cctx, cfg)
}
