package contract

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/snadrus/must"

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
			PDPVerifier: common.HexToAddress("0x445238Eca6c6aB8Dff1Aa6087d9c05734D22f137"),
			AllowedPublicRecordKeepers: []common.Address{
				common.HexToAddress("0x80617b65FD2EEa1D7fDe2B4F85977670690ed348"), // FilecoinWarmStorageService
			},
		}
	case build.BuildMainnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0x1790d465d1FABE85b530B116f385091d52a12a3b"),
			AllowedPublicRecordKeepers: []common.Address{
				common.HexToAddress("0x6170dE2b09b404776197485F3dc6c968Ef948505"), // SimplePDPService
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
