//go:build debug
// +build debug

package build

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

const BlockDelaySecs = uint64(4)

var EquivocationDelaySecs = uint64(0)

const PropagationDelaySecs = uint64(1)

var InsecurePoStValidation = true

var UpgradeSmokeHeight = abi.ChainEpoch(-1)

func init() {
	SetAddressNetwork(address.Testnet)
	BuildType = BuildDebug
}
