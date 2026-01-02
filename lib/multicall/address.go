package multicall

import (
	"fmt"
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
		multicallAddr := os.Getenv("FOC_CONTRACT_MULTICALL")
		if multicallAddr == "" {
			return common.Address{}, xerrors.Errorf("FOC_CONTRACT_MULTICALL environment variable must be set for 2k")
		}
		if !common.IsHexAddress(multicallAddr) {
			return common.Address{}, xerrors.Errorf("FOC_CONTRACT_MULTICALL must be a valid hex address")
		}
		fmt.Fprintf(os.Stderr, "[2K] FOC_CONTRACT_MULTICALL=%s\n", multicallAddr)
		return common.HexToAddress(multicallAddr), nil
	default:
		return common.Address{}, xerrors.Errorf("multicall address not set for this network %s", build.BuildTypeString()[1:])
	}
}
