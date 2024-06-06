// This is a wrapper around the FFI functions that allows them to be called by reflection.
// For the Curio GPU selector, see lib/ffiselect/ffiselect.go.
package ffidirect

import (
	"errors"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/ipfs/go-cid"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/proof"
)

// This allow reflection access to the FFI functions.
type FFI struct{}

func (FFI) GenerateSinglePartitionWindowPoStWithVanilla(
	proofType abi.RegisteredPoStProof,
	minerID abi.ActorID,
	randomness abi.PoStRandomness,
	proofs [][]byte,
	partitionIndex uint,
) (*ffi.PartitionProof, error) {
	return ffi.GenerateSinglePartitionWindowPoStWithVanilla(proofType, minerID, randomness, proofs, partitionIndex)
}

func (FFI) SealPreCommitPhase2(
	phase1Output []byte,
	cacheDirPath string,
	sealedSectorPath string,
) (out storiface.SectorCids, err error) {
	sealed, unsealed, err := ffi.SealPreCommitPhase2(phase1Output, cacheDirPath, sealedSectorPath)
	if err != nil {
		return storiface.SectorCids{}, err
	}

	return storiface.SectorCids{
		Unsealed: unsealed,
		Sealed:   sealed,
	}, nil
}

func (FFI) SealCommitPhase2(
	phase1Output []byte,
	sectorNum abi.SectorNumber,
	minerID abi.ActorID,
) ([]byte, error) {
	return ffi.SealCommitPhase2(phase1Output, sectorNum, minerID)
}

func (FFI) GenerateWinningPoStWithVanilla(
	proofType abi.RegisteredPoStProof,
	minerID abi.ActorID,
	randomness abi.PoStRandomness,
	proofs [][]byte,
) ([]proof.PoStProof, error) {
	return ffi.GenerateWinningPoStWithVanilla(proofType, minerID, randomness, proofs)
}

func (FFI) SelfTest(val1 int, val2 cid.Cid) (cid.Cid, error) {
	if val1 != 12345678 {
		return cid.Undef, errors.New("val1 was not as expected")
	}

	return val2, nil
}
