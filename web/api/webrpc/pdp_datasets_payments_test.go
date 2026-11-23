package webrpc

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMonthlyIncome(t *testing.T) {
	// 86,400 epochs in 30 days: this rate pays 2.5 USDFC over them.
	rate, ok := new(big.Int).SetString("28935185185185", 10)
	require.True(t, ok)

	require.Equal(t, "2.500", monthlyIncome(rate))
	require.Empty(t, monthlyIncome(big.NewInt(0)))
	require.Empty(t, monthlyIncome(nil))
}

func TestFundedDays(t *testing.T) {
	const head = int64(1_000_000)
	noEnd := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	cases := []struct {
		name        string
		fundedUntil *big.Int
		head        int64
		want        *int64
	}{
		{"41 days and a bit", big.NewInt(head + 41*2880 + 100), head, new(int64(41))},
		{"less than a day", big.NewInt(head + 2879), head, new(int64(0))},
		{"already past", big.NewInt(head - 5000), head, new(int64(0))},
		{"funds with no end", noEnd, head, nil},
		{"too far out to date", big.NewInt(head + MAX_DATED_EPOCHS + 1), head, nil},
		{"unknown funded-until", nil, head, nil},
		{"unknown chain head", big.NewInt(head), 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, fundedDays(tc.fundedUntil, tc.head))
		})
	}
}
