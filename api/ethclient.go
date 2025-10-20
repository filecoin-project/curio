package api

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	erpc "github.com/ethereum/go-ethereum/rpc"
)

type EthClientInterface interface {
	ChainID(ctx context.Context) (*big.Int, error)
	BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	BlockNumber(ctx context.Context) (uint64, error)
	PeerCount(ctx context.Context) (uint64, error)
	BlockReceipts(ctx context.Context, blockNrOrHash erpc.BlockNumberOrHash) ([]*types.Receipt, error)
	HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	TransactionByHash(ctx context.Context, hash common.Hash) (tx *types.Transaction, isPending bool, err error)
	TransactionSender(ctx context.Context, tx *types.Transaction, block common.Hash, index uint) (common.Address, error)
	TransactionCount(ctx context.Context, blockHash common.Hash) (uint, error)
	TransactionInBlock(ctx context.Context, blockHash common.Hash, index uint) (*types.Transaction, error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error)
	NetworkID(ctx context.Context) (*big.Int, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	BalanceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (*big.Int, error)
	StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
	StorageAtHash(ctx context.Context, account common.Address, key common.Hash, blockHash common.Hash) ([]byte, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	CodeAtHash(ctx context.Context, account common.Address, blockHash common.Hash) ([]byte, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	NonceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (uint64, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)
	PendingBalanceAt(ctx context.Context, account common.Address) (*big.Int, error)
	PendingStorageAt(ctx context.Context, account common.Address, key common.Hash) ([]byte, error)
	PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	PendingTransactionCount(ctx context.Context) (uint, error)
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	CallContractAtHash(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) ([]byte, error)
	PendingCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error)
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

// EthClientStruct is a proxy struct that implements ethclient.Client interface
// with dynamic provider switching and retry logic
type EthClientStruct struct {
	Internal EthClientMethods
}

// EthClientMethods contains all the settable method fields for ethclient.Client
type EthClientMethods struct {
	// Connection methods
	Close  func()
	Client func() *erpc.Client

	// Blockchain Access
	ChainID            func(ctx context.Context) (*big.Int, error)
	BlockByHash        func(ctx context.Context, hash common.Hash) (*types.Block, error)
	BlockByNumber      func(ctx context.Context, number *big.Int) (*types.Block, error)
	BlockNumber        func(ctx context.Context) (uint64, error)
	PeerCount          func(ctx context.Context) (uint64, error)
	BlockReceipts      func(ctx context.Context, blockNrOrHash erpc.BlockNumberOrHash) ([]*types.Receipt, error)
	HeaderByHash       func(ctx context.Context, hash common.Hash) (*types.Header, error)
	HeaderByNumber     func(ctx context.Context, number *big.Int) (*types.Header, error)
	TransactionByHash  func(ctx context.Context, hash common.Hash) (tx *types.Transaction, isPending bool, err error)
	TransactionSender  func(ctx context.Context, tx *types.Transaction, block common.Hash, index uint) (common.Address, error)
	TransactionCount   func(ctx context.Context, blockHash common.Hash) (uint, error)
	TransactionInBlock func(ctx context.Context, blockHash common.Hash, index uint) (*types.Transaction, error)
	TransactionReceipt func(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	SyncProgress       func(ctx context.Context) (*ethereum.SyncProgress, error)
	SubscribeNewHead   func(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error)

	// State Access
	NetworkID     func(ctx context.Context) (*big.Int, error)
	BalanceAt     func(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	BalanceAtHash func(ctx context.Context, account common.Address, blockHash common.Hash) (*big.Int, error)
	StorageAt     func(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
	StorageAtHash func(ctx context.Context, account common.Address, key common.Hash, blockHash common.Hash) ([]byte, error)
	CodeAt        func(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	CodeAtHash    func(ctx context.Context, account common.Address, blockHash common.Hash) ([]byte, error)
	NonceAt       func(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	NonceAtHash   func(ctx context.Context, account common.Address, blockHash common.Hash) (uint64, error)

	// Filters
	FilterLogs          func(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs func(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)

	// Pending State
	PendingBalanceAt        func(ctx context.Context, account common.Address) (*big.Int, error)
	PendingStorageAt        func(ctx context.Context, account common.Address, key common.Hash) ([]byte, error)
	PendingCodeAt           func(ctx context.Context, account common.Address) ([]byte, error)
	PendingNonceAt          func(ctx context.Context, account common.Address) (uint64, error)
	PendingTransactionCount func(ctx context.Context) (uint, error)

	// Contract Calling
	CallContract        func(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	CallContractAtHash  func(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) ([]byte, error)
	PendingCallContract func(ctx context.Context, msg ethereum.CallMsg) ([]byte, error)
	SuggestGasPrice     func(ctx context.Context) (*big.Int, error)
	SuggestGasTipCap    func(ctx context.Context) (*big.Int, error)
	FeeHistory          func(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error)
	EstimateGas         func(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SendTransaction     func(ctx context.Context, tx *types.Transaction) error
}

// Connection methods

func (s *EthClientStruct) Close() {
	if s.Internal.Close == nil {
		return
	}
	s.Internal.Close()
}

func (s *EthClientStruct) Client() *erpc.Client {
	if s.Internal.Client == nil {
		return nil
	}
	return s.Internal.Client()
}

// Blockchain Access methods

func (s *EthClientStruct) ChainID(ctx context.Context) (*big.Int, error) {
	if s.Internal.ChainID == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.ChainID(ctx)
}

func (s *EthClientStruct) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if s.Internal.BlockByHash == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.BlockByHash(ctx, hash)
}

func (s *EthClientStruct) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	if s.Internal.BlockByNumber == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.BlockByNumber(ctx, number)
}

func (s *EthClientStruct) BlockNumber(ctx context.Context) (uint64, error) {
	if s.Internal.BlockNumber == nil {
		return 0, ErrNotSupported
	}
	return s.Internal.BlockNumber(ctx)
}

func (s *EthClientStruct) PeerCount(ctx context.Context) (uint64, error) {
	if s.Internal.PeerCount == nil {
		return 0, ErrNotSupported
	}
	return s.Internal.PeerCount(ctx)
}

func (s *EthClientStruct) BlockReceipts(ctx context.Context, blockNrOrHash erpc.BlockNumberOrHash) ([]*types.Receipt, error) {
	if s.Internal.BlockReceipts == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.BlockReceipts(ctx, blockNrOrHash)
}

func (s *EthClientStruct) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	if s.Internal.HeaderByHash == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.HeaderByHash(ctx, hash)
}

func (s *EthClientStruct) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if s.Internal.HeaderByNumber == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.HeaderByNumber(ctx, number)
}

func (s *EthClientStruct) TransactionByHash(ctx context.Context, hash common.Hash) (tx *types.Transaction, isPending bool, err error) {
	if s.Internal.TransactionByHash == nil {
		return nil, false, ErrNotSupported
	}
	return s.Internal.TransactionByHash(ctx, hash)
}

func (s *EthClientStruct) TransactionSender(ctx context.Context, tx *types.Transaction, block common.Hash, index uint) (common.Address, error) {
	if s.Internal.TransactionSender == nil {
		return common.Address{}, ErrNotSupported
	}
	return s.Internal.TransactionSender(ctx, tx, block, index)
}

func (s *EthClientStruct) TransactionCount(ctx context.Context, blockHash common.Hash) (uint, error) {
	if s.Internal.TransactionCount == nil {
		return 0, ErrNotSupported
	}
	return s.Internal.TransactionCount(ctx, blockHash)
}

func (s *EthClientStruct) TransactionInBlock(ctx context.Context, blockHash common.Hash, index uint) (*types.Transaction, error) {
	if s.Internal.TransactionInBlock == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.TransactionInBlock(ctx, blockHash, index)
}

func (s *EthClientStruct) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if s.Internal.TransactionReceipt == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.TransactionReceipt(ctx, txHash)
}

func (s *EthClientStruct) SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	if s.Internal.SyncProgress == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.SyncProgress(ctx)
}

func (s *EthClientStruct) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	if s.Internal.SubscribeNewHead == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.SubscribeNewHead(ctx, ch)
}

// State Access methods

func (s *EthClientStruct) NetworkID(ctx context.Context) (*big.Int, error) {
	if s.Internal.NetworkID == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.NetworkID(ctx)
}

func (s *EthClientStruct) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	if s.Internal.BalanceAt == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.BalanceAt(ctx, account, blockNumber)
}

func (s *EthClientStruct) BalanceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (*big.Int, error) {
	if s.Internal.BalanceAtHash == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.BalanceAtHash(ctx, account, blockHash)
}

func (s *EthClientStruct) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	if s.Internal.StorageAt == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.StorageAt(ctx, account, key, blockNumber)
}

func (s *EthClientStruct) StorageAtHash(ctx context.Context, account common.Address, key common.Hash, blockHash common.Hash) ([]byte, error) {
	if s.Internal.StorageAtHash == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.StorageAtHash(ctx, account, key, blockHash)
}

func (s *EthClientStruct) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	if s.Internal.CodeAt == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.CodeAt(ctx, account, blockNumber)
}

func (s *EthClientStruct) CodeAtHash(ctx context.Context, account common.Address, blockHash common.Hash) ([]byte, error) {
	if s.Internal.CodeAtHash == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.CodeAtHash(ctx, account, blockHash)
}

func (s *EthClientStruct) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	if s.Internal.NonceAt == nil {
		return 0, ErrNotSupported
	}
	return s.Internal.NonceAt(ctx, account, blockNumber)
}

