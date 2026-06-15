//go:build pdp && !darwin

package resources

import (
	"os"
	"strconv"
)

func getGPUDevices() float64 {
	if nstr := os.Getenv("HARMONY_OVERRIDE_GPUS"); nstr != "" {
		n, err := strconv.ParseFloat(nstr, 64)
		if err != nil {
			logger.Errorf("parsing HARMONY_OVERRIDE_GPUS failed: %+v", err)
		} else {
			return n
		}
	}
	return 0
}
