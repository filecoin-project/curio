package supraffi

/*
   #cgo CFLAGS: -I${SRCDIR}/../../extern/supra_seal/sealing
   #cgo LDFLAGS: -Wl,-z,noexecstack -Wl,--no-as-needed -lstdc++ -L${SRCDIR}/../../extern/supra_seal/obj -L${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/build/lib -L${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/isa-l/.libs -lsupraseal -lspdk_bdev_malloc -lspdk_bdev_null -lspdk_bdev_nvme -lspdk_bdev_passthru -lspdk_bdev_lvol -lspdk_bdev_raid -lspdk_bdev_error -lspdk_bdev_gpt -lspdk_bdev_split -lspdk_bdev_delay -lspdk_bdev_zone_block -lspdk_blobfs_bdev -lspdk_blobfs -lspdk_blob_bdev -lspdk_lvol -lspdk_blob -lspdk_nvme -lspdk_bdev_ftl -lspdk_ftl -lspdk_bdev_aio -lspdk_bdev_virtio -lspdk_virtio -lspdk_vfio_user -lspdk_accel_ioat -lspdk_ioat -lspdk_scheduler_dynamic -lspdk_env_dpdk -lspdk_scheduler_dpdk_governor -lspdk_scheduler_gscheduler -lspdk_sock_posix -lspdk_event -lspdk_event_bdev -lspdk_bdev -lspdk_notify -lspdk_dma -lspdk_event_accel -lspdk_accel -lspdk_event_vmd -lspdk_vmd -lspdk_event_sock -lspdk_init -lspdk_thread -lspdk_trace -lspdk_sock -lspdk_rpc -lspdk_jsonrpc -lspdk_json -lspdk_util -lspdk_log ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/build/lib/libspdk_env_dpdk.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_bus_pci.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_cryptodev.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_dmadev.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_eal.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_ethdev.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_hash.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_kvargs.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_mbuf.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_mempool.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_mempool_ring.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_net.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_pci.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_power.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_rcu.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_ring.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_telemetry.a ${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_vhost.a -lnuma -lisal -pthread -ldl -lrt -luuid -lssl -lcrypto -lm -laio -lcudart_static -L${SRCDIR}/../../extern/supra_seal/deps/blst -lblst -lconfig++ -lgmp
   #include <stdint.h>
   #include <stdbool.h>
   #include "supra_seal.h"
   #include <stdlib.h>
*/
import "C"
import (
	"bytes"
	"encoding/binary"
	"unsafe"
)

/*
root = {SRCDIR}/../../extern/supra_seal/

+ c++ -Ideps/spdk-v22.09/include -Ideps/spdk-v22.09/isa-l/.. -Ideps/spdk-v22.09/dpdk/build/include
-g -O2 -march=native -fPIC -fno-omit-frame-pointer -fno-strict-aliasing -fstack-protector -fno-common
-D_GNU_SOURCE -U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=2
-DSPDK_GIT_COMMIT=4be6d3043
-pthread -Wall -Wextra -Wno-unused-variable -Wno-unused-parameter -Wno-missing-field-initializers -Wformat -Wformat-security
-Ideps/spdk-v22.09/include -Ideps/spdk-v22.09/isa-l/.. -Ideps/spdk-v22.09/dpdk/build/include
-Iposeidon -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src -c sealing/supra_seal.cpp -o obj/supra_seal.o -Wno-subobject-linkage

---

-#cgo LDFLAGS:
-Wl,-z,noexecstack
-Wl,--no-as-needed
-lstdc++
-L${SRCDIR}/../../extern/supra_seal/obj
-L${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/build/lib
-L${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/isa-l/.libs
-lsupraseal
-lspdk_bdev_malloc
-lspdk_bdev_null
-lspdk_bdev_nvme
-lspdk_bdev_passthru
-lspdk_bdev_lvol
-lspdk_bdev_raid
-lspdk_bdev_error
-lspdk_bdev_gpt
-lspdk_bdev_split
-lspdk_bdev_delay
-lspdk_bdev_zone_block
-lspdk_blobfs_bdev
-lspdk_blobfs
-lspdk_blob_bdev
-lspdk_lvol
-lspdk_blob
-lspdk_nvme
-lspdk_bdev_ftl
-lspdk_ftl
-lspdk_bdev_aio
-lspdk_bdev_virtio
-lspdk_virtio
-lspdk_vfio_user
-lspdk_accel_ioat
-lspdk_ioat
-lspdk_scheduler_dynamic
-lspdk_env_dpdk
-lspdk_scheduler_dpdk_governor
-lspdk_scheduler_gscheduler
-lspdk_sock_posix
-lspdk_event
-lspdk_event_bdev
-lspdk_bdev
-lspdk_notify
-lspdk_dma
-lspdk_event_accel
-lspdk_accel
-lspdk_event_vmd
-lspdk_vmd
-lspdk_event_sock
-lspdk_init
-lspdk_thread
-lspdk_trace
-lspdk_sock
-lspdk_rpc
-lspdk_jsonrpc
-lspdk_json
-lspdk_util
-lspdk_log
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/build/lib/libspdk_env_dpdk.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_bus_pci.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_cryptodev.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_dmadev.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_eal.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_ethdev.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_hash.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_kvargs.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_mbuf.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_mempool.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_mempool_ring.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_net.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_pci.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_power.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_rcu.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_ring.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_telemetry.a
${SRCDIR}/../../extern/supra_seal/deps/spdk-v22.09/dpdk/build/lib/librte_vhost.a
-lnuma
-lisal
-pthread
-ldl
-lrt
-luuid
-lssl
-lcrypto
-lm
-laio
-lcudart_static
-L${SRCDIR}/../../extern/supra_seal/deps/blst -lblst
-lconfig++
-lgmp

*/

