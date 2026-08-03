package usdfcswap

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
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

func TestPackSwapToNativeFilMulticall(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(routerABI))
	if err != nil {
		t.Fatal(err)
	}
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	usdfc := common.HexToAddress("0x80B98d3aa09ffff255c3ba4A241111Ff1262F045")
	wfil := common.HexToAddress("0x60E1773636CF5E4A227D9AC24F20FECA034EE25A")
	amountIn := big.NewInt(1e18)
	amountOutMin := big.NewInt(5e17)
	deadline := big.NewInt(1_700_000_000)

	swapData, err := parsed.Pack("exactInputSingle", exactInputSingleParams{
		TokenIn:           usdfc,
		TokenOut:          wfil,
		Fee:               big.NewInt(500),
		Recipient:         common.Address{},
		Deadline:          deadline,
		AmountIn:          amountIn,
		AmountOutMinimum:  amountOutMin,
		SqrtPriceLimitX96: big.NewInt(0),
	})
	if err != nil {
		t.Fatalf("pack exactInputSingle: %v", err)
	}
	unwrapData, err := parsed.Pack("unwrapWETH9", amountOutMin, from)
	if err != nil {
		t.Fatalf("pack unwrapWETH9: %v", err)
	}
	data, err := parsed.Pack("multicall", [][]byte{swapData, unwrapData})
	if err != nil {
		t.Fatalf("pack multicall: %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("expected selector + payload, got %d bytes", len(data))
	}
	method, err := parsed.MethodById(data[:4])
	if err != nil {
		t.Fatal(err)
	}
	if method.Name != "multicall" {
		t.Fatalf("got method %s", method.Name)
	}
}
