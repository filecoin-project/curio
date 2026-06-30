//go:build maxboom

package pdpnode

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/deps/config"
)

func maxboomStorageRoot(cctx *cli.Context, cfg *config.CurioConfig, _ string) string {
	return resolveMaxBoomDataPath(cctx, cfg)
}
