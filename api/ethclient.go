package api

import (
	"context"
	mathbig "math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	erpc "github.com/ethereum/go-ethereum/rpc"
)

type EthClientInterface interface {
	ChainID(ctx context.Context) (*mathbig.Int, error)
	BlockByHash(ctx context.Context, hash common.Hash) (*ethtypes.Block, error)
	BlockByNumber(ctx context.Context, number *mathbig.Int) (*ethtypes.Block, error)
	BlockNumber(ctx context.Context) (uint64, error)
	PeerCount(ctx context.Context) (uint64, error)
	BlockReceipts(ctx context.Context, blockNrOrHash erpc.BlockNumberOrHash) ([]*ethtypes.Receipt, error)
	HeaderByHash(ctx context.Context, hash common.Hash) (*ethtypes.Header, error)
	HeaderByNumber(ctx context.Context, number *mathbig.Int) (*ethtypes.Header, error)
	TransactionByHash(ctx context.Context, hash common.Hash) (tx *ethtypes.Transaction, isPending bool, err error)
	TransactionSender(ctx context.Context, tx *ethtypes.Transaction, block common.Hash, index uint) (common.Address, error)
	TransactionCount(ctx context.Context, blockHash common.Hash) (uint, error)
	TransactionInBlock(ctx context.Context, blockHash common.Hash, index uint) (*ethtypes.Transaction, error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethtypes.Receipt, error)
	SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	SubscribeNewHead(ctx context.Context, ch chan<- *ethtypes.Header) (ethereum.Subscription, error)
	NetworkID(ctx context.Context) (*mathbig.Int, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *mathbig.Int) (*mathbig.Int, error)
	BalanceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (*mathbig.Int, error)
	StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *mathbig.Int) ([]byte, error)
	StorageAtHash(ctx context.Context, account common.Address, key common.Hash, blockHash common.Hash) ([]byte, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *mathbig.Int) ([]byte, error)
	CodeAtHash(ctx context.Context, account common.Address, blockHash common.Hash) ([]byte, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *mathbig.Int) (uint64, error)
	NonceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (uint64, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]ethtypes.Log, error)
	SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- ethtypes.Log) (ethereum.Subscription, error)
	PendingBalanceAt(ctx context.Context, account common.Address) (*mathbig.Int, error)
	PendingStorageAt(ctx context.Context, account common.Address, key common.Hash) ([]byte, error)
	PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	PendingTransactionCount(ctx context.Context) (uint, error)
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *mathbig.Int) ([]byte, error)
	CallContractAtHash(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) ([]byte, error)
	PendingCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error)
	SuggestGasPrice(ctx context.Context) (*mathbig.Int, error)
	SuggestGasTipCap(ctx context.Context) (*mathbig.Int, error)
	FeeHistory(ctx context.Context, blockCount uint64, lastBlock *mathbig.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error)
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SendTransaction(ctx context.Context, tx *ethtypes.Transaction) error
}
