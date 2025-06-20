package contract

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/filecoin-project/curio/build"
)

// AllowedRecordKeepers returns the whitelist of allowed recordkeeper addresses
// for the public PDP service based on the build type
func AllowedRecordKeepers() []common.Address {
	switch build.BuildType {
	case build.BuildCalibnet:
		return []common.Address{
			common.HexToAddress("0xf49ba5eaCdFD5EE3744efEdf413791935FE4D4c5"),
		}
	case build.BuildMainnet:
		// Add mainnet allowed contracts here when ready
		return []common.Address{}
	default:
		// For other networks (debug, 2k, etc), allow any recordkeeper
		return nil
	}
}

// IsPublicService checks if a service label indicates a public service
func IsPublicService(serviceLabel string) bool {
	return serviceLabel == "public"
}

// IsRecordKeeperAllowed checks if a recordkeeper address is in the whitelist
// Returns true if the address is allowed, or if there's no whitelist for the network
func IsRecordKeeperAllowed(recordKeeper common.Address) bool {
	allowedList := AllowedRecordKeepers()
	
	// If allowedList is nil, it means any recordkeeper is allowed
	if allowedList == nil {
		return true
	}
	
	// If allowedList is empty (but not nil), no recordkeepers are allowed
	if len(allowedList) == 0 {
		return false
	}
	
	// Check if the recordkeeper is in the whitelist
	for _, allowed := range allowedList {
		if recordKeeper == allowed {
			return true
		}
	}
	
	return false
}