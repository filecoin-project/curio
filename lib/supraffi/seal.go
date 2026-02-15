//go:build linux && !nosupraseal

package supraffi

/*
   #cgo CFLAGS: -I${SRCDIR}/../../extern/supraseal/sealing -fno-omit-frame-pointer
   #cgo LDFLAGS: -Wl,-z,noexecstack -Wl,-z,relro,-z,now -Wl,--allow-multiple-definition -L${SRCDIR}/../../extern/supraseal/obj -lsupraseal -lcudart_static -L${SRCDIR}/../../extern/supraseal/deps/blst -lblst -lconfig++ -lgmp -lstdc++ -pthread -ldl -lrt
   #include <stdint.h>
   #include <stdbool.h>
   #include "supra_seal.h"
   #include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"unsafe"
)

const libsupra_version = 0x10_00_01

func init() {
	libVer := int(C.supra_version())

	if libVer != libsupra_version {
		panic(fmt.Sprintf("libsupra version mismatch: %x != %x", libVer, libsupra_version))
	}
}

// TreeRFile builds tree-r from a last-layer file (optionally with a staged data file).
// Used for snap updates, does not require NVMe devices.
// If the required CPU or GPU features are not available, this returns -1 so
// callers can fallback to filecoin-ffi's implementation.
func TreeRFile(lastLayerFilename, dataFilename, outputDir string, sectorSize uint64) int {
	// Check if the host can run supraseal's CUDA TreeR path
	if !HasAMD64v4() || !HasUsableCUDAGPU() {
		// Return a special error code to indicate fallback is needed
		// -1 indicates CPU feature not available, caller should use filecoin-ffi
		return -1
	}

	cLastLayerFilename := C.CString(lastLayerFilename)
	cDataFilename := C.CString(dataFilename)
	cOutputDir := C.CString(outputDir)
	defer C.free(unsafe.Pointer(cLastLayerFilename))
	defer C.free(unsafe.Pointer(cDataFilename))
	defer C.free(unsafe.Pointer(cOutputDir))
	return int(C.tree_r_file(cLastLayerFilename, cDataFilename, cOutputDir, C.size_t(sectorSize)))
}

// GetCommRLastFromTree returns comm_r_last after calculating from tree file(s).
// Returns true on success.
func GetCommRLastFromTree(commRLast []byte, cachePath string, sectorSize uint64) bool {
	if len(commRLast) < 32 {
		return false
	}
	cCommRLast := (*C.uint8_t)(unsafe.Pointer(&commRLast[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_r_last_from_tree(cCommRLast, cCachePath, C.size_t(sectorSize)))
}

// GetCommRLast returns comm_r_last from p_aux file.
// Returns true on success.
func GetCommRLast(commRLast []byte, cachePath string) bool {
	if len(commRLast) < 32 {
		return false
	}
	cCommRLast := (*C.uint8_t)(unsafe.Pointer(&commRLast[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_r_last(cCommRLast, cCachePath))
}
