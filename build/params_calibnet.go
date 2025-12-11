//go:build calibnet

package build

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin"
)

const BlockDelaySecs = builtin.EpochDurationSeconds

var EquivocationDelaySecs = uint64(2)
var PropagationDelaySecs = uint64(10)

const UpgradeSmokeHeight = -2

func init() {
	SetAddressNetwork(address.Testnet)
	BuildType = BuildCalibnet
}
