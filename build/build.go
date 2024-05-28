package build

import (
	"os"

	// TODO remove this dependency
	"github.com/filecoin-project/lotus/build"
)

// IsOpencl is set to the value of FFI_USE_OPENCL
var IsOpencl string

// Format: 8 HEX then underscore then ISO8701 date
// Ex: 4c5e98f28_2024-05-17T18:42:27-04:00
// NOTE: git date for repeatabile builds.
var Commit string

// BuildVersion is the local build version
const BuildVersion = "1.22-dev"

func UserVersion() string {
	if os.Getenv("CURIO_VERSION_IGNORE_COMMIT") == "1" {
		return BuildVersion
	}

	return BuildVersion + build.BuildTypeString() + Commit
}
