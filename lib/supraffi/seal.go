//go:build linux

package supraffi

/*
   #cgo CFLAGS: -I${SRCDIR}/../../extern/supraseal/sealing -fno-omit-frame-pointer
   #cgo LDFLAGS: -Wl,-z,noexecstack -Wl,-z,relro,-z,now -L${SRCDIR}/../../extern/supraseal/obj -lsupraseal -lcudart_static -L${SRCDIR}/../../extern/supraseal/deps/blst -lblst -lconfig++ -lgmp -lstdc++ -pthread -ldl -lrt
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
func TreeRFile(lastLayerFilename, dataFilename, outputDir string, sectorSize uint64) int {
	cLastLayerFilename := C.CString(lastLayerFilename)
	cDataFilename := C.CString(dataFilename)
	cOutputDir := C.CString(outputDir)
	defer C.free(unsafe.Pointer(cLastLayerFilename))
	defer C.free(unsafe.Pointer(cDataFilename))
	defer C.free(unsafe.Pointer(cOutputDir))
	return int(C.tree_r_file(cLastLayerFilename, cDataFilename, cOutputDir, C.size_t(sectorSize)))
}
