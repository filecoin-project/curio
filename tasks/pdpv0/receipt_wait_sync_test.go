package pdpv0

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/build"
)

func TestStaleReceiptAge(t *testing.T) {
	// MaxProvingPeriod of 1/5 day => 5 periods/day => stale age = 1/5 day.
	fifthDayEpochs := uint64((24 * time.Hour / time.Second) / time.Duration(build.BlockDelaySecs) / 5)
	got := StaleReceiptAge(fifthDayEpochs)
	want := (24 * time.Hour) / 5
	if got != want {
		t.Fatalf("StaleReceiptAge(%d)=%v, want %v", fifthDayEpochs, got, want)
	}
	if n := provingPeriodsPerDay(fifthDayEpochs); n != 5 {
		t.Fatalf("provingPeriodsPerDay=%d, want 5", n)
	}

	// Full-day proving period => 1 period/day => stale age = 1 day.
	dayEpochs := fifthDayEpochs * 5
	if got := StaleReceiptAge(dayEpochs); got != 24*time.Hour {
		t.Fatalf("StaleReceiptAge(day)=%v, want 24h", got)
	}
	if got := StaleReceiptAge(0); got != 24*time.Hour {
		t.Fatalf("StaleReceiptAge(0)=%v, want 24h fallback", got)
	}
}

func TestNormalizeTxHash(t *testing.T) {
	if got := normalizeTxHash(" 0xAbC "); got != "0xabc" {
		t.Fatalf("got %q", got)
	}
}

func TestDataSetIdFromClientNonce(t *testing.T) {
	require.Equal(t, int64(123), dataSetIdFromClientNonce(big.NewInt(123)))

	// AddPieces packing: (nextPieceId << 128) | dataSetId
	packed := new(big.Int).Lsh(big.NewInt(5), 128)
	packed.Or(packed, big.NewInt(99))
	require.Equal(t, int64(99), dataSetIdFromClientNonce(packed))

	require.Equal(t, int64(0), dataSetIdFromClientNonce(big.NewInt(0)))
}
