package contract

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/filecoin-project/curio/build"
)

type PDPContracts struct {
	PDPService      common.Address
	PDPRecordKeeper common.Address
}

func ContractAddresses() PDPContracts {
	switch build.BuildType {
	case build.BuildCalibnet:
		return PDPContracts{
			PDPService:      common.HexToAddress("0xD4830739a0B262A55Fa6747AA6e8B149756bbA36"),
			PDPRecordKeeper: common.HexToAddress("0xE4561Db8404Ed91bD6804Fad610ac1cC721bF7C1"),
		}
	default:
		panic("pdp contracts unknown for this network")
	}
}
