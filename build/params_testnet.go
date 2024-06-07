//go:build debug
// +build debug

package build

import (
	"github.com/filecoin-project/go-address"
)

func init() {
	SetAddressNetwork(address.Testnet)
	BuildType = BuildDebug
}
