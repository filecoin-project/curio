//go:build !skiff

package pdpnode

import (
	"path"

	"github.com/filecoin-project/curio/lib/paths"
)

func newLocalStorage(repoPath string) (paths.LocalStorage, error) {
	return &paths.BasicLocalStorage{
		PathToJSON: path.Join(repoPath, "storage.json"),
	}, nil
}
