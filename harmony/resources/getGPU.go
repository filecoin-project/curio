//go:build !darwin

package resources

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
)

var GpuOverprovisionFactor = 1

func init() {
	if nstr := os.Getenv("HARMONY_GPU_OVERPROVISION_FACTOR"); nstr != "" {
		n, err := strconv.Atoi(nstr)
		if err != nil {
			logger.Errorf("parsing HARMONY_GPU_OVERPROVISION_FACTOR failed: %+v", err)
		} else {
			GpuOverprovisionFactor = n
		}
	}
}

func getGPUDevices() (float64, error) { // GPU boolean
	if nstr := os.Getenv("HARMONY_OVERRIDE_GPUS"); nstr != "" {
		n, err := strconv.ParseFloat(nstr, 64)
		if err != nil {
			logger.Errorf("parsing HARMONY_OVERRIDE_GPUS failed: %+v", err)
		} else {
			return n, nil
		}
	}

	gpus, err := ffi.GetGPUDevices()
	if err != nil {
		logger.Errorf("getting gpu devices failed: %+v", err)
		return 0, xerrors.Errorf("getting gpu devices failed: %w", err)
	}
	logger.Infow("GPUs", "list", gpus, "overprovision_factor", GpuOverprovisionFactor)
	all := strings.ToLower(strings.Join(gpus, ","))
	if len(gpus) > 1 || strings.Contains(all, "ati") || strings.Contains(all, "nvidia") {
		return float64(len(gpus) * GpuOverprovisionFactor), nil
	}
	return 0, nil
}
