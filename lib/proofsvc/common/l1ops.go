package common

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	"github.com/ipfs/go-cid"

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
const RouterMainnet = "0x5D2Ce039F95AaF167DEcef2028F48f6bAcC5a586"


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

const InitiateClientWithdrawalABI = `[
	{
	  "inputs": [
		{"internalType": "uint256", "name": "amount", "type": "uint256"}
	  ],
	  "name": "initiateClientWithdrawal",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

const CompleteClientWithdrawalABI = `[
	{
	  "inputs": [],
	  "name": "completeClientWithdrawal",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

const CancelClientWithdrawalABI = `[
	{
	  "inputs": [],
	  "name": "cancelClientWithdrawal",
	  "outputs": [],
	  "stateMutability": "nonpayable",
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

const InitiateServiceWithdrawalABI = `[
	{
	  "inputs": [
		{"internalType": "uint256", "name": "amount", "type": "uint256"}
	  ],
	  "name": "initiateServiceWithdrawal",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

const CompleteServiceWithdrawalABI = `[
	{
	  "inputs": [],
	  "name": "completeServiceWithdrawal",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

const CancelServiceWithdrawalABI = `[
	{
	  "inputs": [],
	  "name": "cancelServiceWithdrawal",
	  "outputs": [],
	  "stateMutability": "nonpayable",
	  "type": "function"
	}
]`

const ServiceDepositABI = `[
	{
	  "inputs": [],
	  "name": "serviceDeposit",
	  "outputs": [],
	  "stateMutability": "payable",
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
	    {"internalType": "uint64", "name": "lastNonce", "type": "uint64"},
	    {"internalType": "uint256", "name": "withdrawAmount", "type": "uint256"},
	    {"internalType": "uint256", "name": "withdrawTimestamp", "type": "uint256"}
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
	    {"internalType": "uint256", "name": "servicePool", "type": "uint256"},
	    {"internalType": "uint256", "name": "pendingWithdrawalAmount", "type": "uint256"},
	    {"internalType": "uint256", "name": "pendingWithdrawalTimestamp", "type": "uint256"},
	    {"internalType": "CommonTypes.FilActorId", "name": "pendingActor", "type": "uint64"},
	    {"internalType": "uint256", "name": "actorChangeTimestamp", "type": "uint256"}
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

const ProposeServiceActorABI = `[
	{
		"inputs": [
			{ "internalType": "uint64", "name": "newServiceActor", "type": "uint64" }
		],
		"name": "proposeServiceActor",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

const AcceptServiceActorABI = `[
	{
		"inputs": [],
		"name": "acceptServiceActor",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

type Service struct {
	router address.Address
	full   api.FullNode
	sendMessage func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error)
}

func NewServiceCustomSend(full api.FullNode, sendMessage func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error)) *Service {
	return &Service{router: Router(), full: full, sendMessage: sendMessage}
}

func NewService(full api.FullNode) *Service {
	return &Service{router: Router(), full: full, sendMessage: func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error) {
		sm, err := full.MpoolPushMessage(ctx, msg, mss)
		if err != nil {
			return cid.Undef, err
		}
		return sm.Cid(), nil
	}}
}

// ClientDeposit calls `deposit()` with a pay value = the deposit amount in attoFIL and returns the deposit cid
func (s *Service) ClientDeposit(
	ctx context.Context,
	from address.Address,
	amount fbig.Int,
) (cid.Cid, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(DepositABI))
	if err != nil {
		return cid.Undef, fmt.Errorf("parse deposit ABI: %w", err)
	}
	data, err := parsedABI.Pack("deposit")
	if err != nil {
		return cid.Undef, fmt.Errorf("pack deposit call: %w", err)
	}
	depositCid, err := s.sendEVMMessage(ctx, from, s.router, amount, data)
	if err != nil {
		return cid.Undef, fmt.Errorf("deposit message failed: %w", err)
	}
	return depositCid, nil
}

func (s *Service) ClientInitiateWithdrawal(ctx context.Context, from address.Address, amount fbig.Int) error {
	parsedABI, err := eabi.JSON(strings.NewReader(InitiateClientWithdrawalABI))
	if err != nil {
		return fmt.Errorf("parse initiateClientWithdrawal ABI: %w", err)
	}
	data, err := parsedABI.Pack("initiateClientWithdrawal", amount.Int)
	if err != nil {
		return fmt.Errorf("pack initiateClientWithdrawal: %w", err)
	}

	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("initiateClientWithdrawal message failed: %w", err)
	}
	return nil
}

func (s *Service) ClientCompleteWithdrawal(ctx context.Context, from address.Address) error {
	parsedABI, err := eabi.JSON(strings.NewReader(CompleteClientWithdrawalABI))
	if err != nil {
		return fmt.Errorf("parse completeClientWithdrawal ABI: %w", err)
	}
	data, err := parsedABI.Pack("completeClientWithdrawal")
	if err != nil {
		return fmt.Errorf("pack completeClientWithdrawal: %w", err)
	}

	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("completeClientWithdrawal message failed: %w", err)
	}
	return nil
}

func (s *Service) ClientCancelWithdrawal(ctx context.Context, from address.Address) error {
	parsedABI, err := eabi.JSON(strings.NewReader(CancelClientWithdrawalABI))
	if err != nil {
		return fmt.Errorf("parse cancelClientWithdrawal ABI: %w", err)
	}
	data, err := parsedABI.Pack("cancelClientWithdrawal")
	if err != nil {
		return fmt.Errorf("pack cancelClientWithdrawal: %w", err)
	}

	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("cancelClientWithdrawal message failed: %w", err)
	}
	return nil
}

// ServiceRedeemClientVoucher calls `redeemClientVoucher(clientID, cumulativeAmount, nonce, signature)`.
func (s *Service) ServiceRedeemClientVoucher(
	ctx context.Context,
	from address.Address,
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
	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("redeemClientVoucher message failed: %w", err)
	}
	return nil
}

