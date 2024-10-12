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
			PDPService:      common.HexToAddress("0x7B45eD1333f7cAf58A9F6A47226d5aB385847774"),
			PDPRecordKeeper: common.HexToAddress("0x674Cb3aaC68740D32eD05e583df96F3dF810b439"),
		}
	default:
		panic("pdp contracts unknown for this network")
	}
}
