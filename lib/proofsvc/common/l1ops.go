package common

import (
	"context"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"strings"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

const RouterMainnet = "0xFF3e05AEe9349d40fD52FaF5cC1B567536a100Bb"

func Router() address.Address {
	to, err := ethtypes.ParseEthAddress(RouterMainnet)
	if err != nil {
		panic(err)
	}

	toAddr, err := to.ToFilecoinAddress()
	if err != nil {
		panic(err)
	}

	return toAddr
}

// --- ABI definitions for the Router contract ---

const DepositABI = `[
	{
	  "inputs": [],
	  "name": "deposit",
	  "outputs": [],
	  "stateMutability": "payable",
	  "type": "function"
	}
]`

const RedeemClientVoucherABI = `[
	{
	  "inputs": [
		{
		  "internalType": "uint64",
		  "name": "clientID",
		  "type": "uint64"
		},
		{
		  "internalType": "uint256",
		  "name": "cumulativeAmount",
		  "type": "uint256"
		},
		{
		  "internalType": "uint64",
		  "name": "nonce",
		  "type": "uint64"
		},
		{
		  "internalType": "bytes",
		  "name": "signature",
		  "type": "bytes"
		}
	  ],
	  "name": "redeemClientVoucher",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

const RedeemProviderVoucherABI = `[
	{
	  "inputs": [
		{
		  "internalType": "uint64",
		  "name": "providerID",
		  "type": "uint64"
		},
		{
		  "internalType": "uint256",
		  "name": "cumulativeAmount",
		  "type": "uint256"
		},
		{
		  "internalType": "uint64",
		  "name": "nonce",
		  "type": "uint64"
		},
		{
		  "internalType": "bytes",
		  "name": "signature",
		  "type": "bytes"
		}
	  ],
	  "name": "redeemProviderVoucher",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

const ServiceWithdrawABI = `[
	{
	  "inputs": [
		{
		  "internalType": "uint256",
		  "name": "amount",
		  "type": "uint256"
		}
	  ],
	  "name": "serviceWithdraw",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

// --- Helper: send a message invoking an EVM contract ---

// sendEVMMessage creates and pushes a message to the Lotus full‐node.
// It uses the EVM invoke method (builtin.MethodsEVM.InvokeContract) and serializes the call data as CBOR.
// For brevity, gas–estimation and error–handling here are minimal.
func sendEVMMessage(
	ctx context.Context,
	full api.FullNode,
	from address.Address,
	to address.Address,
	value abi.TokenAmount,
	data []byte,
) (*types.Message, error) {

	// Wrap the packed call data in an abi.CborBytes type and serialize it.
	param := abi.CborBytes(data)
	serialized, aerr := actors.SerializeParams(&param)
	if aerr != nil {
		return nil, fmt.Errorf("failed to serialize params: %w", aerr)
	}

	// Construct the message.
	msg := &types.Message{
		To:     to,
		From:   from,
		Value:  value,
		Method: builtin.MethodsEVM.InvokeContract,
		Params: serialized,
	}

	// Push the message (here we assume synchronous submission)
	signedMsg, err := full.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to push message: %w", err)
	}

	// Optionally, wait for the message to be included.
	_, err = full.StateWaitMsg(ctx, signedMsg.Cid(), 1, 600, true)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for message: %w", err)
	}

	return signedMsg.VMMessage(), nil
}

// --- Client API ---

// ClientDeposit submits a deposit transaction from a client into the Router contract.
// from is the client’s Filecoin EVM address; router is the deployed contract address.
// amount is the FIL amount to deposit (in attoFIL).
func ClientDeposit(ctx context.Context, full api.FullNode, from, router address.Address, amount abi.TokenAmount) error {
	parsedABI, err := eabi.JSON(strings.NewReader(DepositABI))
	if err != nil {
		return fmt.Errorf("failed to parse deposit ABI: %w", err)
	}

	// Pack the call (deposit has no parameters)
	data, err := parsedABI.Pack("deposit")
	if err != nil {
		return fmt.Errorf("failed to pack deposit call: %w", err)
	}

	_, err = sendEVMMessage(ctx, full, from, router, amount, data)
	if err != nil {
		return fmt.Errorf("deposit message failed: %w", err)
	}

	return nil
}

// --- Service API ---

// ServiceRedeemClientVoucher allows the service to redeem a client voucher.
// service is the service’s Filecoin EVM address (which must correspond to serviceActor).
// clientID is the client’s actor ID (uint64).
// cumulativeAmount is the new cumulative amount (as *big.Int).
// nonce is the voucher nonce.
// signature is the raw signature bytes provided by the client.
func ServiceRedeemClientVoucher(
	ctx context.Context,
	full api.FullNode,
	service, router address.Address,
	clientID uint64,
	cumulativeAmount abi.TokenAmount,
	nonce uint64,
	signature []byte,
) error {
	parsedABI, err := eabi.JSON(strings.NewReader(RedeemClientVoucherABI))
	if err != nil {
		return fmt.Errorf("failed to parse redeemClientVoucher ABI: %w", err)
	}

	// Pack the function call data.
	data, err := parsedABI.Pack("redeemClientVoucher", clientID, cumulativeAmount, nonce, signature)
	if err != nil {
		return fmt.Errorf("failed to pack redeemClientVoucher call: %w", err)
	}

	// Service calls this with zero value.
	_, err = sendEVMMessage(ctx, full, service, router, big.Zero(), data)
	if err != nil {
		return fmt.Errorf("redeemClientVoucher message failed: %w", err)
	}

	return nil
}

// ServiceWithdraw allows the service to withdraw funds from the service pool.
// amount is the FIL amount to withdraw.
func ServiceWithdraw(
	ctx context.Context,
	full api.FullNode,
	service, router address.Address,
	amount abi.TokenAmount,
) error {
	parsedABI, err := eabi.JSON(strings.NewReader(ServiceWithdrawABI))
	if err != nil {
		return fmt.Errorf("failed to parse serviceWithdraw ABI: %w", err)
	}

	data, err := parsedABI.Pack("serviceWithdraw", amount)
	if err != nil {
		return fmt.Errorf("failed to pack serviceWithdraw call: %w", err)
	}

	_, err = sendEVMMessage(ctx, full, service, router, big.Zero(), data)
	if err != nil {
		return fmt.Errorf("serviceWithdraw message failed: %w", err)
	}

	return nil
}

// --- Provider API ---

// ProviderRedeemVoucher allows a provider to redeem a service voucher.
// provider is the provider’s Filecoin EVM address.
// providerID is the provider’s actor ID (uint64).
// cumulativeAmount is the new cumulative amount (as *big.Int).
// nonce is the voucher nonce.
// signature is the raw signature provided by the service.
func ProviderRedeemVoucher(
	ctx context.Context,
	full api.FullNode,
	provider, router address.Address,
	providerID uint64,
	cumulativeAmount abi.TokenAmount,
	nonce uint64,
	signature []byte,
) error {
	parsedABI, err := eabi.JSON(strings.NewReader(RedeemProviderVoucherABI))
	if err != nil {
		return fmt.Errorf("failed to parse redeemProviderVoucher ABI: %w", err)
	}

	data, err := parsedABI.Pack("redeemProviderVoucher", providerID, cumulativeAmount, nonce, signature)
	if err != nil {
		return fmt.Errorf("failed to pack redeemProviderVoucher call: %w", err)
	}

	_, err = sendEVMMessage(ctx, full, provider, router, big.Zero(), data)
	if err != nil {
		return fmt.Errorf("redeemProviderVoucher message failed: %w", err)
	}

	return nil
}
