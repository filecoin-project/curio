package usdfcswap

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

const (
	erc20ABI = `[{"inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"}],"name":"allowance","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

	quoterABI = `[{"inputs":[{"components":[{"name":"tokenIn","type":"address"},{"name":"tokenOut","type":"address"},{"name":"amountIn","type":"uint256"},{"name":"fee","type":"uint24"},{"name":"sqrtPriceLimitX96","type":"uint160"}],"name":"params","type":"tuple"}],"name":"quoteExactInputSingle","outputs":[{"name":"amountOut","type":"uint256"},{"name":"sqrtPriceX96After","type":"uint160"},{"name":"initializedTicksCrossed","type":"uint32"},{"name":"gasEstimate","type":"uint256"}],"stateMutability":"nonpayable","type":"function"}]`

	// SwapRouter multicall: exactInputSingle (recipient=0 → router holds WFIL) then unwrapWETH9 → native FIL.
	routerABI = `[
		{"inputs":[{"components":[{"name":"tokenIn","type":"address"},{"name":"tokenOut","type":"address"},{"name":"fee","type":"uint24"},{"name":"recipient","type":"address"},{"name":"deadline","type":"uint256"},{"name":"amountIn","type":"uint256"},{"name":"amountOutMinimum","type":"uint256"},{"name":"sqrtPriceLimitX96","type":"uint160"}],"name":"params","type":"tuple"}],"name":"exactInputSingle","outputs":[{"name":"amountOut","type":"uint256"}],"stateMutability":"payable","type":"function"},
		{"inputs":[{"name":"amountMinimum","type":"uint256"},{"name":"recipient","type":"address"}],"name":"unwrapWETH9","outputs":[],"stateMutability":"payable","type":"function"},
		{"inputs":[{"name":"data","type":"bytes[]"}],"name":"multicall","outputs":[{"name":"results","type":"bytes[]"}],"stateMutability":"payable","type":"function"}
	]`

	defaultConfirmTimeout = 5 * time.Minute
	confirmPollInterval   = 3 * time.Second
	swapDeadlineSkew      = 20 * time.Minute
)

type quoteParams struct {
	TokenIn           common.Address
	TokenOut          common.Address
	AmountIn          *big.Int
	Fee               *big.Int
	SqrtPriceLimitX96 *big.Int
}

type exactInputSingleParams struct {
	TokenIn           common.Address
	TokenOut          common.Address
	Fee               *big.Int
	Recipient         common.Address
	Deadline          *big.Int
	AmountIn          *big.Int
	AmountOutMinimum  *big.Int
	SqrtPriceLimitX96 *big.Int
}

// QuoteResult is the expected native FIL out for a USDFC amount in.
type QuoteResult struct {
	AmountOut *big.Int
}

// ConvertResult holds hashes for the convert flow.
type ConvertResult struct {
	ApproveTxHash string
	SwapTxHash    string
	AmountOutMin  *big.Int
	QuotedOut     *big.Int
}

// Quote returns expected native FIL out for amountIn USDFC (18-decimal base units).
// Quotes the USDFC/WFIL pool; 1 WFIL = 1 FIL after router unwrap.
func Quote(ctx context.Context, client ethchain.EthClient, amountIn *big.Int) (*QuoteResult, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, xerrors.Errorf("amountIn must be positive")
	}
	usdfc, err := contract.USDFCAddress()
	if err != nil {
		return nil, err
	}
	wfil, _, quoter, fee, err := contract.SushiUsdfcFilAddresses()
	if err != nil {
		return nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(quoterABI))
	if err != nil {
		return nil, xerrors.Errorf("parse quoter abi: %w", err)
	}
	data, err := parsed.Pack("quoteExactInputSingle", quoteParams{
		TokenIn:           usdfc,
		TokenOut:          wfil,
		AmountIn:          amountIn,
		Fee:               big.NewInt(int64(fee)),
		SqrtPriceLimitX96: big.NewInt(0),
	})
	if err != nil {
		return nil, xerrors.Errorf("pack quoteExactInputSingle: %w", err)
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &quoter, Data: data}, nil)
	if err != nil {
		return nil, xerrors.Errorf("quoteExactInputSingle call: %w", err)
	}
	vals, err := parsed.Unpack("quoteExactInputSingle", out)
	if err != nil {
		return nil, xerrors.Errorf("unpack quoteExactInputSingle: %w", err)
	}
	if len(vals) == 0 {
		return nil, xerrors.Errorf("empty quoteExactInputSingle result")
	}
	amountOut, ok := vals[0].(*big.Int)
	if !ok || amountOut == nil {
		return nil, xerrors.Errorf("unexpected amountOut type %T", vals[0])
	}
	return &QuoteResult{AmountOut: new(big.Int).Set(amountOut)}, nil
}

// Convert swaps USDFC to native FIL via SushiSwap V3 in one router multicall
// (exactInputSingle into the router, then unwrapWETH9 to the PDP wallet).
// slippageBps is basis points (100 = 1%). If zero, defaults to 100.
func Convert(ctx context.Context, db *harmonydb.DB, client ethchain.EthClient, sender *message.SenderETH, from common.Address, amountIn *big.Int, slippageBps int) (*ConvertResult, error) {
	if sender == nil {
		return nil, xerrors.Errorf("ETH sender not available; enable PDP so the ETH send task is running")
	}
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, xerrors.Errorf("amountIn must be positive")
	}
	if slippageBps < 0 || slippageBps > 5000 {
		return nil, xerrors.Errorf("slippageBps must be between 0 and 5000")
	}
	if slippageBps == 0 {
		slippageBps = 100
	}

	usdfc, err := contract.USDFCAddress()
	if err != nil {
		return nil, err
	}
	wfil, router, _, fee, err := contract.SushiUsdfcFilAddresses()
	if err != nil {
		return nil, err
	}

	bal, err := erc20BalanceOf(ctx, client, usdfc, from)
	if err != nil {
		return nil, xerrors.Errorf("USDFC balance: %w", err)
	}
	if bal.Cmp(amountIn) < 0 {
		return nil, xerrors.Errorf("insufficient USDFC balance: have %s, need %s", bal.String(), amountIn.String())
	}

	filBal, err := client.BalanceAt(ctx, from, nil)
	if err != nil {
		return nil, xerrors.Errorf("FIL balance: %w", err)
	}
	if filBal == nil || filBal.Sign() <= 0 {
		return nil, xerrors.Errorf("PDP wallet has no FIL for gas")
	}

	quote, err := Quote(ctx, client, amountIn)
	if err != nil {
		return nil, err
	}
	amountOutMin := applySlippage(quote.AmountOut, slippageBps)
	if amountOutMin.Sign() <= 0 {
		return nil, xerrors.Errorf("amountOutMinimum is zero after slippage")
	}

	result := &ConvertResult{
		QuotedOut:    new(big.Int).Set(quote.AmountOut),
		AmountOutMin: amountOutMin,
	}

	erc20, err := abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		return nil, xerrors.Errorf("parse erc20 abi: %w", err)
	}

	allowance, err := erc20Allowance(ctx, client, erc20, usdfc, from, router)
	if err != nil {
		return nil, err
	}
	if allowance.Cmp(amountIn) < 0 {
		approveHash, err := sendApprove(ctx, db, sender, erc20, from, usdfc, router)
		if err != nil {
			return nil, err
		}
		result.ApproveTxHash = strings.ToLower(approveHash.Hex())
		if err := waitConfirmed(ctx, db, result.ApproveTxHash, defaultConfirmTimeout); err != nil {
			return result, xerrors.Errorf("waiting for USDFC approve confirmation: %w", err)
		}
	}

	swapHash, err := sendSwapToNativeFil(ctx, db, sender, from, usdfc, wfil, router, fee, amountIn, amountOutMin)
	if err != nil {
		return result, err
	}
	result.SwapTxHash = strings.ToLower(swapHash.Hex())
	return result, nil
}

func applySlippage(amountOut *big.Int, slippageBps int) *big.Int {
	// amountOut * (10000 - slippageBps) / 10000
	num := new(big.Int).Mul(amountOut, big.NewInt(int64(10000-slippageBps)))
	return num.Div(num, big.NewInt(10000))
}

func erc20BalanceOf(ctx context.Context, client ethchain.EthClient, token, account common.Address) (*big.Int, error) {
	parsed, err := abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		return nil, err
	}
	data, err := parsed.Pack("balanceOf", account)
	if err != nil {
		return nil, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	vals, err := parsed.Unpack("balanceOf", out)
	if err != nil {
		return nil, err
	}
	bal, ok := vals[0].(*big.Int)
	if !ok {
		return nil, xerrors.Errorf("unexpected balanceOf type %T", vals[0])
	}
	return bal, nil
}

func erc20Allowance(ctx context.Context, client ethchain.EthClient, parsed abi.ABI, token, owner, spender common.Address) (*big.Int, error) {
	data, err := parsed.Pack("allowance", owner, spender)
	if err != nil {
		return nil, xerrors.Errorf("pack allowance: %w", err)
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		return nil, xerrors.Errorf("allowance call: %w", err)
	}
	vals, err := parsed.Unpack("allowance", out)
	if err != nil {
		return nil, xerrors.Errorf("unpack allowance: %w", err)
	}
	a, ok := vals[0].(*big.Int)
	if !ok {
		return nil, xerrors.Errorf("unexpected allowance type %T", vals[0])
	}
	return a, nil
}

func sendApprove(ctx context.Context, db *harmonydb.DB, sender *message.SenderETH, erc20 abi.ABI, from, token, spender common.Address) (common.Hash, error) {
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	data, err := erc20.Pack("approve", spender, maxUint)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("pack approve: %w", err)
	}
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &token,
		Value:    big.NewInt(0),
		Gas:      0,
		GasPrice: nil,
		Data:     data,
	})
	hash, err := sender.Send(ctx, from, tx, "usdfc-approve-sushi")
	if err != nil {
		return common.Hash{}, xerrors.Errorf("send approve: %w", err)
	}
	if err := insertWait(ctx, db, hash); err != nil {
		return hash, err
	}
	return hash, nil
}

// sendSwapToNativeFil multicalls exactInputSingle + unwrapWETH9 so the wallet receives native FIL.
// Pool tokenOut is still WFIL (V3 pools are ERC-20 only); recipient address(0) keeps WFIL on the
// router, then unwrapWETH9 converts it to FIL and transfers to `from`.
func sendSwapToNativeFil(ctx context.Context, db *harmonydb.DB, sender *message.SenderETH, from, usdfc, wfil, router common.Address, fee uint32, amountIn, amountOutMin *big.Int) (common.Hash, error) {
	parsed, err := abi.JSON(strings.NewReader(routerABI))
	if err != nil {
		return common.Hash{}, xerrors.Errorf("parse router abi: %w", err)
	}
	deadline := big.NewInt(time.Now().Add(swapDeadlineSkew).Unix())
	swapData, err := parsed.Pack("exactInputSingle", exactInputSingleParams{
		TokenIn:           usdfc,
		TokenOut:          wfil,
		Fee:               big.NewInt(int64(fee)),
		Recipient:         common.Address{}, // address(0) → router custodians WFIL for unwrap
		Deadline:          deadline,
		AmountIn:          amountIn,
		AmountOutMinimum:  amountOutMin,
		SqrtPriceLimitX96: big.NewInt(0),
	})
	if err != nil {
		return common.Hash{}, xerrors.Errorf("pack exactInputSingle: %w", err)
	}
	unwrapData, err := parsed.Pack("unwrapWETH9", amountOutMin, from)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("pack unwrapWETH9: %w", err)
	}
	data, err := parsed.Pack("multicall", [][]byte{swapData, unwrapData})
	if err != nil {
		return common.Hash{}, xerrors.Errorf("pack multicall: %w", err)
	}
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &router,
		Value:    big.NewInt(0),
		Gas:      0,
		GasPrice: nil,
		Data:     data,
	})
	hash, err := sender.Send(ctx, from, tx, "usdfc-to-fil")
	if err != nil {
		return common.Hash{}, xerrors.Errorf("send swap: %w", err)
	}
	if err := insertWait(ctx, db, hash); err != nil {
		return hash, err
	}
	return hash, nil
}

func insertWait(ctx context.Context, db *harmonydb.DB, hash common.Hash) error {
	txHashHex := strings.ToLower(hash.Hex())
	_, err := db.Exec(ctx, `INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending') ON CONFLICT (signed_tx_hash) DO NOTHING`, txHashHex)
	if err != nil {
		return xerrors.Errorf("insert message_waits_eth: %w", err)
	}
	return nil
}

func waitConfirmed(ctx context.Context, db *harmonydb.DB, txHashHex string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for eth tx %s confirmation", txHashHex)
		}
		var status string
		var success *bool
		err := db.QueryRow(ctx, `SELECT tx_status, tx_success FROM message_waits_eth WHERE signed_tx_hash = $1`, txHashHex).Scan(&status, &success)
		if err != nil {
			return xerrors.Errorf("query message_waits_eth: %w", err)
		}
		if status == "failed" {
			return fmt.Errorf("eth tx %s failed", txHashHex)
		}
		if status == "confirmed" {
			if success != nil && !*success {
				return fmt.Errorf("eth tx %s confirmed but failed on-chain", txHashHex)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(confirmPollInterval):
		}
	}
}
