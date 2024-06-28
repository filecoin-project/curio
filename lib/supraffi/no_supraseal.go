//go:build !supraseal

package supraffi

import (
	"bytes"
	"encoding/binary"
)

// SupraSealInit initializes the supra seal with a sector size and optional config file.
func SupraSealInit(sectorSize uint64, configFile string) {
	panic("SupraSealInit: supraseal build tag not enabled")
}

// Pc1 performs the pc1 operation.
func Pc1(blockOffset uint64, replicaIDs [][32]byte, parentsFilename string, sectorSize uint64) int {
	panic("Pc1: supraseal build tag not enabled")
}

type Path struct {
	Replica string
	Cache   string
}

// GenerateMultiString generates a //multi// string from an array of Path structs
func GenerateMultiString(paths []Path) (string, error) {
	var buffer bytes.Buffer
	buffer.WriteString("//multi//")

	for _, path := range paths {
		replicaPath := []byte(path.Replica)
		cachePath := []byte(path.Cache)

		// Write the length and path for the replica
		if err := binary.Write(&buffer, binary.LittleEndian, uint32(len(replicaPath))); err != nil {
			return "", err
		}
		buffer.Write(replicaPath)

		// Write the length and path for the cache
		if err := binary.Write(&buffer, binary.LittleEndian, uint32(len(cachePath))); err != nil {
			return "", err
		}
		buffer.Write(cachePath)
	}

	return buffer.String(), nil
}

// Pc2 performs the pc2 operation.
func Pc2(blockOffset uint64, numSectors int, outputDir string, sectorSize uint64) int {
	panic("Pc2: supraseal build tag not enabled")
}

// Pc2Cleanup deletes files associated with pc2.
func Pc2Cleanup(numSectors int, outputDir string, sectorSize uint64) int {
	panic("Pc2Cleanup: supraseal build tag not enabled")
}

// C1 performs the c1 operation.
func C1(blockOffset uint64, numSectors, sectorSlot int, replicaID, seed, ticket []byte, cachePath, parentsFilename, replicaPath string, sectorSize uint64) int {
	panic("C1: supraseal build tag not enabled")
}

// GetMaxBlockOffset returns the highest available block offset.
func GetMaxBlockOffset(sectorSize uint64) uint64 {
	panic("GetMaxBlockOffset: supraseal build tag not enabled")
}

// GetSlotSize returns the size in blocks required for the given number of sectors.
func GetSlotSize(numSectors int, sectorSize uint64) uint64 {
	panic("GetSlotSize: supraseal build tag not enabled")
}

// GetCommCFromTree returns comm_c after calculating from tree file(s).
func GetCommCFromTree(commC []byte, cachePath string, sectorSize uint64) bool {
	panic("GetCommCFromTree: supraseal build tag not enabled")
}

// GetCommC returns comm_c from p_aux file.
func GetCommC(commC []byte, cachePath string) bool {
	panic("GetCommC: supraseal build tag not enabled")
}

// SetCommC sets comm_c in the p_aux file.
func SetCommC(commC []byte, cachePath string) bool {
	panic("SetCommC: supraseal build tag not enabled")
}

// GetCommRLastFromTree returns comm_r_last after calculating from tree file(s).
func GetCommRLastFromTree(commRLast []byte, cachePath string, sectorSize uint64) bool {
	panic("GetCommRLastFromTree: supraseal build tag not enabled")
}

// GetCommRLast returns comm_r_last from p_aux file.
func GetCommRLast(commRLast []byte, cachePath string) bool {
	panic("GetCommRLast: supraseal build tag not enabled")
}

// SetCommRLast sets comm_r_last in the p_aux file.
func SetCommRLast(commRLast []byte, cachePath string) bool {
	panic("SetCommRLast: supraseal build tag not enabled")
}

// GetCommR returns comm_r after calculating from p_aux file.
func GetCommR(commR []byte, cachePath string) bool {
	panic("GetCommR: supraseal build tag not enabled")
}

// GetCommD returns comm_d from tree_d file.
func GetCommD(commD []byte, cachePath string) bool {
	panic("GetCommD: supraseal build tag not enabled")
}

// GetCCCommD returns comm_d for a cc sector.
func GetCCCommD(commD []byte, sectorSize int) bool {
	panic("GetCCCommD: supraseal build tag not enabled")
}
