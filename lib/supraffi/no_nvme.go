//go:build !supraseal_nvme

package supraffi

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// GetHealthInfo retrieves health information for all NVMe devices
// This function requires supraseal_nvme build tag
func GetHealthInfo() ([]HealthInfo, error) {
	return nil, fmt.Errorf("GetHealthInfo: supraseal_nvme build tag not enabled")
}

// SetupSPDK runs the SPDK setup script to configure NVMe devices for use with SupraSeal.
func SetupSPDK(nrHuge int) error {
	return fmt.Errorf("SetupSPDK: supraseal_nvme build tag not enabled")
}

// CheckAndSetupSPDK checks if SPDK is set up, and if not, runs the setup script.
func CheckAndSetupSPDK(nrHuge int, minPages int) error {
	return fmt.Errorf("CheckAndSetupSPDK: supraseal_nvme build tag not enabled")
}

// SupraSealInit initializes the supra seal with a sector size and optional config file.
// Requires NVMe devices for batch sealing.
func SupraSealInit(sectorSize uint64, configFile string) {
	panic("SupraSealInit: supraseal_nvme build tag required for batch sealing")
}

// Pc1 performs the pc1 operation for batch sealing.
// Requires NVMe devices for layer storage.
func Pc1(blockOffset uint64, replicaIDs [][32]byte, parentsFilename string, sectorSize uint64) int {
	panic("Pc1: supraseal_nvme build tag required for batch sealing")
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

// Pc2 performs the pc2 operation for batch sealing.
// Requires NVMe devices for layer storage.
func Pc2(blockOffset uint64, numSectors int, outputDir string, sectorSize uint64) int {
	panic("Pc2: supraseal_nvme build tag required for batch sealing")
}

// C1 performs the c1 operation for batch sealing.
// Requires NVMe devices for batch operations.
func C1(blockOffset uint64, numSectors, sectorSlot int, replicaID, seed, ticket []byte, cachePath, parentsFilename, replicaPath string, sectorSize uint64) int {
	panic("C1: supraseal_nvme build tag required for batch sealing")
}

// GetMaxBlockOffset returns the highest available block offset from NVMe devices.
func GetMaxBlockOffset(sectorSize uint64) uint64 {
	panic("GetMaxBlockOffset: supraseal_nvme build tag required")
}

// GetSlotSize returns the size in blocks required for the given number of sectors.
// Used for batch sealing with NVMe devices.
func GetSlotSize(numSectors int, sectorSize uint64) uint64 {
	panic("GetSlotSize: supraseal_nvme build tag required")
}

// GetCommR returns comm_r after calculating from p_aux file. Returns true on success.
// Used in batch sealing context.
func GetCommR(commR []byte, cachePath string) bool {
	panic("GetCommR: supraseal_nvme build tag required")
}
