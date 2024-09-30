package contract

import (
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/build"
)

type PDPContracts struct {
	PDPService      string
	PDPRecordKeeper string
}

func ContractAddresses() PDPContracts {
	switch build.BuildType {
	case build.BuildCalibnet:
		return PDPContracts{
			PDPService:      "0x3971B35597849701F6535300e537745bfdfc67b1",
			PDPRecordKeeper: "0x32fb106CF8F1C6DD2FD177e5c06523104b28B8EA",
		}
	default:
		panic("pdp contracts unknown for this network")
	}
}

func SetupContractAccess() {
	// todo: create loopback go-jsonrpc server into the lotus API with DialIO + something
	ethclient.Dial("https://api.calibration.node.glif.io/rpc/v1")
}