// ServiceRedeemProviderVoucher calls `redeemProviderVoucher(providerID, cumulativeAmount, nonce, signature)`.
func (s *Service) ServiceRedeemProviderVoucher(
	ctx context.Context,
	from address.Address,
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

	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("redeemProviderVoucher message failed: %w", err)
	}
	return nil
}

func (s *Service) ServiceInitiateWithdrawal(ctx context.Context, from address.Address, amount fbig.Int) error {
	parsedABI, err := eabi.JSON(strings.NewReader(InitiateServiceWithdrawalABI))
	if err != nil {
		return fmt.Errorf("parse initiateServiceWithdrawal ABI: %w", err)
	}
	data, err := parsedABI.Pack("initiateServiceWithdrawal", amount.Int)
	if err != nil {
		return fmt.Errorf("pack initiateServiceWithdrawal: %w", err)
	}

	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("initiateServiceWithdrawal message failed: %w", err)
	}
	return nil
}

func (s *Service) ServiceCompleteWithdrawal(ctx context.Context, from address.Address) error {
	parsedABI, err := eabi.JSON(strings.NewReader(CompleteServiceWithdrawalABI))
	if err != nil {
		return fmt.Errorf("parse completeServiceWithdrawal ABI: %w", err)
	}
	data, err := parsedABI.Pack("completeServiceWithdrawal")
	if err != nil {
		return fmt.Errorf("pack completeServiceWithdrawal: %w", err)
	}

	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("completeServiceWithdrawal message failed: %w", err)
	}
	return nil
}

func (s *Service) ServiceCancelWithdrawal(ctx context.Context, from address.Address) error {
	parsedABI, err := eabi.JSON(strings.NewReader(CancelServiceWithdrawalABI))
	if err != nil {
		return fmt.Errorf("parse cancelServiceWithdrawal ABI: %w", err)
	}
	data, err := parsedABI.Pack("cancelServiceWithdrawal")
	if err != nil {
		return fmt.Errorf("pack cancelServiceWithdrawal: %w", err)
	}

	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("cancelServiceWithdrawal message failed: %w", err)
	}
	return nil
}

