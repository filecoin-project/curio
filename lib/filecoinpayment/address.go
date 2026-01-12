package filecoinpayment

import (
	"os"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
)

const PaymentContractMainnet = "0x23b1e018F08BB982348b15a86ee926eEBf7F4DAa"
const PaymentContractCalibnet = "0x09a0fDc2723fAd1A7b8e3e00eE5DF73841df55a0"

func PaymentContractAddress() (common.Address, error) {
	switch build.BuildType {
	case build.BuildCalibnet:
		return common.HexToAddress(PaymentContractCalibnet), nil
	case build.BuildMainnet:
		return common.HexToAddress(PaymentContractMainnet), nil
	case build.Build2k:
		// For localnet, use env var CURIO_LOCALNET_PAYMENT_CONTRACT
		if addr := os.Getenv("CURIO_LOCALNET_PAYMENT_CONTRACT"); addr != "" {
			return common.HexToAddress(addr), nil
		}
		return common.Address{}, xerrors.Errorf("payment contract address not configured for localnet - set CURIO_LOCALNET_PAYMENT_CONTRACT env var")
	default:
		return common.Address{}, xerrors.Errorf("payment contract address not set for this network %s", build.BuildTypeString()[1:])
	}
}
