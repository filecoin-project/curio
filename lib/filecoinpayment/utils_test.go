package filecoinpayment

import (
	"math/big"
	"testing"

	"github.com/filecoin-project/go-state-types/builtin"
)

func TestShouldInspectRailForSettlement(t *testing.T) {
	tests := []struct {
		name string
		rail PaymentsRailInfo
		want bool
	}{
		{
			name: "active rail",
			rail: PaymentsRailInfo{RailId: big.NewInt(1)},
			want: true,
		},
		{
			name: "terminated rail with end epoch",
			rail: PaymentsRailInfo{RailId: big.NewInt(1), IsTerminated: true, EndEpoch: big.NewInt(100)},
			want: true,
		},
		{
			name: "terminated rail without end epoch",
			rail: PaymentsRailInfo{RailId: big.NewInt(1), IsTerminated: true, EndEpoch: big.NewInt(0)},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldInspectRailForSettlement(tt.rail); got != tt.want {
				t.Fatalf("shouldInspectRailForSettlement = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRailNeedsSettlement(t *testing.T) {
	current := uint64(1_000_000)

	tests := []struct {
		name string
		rail PaymentsRailView
		want bool
	}{
		{
			name: "terminated rail retries until final settlement reaches end epoch",
			rail: PaymentsRailView{
				SettledUpTo: big.NewInt(100),
				EndEpoch:    big.NewInt(200),
			},
			want: true,
		},
		{
			name: "terminated rail at end epoch still needs finalization",
			rail: PaymentsRailView{
				SettledUpTo: big.NewInt(200),
				EndEpoch:    big.NewInt(200),
			},
			want: true,
		},
		{
			name: "active rail inside lockup horizon is not due",
			rail: PaymentsRailView{
				SettledUpTo:  new(big.Int).SetUint64(current - uint64(builtin.EpochsInDay*8)),
				LockupPeriod: big.NewInt(builtin.EpochsInDay * 30),
			},
			want: false,
		},
		{
			name: "active rail past lockup horizon is due",
			rail: PaymentsRailView{
				SettledUpTo:  new(big.Int).SetUint64(current - uint64(builtin.EpochsInDay*29) - 1),
				LockupPeriod: big.NewInt(builtin.EpochsInDay * 30),
			},
			want: true,
		},
		{
			name: "active rail at lockup horizon is not yet due",
			rail: PaymentsRailView{
				SettledUpTo:  new(big.Int).SetUint64(current - uint64(builtin.EpochsInDay*29)),
				LockupPeriod: big.NewInt(builtin.EpochsInDay * 30),
			},
			want: false,
		},
		{
			name: "active rail settled recently is not due",
			rail: PaymentsRailView{
				SettledUpTo:  new(big.Int).SetUint64(current - uint64(builtin.EpochsInDay)),
				LockupPeriod: big.NewInt(builtin.EpochsInDay * 30),
			},
			want: false,
		},
		{
			name: "short lockup period settles every pass",
			rail: PaymentsRailView{
				SettledUpTo:  big.NewInt(100),
				LockupPeriod: big.NewInt(builtin.EpochsInDay),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := railNeedsSettlement(tt.rail, current); got != tt.want {
				t.Fatalf("railNeedsSettlement = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestDeferredSettleGas(t *testing.T) {
	t.Cleanup(func() { pruneDeferredSettleGas(nil) })

	if deferredSettleGasKnown(7, big.NewInt(100), big.NewInt(3)) {
		t.Fatal("unrecorded rail reported known")
	}

	recordDeferredSettleGas(7, big.NewInt(100), big.NewInt(3))
	if !deferredSettleGasKnown(7, big.NewInt(100), big.NewInt(3)) {
		t.Fatal("recorded rail not known")
	}
	if deferredSettleGasKnown(7, big.NewInt(100), big.NewInt(4)) {
		t.Fatal("queue growth must require a new estimate")
	}
	if deferredSettleGasKnown(7, big.NewInt(200), big.NewInt(3)) {
		t.Fatal("settlement progress must require a new estimate")
	}

	recordDeferredSettleGas(8, big.NewInt(100), big.NewInt(1))
	pruneDeferredSettleGas(map[int64]struct{}{8: {}})
	if deferredSettleGasKnown(7, big.NewInt(100), big.NewInt(3)) {
		t.Fatal("unseen rail not pruned")
	}
	if !deferredSettleGasKnown(8, big.NewInt(100), big.NewInt(1)) {
		t.Fatal("seen rail pruned")
	}
}

func TestIsActiveRail(t *testing.T) {
	tests := []struct {
		name string
		rail PaymentsRailView
		want bool
	}{
		{name: "active with zero end epoch", rail: PaymentsRailView{SettledUpTo: big.NewInt(1), EndEpoch: big.NewInt(0)}, want: true},
		{name: "active with nil end epoch", rail: PaymentsRailView{SettledUpTo: big.NewInt(1)}, want: true},
		{name: "terminated", rail: PaymentsRailView{SettledUpTo: big.NewInt(1), EndEpoch: big.NewInt(100)}, want: false},
		{name: "missing settledUpTo", rail: PaymentsRailView{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isActiveRail(tt.rail); got != tt.want {
				t.Fatalf("isActiveRail = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestIsTerminatedRailFinalizationTarget(t *testing.T) {
	tests := []struct {
		name   string
		rail   PaymentsRailView
		target *big.Int
		want   bool
	}{
		{
			name: "terminated rail at end epoch",
			rail: PaymentsRailView{
				SettledUpTo: big.NewInt(200),
				EndEpoch:    big.NewInt(200),
			},
			target: big.NewInt(200),
			want:   true,
		},
		{
			name: "terminated rail before end epoch",
			rail: PaymentsRailView{
				SettledUpTo: big.NewInt(100),
				EndEpoch:    big.NewInt(200),
			},
			target: big.NewInt(200),
			want:   false,
		},
		{
			name: "active rail at target",
			rail: PaymentsRailView{
				SettledUpTo: big.NewInt(200),
			},
			target: big.NewInt(200),
			want:   false,
		},
		{
			name: "terminated rail with non-end target",
			rail: PaymentsRailView{
				SettledUpTo: big.NewInt(200),
				EndEpoch:    big.NewInt(200),
			},
			target: big.NewInt(199),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTerminatedRailFinalizationTarget(tt.rail, tt.target); got != tt.want {
				t.Fatalf("IsTerminatedRailFinalizationTarget = %t, want %t", got, tt.want)
			}
		})
	}
}
