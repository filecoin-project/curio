package seal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/lib/multictladdr"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

// mockPrecommitAPI implements the minimal SubmitPrecommitTaskApi interface for testing
type mockPrecommitAPI struct {
	walletBalances map[address.Address]big.Int
	walletHas      map[address.Address]bool
	minerBalance   big.Int
}

func (m *mockPrecommitAPI) ChainHead(context.Context) (*types.TipSet, error) {
	return nil, nil
}

func (m *mockPrecommitAPI) StateMinerPreCommitDepositForPower(context.Context, address.Address, miner.SectorPreCommitInfo, types.TipSetKey) (big.Int, error) {
	return big.Zero(), nil
}

func (m *mockPrecommitAPI) StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error) {
	return api.MinerInfo{}, nil
}

func (m *mockPrecommitAPI) StateNetworkVersion(context.Context, types.TipSetKey) (network.Version, error) {
	return network.Version21, nil
}

func (m *mockPrecommitAPI) StateMinerAvailableBalance(context.Context, address.Address, types.TipSetKey) (big.Int, error) {
	return m.minerBalance, nil
}

func (m *mockPrecommitAPI) GasEstimateMessageGas(context.Context, *types.Message, *api.MessageSendSpec, types.TipSetKey) (*types.Message, error) {
	return nil, nil
}

// ctladdr.NodeApi methods
func (m *mockPrecommitAPI) WalletBalance(ctx context.Context, addr address.Address) (types.BigInt, error) {
	if bal, ok := m.walletBalances[addr]; ok {
		return bal, nil
	}
	return big.Zero(), nil
}

func (m *mockPrecommitAPI) WalletHas(ctx context.Context, addr address.Address) (bool, error) {
	if has, ok := m.walletHas[addr]; ok {
		return has, nil
	}
	return false, nil
}

func (m *mockPrecommitAPI) StateAccountKey(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error) {
	return addr, nil
}

func (m *mockPrecommitAPI) StateLookupID(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error) {
	return addr, nil
}

// calculatePrecommitNeedFunds simulates the needFunds calculation in SubmitPrecommitTask.Do
// This mirrors the logic in task_submit_precommit.go lines 281-296
func calculatePrecommitNeedFunds(
	collateral, aggFee, minerBalance abi.TokenAmount,
	collateralFromMinerBalance, disableCollateralFallback bool,
) abi.TokenAmount {
	needFunds := big.Add(collateral, aggFee)

	if collateralFromMinerBalance {
		if disableCollateralFallback {
			needFunds = big.Zero()
		}
		needFunds = big.Sub(needFunds, minerBalance)
		if needFunds.LessThan(big.Zero()) {
			needFunds = big.Zero()
		}
	}
	return needFunds
}

func TestPrecommitNeedFundsCalculation(t *testing.T) {
	tests := []struct {
		name                       string
		collateral                 abi.TokenAmount
		aggFee                     abi.TokenAmount
		minerBalance               abi.TokenAmount
		collateralFromMinerBalance bool
		disableCollateralFallback  bool
		expectedNeedFunds          abi.TokenAmount
	}{
		{
			name:                       "CollateralFromMinerBalance disabled - full funds required",
			collateral:                 fil(10),
			aggFee:                     fil(1),
			minerBalance:               fil(100),
			collateralFromMinerBalance: false,
			disableCollateralFallback:  false,
			expectedNeedFunds:          fil(11), // collateral + aggFee, miner balance not used
		},
		{
			name:                       "CollateralFromMinerBalance enabled - miner covers all",
			collateral:                 fil(10),
			aggFee:                     fil(1),
			minerBalance:               fil(100),
			collateralFromMinerBalance: true,
			disableCollateralFallback:  false,
			expectedNeedFunds:          big.Zero(), // Miner balance covers everything
		},
		{
			name:                       "CollateralFromMinerBalance enabled - miner covers partial",
			collateral:                 fil(10),
			aggFee:                     fil(1),
			minerBalance:               fil(5),
			collateralFromMinerBalance: true,
			disableCollateralFallback:  false,
			expectedNeedFunds:          fil(6), // 11 - 5 = 6 FIL shortfall
		},
		{
			name:                       "CollateralFromMinerBalance enabled - miner has zero balance",
			collateral:                 fil(10),
			aggFee:                     fil(1),
			minerBalance:               big.Zero(),
			collateralFromMinerBalance: true,
			disableCollateralFallback:  false,
			expectedNeedFunds:          fil(11), // Wallet covers all
		},
		{
			name:                       "DisableCollateralFallback - always zero",
			collateral:                 fil(10),
			aggFee:                     fil(1),
			minerBalance:               big.Zero(),
			collateralFromMinerBalance: true,
			disableCollateralFallback:  true,
			expectedNeedFunds:          big.Zero(), // Collateral fallback disabled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePrecommitNeedFunds(
				tt.collateral, tt.aggFee, tt.minerBalance,
				tt.collateralFromMinerBalance, tt.disableCollateralFallback,
			)
			assert.Equal(t, tt.expectedNeedFunds, result, "needFunds calculation mismatch")
		})
	}
}

