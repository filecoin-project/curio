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
			PDPVerifier: common.HexToAddress("0x06279D540BDCd6CA33B073cEAeA1425B6C68c93d"), // PDPVerifier Proxy v3.0.0 - https://github.com/FilOzone/filecoin-services/pull/311
			AllowedPublicRecordKeepers: []common.Address{
				common.HexToAddress("0x92B51cefF7eBc721Ad0F1fB09505E75F67DCAac6"), // Simple
				common.HexToAddress("0xD3De778C05f89e1240ef70100Fb0d9e5b2eFD258"), // FWSS Proxy - https://github.com/FilOzone/filecoin-services/pull/311 (2025-10-21)

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
const ServiceRegistryCalibnet = "0x97Dd879F5a97A8c761B94746d7F5cfF50AAd4452" // FWSS Registry Proxy - https://github.com/FilOzone/filecoin-services/pull/311

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
