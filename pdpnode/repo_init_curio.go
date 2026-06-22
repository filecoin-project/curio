//go:build !maxboom

package pdpnode

import (
	"github.com/filecoin-project/curio/lib/repo"

	lrepo "github.com/filecoin-project/lotus/node/repo"
)

func ensureRepo(repoPath string) error {
	r, err := lrepo.NewFS(repoPath)
	if err != nil {
		return err
	}
	ok, err := r.Exists()
	if err != nil {
		return err
	}
	if !ok {
		return r.Init(repo.Curio)
	}
	return nil
}
