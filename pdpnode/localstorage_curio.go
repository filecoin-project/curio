//go:build !skiff

package pdpnode

import (
	"path"

	"github.com/filecoin-project/curio/lib/paths"
)

func newLocalStorage(repoPath string, readOnly bool) (paths.LocalStorage, error) {
	if readOnly {
		return newReadonlyLocalStorage(), nil
	}
	return &paths.BasicLocalStorage{
		PathToJSON: path.Join(repoPath, "storage.json"),
	}, nil
}