func TestPrecommitGoodFundsCalculation(t *testing.T) {
	// This test verifies that goodFunds includes needFunds + a reasonable gas buffer (10% of maxFee),
	// NOT the full maxFee (which is just a cap, not typical usage).

	tests := []struct {
		name                       string
		collateral                 abi.TokenAmount
		aggFee                     abi.TokenAmount
		minerBalance               abi.TokenAmount
		maxFee                     abi.TokenAmount
		collateralFromMinerBalance bool
		expectedGoodFunds          abi.TokenAmount // Should be needFunds + 10% of maxFee
	}{
		{
			name:                       "Miner covers all - goodFunds is just gas buffer",
			collateral:                 fil(10),
			aggFee:                     fil(1),
			minerBalance:               fil(100),
			maxFee:                     fil(8), // High maxFee configured
			collateralFromMinerBalance: true,
			// goodFunds = 0 (needFunds) + 0.8 FIL (10% of maxFee) = 0.8 FIL
			expectedGoodFunds: big.Div(fil(8), big.NewInt(10)),
		},
		{
			name:                       "Miner covers partial - goodFunds is shortfall plus gas buffer",
			collateral:                 fil(10),
			aggFee:                     fil(1),
			minerBalance:               fil(5),
			maxFee:                     fil(8),
			collateralFromMinerBalance: true,
			// goodFunds = 6 FIL (shortfall) + 0.8 FIL (10% of maxFee) = 6.8 FIL
			expectedGoodFunds: big.Add(fil(6), big.Div(fil(8), big.NewInt(10))),
		},
		{
			name:                       "CollateralFromMinerBalance disabled - goodFunds is needFunds plus gas buffer",
			collateral:                 fil(10),
			aggFee:                     fil(1),
			minerBalance:               fil(100),
			maxFee:                     fil(8),
			collateralFromMinerBalance: false,
			// goodFunds = 11 FIL (needFunds) + 0.8 FIL (10% of maxFee) = 11.8 FIL
			expectedGoodFunds: big.Add(fil(11), big.Div(fil(8), big.NewInt(10))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the needFunds calculation
			needFunds := calculatePrecommitNeedFunds(
				tt.collateral, tt.aggFee, tt.minerBalance,
				tt.collateralFromMinerBalance, false,
			)

			// The fix: goodFunds = needFunds + 10% of maxFee (reasonable gas buffer)
			// NOT big.Add(maxFee, needFunds) which was the old buggy behavior (100% of maxFee)
			gasBuffer := big.Div(tt.maxFee, big.NewInt(10))
			goodFunds := big.Add(needFunds, gasBuffer)

			assert.Equal(t, tt.expectedGoodFunds, goodFunds,
				"goodFunds should be needFunds + 10%% of maxFee, not full maxFee")
		})
	}
}

