// This is a wrapper around the FFI functions that allows them to be called by reflection.
// For the Curio GPU selector, see lib/ffiselect/ffiselect.go.
package ffidirect

import (
	"errors"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/filecoin-ffi/cgo"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/supraffi"
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

func (FFI) EncodeInto(
	proofType abi.RegisteredUpdateProof,
	newReplicaPath string,
	newReplicaCachePath string,
	sectorKeyPath string,
	sectorKeyCachePath string,
	stagedDataPath string,
	pieces []abi.PieceInfo,
) (out storiface.SectorCids, err error) {
	sealed, unsealed, err := ffi.SectorUpdate.EncodeInto(proofType, newReplicaPath, newReplicaCachePath, sectorKeyPath, sectorKeyCachePath, stagedDataPath, pieces)
	if err != nil {
		return storiface.SectorCids{}, err
	}

	return storiface.SectorCids{
		Unsealed: unsealed,
		Sealed:   sealed,
	}, nil
}

func (FFI) GenerateUpdateProofWithVanilla(
	proofType abi.RegisteredUpdateProof,
	key, sealed, unsealed cid.Cid,
	vproofs [][]byte,
) ([]byte, error) {
	return ffi.SectorUpdate.GenerateUpdateProofWithVanilla(proofType, key, sealed, unsealed, vproofs)
}

func (FFI) TreeRFile(lastLayerFilename, dataFilename, outputDir string, sectorSize uint64) error {
	// Check CPU features and CUDA availability before calling supraseal
	if !supraffi.HasAMD64v4() || !supraffi.HasUsableCUDAGPU() {
		// Missing prerequisites, fallback to filecoin-ffi's GenerateTreeRLast
		// Convert sector size to RegisteredPoStProof (WindowPoSt version)
		var postProof abi.RegisteredPoStProof
		switch sectorSize {
		case 32 << 30: // 32GiB
			postProof = abi.RegisteredPoStProof_StackedDrgWindow32GiBV1_1
		case 64 << 30: // 64GiB
			postProof = abi.RegisteredPoStProof_StackedDrgWindow64GiBV1_1
		case 2 << 10: // 2KiB
			postProof = abi.RegisteredPoStProof_StackedDrgWindow2KiBV1_1
		case 8 << 20: // 8MiB
			postProof = abi.RegisteredPoStProof_StackedDrgWindow8MiBV1_1
		case 512 << 20: // 512MiB
			postProof = abi.RegisteredPoStProof_StackedDrgWindow512MiBV1_1
		default:
			return xerrors.Errorf("unsupported sector size for TreeR fallback: %d", sectorSize)
		}

		// Use filecoin-ffi's GenerateTreeRLast
		// Convert abi.RegisteredPoStProof to cgo.RegisteredPoStProof
		var cgoPostProof cgo.RegisteredPoStProof
		switch postProof {
		case abi.RegisteredPoStProof_StackedDrgWindow32GiBV1_1:
			cgoPostProof = cgo.RegisteredPoStProofStackedDrgWindow32GiBV1_1
		case abi.RegisteredPoStProof_StackedDrgWindow64GiBV1_1:
			cgoPostProof = cgo.RegisteredPoStProofStackedDrgWindow64GiBV1_1
		case abi.RegisteredPoStProof_StackedDrgWindow2KiBV1_1:
			cgoPostProof = cgo.RegisteredPoStProofStackedDrgWindow2KiBV1_1
		case abi.RegisteredPoStProof_StackedDrgWindow8MiBV1_1:
			cgoPostProof = cgo.RegisteredPoStProofStackedDrgWindow8MiBV1_1
		case abi.RegisteredPoStProof_StackedDrgWindow512MiBV1_1:
			cgoPostProof = cgo.RegisteredPoStProofStackedDrgWindow512MiBV1_1
		default:
			return xerrors.Errorf("unsupported proof type for TreeR fallback: %v", postProof)
		}

		_, err := cgo.GenerateTreeRLast(cgoPostProof, cgo.AsSliceRefUint8([]byte(lastLayerFilename)), cgo.AsSliceRefUint8([]byte(outputDir)))
		if err != nil {
			return xerrors.Errorf("filecoin-ffi GenerateTreeRLast fallback: %w", err)
		}
		return nil
	}

	r := supraffi.TreeRFile(lastLayerFilename, dataFilename, outputDir, sectorSize)
	if r != 0 {
		return xerrors.Errorf("tree r file: %d", r)
	}

	return nil
}

func (FFI) SelfTest(val1 int, val2 cid.Cid) (cid.Cid, error) {
	if val1 != 12345678 {
		return cid.Undef, errors.New("val1 was not as expected")
	}

	return val2, nil
}
