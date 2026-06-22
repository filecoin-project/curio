//go:build !maxboom

package pdpnode

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/deps/config"
)

func maxboomStorageRoot(_ *cli.Context, _ *config.CurioConfig, repoPath string) string {
	return repoPath
}
