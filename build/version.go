package build

var CurrentCommit string
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

const BuildVersion = "1.22.0"

func UserVersion() string {
	return BuildVersion + BuildTypeString() + CurrentCommit
}
