//go:build !skiff && !darwin

package ffigpu

import (
	"strings"

	ffi "github.com/filecoin-project/filecoin-ffi"
)

// gpuCount reports the number of usable GPUs (scaled by GpuOverprovisionFactor),
// detected via filecoin-ffi. HARMONY_OVERRIDE_GPUS, if set, is applied by
// resources.System and takes precedence over this value.
func gpuCount() float64 {
	gpus, err := ffi.GetGPUDevices()
	logger.Infow("GPUs", "list", gpus, "overprovision_factor", GpuOverprovisionFactor)
	if err != nil {
		logger.Errorf("getting gpu devices failed: %+v", err)
	}
	all := strings.ToLower(strings.Join(gpus, ","))
	if len(gpus) > 1 || strings.Contains(all, "ati") || strings.Contains(all, "nvidia") {
		return float64(len(gpus) * GpuOverprovisionFactor)
	}
	return 0
}
