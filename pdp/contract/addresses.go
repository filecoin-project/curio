package contract

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"

	"github.com/filecoin-project/lotus/chain/types"
)

type PDPContracts struct {
	PDPVerifier                common.Address
	AllowedPublicRecordKeepers []common.Address
}

func ContractAddresses() PDPContracts {
	switch build.BuildType {
	case build.BuildCalibnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0x579dD9E561D4Cd1776CF3e52E598616E77D5FBcb"), // PDPVerifier Proxy v2.2.1 - https://github.com/FilOzone/pdp/releases/tag/v2.2.1
			AllowedPublicRecordKeepers: []common.Address{
				common.HexToAddress("0x92B51cefF7eBc721Ad0F1fB09505E75F67DCAac6"), // Simple
				common.HexToAddress("0x80617b65FD2EEa1D7fDe2B4F85977670690ed348"), // FilecoinWarmStorageService Proxy v0.1.0 - https://github.com/FilOzone/filecoin-services/releases/tag/alpha%2Fcalibnet%2F0x80617b65FD2EEa1D7fDe2B4F85977670690ed348-v2
				common.HexToAddress("0x468342072e0dc86AFFBe15519bc5B1A1aa86e4dc"), // FilecoinWarmStorageService Proxy v0.3.0 - https://github.com/FilOzone/filecoin-services/releases/tag/v0.3.0
			},
		}
	case build.BuildMainnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0x1790d465d1FABE85b530B116f385091d52a12a3b"),
			AllowedPublicRecordKeepers: []common.Address{
				common.HexToAddress("0x81DFD9813aDd354f03704F31419b0c6268d46232"), // FilecoinWarmStorageService
			},
		}
	default:
		panic("PDP contract unknown for this network")
	}
}

const NumChallenges = 5

func SybilFee() *big.Int {
	return must.One(types.ParseFIL("0.1")).Int
}

// IsPublicService checks if a service label indicates a public service
func IsPublicService(serviceLabel string) bool {
	return serviceLabel == "public"
}

// IsRecordKeeperAllowed checks if a recordkeeper address is in the whitelist
// Returns true if the address is allowed, or if there's no whitelist for the network
func IsRecordKeeperAllowed(recordKeeper common.Address) bool {
	// Check if the recordkeeper is in the whitelist
	for _, allowed := range ContractAddresses().AllowedPublicRecordKeepers {
		if recordKeeper == allowed {
			return true
		}
	}
	return false
}

const ServiceRegistryMainnet = "0x9C65E8E57C98cCc040A3d825556832EA1e9f4Df6"
const ServiceRegistryCalibnet = "0xA8a7e2130C27e4f39D1aEBb3D538D5937bCf8ddb"

func ServiceRegistryAddress() (common.Address, error) {
	switch build.BuildType {
	case build.BuildCalibnet:
		return common.HexToAddress(ServiceRegistryCalibnet), nil
	case build.BuildMainnet:
		return common.HexToAddress(ServiceRegistryMainnet), nil
	default:
		return common.Address{}, xerrors.Errorf("service registry address not set for this network %s", build.BuildTypeString()[1:])
	}
}

const USDFCAddressMainnet = "0x80B98d3aa09ffff255c3ba4A241111Ff1262F045"
const USDFCAddressCalibnet = "0xb3042734b608a1B16e9e86B374A3f3e389B4cDf0"

func USDFCAddress() (common.Address, error) {
	switch build.BuildType {
	case build.BuildCalibnet:
		return common.HexToAddress(USDFCAddressCalibnet), nil
	case build.BuildMainnet:
		return common.HexToAddress(USDFCAddressMainnet), nil
	default:
		return common.Address{}, xerrors.Errorf("USDFC address not set for this network %s", build.BuildTypeString()[1:])
	}
}
