//go:build localnet
// +build localnet

package build

import (
	"os"
	"strconv"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

// BlockDelaySecs can be overridden via CURIO_LOCALNET_BLOCK_DELAY env var
var BlockDelaySecs = getEnvUint64("CURIO_LOCALNET_BLOCK_DELAY", 4)

// EquivocationDelaySecs can be overridden via CURIO_LOCALNET_EQUIVOCATION_DELAY env var
var EquivocationDelaySecs = getEnvUint64("CURIO_LOCALNET_EQUIVOCATION_DELAY", 0)

// PropagationDelaySecs can be overridden via CURIO_LOCALNET_PROPAGATION_DELAY env var
const PropagationDelaySecs = uint64(1)

var UpgradeSmokeHeight = abi.ChainEpoch(-1)

func init() {
	// Default to Testnet address network, but allow override via CURIO_LOCALNET_ADDRESS_NETWORK
	// Valid values: "mainnet", "testnet"
	addressNetwork := os.Getenv("CURIO_LOCALNET_ADDRESS_NETWORK")
	switch addressNetwork {
	case "mainnet":
		SetAddressNetwork(address.Mainnet)
	case "testnet", "":
		SetAddressNetwork(address.Testnet)
	default:
		// Default to testnet for unknown values
		SetAddressNetwork(address.Testnet)
	}

	BuildType = BuildLocalnet
}

func getEnvUint64(key string, defaultVal uint64) uint64 {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.ParseUint(val, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultVal
}
