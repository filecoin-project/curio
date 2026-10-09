//go:build skiff

package pdpnode

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/lib/skiffdata"
)

const defaultSkiffDataPath = skiffdata.DefaultDataPath

func resolveSkiffDataPath(cctx *cli.Context, cfg *config.CurioConfig) string {
	if cctx != nil && cctx.IsSet("data") {
		return cctx.String("data")
	}
	return skiffdata.ResolveDataRoot(cfg)
}
