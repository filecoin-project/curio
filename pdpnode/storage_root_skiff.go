//go:build skiff

package pdpnode

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/deps/config"
)

// skiffStorageRoot is the repo path used for storage.json persistence.
// Candidate folders for attach live under resolveSkiffDataPath (/data by default).
func skiffStorageRoot(_ *cli.Context, _ *config.CurioConfig, repoPath string) string {
	return repoPath
}
