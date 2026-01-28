package resources

import (
	"fmt"
	"os"
	"strconv"

	"github.com/samber/lo"

	"github.com/filecoin-project/curio/harmony/resources/miniopencl"
)

// GPURamTaskSize is the amount of RAM required for a GPU task in bytes.
// It is used to assure that each task has this much RAM available.
// NOTE: This transitional value will be replaced by returning []GpuRam once tasks report their GpuRAM needs.
var GPURamTaskSize = 10 << 40

func GetGpuProvisioning() ([]byte, error) {
	if nstr := os.Getenv("HARMONY_OVERRIDE_GPUS"); nstr != "" { // Dev path.
		n, err := strconv.ParseInt(nstr, 10, 8)
		if err != nil {
			logger.Errorf("parsing HARMONY_OVERRIDE_GPUS failed: %+v", err)
		} else {
			return []byte{byte(n)}, nil
		}
	}

	platforms, err := miniopencl.GetPlatforms()
	if err != nil {
		return nil, err
	}

	var devices []*miniopencl.Device
	for _, p := range platforms {
		devicesTmp, err := p.GetAllDevices()
		if err != nil {
			logger.Errorf("getting gpu devices failed for platform %v: %+v", p, err)
			continue
		}
		devices = append(devices, devicesTmp...)
	}

	slotsCalc := func(item byte, index int) byte { // auto-provision based on device memory
		return byte(devices[index].GlobalMemSize() >> GPURamTaskSize)
	}

	if nstr := os.Getenv("HARMONY_GPU_OVERPROVISION_FACTOR"); nstr != "" { // Legacy / Bugfix path.
		GpuOverprovisionFactor, err := strconv.ParseInt(nstr, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("parsing HARMONY_GPU_OVERPROVISION_FACTOR failed: %+v", err)
		}
		slotsCalc = func(item byte, index int) byte {
			return byte(GpuOverprovisionFactor)
		}
	}
	return lo.Map(make([]byte, len(devices)), slotsCalc), nil
}

func GetGpuSlotsCount() (int, error) {
	p, err := GetGpuProvisioning()
	if err != nil {
		return 0, err
	}
	return int(lo.Sum(p)), nil
}
