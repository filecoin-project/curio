package pdpv0

import (
	"testing"
	"time"

	"github.com/filecoin-project/lotus/build"
)

func TestStaleReceiptWaitEpochsGate(t *testing.T) {
	proveAt := int64(1000)
	canMarkLost := func(proveAtEpoch *int64, height int64) bool {
		return proveAtEpoch != nil && height >= *proveAtEpoch+StaleReceiptWaitEpochs
	}
	if canMarkLost(nil, 2000) {
		t.Fatal("nil proveAtEpoch should not mark lost")
	}
	if canMarkLost(&proveAt, proveAt+StaleReceiptWaitEpochs-1) {
		t.Fatal("should not mark lost before age gate")
	}
	if !canMarkLost(&proveAt, proveAt+StaleReceiptWaitEpochs) {
		t.Fatal("should mark lost at age gate")
	}
}

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
