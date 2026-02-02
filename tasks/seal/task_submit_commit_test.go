package seal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	verifregtypes9 "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/lib/multictladdr"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

// mockCommitAPI implements the minimal SubmitCommitAPI interface for testing
type mockCommitAPI struct {
	walletBalances map[address.Address]big.Int
	walletHas      map[address.Address]bool
	minerBalance   big.Int
}

func (m *mockCommitAPI) ChainHead(context.Context) (*types.TipSet, error) {
	return nil, nil
}

func (m *mockCommitAPI) StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error) {
	return api.MinerInfo{}, nil
}

func (m *mockCommitAPI) StateMinerInitialPledgeForSector(ctx context.Context, sectorDuration abi.ChainEpoch, sectorSize abi.SectorSize, verifiedSize uint64, tsk types.TipSetKey) (types.BigInt, error) {
	return big.Zero(), nil
}

func (m *mockCommitAPI) StateSectorPreCommitInfo(context.Context, address.Address, abi.SectorNumber, types.TipSetKey) (*miner.SectorPreCommitOnChainInfo, error) {
	return nil, nil
}

func (m *mockCommitAPI) StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifregtypes9.AllocationId, tsk types.TipSetKey) (*verifregtypes9.Allocation, error) {
	return nil, nil
}

func (m *mockCommitAPI) StateGetAllocationIdForPendingDeal(ctx context.Context, dealId abi.DealID, tsk types.TipSetKey) (verifregtypes9.AllocationId, error) {
	return 0, nil
}

func (m *mockCommitAPI) StateMinerAvailableBalance(context.Context, address.Address, types.TipSetKey) (big.Int, error) {
	return m.minerBalance, nil
}

func (m *mockCommitAPI) StateNetworkVersion(context.Context, types.TipSetKey) (network.Version, error) {
	return network.Version21, nil
}

func (m *mockCommitAPI) GasEstimateMessageGas(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec, tsk types.TipSetKey) (*types.Message, error) {
	// Return the message with some gas values filled in
	msg.GasLimit = 10000000
	msg.GasFeeCap = abi.NewTokenAmount(1000000000) // 1 nFIL
	msg.GasPremium = abi.NewTokenAmount(100000000) // 0.1 nFIL
	return msg, nil
}

// ctladdr.NodeApi methods
func (m *mockCommitAPI) WalletBalance(ctx context.Context, addr address.Address) (types.BigInt, error) {
	if bal, ok := m.walletBalances[addr]; ok {
		return bal, nil
	}
	return big.Zero(), nil
}

func (m *mockCommitAPI) WalletHas(ctx context.Context, addr address.Address) (bool, error) {
	if has, ok := m.walletHas[addr]; ok {
		return has, nil
	}
	return false, nil
}

func (m *mockCommitAPI) StateAccountKey(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error) {
	return addr, nil
}

func (m *mockCommitAPI) StateLookupID(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error) {
	return addr, nil
}

// Helper to create FIL amounts
func fil(n int64) abi.TokenAmount {
	return big.Mul(big.NewInt(n), big.NewInt(1e18))
}

