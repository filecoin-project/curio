package contract

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/filecoin-project/curio/build"
)

type PDPContracts struct {
	PDPVerifier common.Address
}

func ContractAddresses() PDPContracts {
	switch build.BuildType {
	case build.BuildCalibnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0xD049b26b88BEf47E505C2Ba41209fEC2E11b910E"),
		}
	default:
		panic("pdp contracts unknown for this network")
	}
}
