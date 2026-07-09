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
			name: "fully settled terminated rail does not need work",
			rail: PaymentsRailView{
				SettledUpTo: big.NewInt(200),
				EndEpoch:    big.NewInt(200),
			},
			want: false,
		},
		{
			name: "active rail due after settlement interval",
			rail: PaymentsRailView{
				SettledUpTo:  new(big.Int).SetUint64(current - uint64(builtin.EpochsInDay*8)),
				LockupPeriod: big.NewInt(builtin.EpochsInDay * 30),
			},
			want: true,
		},
		{
			name: "active rail not due before settlement interval",
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
