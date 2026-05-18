package pdpv0

import (
	"database/sql"
	"math/big"
	"testing"
)

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
