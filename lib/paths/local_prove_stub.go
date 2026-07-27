//go:build skiff

package paths

import (
	"context"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/storiface"
)

func (st *Local) GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, si storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error) {
	return nil, xerrors.Errorf("GenerateSingleVanillaProof is not available in the skiff build")
}

func (st *Local) GeneratePoRepVanillaProof(ctx context.Context, sr storiface.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error) {
	return nil, xerrors.Errorf("GeneratePoRepVanillaProof is not available in the skiff build")
}

func (st *Local) ReadSnapVanillaProof(ctx context.Context, sr storiface.SectorRef) ([]byte, error) {
	return nil, xerrors.Errorf("ReadSnapVanillaProof is not available in the skiff build")
}

var _ Store = &Local{}
