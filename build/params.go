package build

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

func SetAddressNetwork(n address.Network) {
	address.CurrentNetwork = n
}

const TicketRandomnessLookback = abi.ChainEpoch(1)
