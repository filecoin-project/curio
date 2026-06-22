//go:build !maxboom

package deps

import (
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
)

func setDefaultVerifProver(deps *Deps) {
	if deps.Verif == nil {
		deps.Verif = ffiwrapper.ProofVerifier
	}
	if deps.Prover == nil {
		deps.Prover = ffiwrapper.ProofProver
	}
}
