package supraffi

/*
   #cgo CFLAGS: -I${SRCDIR}/../../extern/supra_seal/sealing
   #cgo LDFLAGS: -L${SRCDIR}/../../extern/supra_seal/obj -lsupraseal
   #include "supra_seal.h"
   #include <stdlib.h>
*/
import "C"
import (
	"unsafe"
)

// SupraSealInit initializes the supra seal with a sector size and optional config file.
func SupraSealInit(sectorSize int, configFile string) {
	cConfigFile := C.CString(configFile)
	defer C.free(unsafe.Pointer(cConfigFile))
	C.supra_seal_init(C.size_t(sectorSize), cConfigFile)
}

// Pc1 performs the pc1 operation.
func Pc1(blockOffset uint64, numSectors int, replicaIDs []byte, parentsFilename string, sectorSize int) int {
	cReplicaIDs := (*C.uint8_t)(unsafe.Pointer(&replicaIDs[0]))
	cParentsFilename := C.CString(parentsFilename)
	defer C.free(unsafe.Pointer(cParentsFilename))
	return int(C.pc1(C.uint64_t(blockOffset), C.size_t(numSectors), cReplicaIDs, cParentsFilename, C.size_t(sectorSize)))
}

// Pc2 performs the pc2 operation.
func Pc2(blockOffset, numSectors int, outputDir string, dataFilenames []string, sectorSize int) int {
	cOutputDir := C.CString(outputDir)
	defer C.free(unsafe.Pointer(cOutputDir))
	cDataFilenames := make([]*C.char, len(dataFilenames))
	for i, filename := range dataFilenames {
		cDataFilenames[i] = C.CString(filename)
		defer C.free(unsafe.Pointer(cDataFilenames[i]))
	}
	return int(C.pc2(C.size_t(blockOffset), C.size_t(numSectors), cOutputDir, &cDataFilenames[0], C.size_t(sectorSize)))
}

// Pc2Cleanup deletes files associated with pc2.
func Pc2Cleanup(numSectors int, outputDir string, sectorSize int) int {
	cOutputDir := C.CString(outputDir)
	defer C.free(unsafe.Pointer(cOutputDir))
	return int(C.pc2_cleanup(C.size_t(numSectors), cOutputDir, C.size_t(sectorSize)))
}

// C1 performs the c1 operation.
func C1(blockOffset, numSectors, sectorSlot int, replicaID, seed, ticket []byte, cachePath, parentsFilename, replicaPath string, sectorSize int) int {
	cReplicaID := (*C.uint8_t)(unsafe.Pointer(&replicaID[0]))
	cSeed := (*C.uint8_t)(unsafe.Pointer(&seed[0]))
	cTicket := (*C.uint8_t)(unsafe.Pointer(&ticket[0]))
	cCachePath := C.CString(cachePath)
	cParentsFilename := C.CString(parentsFilename)
	cReplicaPath := C.CString(replicaPath)
	defer C.free(unsafe.Pointer(cCachePath))
	defer C.free(unsafe.Pointer(cParentsFilename))
	defer C.free(unsafe.Pointer(cReplicaPath))
	return int(C.c1(C.size_t(blockOffset), C.size_t(numSectors), C.size_t(sectorSlot), cReplicaID, cSeed, cTicket, cCachePath, cParentsFilename, cReplicaPath, C.size_t(sectorSize)))
}

// GetMaxBlockOffset returns the highest available block offset.
func GetMaxBlockOffset(sectorSize int) int {
	return int(C.get_max_block_offset(C.size_t(sectorSize)))
}

// GetSlotSize returns the size in blocks required for the given number of sectors.
func GetSlotSize(numSectors, sectorSize int) int {
	return int(C.get_slot_size(C.size_t(numSectors), C.size_t(sectorSize)))
}

// GetCommCFromTree returns comm_c after calculating from tree file(s).
func GetCommCFromTree(commC []byte, cachePath string, sectorSize int) bool {
	cCommC := (*C.uint8_t)(unsafe.Pointer(&commC[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_c_from_tree(cCommC, cCachePath, C.size_t(sectorSize)))
}

// GetCommC returns comm_c from p_aux file.
func GetCommC(commC []byte, cachePath string) bool {
	cCommC := (*C.uint8_t)(unsafe.Pointer(&commC[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_c(cCommC, cCachePath))
}

// SetCommC sets comm_c in the p_aux file.
func SetCommC(commC []byte, cachePath string) bool {
	cCommC := (*C.uint8_t)(unsafe.Pointer(&commC[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.set_comm_c(cCommC, cCachePath))
}

// GetCommRLastFromTree returns comm_r_last after calculating from tree file(s).
func GetCommRLastFromTree(commRLast []byte, cachePath string, sectorSize int) bool {
	cCommRLast := (*C.uint8_t)(unsafe.Pointer(&commRLast[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_r_last_from_tree(cCommRLast, cCachePath, C.size_t(sectorSize)))
}

// GetCommRLast returns comm_r_last from p_aux file.
func GetCommRLast(commRLast []byte, cachePath string) bool {
	cCommRLast := (*C.uint8_t)(unsafe.Pointer(&commRLast[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_r_last(cCommRLast, cCachePath))
}

// SetCommRLast sets comm_r_last in the p_aux file.
func SetCommRLast(commRLast []byte, cachePath string) bool {
	cCommRLast := (*C.uint8_t)(unsafe.Pointer(&commRLast[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.set_comm_r_last(cCommRLast, cCachePath))
}

// GetCommR returns comm_r after calculating from p_aux file.
func GetCommR(commR []byte, cachePath string) bool {
	cCommR := (*C.uint8_t)(unsafe.Pointer(&commR[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_r(cCommR, cCachePath))
}

// GetCommD returns comm_d from tree_d file.
func GetCommD(commD []byte, cachePath string) bool {
	cCommD := (*C.uint8_t)(unsafe.Pointer(&commD[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_d(cCommD, cCachePath))
}

// GetCCCommD returns comm_d for a cc sector.
func GetCCCommD(commD []byte, sectorSize int) bool {
	cCommD := (*C.uint8_t)(unsafe.Pointer(&commD[0]))
	return bool(C.get_cc_comm_d(cCommD, C.size_t(sectorSize)))
}
