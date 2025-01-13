//go:build 2k
// +build 2k

package build

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

const BlockDelaySecs = uint64(4)

var EquivocationDelaySecs = uint64(0)

const PropagationDelaySecs = uint64(1)

var UpgradeSmokeHeight = abi.ChainEpoch(-1)

func init() {
	SetAddressNetwork(address.Testnet)
	BuildType = Build2k
}