// SupraSealInit initializes the supra seal with a sector size and optional config file.
func SupraSealInit(sectorSize uint64, configFile string) {
	cConfigFile := C.CString(configFile)
	defer C.free(unsafe.Pointer(cConfigFile))
	C.supra_seal_init(C.size_t(sectorSize), cConfigFile)
}

// Pc1 performs the pc1 operation.
func Pc1(blockOffset uint64, replicaIDs [][32]byte, parentsFilename string, sectorSize uint64) int {
	flatReplicaIDs := make([]byte, len(replicaIDs)*32)
	for i, id := range replicaIDs {
		copy(flatReplicaIDs[i*32:], id[:])
	}
	numSectors := len(replicaIDs)

	cReplicaIDs := (*C.uint8_t)(unsafe.Pointer(&flatReplicaIDs[0]))
	cParentsFilename := C.CString(parentsFilename)
	defer C.free(unsafe.Pointer(cParentsFilename))
	return int(C.pc1(C.uint64_t(blockOffset), C.size_t(numSectors), cReplicaIDs, cParentsFilename, C.size_t(sectorSize)))
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
	/*
		int pc2(size_t block_offset, size_t num_sectors, const char* output_dir,
		        const char** data_filenames, size_t sector_size);
	*/
	cOutputDir := C.CString(outputDir)
	defer C.free(unsafe.Pointer(cOutputDir))

	// data filenames is for unsealed data to be encoded
	// https://github.com/supranational/supra_seal/blob/a64e4060fbffea68adc0ac4512062e5a03e76048/pc2/cuda/pc2.cu#L329
	// not sure if that works correctly, but that's where we could encode data in the future
	// for now pass a null as the pointer to the array of filenames

	var cDataFilenames **C.char
	cDataFilenames = nil

	return int(C.pc2(C.size_t(blockOffset), C.size_t(numSectors), cOutputDir, cDataFilenames, C.size_t(sectorSize)))
}

// Pc2Cleanup deletes files associated with pc2.
func Pc2Cleanup(numSectors int, outputDir string, sectorSize uint64) int {
	cOutputDir := C.CString(outputDir)
	defer C.free(unsafe.Pointer(cOutputDir))
	return int(C.pc2_cleanup(C.size_t(numSectors), cOutputDir, C.size_t(sectorSize)))
}

// C1 performs the c1 operation.
// Outputs to cachePath/commit-phase1-output
func C1(blockOffset uint64, numSectors, sectorSlot int, replicaID, seed, ticket []byte, cachePath, parentsFilename, replicaPath string, sectorSize uint64) int {
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
func GetMaxBlockOffset(sectorSize uint64) uint64 {
	return uint64(C.get_max_block_offset(C.size_t(sectorSize)))
}

// GetSlotSize returns the size in blocks required for the given number of sectors.
func GetSlotSize(numSectors int, sectorSize uint64) uint64 {
	return uint64(C.get_slot_size(C.size_t(numSectors), C.size_t(sectorSize)))
}

// GetCommCFromTree returns comm_c after calculating from tree file(s).
func GetCommCFromTree(commC []byte, cachePath string, sectorSize uint64) bool {
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
func GetCommRLastFromTree(commRLast []byte, cachePath string, sectorSize uint64) bool {
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
