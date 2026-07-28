package usdfcswap

import (
	"math/big"
	"testing"
)

func TestApplySlippage(t *testing.T) {
	out := applySlippage(big.NewInt(1_000_000), 100) // 1%
	if out.Cmp(big.NewInt(990_000)) != 0 {
		t.Fatalf("got %s want 990000", out)
	}
	out = applySlippage(big.NewInt(100), 0)
	if out.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("0 bps: got %s", out)
	}
}
