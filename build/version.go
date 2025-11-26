package build

import (
	"os"
	"strconv"
	"strings"

	"github.com/samber/lo"
)

// /////START BUILD_TIME POPULATED VARS///////
var IsOpencl string

var CurrentCommit string

// /////END BUILD_TIME POPULATED VARS///////

// Populated by Params
var BuildType int

const (
	BuildMainnet  = 0x1
	Build2k       = 0x2
	BuildDebug    = 0x3
	BuildCalibnet = 0x4
	BuildLocalnet = 0x5
)

func BuildTypeString() string {
	switch BuildType {
	case BuildMainnet:
		return "+mainnet"
	case Build2k:
		return "+2k"
	case BuildDebug:
		return "+debug"
	case BuildCalibnet:
		return "+calibnet"
	case BuildLocalnet:
		return "+localnet"
	default:
		return "+huh?"
	}
}

// Intent: Major.Network.Patch
var BuildVersionArray = [3]int{1, 27, 0}

// RC
var BuildVersionRC = 0

// Ex: "1.2.3" or "1.2.3-rcX"
var BuildVersion string

func init() {
	version := strings.Join(lo.Map(BuildVersionArray[:],
		func(i int, _ int) string { return strconv.Itoa(i) }), ".")

	if BuildVersionRC > 0 {
		version += "-rc" + strconv.Itoa(BuildVersionRC)
	}
	BuildVersion = version
}

func UserVersion() string {
	if os.Getenv("CURIO_VERSION_IGNORE_COMMIT") == "1" {
		return BuildVersion
	}
	return BuildVersion + BuildTypeString() + CurrentCommit
}
