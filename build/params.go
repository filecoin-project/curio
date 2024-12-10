package build

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

func SetAddressNetwork(n address.Network) {
	address.CurrentNetwork = n
}

const TicketRandomnessLookback = abi.ChainEpoch(1)

const (
	NetworkMainnet    = "mainnet"
	NetworkCalibnet   = "calibnet"
	NetworkTestnet    = "testnet"
)

func CurrentNetwork() string {
	switch BuildType {
	case BuildMainnet:
		return NetworkMainnet
	case BuildCalibnet:
		return NetworkCalibnet
	case Build2k:
		return NetworkTestnet
	default:
		return "unknown"
	}
}
