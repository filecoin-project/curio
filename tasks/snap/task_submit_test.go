package snap

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/mock"
)

type mockSubmitPledgeAPI struct {
	SubmitTaskNodeAPI
	pledge func(context.Context, abi.ChainEpoch, abi.SectorSize, uint64, types.TipSetKey) (types.BigInt, error)
}

func (m *mockSubmitPledgeAPI) StateMinerInitialPledgeForSector(ctx context.Context, duration abi.ChainEpoch, size abi.SectorSize, verifiedSize uint64, tsk types.TipSetKey) (types.BigInt, error) {
	return m.pledge(ctx, duration, size, verifiedSize, tsk)
}

func TestCalculateSectorCollateralPower(t *testing.T) {
	block := mock.MkBlock(nil, 1, 1)
	block.Height = 300
	ts := mock.TipSet(block)

	tests := []struct {
		name           string
		verifiedWeight int64
		flags          miner.SectorOnChainInfoFlags
		wantSize       uint64
		wantCalls      int
		wantCollateral int64
	}{
		{
			name:           "legacy 1x power requests full size",
			wantSize:       2048,
			wantCalls:      1,
			wantCollateral: 660,
		},
		{
			name:           "legacy partial power requests full size",
			verifiedWeight: 512000,
			wantSize:       2048,
			wantCalls:      1,
			wantCollateral: 660,
		},
		{
			name:  "full QA flag skips estimate",
			flags: miner.FULL_QA_POWER,
		},
		{
			name:           "legacy full weight skips estimate",
			verifiedWeight: 614400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			task := &SubmitTask{api: &mockSubmitPledgeAPI{
				pledge: func(_ context.Context, duration abi.ChainEpoch, size abi.SectorSize, verifiedSize uint64, tsk types.TipSetKey) (types.BigInt, error) {
					calls++
					require.Equal(t, abi.ChainEpoch(200), duration)
					require.Equal(t, abi.SectorSize(2048), size)
					require.Equal(t, tt.wantSize, verifiedSize)
					require.Equal(t, ts.Key(), tsk)
					return big.NewInt(1100), nil
				},
			}}
			info := &miner.SectorOnChainInfo{
				SealProof:          abi.RegisteredSealProof_StackedDrg2KiBV1_1,
				Activation:         50,
				PowerBaseEpoch:     200,
				Expiration:         500,
				VerifiedDealWeight: big.NewInt(tt.verifiedWeight),
				Flags:              tt.flags,
				InitialPledge:      big.NewInt(400),
			}

			collateral, err := task.calculateSectorCollateral(context.Background(), info, ts)
			require.NoError(t, err)
			require.Equal(t, tt.wantCalls, calls)
			require.Equal(t, big.NewInt(tt.wantCollateral), collateral)
		})
	}
}

func TestCalculateSectorCollateralDelta(t *testing.T) {
	apiErr := errors.New("pledge estimate unavailable")
	tests := []struct {
		name           string
		estimate       int64
		initial        int64
		wantCollateral int64
		apiErr         error
	}{
		{name: "buffer only the additional pledge", estimate: 1100, initial: 400, wantCollateral: 660},
		{name: "equal pledge", estimate: 1100, initial: 1000},
		{name: "initial above buffered estimate", estimate: 1100, initial: 1200},
		{name: "initial between pledge and buffered estimate", estimate: 1100, initial: 1050},
		{name: "recover pledge from truncated estimate", estimate: 1107, initial: 1000, wantCollateral: 7},
		{name: "equal pledge with truncated estimate", estimate: 1107, initial: 1007},
		{name: "round down final delta buffer", estimate: 1120, initial: 1000, wantCollateral: 20},
		{name: "propagate API error", apiErr: apiErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &SubmitTask{api: &mockSubmitPledgeAPI{
				pledge: func(context.Context, abi.ChainEpoch, abi.SectorSize, uint64, types.TipSetKey) (types.BigInt, error) {
					return big.NewInt(tt.estimate), tt.apiErr
				},
			}}
			info := &miner.SectorOnChainInfo{
				SealProof:          abi.RegisteredSealProof_StackedDrg2KiBV1_1,
				Expiration:         500,
				VerifiedDealWeight: big.Zero(),
				InitialPledge:      big.NewInt(tt.initial),
			}

			collateral, err := task.calculateSectorCollateral(context.Background(), info, &types.TipSet{})
			if tt.apiErr != nil {
				require.ErrorIs(t, err, tt.apiErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, big.NewInt(tt.wantCollateral), collateral)
		})
	}
}
