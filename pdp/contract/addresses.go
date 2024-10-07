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
			PDPService:      common.HexToAddress("0x3971B35597849701F6535300e537745bfdfc67b1"),
			PDPRecordKeeper: common.HexToAddress("0x32fb106CF8F1C6DD2FD177e5c06523104b28B8EA"),
		}
	default:
		panic("pdp contracts unknown for this network")
	}
}