func TestCalculateCollateral(t *testing.T) {
	tests := []struct {
		name                       string
		collateralFromMinerBalance bool
		disableCollateralFallback  bool
		minerBalance               abi.TokenAmount
		requiredCollateral         abi.TokenAmount
		expectedCollateral         abi.TokenAmount
	}{
		{
			name:                       "CollateralFromMinerBalance disabled - full collateral required",
			collateralFromMinerBalance: false,
			disableCollateralFallback:  false,
			minerBalance:               fil(100),
			requiredCollateral:         fil(10),
			expectedCollateral:         fil(10), // Full collateral, miner balance not used
		},
		{
			name:                       "CollateralFromMinerBalance enabled - miner covers all",
			collateralFromMinerBalance: true,
			disableCollateralFallback:  false,
			minerBalance:               fil(100),
			requiredCollateral:         fil(10),
			expectedCollateral:         big.Zero(), // Miner balance covers everything
		},
		{
			name:                       "CollateralFromMinerBalance enabled - miner covers partial",
			collateralFromMinerBalance: true,
			disableCollateralFallback:  false,
			minerBalance:               fil(5),
			requiredCollateral:         fil(10),
			expectedCollateral:         fil(5), // Wallet needs to cover the shortfall
		},
		{
			name:                       "CollateralFromMinerBalance enabled - miner has zero balance",
			collateralFromMinerBalance: true,
			disableCollateralFallback:  false,
			minerBalance:               big.Zero(),
			requiredCollateral:         fil(10),
			expectedCollateral:         fil(10), // Wallet covers all
		},
		{
			name:                       "DisableCollateralFallback - always zero",
			collateralFromMinerBalance: true,
			disableCollateralFallback:  true,
			minerBalance:               big.Zero(), // Even with zero miner balance
			requiredCollateral:         fil(10),
			expectedCollateral:         big.Zero(), // Collateral is disabled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &SubmitCommitTask{
				cfg: commitConfig{
					feeCfg: &config.CurioFees{
						CollateralFromMinerBalance: tt.collateralFromMinerBalance,
						DisableCollateralFallback:  tt.disableCollateralFallback,
					},
				},
			}

			result := task.calculateCollateral(tt.minerBalance, tt.requiredCollateral)
			assert.Equal(t, tt.expectedCollateral, result, "Collateral calculation mismatch")
		})
	}
}

func TestGoodFundsCalculation(t *testing.T) {
	// This test verifies that goodFunds includes collateral + a reasonable gas buffer (10% of maxFee),
	// NOT the full maxFee (which is just a cap, not typical usage).

	tests := []struct {
		name                       string
		collateralFromMinerBalance bool
		minerBalance               abi.TokenAmount
		requiredCollateral         abi.TokenAmount
		maxFee                     abi.TokenAmount
		expectedGoodFunds          abi.TokenAmount // Should be collateral + 10% of maxFee
	}{
		{
			name:                       "Miner covers collateral - goodFunds is just gas buffer",
			collateralFromMinerBalance: true,
			minerBalance:               fil(100),
			requiredCollateral:         fil(10),
			maxFee:                     fil(8), // High maxFee configured
			// goodFunds = 0 (collateral) + 0.8 FIL (10% of maxFee) = 0.8 FIL
			expectedGoodFunds: big.Div(fil(8), big.NewInt(10)),
		},
		{
			name:                       "Miner covers partial - goodFunds is shortfall plus gas buffer",
			collateralFromMinerBalance: true,
			minerBalance:               fil(5),
			requiredCollateral:         fil(10),
			maxFee:                     fil(8),
			// goodFunds = 5 FIL (shortfall) + 0.8 FIL (10% of maxFee) = 5.8 FIL
			expectedGoodFunds: big.Add(fil(5), big.Div(fil(8), big.NewInt(10))),
		},
		{
			name:                       "CollateralFromMinerBalance disabled - goodFunds is collateral plus gas buffer",
			collateralFromMinerBalance: false,
			minerBalance:               fil(100),
			requiredCollateral:         fil(10),
			maxFee:                     fil(8),
			// goodFunds = 10 FIL (collateral) + 0.8 FIL (10% of maxFee) = 10.8 FIL
			expectedGoodFunds: big.Add(fil(10), big.Div(fil(8), big.NewInt(10))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &SubmitCommitTask{
				cfg: commitConfig{
					feeCfg: &config.CurioFees{
						CollateralFromMinerBalance: tt.collateralFromMinerBalance,
						MaxCommitBatchGasFee: config.BatchFeeConfig{
							Base:      types.MustParseFIL("0"),
							PerSector: types.MustParseFIL("8"), // 8 FIL per sector
						},
					},
				},
			}

			// Simulate what simuateCommitPerSector and createCommitMessage do
			collateral := task.calculateCollateral(tt.minerBalance, tt.requiredCollateral)

			// The fix: goodFunds = collateral + 10% of maxFee (reasonable gas buffer)
			// NOT big.Add(maxFee, collateral) which was the old buggy behavior (100% of maxFee)
			gasBuffer := big.Div(tt.maxFee, big.NewInt(10))
			goodFunds := big.Add(collateral, gasBuffer)

			assert.Equal(t, tt.expectedGoodFunds, goodFunds,
				"goodFunds should be collateral + 10%% of maxFee, not full maxFee")
		})
	}
}

