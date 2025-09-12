package contract

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"
)

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
