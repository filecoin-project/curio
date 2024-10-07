package contract

import (
	"context"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

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

func SetupContractAccess() error {

	chainID, err := ec.NetworkID(context.Background())
	if err != nil {
		return xerrors.Errorf("getting chain ID: %w", err)
	}

	ca := ContractAddresses()

	pdpService, err := NewPDPService(ca.PDPService, ec)
	if err != nil {
		return xerrors.Errorf("creating PDP service: %w", err)
	}

	pdpService.CreatePofSet()
}
