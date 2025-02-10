package common

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"math/big"
	"strings"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/filecoin-project/go-address"
	abi2 "github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

const RouterMainnet = "0xaf44852Fd59169B8C079BA2e8c8380471b040C14"

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
		{"internalType": "uint64", "name": "clientID", "type": "uint64"},
		{"internalType": "uint256", "name": "cumulativeAmount", "type": "uint256"},
		{"internalType": "uint64", "name": "nonce", "type": "uint64"},
		{"internalType": "bytes", "name": "signature", "type": "bytes"}
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
		{"internalType": "uint64", "name": "providerID", "type": "uint64"},
		{"internalType": "uint256", "name": "cumulativeAmount", "type": "uint256"},
		{"internalType": "uint64", "name": "nonce", "type": "uint64"},
		{"internalType": "bytes", "name": "signature", "type": "bytes"}
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
		{"internalType": "uint256", "name": "amount", "type": "uint256"}
	  ],
	  "name": "serviceWithdraw",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

const GetClientStateABI = `[
	{
	  "inputs": [
	    {"internalType": "uint64", "name": "clientID", "type": "uint64"}
	  ],
	  "name": "getClientState",
	  "outputs": [
	    {"internalType": "uint256", "name": "balance", "type": "uint256"},
	    {"internalType": "uint256", "name": "voucherRedeemed", "type": "uint256"},
	    {"internalType": "uint64", "name": "lastNonce", "type": "uint64"}
	  ],
	  "stateMutability": "view",
	  "type": "function"
	}
]`

const GetProviderStateABI = `[
	{
	  "inputs": [
	    {"internalType": "uint64", "name": "providerID", "type": "uint64"}
	  ],
	  "name": "getProviderState",
	  "outputs": [
	    {"internalType": "uint256", "name": "voucherRedeemed", "type": "uint256"},
	    {"internalType": "uint64", "name": "lastNonce", "type": "uint64"}
	  ],
	  "stateMutability": "view",
	  "type": "function"
	}
]`

const GetServiceStateABI = `[
	{
	  "inputs": [],
	  "name": "getServiceState",
	  "outputs": [
	    {"internalType": "uint64", "name": "", "type": "uint64"},
	    {"internalType": "uint256", "name": "", "type": "uint256"}
	  ],
	  "stateMutability": "view",
	  "type": "function"
	}
]`

// --- Off-chain Voucher Types & Helpers ---

// ClientVoucher represents a voucher from a client.
type ClientVoucher struct {
	ClientID         uint64
	CumulativeAmount *big.Int
	Nonce            uint64
	Signature        []byte
}

// ProviderVoucher represents a voucher from the service authorizing payment to a provider.
type ProviderVoucher struct {
	ProviderID       uint64
	CumulativeAmount *big.Int
	Nonce            uint64
	Signature        []byte
}

// CreateClientVoucher creates the message bytes that the client must sign.
// It returns the bytes that should be signed (the same as constructed in the contract).
func CreateClientVoucher(router ethcommon.Address, clientID uint64, serviceActor uint64, cumulativeAmount *big.Int, nonce uint64) []byte {
	// We use tight packing: router (20 bytes), clientID (8 bytes), serviceActor (8 bytes), cumulativeAmount (32 bytes) and nonce (8 bytes).
	var buf bytes.Buffer
	// Write router (as 20 bytes)
	buf.Write(ethcommon.LeftPadBytes(router.Bytes(), 20))
	// Write clientID (big-endian 8 bytes)
	tmp := make([]byte, 8)
	binary.BigEndian.PutUint64(tmp, clientID)
	buf.Write(tmp)
	// Write serviceActor (8 bytes)
	binary.BigEndian.PutUint64(tmp, serviceActor)
	buf.Write(tmp)
	// Write cumulativeAmount as 32 bytes (left padded)
	caBytes := cumulativeAmount.Bytes()
	buf.Write(ethcommon.LeftPadBytes(caBytes, 32))
	// Write nonce (8 bytes)
	binary.BigEndian.PutUint64(tmp, nonce)
	buf.Write(tmp)
	return buf.Bytes()
}

