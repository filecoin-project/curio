package pdpv0

import (
	"testing"
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

func TestNormalizeTxHash(t *testing.T) {
	if got := normalizeTxHash(" 0xAbC "); got != "0xabc" {
		t.Fatalf("got %q", got)
	}
}
