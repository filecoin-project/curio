package multicall

import (
	"os"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
)

const MultiCallAddressMainnet = "0xcA11bde05977b3631167028862bE2a173976CA11"
const MultiCallAddressCalibnet = "0xcA11bde05977b3631167028862bE2a173976CA11"

func MultiCallAddress() (common.Address, error) {
	switch build.BuildType {
	case build.BuildCalibnet:
		return common.HexToAddress(MultiCallAddressCalibnet), nil
	case build.BuildMainnet:
		return common.HexToAddress(MultiCallAddressMainnet), nil
	case build.Build2k:
		// For localnet, use env var CURIO_LOCALNET_MULTICALL_ADDRESS
		if addr := os.Getenv("CURIO_LOCALNET_MULTICALL_ADDRESS"); addr != "" {
			return common.HexToAddress(addr), nil
		}
		return common.Address{}, xerrors.Errorf("multicall address not configured for localnet - set CURIO_LOCALNET_MULTICALL_ADDRESS env var")
	default:
		return common.Address{}, xerrors.Errorf("multicall address not set for this network %s", build.BuildTypeString()[1:])
	}
}