func (s *EthClientStruct) NonceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (uint64, error) {
	if s.Internal.NonceAtHash == nil {
		return 0, ErrNotSupported
	}
	return s.Internal.NonceAtHash(ctx, account, blockHash)
}

// Filter methods

func (s *EthClientStruct) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if s.Internal.FilterLogs == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.FilterLogs(ctx, q)
}

func (s *EthClientStruct) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	if s.Internal.SubscribeFilterLogs == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.SubscribeFilterLogs(ctx, q, ch)
}

// Pending State methods

func (s *EthClientStruct) PendingBalanceAt(ctx context.Context, account common.Address) (*big.Int, error) {
	if s.Internal.PendingBalanceAt == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.PendingBalanceAt(ctx, account)
}

func (s *EthClientStruct) PendingStorageAt(ctx context.Context, account common.Address, key common.Hash) ([]byte, error) {
	if s.Internal.PendingStorageAt == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.PendingStorageAt(ctx, account, key)
}

func (s *EthClientStruct) PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	if s.Internal.PendingCodeAt == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.PendingCodeAt(ctx, account)
}

func (s *EthClientStruct) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	if s.Internal.PendingNonceAt == nil {
		return 0, ErrNotSupported
	}
	return s.Internal.PendingNonceAt(ctx, account)
}