// CreateProviderVoucher creates the message bytes that the service must sign for provider voucher.
func CreateProviderVoucher(router ethcommon.Address, serviceActor uint64, providerID uint64, cumulativeAmount *big.Int, nonce uint64) []byte {
	var buf bytes.Buffer
	buf.Write(ethcommon.LeftPadBytes(router.Bytes(), 20))
	// Write serviceActor (8 bytes)
	tmp := make([]byte, 8)
	binary.BigEndian.PutUint64(tmp, serviceActor)
	buf.Write(tmp)
	// Write providerID (8 bytes)
	binary.BigEndian.PutUint64(tmp, providerID)
	buf.Write(tmp)
	// Write cumulativeAmount (32 bytes)
	caBytes := cumulativeAmount.Bytes()
	buf.Write(ethcommon.LeftPadBytes(caBytes, 32))
	// Write nonce (8 bytes)
	binary.BigEndian.PutUint64(tmp, nonce)
	buf.Write(tmp)
	return buf.Bytes()
}

// SignVoucher signs the given voucher message with the provided private key.
func SignVoucher(message []byte, privKey *ecdsa.PrivateKey) ([]byte, error) {
	hash := crypto.Keccak256Hash(message)
	sig, err := crypto.Sign(hash.Bytes(), privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign voucher: %w", err)
	}
	return sig, nil
}

// VerifyVoucher verifies that the given signature is valid for the message.
func VerifyVoucher(message []byte, signature []byte, pubKey *ecdsa.PublicKey) bool {
	hash := crypto.Keccak256Hash(message)
	// Remove recovery byte if present.
	if len(signature) == 65 {
		signature = signature[:64]
	}
	return crypto.VerifySignature(crypto.FromECDSAPub(pubKey), hash.Bytes(), signature)
}

// VerifyVoucherUpdate compares two vouchers (the best accepted so far and the newly proposed voucher).
// It checks that the new voucher’s nonce is higher than the best accepted voucher’s nonce
// and that the new cumulative amount is higher. It returns the balance increase (difference).
func VerifyVoucherUpdate(best, proposed *ClientVoucher) (*big.Int, error) {
	if proposed.Nonce <= best.Nonce {
		return nil, fmt.Errorf("proposed voucher nonce is not higher than best accepted: %d <= %d", proposed.Nonce, best.Nonce)
	}
	if proposed.CumulativeAmount.Cmp(best.CumulativeAmount) <= 0 {
		return nil, fmt.Errorf("proposed voucher amount is not greater than best accepted")
	}
	increment := new(big.Int).Sub(proposed.CumulativeAmount, best.CumulativeAmount)
	return increment, nil
}

// --- Contract State Query Helpers ---

// GetClientState queries the contract for a client’s state.
func GetClientState(ctx context.Context, full api.FullNode, from, router address.Address, clientID uint64) (balance *big.Int, voucherRedeemed *big.Int, lastNonce uint64, err error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetClientStateABI))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to parse getClientState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getClientState", clientID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to pack getClientState call: %w", err)
	}
	// Use a StateCall message (read-only)
	res, err := full.StateCall(ctx, &types.Message{
		To:     router,
		From:   from,
		Value:  abi2.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}, types.EmptyTSK)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("StateCall failed: %w", err)
	}
	// Unpack the returned tuple (balance, voucherRedeemed, lastNonce)
	var out struct {
		Balance         *big.Int
		VoucherRedeemed *big.Int
		LastNonce       uint64
	}
	err = parsedABI.UnpackIntoInterface(&out, "getClientState", res.MsgRct.Return)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to unpack getClientState result: %w", err)
	}
	return out.Balance, out.VoucherRedeemed, out.LastNonce, nil
}