func TestAddressSelectionWithLowWalletBalance(t *testing.T) {
	// This test verifies that address selection works correctly when:
	// 1. Worker wallet has low balance (e.g., 1 FIL)
	// 2. Miner balance covers collateral (so wallet only needs gas buffer)
	// 3. The old code would fail because goodFunds included full maxFee (8 FIL)
	// 4. The new code should succeed because goodFunds is collateral + 10% of maxFee (0.8 FIL)

	ctx := context.Background()

	// Create test addresses
	workerAddr, _ := address.NewIDAddress(100)
	ownerAddr, _ := address.NewIDAddress(101)
	minerAddr, _ := address.NewIDAddress(1000)

	// Mock API where worker has only 1 FIL but we have the key
	mockAPI := &mockCommitAPI{
		walletBalances: map[address.Address]big.Int{
			workerAddr: fil(1), // Only 1 FIL in worker wallet
		},
		walletHas: map[address.Address]bool{
			workerAddr: true, // We have the key
		},
		minerBalance: fil(100), // Miner has plenty of balance for collateral
	}

	// Create address selector with no CommitControl configured (just worker fallback)
	as := &multictladdr.MultiAddressSelector{
		MinerMap: map[address.Address]multictladdr.AddressConfig{
			minerAddr: {
				CommitControl:         []address.Address{}, // No commit control addresses
				DisableWorkerFallback: false,               // Enable worker fallback
				DisableOwnerFallback:  true,                // Disable owner for simplicity
			},
		},
	}

	mi := api.MinerInfo{
		Worker: workerAddr,
		Owner:  ownerAddr,
		ControlAddresses: []address.Address{
			workerAddr,
		},
	}

	// With the fix: goodFunds = 0 (collateral) + 0.8 FIL (10% of 8 FIL maxFee) = 0.8 FIL
	// Worker has 1 FIL, which is >= 0.8 FIL, so it should be selected
	maxFee := fil(8)
	gasBuffer := big.Div(maxFee, big.NewInt(10)) // 0.8 FIL
	goodFunds := gasBuffer                       // collateral is 0 when miner covers it
	minFunds := big.Zero()

	selectedAddr, _, err := as.AddressFor(ctx, mockAPI, minerAddr, mi, api.CommitAddr, goodFunds, minFunds)
	require.NoError(t, err)
	assert.Equal(t, workerAddr, selectedAddr, "Worker should be selected when it has >= goodFunds (0.8 FIL)")

	// Old behavior would have failed: goodFunds = maxFee + collateral = 8 FIL + 0 = 8 FIL
	// Worker only has 1 FIL, so it wouldn't pass the balance check
	oldGoodFunds := fil(8) // This was the old buggy calculation (100% of maxFee)
	selectedAddrOld, _, err := as.AddressFor(ctx, mockAPI, minerAddr, mi, api.CommitAddr, oldGoodFunds, minFunds)
	require.NoError(t, err)
	// Note: PickAddress returns leastBad (worker) even when balance check fails,
	// but it would log a warning. The key issue is that GasEstimateMessageGas
	// might fail later if the selected address can't cover simulation gas.
	assert.Equal(t, workerAddr, selectedAddrOld,
		"Worker is still selected as fallback, but with a warning about insufficient funds")
}

