//go:build linux

package supraffi

/*
   #cgo CFLAGS: -I${SRCDIR}/../../extern/supraseal/sealing -Ideps/spdk-v24.05/include -Ideps/spdk-v24.05/isa-l/.. -Ideps/spdk-v24.05/dpdk/build/include -fno-omit-frame-pointer
   #cgo linux LDFLAGS: -Wl,-z,noexecstack -Wl,-z,relro,-z,now -L${SRCDIR}/../../extern/supraseal/obj -L${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/build/lib -L${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/isa-l/.libs -L${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/isa-l-crypto/.libs -lsupraseal -Wl,--whole-archive -Wl,--no-as-needed -lspdk_log -lspdk_bdev_malloc -lspdk_bdev_null -lspdk_bdev_nvme -lspdk_bdev_passthru -lspdk_bdev_lvol -lspdk_bdev_raid -lspdk_bdev_error -lspdk_bdev_gpt -lspdk_bdev_split -lspdk_bdev_delay -lspdk_bdev_zone_block -lspdk_blobfs_bdev -lspdk_blobfs -lspdk_blob_bdev -lspdk_lvol -lspdk_blob -lspdk_nvme -lspdk_bdev_ftl -lspdk_ftl -lspdk_bdev_aio -lspdk_bdev_virtio -lspdk_virtio -lspdk_vfio_user -lspdk_accel_ioat -lspdk_ioat -lspdk_scheduler_dynamic -lspdk_env_dpdk -lspdk_scheduler_dpdk_governor -lspdk_scheduler_gscheduler -lspdk_sock_posix -lspdk_event -lspdk_event_bdev -lspdk_bdev -lspdk_notify -lspdk_dma -lspdk_event_accel -lspdk_accel -lspdk_event_vmd -lspdk_vmd -lspdk_event_sock -lspdk_init -lspdk_thread -lspdk_trace -lspdk_sock -lspdk_rpc -lspdk_jsonrpc -lspdk_json -lspdk_util -lspdk_keyring -lspdk_keyring_file -lspdk_keyring_linux -lspdk_event_keyring -Wl,--no-whole-archive ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/build/lib/libspdk_env_dpdk.a -Wl,--whole-archive ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_bus_pci.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_cryptodev.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_dmadev.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_eal.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_ethdev.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_hash.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_kvargs.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_log.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_mbuf.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_mempool.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_mempool_ring.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_net.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_pci.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_power.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_rcu.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_ring.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_telemetry.a ${SRCDIR}/../../extern/supraseal/deps/spdk-v24.05/dpdk/build/lib/librte_vhost.a -Wl,--no-whole-archive -lnuma -lisal -lisal_crypto -pthread -ldl -lrt -luuid -lssl -lcrypto -lm -laio -lfuse3 -larchive -lkeyutils -lcudart_static -L${SRCDIR}/../../extern/supraseal/deps/blst -lblst -lconfig++ -lgmp -lstdc++
   #include <stdint.h>
   #include <stdbool.h>
   #include "supra_seal.h"
   #include <stdlib.h>

typedef struct nvme_health_info {
        uint8_t  critical_warning;
        int16_t  temperature;
        uint8_t  available_spare;
        uint8_t  available_spare_threshold;
        uint8_t  percentage_used;
        uint64_t data_units_read;
        uint64_t data_units_written;
        uint64_t host_read_commands;
        uint64_t host_write_commands;
        uint64_t controller_busy_time;
        uint64_t power_cycles;
        uint64_t power_on_hours;
        uint64_t unsafe_shutdowns;
        uint64_t media_errors;
        uint64_t num_error_info_log_entries;
        uint32_t warning_temp_time;
        uint32_t critical_temp_time;
        int16_t  temp_sensors[8];
  } nvme_health_info_t;

size_t get_nvme_health_info(nvme_health_info_t* health_infos, size_t max_controllers);

*/
import "C"
import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"
)

