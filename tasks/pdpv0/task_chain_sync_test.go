package pdpv0

import (
	"database/sql"
	"math/big"
	"testing"
)

func TestProvingPeriodForEpoch(t *testing.T) {
	tests := []struct {
		name             string
		activationEpoch  *big.Int
		epoch            *big.Int
		maxProvingPeriod uint64
		wantPeriod       *big.Int
		wantOK           bool
	}{
		{
			name:             "nil activation",
			activationEpoch:  nil,
			epoch:            big.NewInt(101),
			maxProvingPeriod: 50,
			wantOK:           false,
		},
		{
			name:             "zero activation",
			activationEpoch:  big.NewInt(0),
			epoch:            big.NewInt(101),
			maxProvingPeriod: 50,
			wantOK:           false,
		},
		{
			name:             "epoch at activation",
			activationEpoch:  big.NewInt(100),
			epoch:            big.NewInt(100),
			maxProvingPeriod: 50,
			wantOK:           false,
		},
		{
			name:             "first epoch after activation",
			activationEpoch:  big.NewInt(100),
			epoch:            big.NewInt(101),
			maxProvingPeriod: 50,
			wantPeriod:       big.NewInt(0),
			wantOK:           true,
		},
		{
			name:             "last epoch in first period",
			activationEpoch:  big.NewInt(100),
			epoch:            big.NewInt(150),
			maxProvingPeriod: 50,
			wantPeriod:       big.NewInt(0),
			wantOK:           true,
		},
		{
			name:             "first epoch in second period",
			activationEpoch:  big.NewInt(100),
			epoch:            big.NewInt(151),
			maxProvingPeriod: 50,
			wantPeriod:       big.NewInt(1),
			wantOK:           true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotPeriod, gotOK := provingPeriodForEpoch(test.activationEpoch, test.epoch, test.maxProvingPeriod)
			if gotOK != test.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, test.wantOK)
			}
			if !test.wantOK {
				return
			}
			if gotPeriod.Cmp(test.wantPeriod) != 0 {
				t.Fatalf("period = %s, want %s", gotPeriod, test.wantPeriod)
			}
		})
	}
}

func TestDataSetProofCoversLocalFailure(t *testing.T) {
	validEpoch := func(epoch int64) sql.NullInt64 {
		return sql.NullInt64{Int64: epoch, Valid: true}
	}

	tests := []struct {
		name                  string
		proveAtEpoch          sql.NullInt64
		consecutiveFailures   int
		nextProveAttemptEpoch sql.NullInt64
		lastProvenEpoch       *big.Int
		want                  bool
	}{
		{
			name:            "nil last proven epoch",
			lastProvenEpoch: nil,
			want:            false,
		},
		{
			name:            "zero last proven epoch",
			lastProvenEpoch: big.NewInt(0),
			want:            false,
		},
		{
			name:            "proof reaches local prove epoch",
			proveAtEpoch:    validEpoch(200),
			lastProvenEpoch: big.NewInt(250),
			want:            true,
		},
		{
			name:            "proof before local prove epoch without failure epoch",
			proveAtEpoch:    validEpoch(300),
			lastProvenEpoch: big.NewInt(250),
			want:            false,
		},
		{
			name:                  "proof after reconstructed failure epoch",
			proveAtEpoch:          validEpoch(400),
			consecutiveFailures:   2,
			nextProveAttemptEpoch: validEpoch(500),
			lastProvenEpoch:       big.NewInt(350),
			want:                  true,
		},
		{
			name:                  "proof at reconstructed failure epoch",
			consecutiveFailures:   2,
			nextProveAttemptEpoch: validEpoch(500),
			lastProvenEpoch:       big.NewInt(300),
			want:                  false,
		},
		{
			name:                  "no useful local epoch",
			consecutiveFailures:   2,
			nextProveAttemptEpoch: sql.NullInt64{},
			lastProvenEpoch:       big.NewInt(350),
			want:                  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := dataSetProofCoversLocalFailure(test.proveAtEpoch, test.consecutiveFailures, test.nextProveAttemptEpoch, test.lastProvenEpoch)
			if got != test.want {
				t.Fatalf("dataSetProofCoversLocalFailure() = %v, want %v", got, test.want)
			}
		})
	}
}
