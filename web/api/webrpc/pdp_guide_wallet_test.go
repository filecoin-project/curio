package webrpc

import (
	"testing"

	"github.com/filecoin-project/lotus/chain/types"
)

func TestFilBalanceAboveMinimum(t *testing.T) {
	minimum := types.MustParseFIL("5 FIL")
	cases := []struct {
		name string
		atto string
		want bool
	}{
		{"below", "4500000000000000000", false},
		{"equal", "5000000000000000000", false},
		{"above", "5000000000000000001", true},
		{"zero", "0", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := filBalanceAboveMinimum(tc.atto, minimum); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
