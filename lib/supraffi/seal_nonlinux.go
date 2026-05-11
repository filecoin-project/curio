//go:build !linux || nosupraseal

package supraffi

import "fmt"

// TreeRFile builds tree-r from a last-layer file (optionally with a staged data file).
// Used for snap updates, does not require NVMe devices.
// This is a stub implementation for non-Linux platforms.
func TreeRFile(lastLayerFilename, dataFilename, outputDir string, sectorSize uint64) int {
	panic("TreeRFile: supraseal is only available on Linux")
}

// GetHealthInfo retrieves health information for all NVMe devices.
// This is a stub implementation for non-Linux platforms.
func GetHealthInfo() ([]HealthInfo, error) {
	return nil, fmt.Errorf("GetHealthInfo: supraseal is only available on Linux")
}

// SupraSealInit initializes the supra seal with a sector size and optional config file.
// This is a stub implementation for non-Linux platforms.
func SupraSealInit(sectorSize uint64, configFile string) {
	panic("SupraSealInit: supraseal is only available on Linux")
}

// Pc1 performs the pc1 operation for batch sealing.
// This is a stub implementation for non-Linux platforms.
func Pc1(blockOffset uint64, replicaIDs [][32]byte, parentsFilename string, sectorSize uint64) int {
	panic("Pc1: supraseal is only available on Linux")
}

type Path struct {
	Replica string
	Cache   string
}

// GenerateMultiString generates a //multi// string from an array of Path structs.
// This is a stub implementation for non-Linux platforms.
func GenerateMultiString(paths []Path) (string, error) {
	return "", fmt.Errorf("GenerateMultiString: supraseal is only available on Linux")
}

// Pc2 performs the pc2 operation for batch sealing.
// This is a stub implementation for non-Linux platforms.
func Pc2(blockOffset uint64, numSectors int, outputDir string, sectorSize uint64) int {
	panic("Pc2: supraseal is only available on Linux")
}

// C1 performs the c1 operation for batch sealing.
// This is a stub implementation for non-Linux platforms.
func C1(blockOffset uint64, numSectors, sectorSlot int, replicaID, seed, ticket []byte, cachePath, parentsFilename, replicaPath string, sectorSize uint64) int {
	panic("C1: supraseal is only available on Linux")
}

// GetMaxBlockOffset returns the highest available block offset from NVMe devices.
// This is a stub implementation for non-Linux platforms.
func GetMaxBlockOffset(sectorSize uint64) uint64 {
	panic("GetMaxBlockOffset: supraseal is only available on Linux")
}

// GetSlotSize returns the size in blocks required for the given number of sectors.
// This is a stub implementation for non-Linux platforms.
func GetSlotSize(numSectors int, sectorSize uint64) uint64 {
	panic("GetSlotSize: supraseal is only available on Linux")
}

// GetCommR returns comm_r after calculating from p_aux file. Returns true on success.
// This is a stub implementation for non-Linux platforms.
func GetCommR(commR []byte, cachePath string) bool {
	panic("GetCommR: supraseal is only available on Linux")
}

// GetCommRLastFromTree returns comm_r_last after calculating from tree file(s).
// This is a stub implementation for non-Linux platforms.
func GetCommRLastFromTree(commRLast []byte, cachePath string, sectorSize uint64) bool {
	panic("GetCommRLastFromTree: supraseal is only available on Linux")
}

// GetCommRLast returns comm_r_last from p_aux file.
// This is a stub implementation for non-Linux platforms.
func GetCommRLast(commRLast []byte, cachePath string) bool {
	panic("GetCommRLast: supraseal is only available on Linux")
}

// CheckAndSetupSPDK prepares SPDK hugepages and environment.
// This is a stub implementation for non-Linux platforms.
func CheckAndSetupSPDK(nrHuge int, minPages int) error {
	return fmt.Errorf("CheckAndSetupSPDK: supraseal/SPDK is only available on Linux")
}
