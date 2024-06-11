package build

import (
	"github.com/filecoin-project/go-address"

	lbuild "github.com/filecoin-project/lotus/build"
)

func SetAddressNetwork(n address.Network) {
	address.CurrentNetwork = n
}

var BlockDelaySecs = lbuild.BlockDelaySecs
