// Package ffigpu provides Curio's filecoin-ffi-backed GPU probe as a
// resources.ResourceInspector. It is the only piece of the resource-inspection
// path that depends on filecoin-ffi/CGO, so it lives in its own package that
// FFI-free consumers of harmony/resources (and harmony/harmonytask) never
// import — they supply their own inspector instead.
package ffigpu

import (
	"os"
	"strconv"

	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/harmony/resources"
)

var logger = logging.Logger("ffigpu")

// GpuOverprovisionFactor controls how many task slots each physical GPU is
// advertised as. It is read once from HARMONY_GPU_OVERPROVISION_FACTOR. It is
// defined here (build-tag agnostic) so lib/ffiselect can reference it in every
// build configuration.
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

// Inspector is the resources.ResourceInspector used by Curio nodes. It combines
// the FFI-free system probe (resources.System) with an FFI-backed GPU count.
type Inspector struct{}

var _ resources.ResourceInspector = Inspector{}

func (Inspector) GetResources() (resources.Resources, error) {
	return resources.System(gpuCount())
}
