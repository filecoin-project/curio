package filecoinpayment

import (
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
)

const PaymentContractMainnet = ""
const PaymentContractCalibnet = ""

func PaymentContractAddress() (common.Address, error) {
	switch build.BuildType {
	case build.BuildCalibnet:
		return common.HexToAddress(PaymentContractCalibnet), nil
	case build.BuildMainnet:
		return common.HexToAddress(PaymentContractMainnet), nil
	default:
		return common.Address{}, xerrors.Errorf("payment contract address not set for this network %s", build.BuildTypeString()[1:])
	}
}
