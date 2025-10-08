package contract

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	logger "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

var log = logger.Logger("pdp-contract")

// GetProvingScheduleFromListener checks if a listener has a view contract and returns
// an IPDPProvingSchedule instance bound to the appropriate address.
// It uses the view contract address if available, otherwise uses the listener address directly.
func GetProvingScheduleFromListener(listenerAddr common.Address, ethClient *ethclient.Client) (*IPDPProvingSchedule, error) {
	// Try to get the view contract address from the listener
	provingScheduleAddr := listenerAddr

	// Check if the listener supports the viewContractAddress method
	listenerService, err := NewListenerServiceWithViewContract(listenerAddr, ethClient)
	if err == nil {
		// Try to get the view contract address
		viewAddr, err := listenerService.ViewContractAddress(nil)
		if err == nil && viewAddr != (common.Address{}) {
			// Use the view contract for proving schedule operations
			provingScheduleAddr = viewAddr
		}
	}

	// Create and return the IPDPProvingSchedule binding
	// This works whether provingScheduleAddr points to:
	// - The view contract (which must implement IPDPProvingSchedule)
	// - The listener itself (where listener must implement IPDPProvingSchedule)
	provingSchedule, err := NewIPDPProvingSchedule(provingScheduleAddr, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("failed to create proving schedule binding: %w", err)
	}

	return provingSchedule, nil
}

func GetDataSetMetadataAtKey(listenerAddr common.Address, ethClient *ethclient.Client, dataSetId *big.Int, key string) (bool, string, error) {
	metadataAddr := listenerAddr

	// Check if the listener supports the viewContractAddress method
	listenerService, err := NewListenerServiceWithViewContract(listenerAddr, ethClient)
	if err == nil {
		viewAddr, err := listenerService.ViewContractAddress(nil)
		if err == nil && viewAddr != (common.Address{}) {
			metadataAddr = viewAddr
		}
	}

	// Create a metadata service viewer.
	mDataService, err := NewListenerServiceWithMetaData(metadataAddr, ethClient)
	if err != nil {
		log.Debugw("Failed to create a meta data service from listener, returning metadata not found", "error", err)
		return false, "", nil
	}
	out, err := mDataService.GetDataSetMetadata(nil, dataSetId, key)
	return out.Exists, out.Value, err
}