// GetHealthInfo retrieves health information for all NVMe devices
// This function is only available on Linux systems
func GetHealthInfo() ([]HealthInfo, error) {
	// Allocate space for raw C struct
	const maxControllers = 64
	rawInfos := make([]C.nvme_health_info_t, maxControllers)

	// Get health info from C
	count := C.get_nvme_health_info(
		(*C.nvme_health_info_t)(unsafe.Pointer(&rawInfos[0])),
		C.size_t(maxControllers),
	)

	if count == 0 {
		return nil, fmt.Errorf("no NVMe controllers found")
	}

	// Convert C structs to Go structs
	healthInfos := make([]HealthInfo, count)
	for i := 0; i < int(count); i++ {
		raw := &rawInfos[i]

		// Convert temperature sensors, filtering out unused ones
		sensors := make([]float64, 0, 8)
		for _, temp := range raw.temp_sensors {
			if temp != 0 {
				sensors = append(sensors, float64(temp))
			}
		}

		// todo likely not entirely correct
		healthInfos[i] = HealthInfo{
			CriticalWarning:         byte(raw.critical_warning),
			Temperature:             float64(raw.temperature), // celsius??
			TemperatureSensors:      sensors,
			WarningTempTime:         time.Duration(raw.warning_temp_time) * time.Minute,
			CriticalTempTime:        time.Duration(raw.critical_temp_time) * time.Minute,
			AvailableSpare:          uint8(raw.available_spare),
			AvailableSpareThreshold: uint8(raw.available_spare_threshold),
			PercentageUsed:          uint8(raw.percentage_used),
			DataUnitsRead:           uint64(raw.data_units_read),
			DataUnitsWritten:        uint64(raw.data_units_written),
			HostReadCommands:        uint64(raw.host_read_commands),
			HostWriteCommands:       uint64(raw.host_write_commands),
			ControllerBusyTime:      time.Duration(raw.controller_busy_time) * time.Minute,
			PowerCycles:             uint64(raw.power_cycles),
			PowerOnHours:            time.Duration(raw.power_on_hours) * time.Hour,
			UnsafeShutdowns:         uint64(raw.unsafe_shutdowns),
			MediaErrors:             uint64(raw.media_errors),
			ErrorLogEntries:         uint64(raw.num_error_info_log_entries),
		}
	}

	return healthInfos, nil
}

// SupraSealInit initializes the supra seal with a sector size and optional config file.
// Requires NVMe devices for batch sealing.
func SupraSealInit(sectorSize uint64, configFile string) {
	cConfigFile := C.CString(configFile)
	defer C.free(unsafe.Pointer(cConfigFile))
	C.supra_seal_init(C.size_t(sectorSize), cConfigFile)
}

// Pc1 performs the pc1 operation for batch sealing.
// Requires NVMe devices for layer storage.
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

// Pc2 performs the pc2 operation for batch sealing.
// Requires NVMe devices for layer storage.
func Pc2(blockOffset uint64, numSectors int, outputDir string, sectorSize uint64) int {
	cOutputDir := C.CString(outputDir)
	defer C.free(unsafe.Pointer(cOutputDir))

	// Pass nil for data_filenames (CC sectors only)
	var cDataFilenames **C.char
	return int(C.pc2(C.size_t(blockOffset), C.size_t(numSectors), cOutputDir, cDataFilenames, C.size_t(sectorSize)))
}

// C1 performs the c1 operation for batch sealing.
// Outputs to cachePath/commit-phase1-output
// Requires NVMe devices for batch operations.
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

// GetMaxBlockOffset returns the highest available block offset from NVMe devices.
func GetMaxBlockOffset(sectorSize uint64) uint64 {
	return uint64(C.get_max_block_offset(C.size_t(sectorSize)))
}

// GetSlotSize returns the size in blocks required for the given number of sectors.
// Used for batch sealing with NVMe devices.
func GetSlotSize(numSectors int, sectorSize uint64) uint64 {
	return uint64(C.get_slot_size(C.size_t(numSectors), C.size_t(sectorSize)))
}

// GetCommR returns comm_r after calculating from p_aux file. Returns true on success.
// Used in batch sealing context.
func GetCommR(commR []byte, cachePath string) bool {
	cCommR := (*C.uint8_t)(unsafe.Pointer(&commR[0]))
	cCachePath := C.CString(cachePath)
	defer C.free(unsafe.Pointer(cCachePath))
	return bool(C.get_comm_r(cCommR, cCachePath))
}
