package common

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"github.com/filecoin-project/lotus/lib/must"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	fbig "github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

// RouterMainnet is the Ethereum form of the router address. This is just an example.
const RouterMainnet = "0x287F8d531655435DF8114BCC0977663dC7F10049"
const serviceActor = 3370466

var Service = must.One(address.NewIDAddress(serviceActor))

// Router returns the Filecoin address of the router.
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

// ToFilBig converts a standard library *big.Int to a filecoin-project big.Int
func ToFilBig(x *big.Int) fbig.Int {
	return fbig.NewFromGo(x)
}

// --- ABI definitions for the Router contract calls ---

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
	    {
          "internalType": "CommonTypes.FilActorId",
          "name": "serviceActor",
          "type": "uint64"
        },
	    {"internalType": "uint256", "name": "servicePool", "type": "uint256"}
	  ],
	  "stateMutability": "view",
	  "type": "function"
	}
]`

const CreateClientVoucherABI = `[
	{
		"inputs": [
			{ "internalType": "uint64", "name": "clientID", "type": "uint64" },
			{ "internalType": "uint256", "name": "cumulativeAmount", "type": "uint256" },
			{ "internalType": "uint64", "name": "nonce", "type": "uint64" }
		],
		"name": "createClientVoucher",
		"outputs": [
			{ "internalType": "bytes", "name": "", "type": "bytes" }
		],
		"stateMutability": "view",
		"type": "function"
	}
]`

const CreateProviderVoucherABI = `[
	{
		"inputs": [
			{ "internalType": "uint64", "name": "providerID", "type": "uint64" },
			{ "internalType": "uint256", "name": "cumulativeAmount", "type": "uint256" },
			{ "internalType": "uint64", "name": "nonce", "type": "uint64" }
		],
		"name": "createProviderVoucher",
		"outputs": [
			{ "internalType": "bytes", "name": "", "type": "bytes" }
		],
		"stateMutability": "view",
		"type": "function"
	}
]`

// --- Implementation for missing deposit, voucher redemption, etc. ---

// ClientDeposit calls `deposit()` with a pay value = the deposit amount in attoFIL
func ClientDeposit(
	ctx context.Context,
	full api.FullNode,
	from, router address.Address,
	amount fbig.Int,
) error {
	parsedABI, err := eabi.JSON(strings.NewReader(DepositABI))
	if err != nil {
		return fmt.Errorf("parse deposit ABI: %w", err)
	}
	data, err := parsedABI.Pack("deposit")
	if err != nil {
		return fmt.Errorf("pack deposit call: %w", err)
	}
	_, err = sendEVMMessage(ctx, full, from, router, amount, data)
	if err != nil {
		return fmt.Errorf("deposit message failed: %w", err)
	}
	return nil
}

// ServiceRedeemClientVoucher calls `redeemClientVoucher(clientID, cumulativeAmount, nonce, signature)`.
func ServiceRedeemClientVoucher(
	ctx context.Context,
	full api.FullNode,
	from, router address.Address,
	clientID uint64,
	cumulativeAmount fbig.Int,
	nonce uint64,
	sig []byte,
) error {
	parsedABI, err := eabi.JSON(strings.NewReader(RedeemClientVoucherABI))
	if err != nil {
		return fmt.Errorf("parse redeemClientVoucher ABI: %w", err)
	}
	data, err := parsedABI.Pack("redeemClientVoucher", clientID, cumulativeAmount.Int, nonce, sig)
	if err != nil {
		return fmt.Errorf("pack redeemClientVoucher: %w", err)
	}
	_, err = sendEVMMessage(ctx, full, from, router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("redeemClientVoucher message failed: %w", err)
	}
	return nil
}

// ServiceRedeemProviderVoucher calls `redeemProviderVoucher(providerID, cumulativeAmount, nonce, signature)`.
func ServiceRedeemProviderVoucher(
	ctx context.Context,
	full api.FullNode,
	from, router address.Address,
	providerID uint64,
	cumulativeAmount fbig.Int,
	nonce uint64,
	sig []byte,
) error {
	parsedABI, err := eabi.JSON(strings.NewReader(RedeemProviderVoucherABI))
	if err != nil {
		return fmt.Errorf("parse redeemProviderVoucher ABI: %w", err)
	}
	data, err := parsedABI.Pack("redeemProviderVoucher", providerID, cumulativeAmount.Int, nonce, sig)
	if err != nil {
		return fmt.Errorf("pack redeemProviderVoucher: %w", err)
	}

	fmt.Printf("redeemProviderVoucher(%d, %d, %d, %x) data: %x\n", providerID, cumulativeAmount.Int, nonce, sig, data)

	_, err = sendEVMMessage(ctx, full, from, router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("redeemProviderVoucher message failed: %w", err)
	}
	return nil
}

// ServiceWithdraw calls `serviceWithdraw(amount)`.
func ServiceWithdraw(
	ctx context.Context,
	full api.FullNode,
	amount fbig.Int,
) error {
	parsedABI, err := eabi.JSON(strings.NewReader(ServiceWithdrawABI))
	if err != nil {
		return fmt.Errorf("parse serviceWithdraw ABI: %w", err)
	}
	data, err := parsedABI.Pack("serviceWithdraw", amount.Int)
	if err != nil {
		return fmt.Errorf("pack serviceWithdraw: %w", err)
	}

	router := Router()
	from := Service

	_, err = sendEVMMessage(ctx, full, from, router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("serviceWithdraw message failed: %w", err)
	}
	return nil
}

// --- Off-chain voucher creation/verification (unchanged) ---

// ClientVoucher ...
type ClientVoucher struct {
	ClientID         uint64
	CumulativeAmount *big.Int
	Nonce            uint64
	Signature        []byte
}

// ProviderVoucher ...
type ProviderVoucher struct {
	ProviderID       uint64
	CumulativeAmount *big.Int
	Nonce            uint64
	Signature        []byte
}

func CreateClientVoucher(ctx context.Context, full api.FullNode, clientID uint64, cumulativeAmount *big.Int, nonce uint64) ([]byte, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(CreateClientVoucherABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse CreateClientVoucher ABI: %w", err)
	}

	data, err := parsedABI.Pack("createClientVoucher", clientID, cumulativeAmount, nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to pack createClientVoucher call: %w", err)
	}

	router := Router()

	msg := &types.Message{
		To:     router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}

	res, err := full.StateCall(ctx, msg, types.EmptyTSK)
	if err != nil {
		return nil, fmt.Errorf("StateCall for createClientVoucher failed: %w", err)
	}

	var rawBytes abi.CborBytes
	if err := rawBytes.UnmarshalCBOR(bytes.NewReader(res.MsgRct.Return)); err != nil {
		return nil, fmt.Errorf("failed to unmarshal createClientVoucher result: %w", err)
	}

	var voucher []byte
	if err := parsedABI.UnpackIntoInterface(&voucher, "createClientVoucher", rawBytes); err != nil {
		return nil, fmt.Errorf("failed to unpack createClientVoucher result: %w", err)
	}

	return voucher, nil
}

func CreateProviderVoucher(ctx context.Context, full api.FullNode, providerID uint64, cumulativeAmount *big.Int, nonce uint64) ([]byte, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(CreateProviderVoucherABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse CreateProviderVoucher ABI: %w", err)
	}

	data, err := parsedABI.Pack("createProviderVoucher", providerID, cumulativeAmount, nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to pack createProviderVoucher call: %w", err)
	}

	router := Router()

	msg := &types.Message{
		To:     router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}

	res, err := full.StateCall(ctx, msg, types.EmptyTSK)
	if err != nil {
		return nil, fmt.Errorf("StateCall for createProviderVoucher failed: %w", err)
	}

	var rawBytes abi.CborBytes
	if err := rawBytes.UnmarshalCBOR(bytes.NewReader(res.MsgRct.Return)); err != nil {
		return nil, fmt.Errorf("failed to unmarshal createProviderVoucher result: %w", err)
	}

	var voucher []byte
	if err := parsedABI.UnpackIntoInterface(&voucher, "createProviderVoucher", rawBytes); err != nil {
		return nil, fmt.Errorf("failed to unpack createProviderVoucher result: %w", err)
	}

	return voucher, nil
}

// VerifyVoucherUpdate ...
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

func GetClientState(ctx context.Context, full api.FullNode, router address.Address, clientID uint64) (fbig.Int, fbig.Int, uint64, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetClientStateABI))
	if err != nil {
		return fbig.Int{}, fbig.Int{}, 0, fmt.Errorf("failed to parse getClientState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getClientState", clientID)
	if err != nil {
		return fbig.Int{}, fbig.Int{}, 0, fmt.Errorf("failed to pack getClientState call: %w", err)
	}
	res, err := full.StateCall(ctx, &types.Message{
		To:     router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}, types.EmptyTSK)
	if err != nil {
		return fbig.Int{}, fbig.Int{}, 0, fmt.Errorf("StateCall failed: %w", err)
	}

	var rawBytes abi.CborBytes
	if err := rawBytes.UnmarshalCBOR(bytes.NewReader(res.MsgRct.Return)); err != nil {
		return fbig.Int{}, fbig.Int{}, 0, fmt.Errorf("failed to unmarshal getClientState result: %w", err)
	}

	var out struct {
		Balance         *big.Int `abi:"balance"`
		VoucherRedeemed *big.Int `abi:"voucherRedeemed"`
		LastNonce       uint64   `abi:"lastNonce"`
	}
	err = parsedABI.UnpackIntoInterface(&out, "getClientState", rawBytes)
	if err != nil {
		return fbig.Int{}, fbig.Int{}, 0, fmt.Errorf("failed to unpack getClientState result: %w", err)
	}
	return fbig.NewFromGo(out.Balance), fbig.NewFromGo(out.VoucherRedeemed), out.LastNonce, nil
}

func GetProviderState(ctx context.Context, full api.FullNode, router address.Address, providerID uint64) (fbig.Int, uint64, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetProviderStateABI))
	if err != nil {
		return fbig.Int{}, 0, fmt.Errorf("failed to parse getProviderState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getProviderState", providerID)
	if err != nil {
		return fbig.Int{}, 0, fmt.Errorf("failed to pack getProviderState call: %w", err)
	}
	res, err := full.StateCall(ctx, &types.Message{
		To:     router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}, types.EmptyTSK)
	if err != nil {
		return fbig.Int{}, 0, fmt.Errorf("StateCall failed: %w", err)
	}

	var rawBytes abi.CborBytes
	if err := rawBytes.UnmarshalCBOR(bytes.NewReader(res.MsgRct.Return)); err != nil {
		return fbig.Int{}, 0, fmt.Errorf("failed to unmarshal getProviderState result: %w", err)
	}

	var out struct {
		VoucherRedeemed *big.Int `abi:"voucherRedeemed"`
		LastNonce       uint64   `abi:"lastNonce"`
	}
	err = parsedABI.UnpackIntoInterface(&out, "getProviderState", rawBytes)
	if err != nil {
		return fbig.Int{}, 0, fmt.Errorf("failed to unpack getProviderState: %w", err)
	}
	return fbig.NewFromGo(out.VoucherRedeemed), out.LastNonce, nil
}

func GetServiceState(ctx context.Context, full api.FullNode, router address.Address) (uint64, fbig.Int, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetServiceStateABI))
	if err != nil {
		return 0, fbig.Int{}, fmt.Errorf("failed to parse getServiceState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getServiceState")
	if err != nil {
		return 0, fbig.Int{}, fmt.Errorf("failed to pack getServiceState call: %w", err)
	}
	res, err := full.StateCall(ctx, &types.Message{
		To:     router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}, types.EmptyTSK)
	if err != nil {
		return 0, fbig.Int{}, fmt.Errorf("StateCall failed: %w", err)
	}
	if res.MsgRct.ExitCode != exitcode.Ok {
		return 0, fbig.Int{}, fmt.Errorf("getServiceState returned non-ok exit code: %s", res.MsgRct.ExitCode)
	}

	var rawBytes abi.CborBytes
	if err := rawBytes.UnmarshalCBOR(bytes.NewReader(res.MsgRct.Return)); err != nil {
		return 0, fbig.Int{}, fmt.Errorf("failed to unmarshal getServiceState result: %w (%x)", err, res.MsgRct.Return)
	}

	var out struct {
		ServiceActor uint64 `abi:"serviceActor"`
		ServicePool  *big.Int `abi:"servicePool"`
	}

	err = parsedABI.UnpackIntoInterface(&out, "getServiceState", rawBytes)
	if err != nil {
		return 0, fbig.Int{}, fmt.Errorf("failed to unpack getServiceState: %w (%x)", err, rawBytes)
	}
	return out.ServiceActor, fbig.NewFromGo(out.ServicePool), nil
}

// --- EVM invocation helper ---

func sendEVMMessage(
	ctx context.Context,
	full api.FullNode,
	from, to address.Address,
	value fbig.Int,
	data []byte,
) (*types.Message, error) {
	param := abi.CborBytes(data)
	ser, aerr := actors.SerializeParams(&param)
	if aerr != nil {
		return nil, fmt.Errorf("failed to serialize params: %w", aerr)
	}
	msg := &types.Message{
		To:     to,
		From:   from,
		Value:  value, // filecoin big.Int
		Method: builtin.MethodsEVM.InvokeContract,
		Params: ser,
	}
	signedMsg, err := full.MpoolPushMessage(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to push message: %w", err)
	}

	fmt.Printf("Pushed message: %s\n", signedMsg.Cid())

	_, err = full.StateWaitMsg(ctx, signedMsg.Cid(), 2, 600, true)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for message: %w", err)
	}
	return signedMsg.VMMessage(), nil
}

// mustSerializeCBOR is a helper that wraps call data in a CBOR byte array.
func mustSerializeCBOR(data []byte) []byte {
	param := abi.CborBytes(data)
	ser, err := actors.SerializeParams(&param)
	if err != nil {
		panic(fmt.Sprintf("failed to serialize params: %v", err))
	}
	return ser
}
