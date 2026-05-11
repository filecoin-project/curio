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
	default:
		return "+huh?"
	}
}

// Intent: Major.Network.Patch
var BuildVersionArray = [3]int{1, 28, 0}

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

// CommitIDPrefix returns the first 7 characters of the git commit id embedded in
// CurrentCommit (+git_<hash>_<committer ISO time>). Empty when no commit segment is present.
func CommitIDPrefix() string {
	const pfx = "+git_"
	if !strings.HasPrefix(CurrentCommit, pfx) {
		return ""
	}
	rest := strings.TrimPrefix(CurrentCommit, pfx)
	if i := strings.Index(rest, "_"); i >= 0 {
		rest = rest[:i]
	}
	if len(rest) > 7 {
		return rest[:7]
	}
	return rest
}

// ClusterMachineVersionLabel returns the dotted release (BuildVersion) plus an optional short git hash for cluster UI, e.g. "1.27.4 abcdef1".
func ClusterMachineVersionLabel() string {
	v := BuildVersion
	if g := CommitIDPrefix(); g != "" {
		return v + " " + g
	}
	return v
}