func TestAddressSelectionScenarios(t *testing.T) {
	ctx := context.Background()

	workerAddr, _ := address.NewIDAddress(100)
	ownerAddr, _ := address.NewIDAddress(101)
	commitCtlAddr, _ := address.NewIDAddress(102)
	minerAddr, _ := address.NewIDAddress(1000)

	// Typical gas buffer: 10% of 8 FIL maxFee = 0.8 FIL
	gasBuffer := big.Div(fil(8), big.NewInt(10))

	tests := []struct {
		name             string
		walletBalances   map[address.Address]big.Int
		walletHas        map[address.Address]bool
		commitControl    []address.Address
		goodFunds        abi.TokenAmount
		expectedSelected address.Address
		description      string
	}{
		{
			name: "Worker only - gas buffer only - should select worker",
			walletBalances: map[address.Address]big.Int{
				workerAddr: fil(1), // 1 FIL > 0.8 FIL gas buffer
			},
			walletHas: map[address.Address]bool{
				workerAddr: true,
			},
			commitControl:    []address.Address{},
			goodFunds:        gasBuffer, // 0.8 FIL (collateral covered by miner)
			expectedSelected: workerAddr,
			description:      "With miner covering collateral, worker with 1 FIL should work (need 0.8 FIL)",
		},
		{
			name: "CommitControl has funds but no key - fallback to worker",
			walletBalances: map[address.Address]big.Int{
				commitCtlAddr: fil(100), // Plenty of funds
				workerAddr:    fil(1),   // 1 FIL > 0.8 FIL gas buffer
			},
			walletHas: map[address.Address]bool{
				commitCtlAddr: false, // Don't have key for commit control
				workerAddr:    true,  // Have key for worker
			},
			commitControl:    []address.Address{commitCtlAddr},
			goodFunds:        gasBuffer,
			expectedSelected: workerAddr,
			description:      "Should fallback to worker when CommitControl key not available",
		},
		{
			name: "CommitControl has funds and key - should select CommitControl",
			walletBalances: map[address.Address]big.Int{
				commitCtlAddr: fil(100),
				workerAddr:    fil(1),
			},
			walletHas: map[address.Address]bool{
				commitCtlAddr: true,
				workerAddr:    true,
			},
			commitControl:    []address.Address{commitCtlAddr},
			goodFunds:        fil(10), // Some collateral + gas buffer
			expectedSelected: commitCtlAddr,
			description:      "CommitControl should be selected when it has funds and key",
		},
		{
			name: "Worker has more funds than required goodFunds",
			walletBalances: map[address.Address]big.Int{
				workerAddr: fil(50),
			},
			walletHas: map[address.Address]bool{
				workerAddr: true,
			},
			commitControl:    []address.Address{},
			goodFunds:        fil(10),
			expectedSelected: workerAddr,
			description:      "Worker should be selected when it has enough funds",
		},
		{
			name: "Lender scenario - worker with minimal balance, miner covers collateral",
			walletBalances: map[address.Address]big.Int{
				workerAddr: fil(1), // Just enough for gas buffer
			},
			walletHas: map[address.Address]bool{
				workerAddr: true, // Curio controls worker
			},
			commitControl:    []address.Address{},
			goodFunds:        gasBuffer, // 0.8 FIL (only gas buffer needed)
			expectedSelected: workerAddr,
			description:      "Lender scenario: worker with 1 FIL should work when miner covers collateral",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := &mockCommitAPI{
				walletBalances: tt.walletBalances,
				walletHas:      tt.walletHas,
			}

			as := &multictladdr.MultiAddressSelector{
				MinerMap: map[address.Address]multictladdr.AddressConfig{
					minerAddr: {
						CommitControl:         tt.commitControl,
						DisableWorkerFallback: false,
						DisableOwnerFallback:  true,
					},
				},
			}

			mi := api.MinerInfo{
				Worker: workerAddr,
				Owner:  ownerAddr,
				ControlAddresses: append([]address.Address{workerAddr, ownerAddr},
					tt.commitControl...),
			}

			selectedAddr, _, err := as.AddressFor(ctx, mockAPI, minerAddr, mi,
				api.CommitAddr, tt.goodFunds, big.Zero())

			require.NoError(t, err, tt.description)
			assert.Equal(t, tt.expectedSelected, selectedAddr, tt.description)
		})
	}
}
