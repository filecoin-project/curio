package FWSS

import (
	"math/big"
	"testing"

	"github.com/filecoin-project/curio/lib/filecoinpayment"
)

func TestFWSSSettleTarget(t *testing.T) {
	tests := []struct {
		name           string
		settledUpTo    int64
		endEpoch       int64
		currentEpoch   int64
		activation     int64
		provingPeriod  int64
		wantTarget     int64
		wantResolvable bool
	}{
		{
			name:           "unactivated dataset can settle to current epoch",
			settledUpTo:    100,
			currentEpoch:   200,
			provingPeriod:  2880,
			wantTarget:     200,
			wantResolvable: true,
		},
		{
			name:           "deadline epoch itself is still open",
			settledUpTo:    900,
			currentEpoch:   3880,
			activation:     1000,
			provingPeriod:  2880,
			wantTarget:     1000,
			wantResolvable: true,
		},
		{
			name:           "first period closes after deadline",
			settledUpTo:    1000,
			currentEpoch:   3881,
			activation:     1000,
			provingPeriod:  2880,
			wantTarget:     3880,
			wantResolvable: true,
		},
		{
			name:           "multiple closed periods",
			settledUpTo:    3880,
			currentEpoch:   7000,
			activation:     1000,
			provingPeriod:  2880,
			wantTarget:     6760,
			wantResolvable: true,
		},
		{
			name:           "terminated rail caps target to end epoch",
			settledUpTo:    3880,
			endEpoch:       5000,
			currentEpoch:   7000,
			activation:     1000,
			provingPeriod:  2880,
			wantTarget:     5000,
			wantResolvable: true,
		},
		{
			name:           "terminated rail before activation has no valid target",
			settledUpTo:    800,
			endEpoch:       900,
			currentEpoch:   7000,
			activation:     1000,
			provingPeriod:  2880,
			wantResolvable: false,
		},
		{
			name:           "no progress skips settlement",
			settledUpTo:    6760,
			currentEpoch:   7000,
			activation:     1000,
			provingPeriod:  2880,
			wantResolvable: false,
		},
		{
			name:           "current before activation has no valid target",
			settledUpTo:    900,
			currentEpoch:   999,
			activation:     1000,
			provingPeriod:  2880,
			wantResolvable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rail := filecoinpayment.PaymentsRailView{
				SettledUpTo: big.NewInt(tt.settledUpTo),
				EndEpoch:    big.NewInt(tt.endEpoch),
			}
			target, ok := settleTarget(rail, big.NewInt(tt.currentEpoch), big.NewInt(tt.activation), big.NewInt(tt.provingPeriod))
			if ok != tt.wantResolvable {
				t.Fatalf("resolvable = %t, want %t", ok, tt.wantResolvable)
			}
			if !ok {
				return
			}
			if target.Int64() != tt.wantTarget {
				t.Fatalf("target = %d, want %d", target.Int64(), tt.wantTarget)
			}
		})
	}
}

func TestFWSSSettleTargetDoesNotMutateInputs(t *testing.T) {
	settledUpTo := big.NewInt(3880)
	endEpoch := big.NewInt(5000)
	currentEpoch := big.NewInt(7000)
	activation := big.NewInt(1000)
	provingPeriod := big.NewInt(2880)

	rail := filecoinpayment.PaymentsRailView{
		SettledUpTo: settledUpTo,
		EndEpoch:    endEpoch,
	}
	target, ok := settleTarget(rail, currentEpoch, activation, provingPeriod)
	if !ok {
		t.Fatal("expected target")
	}
	target.SetInt64(1)

	assertBigInt(t, "settledUpTo", settledUpTo, 3880)
	assertBigInt(t, "endEpoch", endEpoch, 5000)
	assertBigInt(t, "currentEpoch", currentEpoch, 7000)
	assertBigInt(t, "activation", activation, 1000)
	assertBigInt(t, "provingPeriod", provingPeriod, 2880)
}

func assertBigInt(t *testing.T, name string, got *big.Int, want int64) {
	t.Helper()
	if got.Int64() != want {
		t.Fatalf("%s = %d, want %d", name, got.Int64(), want)
	}
}
