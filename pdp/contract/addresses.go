package contract

import (
	"math/big"
	"os"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"

	"github.com/filecoin-project/lotus/chain/types"
)

type PDPContracts struct {
	PDPVerifier                common.Address
	AllowedPublicRecordKeepers RecordKeeperAddresses
}

type RecordKeeperAddresses struct {
	FWSService common.Address
	Simple     common.Address
}

var (
	cachedContracts       *PDPContracts
	cachedServiceRegistry *common.Address
	cachedUSDFC           *common.Address
	cacheMu               sync.RWMutex
)

func (a RecordKeeperAddresses) List() []common.Address {
	return []common.Address{a.FWSService, a.Simple}
}

func ContractAddresses() PDPContracts {
	switch build.BuildType {
	case build.BuildCalibnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0x85e366Cf9DD2c0aE37E963d9556F5f4718d6417C"), // PDPVerifier Proxy v3.1.0 - https://github.com/FilOzone/pdp/releases/tag/v3.1.0
			AllowedPublicRecordKeepers: RecordKeeperAddresses{
				FWSService: common.HexToAddress("0x02925630df557F957f70E112bA06e50965417CA0"), // FWSS Proxy - https://github.com/FilOzone/filecoin-services/releases/tag/v1.0.0
			},
		}
	case build.BuildMainnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0xBADd0B92C1c71d02E7d520f64c0876538fa2557F"), // PDPVerifier Proxy v3.1.0 - https://github.com/FilOzone/pdp/releases/tag/v3.1.0
			AllowedPublicRecordKeepers: RecordKeeperAddresses{
				FWSService: common.HexToAddress("0x8408502033C418E1bbC97cE9ac48E5528F371A9f"), // FWSS Proxy - https://github.com/FilOzone/filecoin-services/releases/tag/v1.0.0
			},
		}
	case build.BuildLocalnet:
		// Check cache first
		cacheMu.RLock()
		if cachedContracts != nil {
			defer cacheMu.RUnlock()
			return *cachedContracts
		}
		cacheMu.RUnlock()

		// Cache miss, load from env vars
		cacheMu.Lock()
		defer cacheMu.Unlock()

		// Double-check after acquiring write lock
		if cachedContracts != nil {
			return *cachedContracts
		}

		// For localnet, use env vars FOC_LOCALNET_*
		pdpVerifier := os.Getenv("FOC_LOCALNET_PDP_VERIFIER")
		if pdpVerifier == "" {
			panic("FOC_LOCALNET_PDP_VERIFIER env var not set for localnet")
		}

		contracts := PDPContracts{
			PDPVerifier: common.HexToAddress(pdpVerifier),
		}

		// Optional record keepers
		if fwsService := os.Getenv("FOC_LOCALNET_FWS_SERVICE"); fwsService != "" {
			contracts.AllowedPublicRecordKeepers.FWSService = common.HexToAddress(fwsService)
		}
		if simple := os.Getenv("FOC_LOCALNET_SIMPLE_RECORD_KEEPER"); simple != "" {
			contracts.AllowedPublicRecordKeepers.Simple = common.HexToAddress(simple)
		}

		// Cache the result
		cachedContracts = &contracts
		return contracts
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
	for _, allowed := range ContractAddresses().AllowedPublicRecordKeepers.List() {
		if recordKeeper == allowed {
			return true
		}
	}
	return false
}

const ServiceRegistryMainnet = "0xf55dDbf63F1b55c3F1D4FA7e339a68AB7b64A5eB"  // ServiceProviderRegistry Proxy - https://github.com/FilOzone/filecoin-services/releases/tag/v1.0.0
const ServiceRegistryCalibnet = "0x839e5c9988e4e9977d40708d0094103c0839Ac9D" // ServiceProviderRegistry Proxy - https://github.com/FilOzone/filecoin-services/releases/tag/v1.0.0

func ServiceRegistryAddress() (common.Address, error) {
	switch build.BuildType {
	case build.BuildCalibnet:
		return common.HexToAddress(ServiceRegistryCalibnet), nil
	case build.BuildMainnet:
		return common.HexToAddress(ServiceRegistryMainnet), nil
	case build.BuildLocalnet:
		// Check cache first
		cacheMu.RLock()
		if cachedServiceRegistry != nil {
			defer cacheMu.RUnlock()
			return *cachedServiceRegistry, nil
		}
		cacheMu.RUnlock()

		// Cache miss, load from env var
		cacheMu.Lock()
		defer cacheMu.Unlock()

		// Double-check after acquiring write lock
		if cachedServiceRegistry != nil {
			return *cachedServiceRegistry, nil
		}

		// For localnet, use env var FOC_LOCALNET_SERVICE_REGISTRY
		if addr := os.Getenv("FOC_LOCALNET_SERVICE_REGISTRY"); addr != "" {
			address := common.HexToAddress(addr)
			cachedServiceRegistry = &address
			return address, nil
		}
		return common.Address{}, xerrors.Errorf("service registry address not configured for localnet - set FOC_LOCALNET_SERVICE_REGISTRY env var")
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
	case build.BuildLocalnet:
		// Check cache first
		cacheMu.RLock()
		if cachedUSDFC != nil {
			defer cacheMu.RUnlock()
			return *cachedUSDFC, nil
		}
		cacheMu.RUnlock()

		// Cache miss, load from env var
		cacheMu.Lock()
		defer cacheMu.Unlock()

		// Double-check after acquiring write lock
		if cachedUSDFC != nil {
			return *cachedUSDFC, nil
		}

		// For localnet, use env var FOC_LOCALNET_USDFC
		if addr := os.Getenv("FOC_LOCALNET_USDFC"); addr != "" {
			address := common.HexToAddress(addr)
			cachedUSDFC = &address
			return address, nil
		}
		return common.Address{}, xerrors.Errorf("USDFC address not configured for localnet - set FOC_LOCALNET_USDFC env var")
	default:
		return common.Address{}, xerrors.Errorf("USDFC address not set for this network %s", build.BuildTypeString()[1:])
	}
}
