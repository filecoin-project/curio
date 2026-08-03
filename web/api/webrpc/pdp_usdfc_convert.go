package webrpc

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/usdfcswap"

	"github.com/filecoin-project/lotus/chain/types"
)

// PDPUsdfcFilQuoteResult is the expected FIL out for a USDFC→FIL SushiSwap quote.
type PDPUsdfcFilQuoteResult struct {
	AmountInUsdfc string `json:"amountInUsdfc"`
	AmountOutFil  string `json:"amountOutFil"`
	AmountOutAtto string `json:"amountOutAtto"`
}

// PDPConvertUsdfcToFilResult holds tx hashes from a convert flow.
type PDPConvertUsdfcToFilResult struct {
	ApproveTxHash string `json:"approveTxHash,omitempty"`
	SwapTxHash    string `json:"swapTxHash"`
	UnwrapTxHash  string `json:"unwrapTxHash"`
	QuotedOutFil  string `json:"quotedOutFil"`
	MinOutFil     string `json:"minOutFil"`
}

// PDPUsdfcFilQuote returns the expected FIL out for converting amountIn USDFC via SushiSwap V3 (mainnet).
func (a *WebRPC) PDPUsdfcFilQuote(ctx context.Context, amountIn string) (*PDPUsdfcFilQuoteResult, error) {
	amount, err := parseUsdfcAmount(amountIn)
	if err != nil {
		return nil, err
	}
	client, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, fmt.Errorf("eth client unavailable: %w", err)
	}
	quote, err := usdfcswap.Quote(ctx, client, amount)
	if err != nil {
		return nil, err
	}
	return &PDPUsdfcFilQuoteResult{
		AmountInUsdfc: strings.TrimSpace(amountIn),
		AmountOutFil:  types.FIL(types.BigFromBytes(quote.AmountOut.Bytes())).Short(),
		AmountOutAtto: quote.AmountOut.String(),
	}, nil
}

// PDPConvertUsdfcToFil converts USDFC to native FIL via SushiSwap V3 (approve if needed, swap, unwrap).
// slippageBps is basis points (100 = 1%). Pass 0 to use the default of 100.
func (a *WebRPC) PDPConvertUsdfcToFil(ctx context.Context, amountIn string, slippageBps int) (*PDPConvertUsdfcToFilResult, error) {
	if a.Deps.EthSender == nil {
		return nil, fmt.Errorf("ETH sender not available; enable PDP so the ETH send task is running")
	}
	amount, err := parseUsdfcAmount(amountIn)
	if err != nil {
		return nil, err
	}
	from, err := a.getPDPAddress(ctx)
	if err != nil {
		return nil, err
	}
	client, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, fmt.Errorf("eth client unavailable: %w", err)
	}

	res, err := usdfcswap.Convert(ctx, a.Deps.DB, client, a.Deps.EthSender, from, amount, slippageBps)
	if err != nil {
		return nil, err
	}
	out := &PDPConvertUsdfcToFilResult{
		ApproveTxHash: res.ApproveTxHash,
		SwapTxHash:    res.SwapTxHash,
		UnwrapTxHash:  res.UnwrapTxHash,
	}
	if res.QuotedOut != nil {
		out.QuotedOutFil = types.FIL(types.BigFromBytes(res.QuotedOut.Bytes())).Short()
	}
	if res.AmountOutMin != nil {
		out.MinOutFil = types.FIL(types.BigFromBytes(res.AmountOutMin.Bytes())).Short()
	}
	return out, nil
}

func parseUsdfcAmount(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " USDFC")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, xerrors.Errorf("amount is required")
	}
	// USDFC uses 18 decimals — same scale as FIL atto units.
	fil, err := types.ParseFIL(s)
	if err != nil {
		return nil, xerrors.Errorf("invalid USDFC amount %q: %w", s, err)
	}
	if fil.Sign() <= 0 {
		return nil, xerrors.Errorf("amount must be positive")
	}
	return fil.Int, nil
}
