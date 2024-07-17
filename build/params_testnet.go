//go:build debug
// +build debug

package build

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/specs-actors/v2/actors/builtin"
)

const BlockDelaySecs = builtin.EpochDurationSeconds

func init() {
	SetAddressNetwork(address.Testnet)
	BuildType = BuildDebug
}