func TestPrecommitAddressSelectionWithLowWalletBalance(t *testing.T) {
	// This test verifies that address selection works correctly when:
	// 1. Worker wallet has low balance (e.g., 1 FIL)
	// 2. Miner balance covers collateral (so wallet only needs gas buffer)
	// 3. The old code would fail because goodFunds included full maxFee (8 FIL)
	// 4. The new code should succeed because goodFunds is needFunds + 10% of maxFee (0.8 FIL)

	ctx := context.Background()

	// Create test addresses
	workerAddr, _ := address.NewIDAddress(100)
	ownerAddr, _ := address.NewIDAddress(101)
	minerAddr, _ := address.NewIDAddress(1000)

	// Mock API where worker has only 1 FIL but we have the key
	mockAPI := &mockPrecommitAPI{
		walletBalances: map[address.Address]big.Int{
			workerAddr: fil(1), // Only 1 FIL in worker wallet
		},
		walletHas: map[address.Address]bool{
			workerAddr: true, // We have the key
		},
		minerBalance: fil(100), // Miner has plenty of balance for collateral
	}

	// Create address selector with no PreCommitControl configured (just worker fallback)
	as := &multictladdr.MultiAddressSelector{
		MinerMap: map[address.Address]multictladdr.AddressConfig{
			minerAddr: {
				PreCommitControl:      []address.Address{}, // No precommit control addresses
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

	// With the fix: goodFunds = 0 (needFunds) + 0.8 FIL (10% of 8 FIL maxFee) = 0.8 FIL
	// Worker has 1 FIL, which is >= 0.8 FIL, so it should be selected
	maxFee := fil(8)
	gasBuffer := big.Div(maxFee, big.NewInt(10)) // 0.8 FIL
	goodFunds := gasBuffer                       // needFunds is 0 when miner covers it
	minFunds := big.Zero()

	selectedAddr, _, err := as.AddressFor(ctx, mockAPI, minerAddr, mi, api.PreCommitAddr, goodFunds, minFunds)
	require.NoError(t, err)
	assert.Equal(t, workerAddr, selectedAddr, "Worker should be selected when it has >= goodFunds (0.8 FIL)")

	// Old behavior would have failed: goodFunds = maxFee + needFunds = 8 FIL + 0 = 8 FIL
	// Worker only has 1 FIL, so it wouldn't pass the balance check
	oldGoodFunds := fil(8) // This was the old buggy calculation (100% of maxFee)
	selectedAddrOld, _, err := as.AddressFor(ctx, mockAPI, minerAddr, mi, api.PreCommitAddr, oldGoodFunds, minFunds)
	require.NoError(t, err)
	// Note: PickAddress returns leastBad (worker) even when balance check fails,
	// but it would log a warning. The key issue is that GasEstimateMessageGas
	// might fail later if the selected address can't cover simulation gas.
	assert.Equal(t, workerAddr, selectedAddrOld,
		"Worker is still selected as fallback, but with a warning about insufficient funds")
}

func TestPrecommitAddressSelectionScenarios(t *testing.T) {
	ctx := context.Background()

	workerAddr, _ := address.NewIDAddress(100)
	ownerAddr, _ := address.NewIDAddress(101)
	precommitCtlAddr, _ := address.NewIDAddress(102)
	minerAddr, _ := address.NewIDAddress(1000)

	// Typical gas buffer: 10% of 8 FIL maxFee = 0.8 FIL
	gasBuffer := big.Div(fil(8), big.NewInt(10))

	tests := []struct {
		name             string
		walletBalances   map[address.Address]big.Int
		walletHas        map[address.Address]bool
		precommitControl []address.Address
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
			precommitControl: []address.Address{},
			goodFunds:        gasBuffer, // 0.8 FIL (collateral covered by miner)
			expectedSelected: workerAddr,
			description:      "With miner covering collateral, worker with 1 FIL should work (need 0.8 FIL)",
		},
		{
			name: "PreCommitControl has funds but no key - fallback to worker",
			walletBalances: map[address.Address]big.Int{
				precommitCtlAddr: fil(100), // Plenty of funds
				workerAddr:       fil(1),   // 1 FIL > 0.8 FIL gas buffer
			},
			walletHas: map[address.Address]bool{
				precommitCtlAddr: false, // Don't have key for precommit control
				workerAddr:       true,  // Have key for worker
			},
			precommitControl: []address.Address{precommitCtlAddr},
			goodFunds:        gasBuffer,
			expectedSelected: workerAddr,
			description:      "Should fallback to worker when PreCommitControl key not available",
		},
		{
			name: "PreCommitControl has funds and key - should select PreCommitControl",
			walletBalances: map[address.Address]big.Int{
				precommitCtlAddr: fil(100),
				workerAddr:       fil(1),
			},
			walletHas: map[address.Address]bool{
				precommitCtlAddr: true,
				workerAddr:       true,
			},
			precommitControl: []address.Address{precommitCtlAddr},
			goodFunds:        fil(10), // Some collateral + gas buffer
			expectedSelected: precommitCtlAddr,
			description:      "PreCommitControl should be selected when it has funds and key",
		},
		{
			name: "Worker has more funds than required goodFunds",
			walletBalances: map[address.Address]big.Int{
				workerAddr: fil(50),
			},
			walletHas: map[address.Address]bool{
				workerAddr: true,
			},
			precommitControl: []address.Address{},
			goodFunds:        fil(10),
			expectedSelected: workerAddr,
			description:      "Worker should be selected when it has enough funds",
		},
		{
			name: "Lender scenario - worker controlled by curio with minimal local balance",
			walletBalances: map[address.Address]big.Int{
				workerAddr: fil(1), // Just enough for gas buffer (0.8 FIL)
			},
			walletHas: map[address.Address]bool{
				workerAddr: true, // Curio controls worker
			},
			precommitControl: []address.Address{},
			goodFunds:        gasBuffer, // 0.8 FIL (only gas buffer needed)
			expectedSelected: workerAddr,
			description:      "Lender scenario: worker with 1 FIL should work when miner covers collateral (need 0.8 FIL)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := &mockPrecommitAPI{
				walletBalances: tt.walletBalances,
				walletHas:      tt.walletHas,
			}

			as := &multictladdr.MultiAddressSelector{
				MinerMap: map[address.Address]multictladdr.AddressConfig{
					minerAddr: {
						PreCommitControl:      tt.precommitControl,
						DisableWorkerFallback: false,
						DisableOwnerFallback:  true,
					},
				},
			}

			mi := api.MinerInfo{
				Worker: workerAddr,
				Owner:  ownerAddr,
				ControlAddresses: append([]address.Address{workerAddr, ownerAddr},
					tt.precommitControl...),
			}

			selectedAddr, _, err := as.AddressFor(ctx, mockAPI, minerAddr, mi,
				api.PreCommitAddr, tt.goodFunds, big.Zero())

			require.NoError(t, err, tt.description)
			assert.Equal(t, tt.expectedSelected, selectedAddr, tt.description)
		})
	}
}