// GetProviderState queries the contract for a provider’s state.
func GetProviderState(ctx context.Context, full api.FullNode, from, router address.Address, providerID uint64) (voucherRedeemed *big.Int, lastNonce uint64, err error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetProviderStateABI))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse getProviderState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getProviderState", providerID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to pack getProviderState call: %w", err)
	}
	res, err := full.StateCall(ctx, &types.Message{
		To:     router,
		From:   from,
		Value:  abi2.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}, types.EmptyTSK)
	if err != nil {
		return nil, 0, fmt.Errorf("StateCall failed: %w", err)
	}
	var out struct {
		VoucherRedeemed *big.Int
		LastNonce       uint64
	}
	err = parsedABI.UnpackIntoInterface(&out, "getProviderState", res.MsgRct.Return)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to unpack getProviderState result: %w", err)
	}
	return out.VoucherRedeemed, out.LastNonce, nil
}

// GetServiceState queries the contract for the service state.
func GetServiceState(ctx context.Context, full api.FullNode, from, router address.Address) (serviceActor uint64, pool *big.Int, err error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetServiceStateABI))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to parse getServiceState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getServiceState")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to pack getServiceState call: %w", err)
	}
	res, err := full.StateCall(ctx, &types.Message{
		To:     router,
		From:   from,
		Value:  abi2.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}, types.EmptyTSK)
	if err != nil {
		return 0, nil, fmt.Errorf("StateCall failed: %w", err)
	}
	var out struct {
		ServiceActor uint64
		Pool         *big.Int
	}
	err = parsedABI.UnpackIntoInterface(&out, "getServiceState", res.MsgRct.Return)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to unpack getServiceState result: %w", err)
	}
	return out.ServiceActor, out.Pool, nil
}

// --- Withdraw Methods ---

// ServiceWithdraw sends a service withdrawal transaction to the Router contract.
// It calls the serviceWithdraw function with the desired amount (in attoFIL).
func ServiceWithdraw(ctx context.Context, full api.FullNode, from, router address.Address, amount abi2.TokenAmount) error {
	parsedABI, err := eabi.JSON(strings.NewReader(ServiceWithdrawABI))
	if err != nil {
		return fmt.Errorf("failed to parse serviceWithdraw ABI: %w", err)
	}
	data, err := parsedABI.Pack("serviceWithdraw", amount)
	if err != nil {
		return fmt.Errorf("failed to pack serviceWithdraw call: %w", err)
	}
	// For serviceWithdraw, the call value is zero.
	_, err = sendEVMMessage(ctx, full, from, router, abi2.NewTokenAmount(0), data)
	if err != nil {
		return fmt.Errorf("serviceWithdraw message failed: %w", err)
	}
	return nil
}

// --- Helper to send EVM messages ---
// sendEVMMessage creates and pushes a message to the Lotus full‐node.
// It wraps the packed call data into CBOR format.
func sendEVMMessage(
	ctx context.Context,
	full api.FullNode,
	from, to address.Address,
	value abi2.TokenAmount,
	data []byte,
) (*types.Message, error) {
	param := abi2.CborBytes(data)
	ser, aerr := actors.SerializeParams(&param)
	if aerr != nil {
		return nil, fmt.Errorf("failed to serialize params: %w", aerr)
	}
	msg := &types.Message{
		To:     to,
		From:   from,
		Value:  value,
		Method: builtin.MethodsEVM.InvokeContract,
		Params: ser,
	}
	signedMsg, err := full.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to push message: %w", err)
	}
	_, err = full.StateWaitMsg(ctx, signedMsg.Cid(), 1, 600, true)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for message: %w", err)
	}
	return signedMsg.VMMessage(), nil
}

// mustSerializeCBOR is a helper that wraps call data in a CBOR byte array.
func mustSerializeCBOR(data []byte) []byte {
	param := abi2.CborBytes(data)
	ser, err := actors.SerializeParams(&param)
	if err != nil {
		panic(fmt.Sprintf("failed to serialize params: %v", err))
	}
	return ser
}
