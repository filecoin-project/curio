package ethchain

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/filecoin-project/curio/api"
)

type EthClient interface {
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	BalanceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (*big.Int, error)
	BlobBaseFee(ctx context.Context) (*big.Int, error)
	BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	BlockNumber(ctx context.Context) (uint64, error)
	//BlockReceipts(ctx context.Context, blockNrOrHash erpc.BlockNumberOrHash) ([]*types.Receipt, error)
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	CallContractAtHash(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) ([]byte, error)
	ChainID(ctx context.Context) (*big.Int, error)
	//Client() *erpc.Client
	Close()
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	CodeAtHash(ctx context.Context, account common.Address, blockHash common.Hash) ([]byte, error)
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	EstimateGasAtBlock(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) (uint64, error)
	EstimateGasAtBlockHash(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) (uint64, error)
	FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	NetworkID(ctx context.Context) (*big.Int, error)
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	NonceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (uint64, error)
	PeerCount(ctx context.Context) (uint64, error)
	PendingBalanceAt(ctx context.Context, account common.Address) (*big.Int, error)
	PendingCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error)
	PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	PendingStorageAt(ctx context.Context, account common.Address, key common.Hash) ([]byte, error)
	PendingTransactionCount(ctx context.Context) (uint, error)
	SendRawTransactionSync(ctx context.Context, rawTx []byte, timeout *time.Duration) (*types.Receipt, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	SendTransactionSync(ctx context.Context, tx *types.Transaction, timeout *time.Duration) (*types.Receipt, error)
	//SimulateV1(ctx context.Context, opts ethclient.SimulateOptions, blockNrOrHash *erpc.BlockNumberOrHash) ([]ethclient.SimulateBlockResult, error)
	StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
	StorageAtHash(ctx context.Context, account common.Address, key common.Hash, blockHash common.Hash) ([]byte, error)
	SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)
	SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error)
	SubscribeTransactionReceipts(ctx context.Context, q *ethereum.TransactionReceiptsQuery, ch chan<- []*types.Receipt) (ethereum.Subscription, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	TransactionByHash(ctx context.Context, hash common.Hash) (tx *types.Transaction, isPending bool, err error)
	TransactionCount(ctx context.Context, blockHash common.Hash) (uint, error)
	TransactionInBlock(ctx context.Context, blockHash common.Hash, index uint) (*types.Transaction, error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	TransactionSender(ctx context.Context, tx *types.Transaction, block common.Hash, index uint) (common.Address, error)
}

type ChainErrorWrap struct {
	EthClient
}

func maybeChainError(err error) error {
	if err == nil {
		return nil
	}
	return &api.ChainError{Err: err}
}
func (c *ChainErrorWrap) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	res, err := c.EthClient.BalanceAt(ctx, account, blockNumber)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) BalanceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (*big.Int, error) {
	res, err := c.EthClient.BalanceAtHash(ctx, account, blockHash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	res, err := c.EthClient.BlobBaseFee(ctx)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	res, err := c.EthClient.BlockByHash(ctx, hash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	res, err := c.EthClient.BlockByNumber(ctx, number)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) BlockNumber(ctx context.Context) (uint64, error) {
	res, err := c.EthClient.BlockNumber(ctx)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	res, err := c.EthClient.CallContract(ctx, msg, blockNumber)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) CallContractAtHash(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) ([]byte, error) {
	res, err := c.EthClient.CallContractAtHash(ctx, msg, blockHash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) ChainID(ctx context.Context) (*big.Int, error) {
	res, err := c.EthClient.ChainID(ctx)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) Close() {
	c.EthClient.Close()
}

func (c *ChainErrorWrap) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	res, err := c.EthClient.CodeAt(ctx, account, blockNumber)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) CodeAtHash(ctx context.Context, account common.Address, blockHash common.Hash) ([]byte, error) {
	res, err := c.EthClient.CodeAtHash(ctx, account, blockHash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	res, err := c.EthClient.EstimateGas(ctx, msg)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) EstimateGasAtBlock(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) (uint64, error) {
	res, err := c.EthClient.EstimateGasAtBlock(ctx, msg, blockNumber)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) EstimateGasAtBlockHash(ctx context.Context, msg ethereum.CallMsg, blockHash common.Hash) (uint64, error) {
	res, err := c.EthClient.EstimateGasAtBlockHash(ctx, msg, blockHash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) FeeHistory(ctx context.Context, blockCount uint64, lastBlock *big.Int, rewardPercentiles []float64) (*ethereum.FeeHistory, error) {
	res, err := c.EthClient.FeeHistory(ctx, blockCount, lastBlock, rewardPercentiles)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	res, err := c.EthClient.FilterLogs(ctx, q)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	res, err := c.EthClient.HeaderByHash(ctx, hash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	res, err := c.EthClient.HeaderByNumber(ctx, number)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) NetworkID(ctx context.Context) (*big.Int, error) {
	res, err := c.EthClient.NetworkID(ctx)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	res, err := c.EthClient.NonceAt(ctx, account, blockNumber)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) NonceAtHash(ctx context.Context, account common.Address, blockHash common.Hash) (uint64, error) {
	res, err := c.EthClient.NonceAtHash(ctx, account, blockHash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) PeerCount(ctx context.Context) (uint64, error) {
	res, err := c.EthClient.PeerCount(ctx)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) PendingBalanceAt(ctx context.Context, account common.Address) (*big.Int, error) {
	res, err := c.EthClient.PendingBalanceAt(ctx, account)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) PendingCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error) {
	res, err := c.EthClient.PendingCallContract(ctx, msg)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	res, err := c.EthClient.PendingCodeAt(ctx, account)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	res, err := c.EthClient.PendingNonceAt(ctx, account)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) PendingStorageAt(ctx context.Context, account common.Address, key common.Hash) ([]byte, error) {
	res, err := c.EthClient.PendingStorageAt(ctx, account, key)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) PendingTransactionCount(ctx context.Context) (uint, error) {
	res, err := c.EthClient.PendingTransactionCount(ctx)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) SendRawTransactionSync(ctx context.Context, rawTx []byte, timeout *time.Duration) (*types.Receipt, error) {
	res, err := c.EthClient.SendRawTransactionSync(ctx, rawTx, timeout)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	err := c.EthClient.SendTransaction(ctx, tx)
	return maybeChainError(err)
}

func (c *ChainErrorWrap) SendTransactionSync(ctx context.Context, tx *types.Transaction, timeout *time.Duration) (*types.Receipt, error) {
	res, err := c.EthClient.SendTransactionSync(ctx, tx, timeout)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	res, err := c.EthClient.StorageAt(ctx, account, key, blockNumber)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) StorageAtHash(ctx context.Context, account common.Address, key common.Hash, blockHash common.Hash) ([]byte, error) {
	res, err := c.EthClient.StorageAtHash(ctx, account, key, blockHash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	res, err := c.EthClient.SubscribeFilterLogs(ctx, q, ch)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	res, err := c.EthClient.SubscribeNewHead(ctx, ch)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) SubscribeTransactionReceipts(ctx context.Context, q *ethereum.TransactionReceiptsQuery, ch chan<- []*types.Receipt) (ethereum.Subscription, error) {
	res, err := c.EthClient.SubscribeTransactionReceipts(ctx, q, ch)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	res, err := c.EthClient.SyncProgress(ctx)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) TransactionByHash(ctx context.Context, hash common.Hash) (tx *types.Transaction, isPending bool, err error) {
	res, isPending, err := c.EthClient.TransactionByHash(ctx, hash)
	return res, isPending, maybeChainError(err)
}

func (c *ChainErrorWrap) TransactionCount(ctx context.Context, blockHash common.Hash) (uint, error) {
	res, err := c.EthClient.TransactionCount(ctx, blockHash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) TransactionInBlock(ctx context.Context, blockHash common.Hash, index uint) (*types.Transaction, error) {
	res, err := c.EthClient.TransactionInBlock(ctx, blockHash, index)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	res, err := c.EthClient.TransactionReceipt(ctx, txHash)
	return res, maybeChainError(err)
}

func (c *ChainErrorWrap) TransactionSender(ctx context.Context, tx *types.Transaction, block common.Hash, index uint) (common.Address, error) {
	res, err := c.EthClient.TransactionSender(ctx, tx, block, index)
	return res, maybeChainError(err)
}

var _ EthClient = &ChainErrorWrap{}