func (s *EthClientStruct) PendingTransactionCount(ctx context.Context) (uint, error) {
	if s.Internal.PendingTransactionCount == nil {
		return 0, ErrNotSupported
	}
	return s.Internal.PendingTransactionCount(ctx)
}

// Contract Calling methods

func (s *EthClientStruct) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if s.Internal.CallContract == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.CallContract(ctx, msg, blockNumber)
}

func (s *EthClientStruct) CallContractAtHash(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) ([]byte, error) {
	if s.Internal.CallContractAtHash == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.CallContractAtHash(ctx, msg, blockHash)
}

func (s *EthClientStruct) PendingCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error) {
	if s.Internal.PendingCallContract == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.PendingCallContract(ctx, msg)
}

func (s *EthClientStruct) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	if s.Internal.SuggestGasPrice == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.SuggestGasPrice(ctx)
}

func (s *EthClientStruct) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	if s.Internal.SuggestGasTipCap == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.SuggestGasTipCap(ctx)
}

func (s *EthClientStruct) FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error) {
	if s.Internal.FeeHistory == nil {
		return nil, ErrNotSupported
	}
	return s.Internal.FeeHistory(ctx, blockCount, lastBlock, rewardPercentiles)
}

func (s *EthClientStruct) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	if s.Internal.EstimateGas == nil {
		return 0, ErrNotSupported
	}
	return s.Internal.EstimateGas(ctx, msg)
}

func (s *EthClientStruct) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	if s.Internal.SendTransaction == nil {
		return ErrNotSupported
	}
	return s.Internal.SendTransaction(ctx, tx)
}