func (s *Service) ServiceDeposit(ctx context.Context, from address.Address, amount fbig.Int) error {
	parsedABI, err := eabi.JSON(strings.NewReader(ServiceDepositABI))
	if err != nil {
		return fmt.Errorf("parse serviceDeposit ABI: %w", err)
	}
	data, err := parsedABI.Pack("serviceDeposit")
	if err != nil {
		return fmt.Errorf("pack serviceDeposit: %w", err)
	}

	_, err = s.sendEVMMessage(ctx, from, s.router, amount, data)
	if err != nil {
		return fmt.Errorf("serviceDeposit message failed: %w", err)
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

func (s *Service) CreateClientVoucher(ctx context.Context, clientID uint64, cumulativeAmount *big.Int, nonce uint64) ([]byte, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(CreateClientVoucherABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse CreateClientVoucher ABI: %w", err)
	}

	data, err := parsedABI.Pack("createClientVoucher", clientID, cumulativeAmount, nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to pack createClientVoucher call: %w", err)
	}

	msg := &types.Message{
		To:     s.router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}

	res, err := s.full.StateCall(ctx, msg, types.EmptyTSK)
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

func (s *Service) CreateProviderVoucher(ctx context.Context, providerID uint64, cumulativeAmount *big.Int, nonce uint64) ([]byte, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(CreateProviderVoucherABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse CreateProviderVoucher ABI: %w", err)
	}

	data, err := parsedABI.Pack("createProviderVoucher", providerID, cumulativeAmount, nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to pack createProviderVoucher call: %w", err)
	}

	msg := &types.Message{
		To:     s.router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}

	res, err := s.full.StateCall(ctx, msg, types.EmptyTSK)
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

func (s *Service) VerifyVoucherUpdate(best, proposed *ClientVoucher) (*big.Int, error) {
	if proposed.Nonce <= best.Nonce {
		return nil, fmt.Errorf("proposed voucher nonce is not higher than best accepted: %d <= %d", proposed.Nonce, best.Nonce)
	}
	if proposed.CumulativeAmount.Cmp(best.CumulativeAmount) <= 0 {
		return nil, fmt.Errorf("proposed voucher amount is not greater than best accepted")
	}
	increment := new(big.Int).Sub(proposed.CumulativeAmount, best.CumulativeAmount)
	return increment, nil
}

type ClientState struct {
	Balance           fbig.Int
	VoucherRedeemed   fbig.Int
	LastNonce         uint64
	WithdrawAmount    fbig.Int
	WithdrawTimestamp fbig.Int
}

func (s *Service) GetClientState(ctx context.Context, clientID uint64) (*ClientState, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetClientStateABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse getClientState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getClientState", clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to pack getClientState call: %w", err)
	}
	res, err := s.full.StateCall(ctx, &types.Message{
		To:     s.router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}, types.EmptyTSK)
	if err != nil {
		return nil, fmt.Errorf("StateCall failed: %w", err)
	}

	var rawBytes abi.CborBytes
	if err := rawBytes.UnmarshalCBOR(bytes.NewReader(res.MsgRct.Return)); err != nil {
		return nil, fmt.Errorf("failed to unmarshal getClientState result: %w", err)
	}

	var out struct {
		Balance           *big.Int `abi:"balance"`
		VoucherRedeemed   *big.Int `abi:"voucherRedeemed"`
		LastNonce         uint64   `abi:"lastNonce"`
		WithdrawAmount    *big.Int `abi:"withdrawAmount"`
		WithdrawTimestamp *big.Int `abi:"withdrawTimestamp"`
	}
	if err := parsedABI.UnpackIntoInterface(&out, "getClientState", rawBytes); err != nil {
		return nil, fmt.Errorf("failed to unpack getClientState result: %w", err)
	}

	return &ClientState{
		Balance:           fbig.NewFromGo(out.Balance),
		VoucherRedeemed:   fbig.NewFromGo(out.VoucherRedeemed),
		LastNonce:         out.LastNonce,
		WithdrawAmount:    fbig.NewFromGo(out.WithdrawAmount),
		WithdrawTimestamp: fbig.NewFromGo(out.WithdrawTimestamp),
	}, nil
}

func (s *Service) GetProviderState(ctx context.Context, providerID uint64) (fbig.Int, uint64, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetProviderStateABI))
	if err != nil {
		return fbig.Int{}, 0, fmt.Errorf("failed to parse getProviderState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getProviderState", providerID)
	if err != nil {
		return fbig.Int{}, 0, fmt.Errorf("failed to pack getProviderState call: %w", err)
	}
	res, err := s.full.StateCall(ctx, &types.Message{
		To:     s.router,
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
	if err := parsedABI.UnpackIntoInterface(&out, "getProviderState", rawBytes); err != nil {
		return fbig.Int{}, 0, fmt.Errorf("failed to unpack getProviderState: %w", err)
	}
	return fbig.NewFromGo(out.VoucherRedeemed), out.LastNonce, nil
}

func (s *Service) GetServiceState(ctx context.Context) (uint64, fbig.Int, fbig.Int, fbig.Int, uint64, fbig.Int, error) {
	parsedABI, err := eabi.JSON(strings.NewReader(GetServiceStateABI))
	if err != nil {
		return 0, fbig.Int{}, fbig.Int{}, fbig.Int{}, 0, fbig.Int{}, fmt.Errorf("failed to parse getServiceState ABI: %w", err)
	}
	data, err := parsedABI.Pack("getServiceState")
	if err != nil {
		return 0, fbig.Int{}, fbig.Int{}, fbig.Int{}, 0, fbig.Int{}, fmt.Errorf("failed to pack getServiceState call: %w", err)
	}
	res, err := s.full.StateCall(ctx, &types.Message{
		To:     s.router,
		From:   builtin.SystemActorAddr,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsEVM.InvokeContract,
		Params: mustSerializeCBOR(data),
	}, types.EmptyTSK)
	if err != nil {
		return 0, fbig.Int{}, fbig.Int{}, fbig.Int{}, 0, fbig.Int{}, fmt.Errorf("StateCall failed: %w", err)
	}
	if res.MsgRct.ExitCode != exitcode.Ok {
		return 0, fbig.Int{}, fbig.Int{}, fbig.Int{}, 0, fbig.Int{}, fmt.Errorf("getServiceState returned non-ok exit code: %s", res.MsgRct.ExitCode)
	}

	var rawBytes abi.CborBytes
	if err := rawBytes.UnmarshalCBOR(bytes.NewReader(res.MsgRct.Return)); err != nil {
		return 0, fbig.Int{}, fbig.Int{}, fbig.Int{}, 0, fbig.Int{}, fmt.Errorf("failed to unmarshal getServiceState result: %w (%x)", err, res.MsgRct.Return)
	}

	var out struct {
		ServiceActor               uint64   `abi:"serviceActor"`
		ServicePool                *big.Int `abi:"servicePool"`
		PendingWithdrawalAmount    *big.Int `abi:"pendingWithdrawalAmount"`
		PendingWithdrawalTimestamp *big.Int `abi:"pendingWithdrawalTimestamp"`
		ProposedServiceActor       uint64   `abi:"pendingActor"`
		ActorChangeTimestamp       *big.Int `abi:"actorChangeTimestamp"`
	}

	if err := parsedABI.UnpackIntoInterface(&out, "getServiceState", rawBytes); err != nil {
		return 0, fbig.Int{}, fbig.Int{}, fbig.Int{}, 0, fbig.Int{}, fmt.Errorf("failed to unpack getServiceState: %w (%x)", err, rawBytes)
	}
	return out.ServiceActor, fbig.NewFromGo(out.ServicePool), fbig.NewFromGo(out.PendingWithdrawalAmount), fbig.NewFromGo(out.PendingWithdrawalTimestamp), out.ProposedServiceActor, fbig.NewFromGo(out.ActorChangeTimestamp), nil
}

func (s *Service) ProposeServiceActor(ctx context.Context, from address.Address, newServiceActor address.Address) error {
	parsedABI, err := eabi.JSON(strings.NewReader(ProposeServiceActorABI))
	if err != nil {
		return fmt.Errorf("parse proposeServiceActor ABI: %w", err)
	}

	resolv, err := s.full.StateLookupID(ctx, newServiceActor, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("StateLookupID failed: %w", err)
	}

	id, err := address.IDFromAddress(resolv)
	if err != nil {
		return fmt.Errorf("failed to convert to FilActorID: %w", err)
	}

	data, err := parsedABI.Pack("proposeServiceActor", id)
	if err != nil {
		return fmt.Errorf("pack proposeServiceActor: %w", err)
	}
	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("proposeServiceActor message failed: %w", err)
	}
	return nil
}

func (s *Service) AcceptServiceActor(ctx context.Context, from address.Address) error {
	parsedABI, err := eabi.JSON(strings.NewReader(AcceptServiceActorABI))
	if err != nil {
		return fmt.Errorf("parse acceptServiceActor ABI: %w", err)
	}
	data, err := parsedABI.Pack("acceptServiceActor")
	if err != nil {
		return fmt.Errorf("pack acceptServiceActor: %w", err)
	}
	_, err = s.sendEVMMessage(ctx, from, s.router, fbig.Zero(), data)
	if err != nil {
		return fmt.Errorf("acceptServiceActor message failed: %w", err)
	}
	return nil
}

// --- EVM invocation helper ---

func (s *Service) sendEVMMessage(
	ctx context.Context,
	from, to address.Address,
	value fbig.Int,
	data []byte,
) (cid.Cid, error) {
	param := abi.CborBytes(data)
	ser, aerr := actors.SerializeParams(&param)
	if aerr != nil {
		return cid.Undef, fmt.Errorf("failed to serialize params: %w", aerr)
	}
	msg := &types.Message{
		To:     to,
		From:   from,
		Value:  value, // filecoin big.Int
		Method: builtin.MethodsEVM.InvokeContract,
		Params: ser,
	}
	mcid, err := s.sendMessage(ctx, msg, &api.MessageSendSpec{
		MaxFee: abi.TokenAmount(types.MustParseFIL("0.1")),
	})
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to push message: %w", err)
	}

	return mcid, nil
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
