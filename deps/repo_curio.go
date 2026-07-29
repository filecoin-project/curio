//go:build !skiff

package deps

import (
	"github.com/filecoin-project/curio/lib/repo"

	lrepo "github.com/filecoin-project/lotus/node/repo"
)

func ensureCurioRepo(repoPath string) error {
	r, err := lrepo.NewFS(repoPath)
	if err != nil {
		return err
	}

	ok, err := r.Exists()
	if err != nil {
		return err
	}
	if !ok {
		if err := r.Init(repo.Curio); err != nil {
			return err
		}
	}
	return nil
}
