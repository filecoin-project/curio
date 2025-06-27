package message

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/resources"

	ltypes "github.com/filecoin-project/lotus/chain/types"
)

// Test helpers

func makeMockTipSet(height uint64) *ltypes.TipSet {
	addr, _ := address.NewIDAddress(1)
	c, _ := cid.Decode("bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4")
	ts, _ := ltypes.NewTipSet([]*ltypes.BlockHeader{{
		Miner:                 addr,
		Height:                abi.ChainEpoch(height),
		ParentStateRoot:       c,
		Messages:              c,
		ParentMessageReceipts: c,
		BlockSig:              &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		BLSAggregate:          &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		Timestamp:             uint64(time.Now().Unix()),
		ParentBaseFee:         ltypes.NewInt(100),
	}})
	return ts
}

// Mocks

type mockEthClient struct {
	receipts     map[common.Hash]*types.Receipt
	transactions map[common.Hash]*types.Transaction
	receiptDelay time.Duration
	receiptCalls int
	txCalls      int
}

func (m *mockEthClient) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	m.receiptCalls++

	if m.receiptDelay > 0 {
		select {
		case <-time.After(m.receiptDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	receipt, ok := m.receipts[txHash]
	if !ok {
		return nil, ethereum.NotFound
	}

	return receipt, nil
}

func (m *mockEthClient) TransactionByHash(ctx context.Context, txHash common.Hash) (*types.Transaction, bool, error) {
	m.txCalls++

	tx, ok := m.transactions[txHash]
	if !ok {
		return nil, false, ethereum.NotFound
	}

	return tx, true, nil
}

func (m *mockEthClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return &types.Header{Number: big.NewInt(100)}, nil
}

type mockTaskEngine struct {
	machineID int64
}

func (m *mockTaskEngine) ResourcesAvailable() resources.Resources {
	return resources.Resources{MachineID: int(m.machineID)}
}

type mockEthTxManager struct {
	txData      map[string]*txRecord
	assignCalls int
	getCalls    int
	updateCalls int
}

type txRecord struct {
	Status      string
	MachineID   *int64
	BlockNumber *int64
	TxSuccess   *bool
}

func newMockEthTxManager() *mockEthTxManager {
	return &mockEthTxManager{
		txData: make(map[string]*txRecord),
	}
}

func (m *mockEthTxManager) AssignPendingToMachine(ctx context.Context, machineID int64) (int, error) {
	m.assignCalls++
	count := 0
	for hash, data := range m.txData {
		if data.MachineID == nil && data.Status == "pending" {
			m.txData[hash].MachineID = &machineID
			count++
		}
	}
	return count, nil
}

func (m *mockEthTxManager) GetPendingForMachine(ctx context.Context, machineID int64) ([]string, error) {
	m.getCalls++
	var results []string
	for hash, data := range m.txData {
		if data.MachineID != nil && *data.MachineID == machineID && data.Status == "pending" {
			results = append(results, hash)
		}
	}
	return results, nil
}

func (m *mockEthTxManager) UpdateToConfirmed(ctx context.Context, signedTxHash string, blockNumber int64, confirmedTxHash string, txData []byte, receipt []byte, success bool) error {
	m.updateCalls++
	if data, ok := m.txData[signedTxHash]; ok {
		data.Status = "confirmed"
		data.MachineID = nil
		data.BlockNumber = &blockNumber
		data.TxSuccess = &success
	}
	return nil
}

// Tests

func TestMessageWatcherEthProcessHeadChange(t *testing.T) {
	mw := &MessageWatcherEth{
		updateCh: make(chan struct{}, 1),
	}

	ts := makeMockTipSet(100)
	err := mw.processHeadChange(context.TODO(), nil, ts)
	require.NoError(t, err)

	// Verify best block number was updated
	bestBlock := mw.bestBlockNumber.Load()
	require.NotNil(t, bestBlock)
	require.Equal(t, int64(100), bestBlock.Int64())

	// Verify update channel received signal
	select {
	case <-mw.updateCh:
		// Good
	default:
		t.Fatal("Expected update signal")
	}
}

func TestMessageWatcherEthWithMocks(t *testing.T) {
	// Set up mocks
	machineID := int64(1)
	mockTxMgr := newMockEthTxManager()
	mockTaskEngine := &mockTaskEngine{machineID: machineID}
	mockClient := &mockEthClient{
		receipts:     make(map[common.Hash]*types.Receipt),
		transactions: make(map[common.Hash]*types.Transaction),
	}

	// Add test transactions
	txHash1 := common.HexToHash("0x1111111111111111")
	txHash2 := common.HexToHash("0x2222222222222222")
	txHash3 := common.HexToHash("0x3333333333333333")

	mockTxMgr.txData[txHash1.Hex()] = &txRecord{Status: "pending"}
	mockTxMgr.txData[txHash2.Hex()] = &txRecord{Status: "pending"}
	mockTxMgr.txData[txHash3.Hex()] = &txRecord{Status: "pending"}

	// Transaction 1: No receipt (stays pending)
	// Transaction 2: Receipt but not enough confirmations (stays pending)
	mockClient.receipts[txHash2] = &types.Receipt{
		Status:      types.ReceiptStatusSuccessful,
		BlockNumber: big.NewInt(100), // 0 confirmations
		TxHash:      txHash2,
	}
	mockClient.transactions[txHash2] = types.NewTransaction(0, common.Address{}, big.NewInt(100), 21000, big.NewInt(1), nil)

	// Transaction 3: Receipt with enough confirmations (gets confirmed)
	mockClient.receipts[txHash3] = &types.Receipt{
		Status:      types.ReceiptStatusSuccessful,
		BlockNumber: big.NewInt(85), // 15 confirmations
		TxHash:      txHash3,
	}
	mockClient.transactions[txHash3] = types.NewTransaction(0, common.Address{}, big.NewInt(200), 21000, big.NewInt(1), nil)

	// Create MessageWatcherEth
	mw := &MessageWatcherEth{
		txMgr:          mockTxMgr,
		ht:             mockTaskEngine,
		api:            mockClient,
		updateCh:       make(chan struct{}, 1),
		ethCallTimeout: time.Second, // Use default timeout
	}

	mw.bestBlockNumber.Store(big.NewInt(100))

	// Run update
	mw.update()

	// Verify results
	require.Equal(t, "pending", mockTxMgr.txData[txHash1.Hex()].Status)
	require.Equal(t, "pending", mockTxMgr.txData[txHash2.Hex()].Status)
	require.Equal(t, "confirmed", mockTxMgr.txData[txHash3.Hex()].Status)

	// Verify calls
	require.Equal(t, 3, mockClient.receiptCalls)
	require.Equal(t, 1, mockClient.txCalls)
	require.Equal(t, 1, mockTxMgr.updateCalls)
}

func TestMessageWatcherEthTimeout(t *testing.T) {
	mockTxMgr := newMockEthTxManager()
	mockTaskEngine := &mockTaskEngine{machineID: 1}

	txHash := common.HexToHash("0x1234")
	mockTxMgr.txData[txHash.Hex()] = &txRecord{Status: "pending"}

	// Mock client with a delay longer than our short timeout
	mockClient := &mockEthClient{
		receipts:     make(map[common.Hash]*types.Receipt),
		receiptDelay: 100 * time.Millisecond,
	}

	mw := &MessageWatcherEth{
		txMgr:          mockTxMgr,
		ht:             mockTaskEngine,
		api:            mockClient,
		updateCh:       make(chan struct{}, 1),
		ethCallTimeout: 10 * time.Millisecond, // Very short timeout for test
	}

	mw.bestBlockNumber.Store(big.NewInt(100))
	mw.update()

	// Transaction should still be pending after timeout
	require.Equal(t, "pending", mockTxMgr.txData[txHash.Hex()].Status)
	// Verify the API was actually called (not bypassed)
	require.Equal(t, 1, mockClient.receiptCalls)
}
