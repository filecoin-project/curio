//go:build skiff

package paths

import (
	"context"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/storiface"
)

func (r *Remote) GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, sinfo storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error) {
	return nil, xerrors.Errorf("GenerateSingleVanillaProof is not available in the skiff build")
}

func (r *Remote) GeneratePoRepVanillaProof(ctx context.Context, sr storiface.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error) {
	return nil, xerrors.Errorf("GeneratePoRepVanillaProof is not available in the skiff build")
}

func (r *Remote) ReadSnapVanillaProof(ctx context.Context, sr storiface.SectorRef) ([]byte, error) {
	return nil, xerrors.Errorf("ReadSnapVanillaProof is not available in the skiff build")
}
