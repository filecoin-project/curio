package multicall

import (
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
)

const MultiCallAddressMainnet = ""
const MultiCallAddressCalibnet = ""

func MultiCallAddress() (common.Address, error) {
	switch build.BuildType {
	case build.BuildCalibnet:
		return common.HexToAddress(MultiCallAddressCalibnet), nil
	case build.BuildMainnet:
		return common.HexToAddress(MultiCallAddressMainnet), nil
	default:
		return common.Address{}, xerrors.Errorf("multicall address not set for this network %s", build.BuildTypeString()[1:])
	}
}