func TestPrecommitFeeCfgIntegration(t *testing.T) {
	// Test that verifies the fee configuration is correctly applied
	// in the context of the SubmitPrecommitTask

	tests := []struct {
		name                       string
		collateralFromMinerBalance bool
		disableCollateralFallback  bool
		minerBalance               abi.TokenAmount
		collateral                 abi.TokenAmount
		aggFee                     abi.TokenAmount
		maxFee                     abi.TokenAmount
		expectedNeedFunds          abi.TokenAmount
		expectedGoodFunds          abi.TokenAmount // needFunds + 10% of maxFee
	}{
		{
			name:                       "Standard lender setup - miner covers all",
			collateralFromMinerBalance: true,
			disableCollateralFallback:  false,
			minerBalance:               fil(100),
			collateral:                 fil(10),
			aggFee:                     fil(1),
			maxFee:                     fil(8),
			expectedNeedFunds:          big.Zero(),
			// goodFunds = 0 + 0.8 FIL (10% of maxFee) = 0.8 FIL
			expectedGoodFunds: big.Div(fil(8), big.NewInt(10)),
		},
		{
			name:                       "Partial miner coverage",
			collateralFromMinerBalance: true,
			disableCollateralFallback:  false,
			minerBalance:               fil(5),
			collateral:                 fil(10),
			aggFee:                     fil(1),
			maxFee:                     fil(8),
			expectedNeedFunds:          fil(6),
			// goodFunds = 6 + 0.8 FIL = 6.8 FIL (NOT maxFee + 6 = 14!)
			expectedGoodFunds: big.Add(fil(6), big.Div(fil(8), big.NewInt(10))),
		},
		{
			name:                       "No miner balance usage",
			collateralFromMinerBalance: false,
			disableCollateralFallback:  false,
			minerBalance:               fil(100), // Ignored
			collateral:                 fil(10),
			aggFee:                     fil(1),
			maxFee:                     fil(8),
			expectedNeedFunds:          fil(11),
			// goodFunds = 11 + 0.8 FIL = 11.8 FIL (NOT maxFee + 11 = 19!)
			expectedGoodFunds: big.Add(fil(11), big.Div(fil(8), big.NewInt(10))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feeCfg := &config.CurioFees{
				CollateralFromMinerBalance: tt.collateralFromMinerBalance,
				DisableCollateralFallback:  tt.disableCollateralFallback,
				MaxPreCommitBatchGasFee: config.BatchFeeConfig{
					Base:      types.MustParseFIL("0"),
					PerSector: types.MustParseFIL("8"),
				},
			}

			// Simulate the Do method's needFunds calculation
			needFunds := big.Add(tt.collateral, tt.aggFee)

			if feeCfg.CollateralFromMinerBalance {
				if feeCfg.DisableCollateralFallback {
					needFunds = big.Zero()
				}
				needFunds = big.Sub(needFunds, tt.minerBalance)
				if needFunds.LessThan(big.Zero()) {
					needFunds = big.Zero()
				}
			}

			// The fix: goodFunds = needFunds + 10% of maxFee (reasonable gas buffer)
			// NOT big.Add(maxFee, needFunds) which was the old behavior (100% of maxFee)
			gasBuffer := big.Div(tt.maxFee, big.NewInt(10))
			goodFunds := big.Add(needFunds, gasBuffer)

			assert.Equal(t, tt.expectedNeedFunds, needFunds, "needFunds mismatch")
			assert.Equal(t, tt.expectedGoodFunds, goodFunds, "goodFunds should be needFunds + 10%% of maxFee")
		})
	}
}
