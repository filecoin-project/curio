//go:build !skiff

package deps

import (
	"github.com/urfave/cli/v2"
)

func resolveChainAPIInfo(cctx *cli.Context, configured []string) ([]string, func(), error) {
	_ = cctx
	return configured, func() {}, nil
}
