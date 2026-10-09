package message

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/resources"

	ltypes "github.com/filecoin-project/lotus/chain/types"
)

// Test helpers

func makeMockTipSet(t *testing.T, height uint64) *ltypes.TipSet {
	t.Helper()

	addr, _ := address.NewIDAddress(1)
	c, _ := cid.Decode("bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4")
	ts, err := ltypes.NewTipSet([]*ltypes.BlockHeader{{
		Miner:                 addr,
		Ticket:                &ltypes.Ticket{VRFProof: []byte{byte(height)}},
		Height:                abi.ChainEpoch(height),
		ParentStateRoot:       c,
		Messages:              c,
		ParentMessageReceipts: c,
		BlockSig:              &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		BLSAggregate:          &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		Timestamp:             uint64(time.Now().Unix()),
		ParentBaseFee:         ltypes.NewInt(100),
	}})
	require.NoError(t, err)
	return ts
}

// Mocks

type mockEthClient struct {
	receipts      map[common.Hash]*types.Receipt
	transactions  map[common.Hash]*types.Transaction
	receiptDelay  time.Duration
	receiptCalls  int
	txCalls       int
	receiptErrors map[common.Hash]error
	receiptHashes []common.Hash
	txHashes      []common.Hash
}

func (m *mockEthClient) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	m.receiptCalls++
	m.receiptHashes = append(m.receiptHashes, txHash)
	if err := m.receiptErrors[txHash]; err != nil {
		return nil, err
	}

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
	m.txHashes = append(m.txHashes, txHash)

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
	Status        string
	MachineID     *int64
	LookupHashes  []string
	BlockNumber   *int64
	ConfirmedHash string
	TxSuccess     *bool
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

func (m *mockEthTxManager) GetPendingForMachine(ctx context.Context, machineID int64) ([]PendingEthTx, error) {
	m.getCalls++
	var results []PendingEthTx
	for hash, data := range m.txData {
		if data.MachineID != nil && *data.MachineID == machineID && data.Status == "pending" {
			lookupHashes := data.LookupHashes
			if len(lookupHashes) == 0 {
				lookupHashes = []string{hash}
			}
			results = append(results, PendingEthTx{
				WaitHash:     hash,
				LookupHashes: lookupHashes,
			})
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
		data.ConfirmedHash = confirmedTxHash
		data.TxSuccess = &success
	}
	return nil
}

// Tests

func TestMessageWatcherEthProcessHeadChange(t *testing.T) {
	mw := &MessageWatcherEth{
		updateCh: make(chan struct{}, 1),
	}

	ts := makeMockTipSet(t, 100)
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

func TestMessageWatcherEthConfirmsReplacementWinner(t *testing.T) {
	for _, tc := range []struct {
		name      string
		winner    int
		firstPass string
		failed    bool
	}{
		{name: "original wins", winner: 0},
		{name: "first replacement wins", winner: 1},
		{name: "latest replacement wins", winner: 2},
		{name: "all pending then original lands", winner: 0, firstPass: "missing"},
		{name: "earlier replacement awaits confidence", winner: 1, firstPass: "confidence"},
		{name: "earlier replacement execution fails", winner: 1, failed: true},
		{name: "RPC error stops fallback until retry", winner: 0, firstPass: "error"},
		{name: "timeout stops fallback until retry", winner: 0, firstPass: "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newEthReplaceHarness(t)
			privateKey, err := gethcrypto.GenerateKey()
			require.NoError(t, err)
			from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
			to := common.HexToAddress("0x1234")
			original, originalData := signedDynamicTx(t, privateKey, to, 42, 100, 10)
			first, firstData := signedDynamicTx(t, privateKey, to, 42, 300, 30)
			latest, latestData := signedDynamicTx(t, privateKey, to, 42, 600, 60)
			txs := []*types.Transaction{original, first, latest}
			h.insertEthMessageSend(t, from, to, original, originalData, h.oldSendTime())
			h.insertSuccessfulReplacement(t, from, 42, original.Hash().Hex(), original.Hash().Hex(), first.Hash().Hex(), firstData, h.oldSendTime())
			h.insertSuccessfulReplacement(t, from, 42, original.Hash().Hex(), first.Hash().Hex(), latest.Hash().Hex(), latestData, h.oldSendTime())
			_, err = h.db.Exec(h.ctx, `
				INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
				VALUES ($1, 'pending')`, original.Hash().Hex())
			require.NoError(t, err)
			var machineID int64
			err = h.db.QueryRow(h.ctx, `
				INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu)
				VALUES ('replacement-winner-test', 1, 1, 0) RETURNING id`).Scan(&machineID)
			require.NoError(t, err)

			winner := txs[tc.winner]
			receipt := &types.Receipt{
				TxHash:      winner.Hash(),
				BlockNumber: big.NewInt(100),
				Status:      types.ReceiptStatusSuccessful,
			}
			if tc.failed {
				receipt.Status = types.ReceiptStatusFailed
			}
			client := &mockEthClient{
				receipts:     map[common.Hash]*types.Receipt{winner.Hash(): receipt},
				transactions: map[common.Hash]*types.Transaction{winner.Hash(): winner},
			}
			watcher := &MessageWatcherEth{
				txMgr:          NewHarmonyEthTxManager(h.db),
				ht:             &mockTaskEngine{machineID: machineID},
				api:            client,
				ethCallTimeout: time.Second,
			}
			watcher.bestBlockNumber.Store(big.NewInt(100 + MinEthConfidence))
			var expectedLookups []common.Hash
			for i := len(txs) - 1; i >= tc.winner; i-- {
				expectedLookups = append(expectedLookups, txs[i].Hash())
			}
			firstLookups := expectedLookups
			switch tc.firstPass {
			case "missing":
				delete(client.receipts, winner.Hash())
			case "confidence":
				watcher.bestBlockNumber.Store(big.NewInt(100))
			case "error":
				client.receiptErrors = map[common.Hash]error{first.Hash(): errors.New("RPC unavailable")}
				firstLookups = []common.Hash{latest.Hash(), first.Hash()}
			case "timeout":
				client.receiptDelay = 100 * time.Millisecond
				watcher.ethCallTimeout = time.Millisecond
				firstLookups = []common.Hash{latest.Hash()}
			}

			watcher.update()
			require.Equal(t, firstLookups, client.receiptHashes)
			if tc.firstPass != "" {
				var status string
				var confirmedHash sql.NullString
				var success sql.NullBool
				var hasReceipt bool
				err = h.db.QueryRow(h.ctx, `
					SELECT tx_status, confirmed_tx_hash, tx_success, tx_receipt IS NOT NULL
					FROM message_waits_eth WHERE signed_tx_hash = $1`, original.Hash().Hex()).
					Scan(&status, &confirmedHash, &success, &hasReceipt)
				require.NoError(t, err)
				require.Equal(t, "pending", status)
				require.False(t, confirmedHash.Valid)
				require.False(t, success.Valid)
				require.False(t, hasReceipt)
				require.Empty(t, client.txHashes)

				client.receipts[winner.Hash()] = receipt
				client.receiptErrors = nil
				client.receiptDelay = 0
				client.receiptHashes = nil
				watcher.ethCallTimeout = time.Second
				watcher.bestBlockNumber.Store(big.NewInt(100 + MinEthConfidence))
				watcher.update()
			}
			require.Equal(t, expectedLookups, client.receiptHashes)
			require.Equal(t, []common.Hash{winner.Hash()}, client.txHashes)

			var waits []struct {
				WaitHash      string        `db:"signed_tx_hash"`
				Status        string        `db:"tx_status"`
				ConfirmedHash string        `db:"confirmed_tx_hash"`
				BlockNumber   int64         `db:"confirmed_block_number"`
				Success       bool          `db:"tx_success"`
				ReceiptHash   string        `db:"receipt_hash"`
				TxDataHash    string        `db:"tx_data_hash"`
				MachineID     sql.NullInt64 `db:"waiter_machine_id"`
			}
			err = h.db.Select(h.ctx, &waits, `
				SELECT signed_tx_hash, tx_status, confirmed_tx_hash, confirmed_block_number, tx_success,
					tx_receipt->>'transactionHash' AS receipt_hash, confirmed_tx_data->>'hash' AS tx_data_hash,
					waiter_machine_id
				FROM message_waits_eth`)
			require.NoError(t, err)
			require.Len(t, waits, 1)
			require.Equal(t, original.Hash().Hex(), waits[0].WaitHash)
			require.Equal(t, "confirmed", waits[0].Status)
			require.Equal(t, winner.Hash().Hex(), waits[0].ConfirmedHash)
			require.Equal(t, int64(100), waits[0].BlockNumber)
			require.Equal(t, !tc.failed, waits[0].Success)
			require.Equal(t, winner.Hash().Hex(), waits[0].ReceiptHash)
			require.Equal(t, winner.Hash().Hex(), waits[0].TxDataHash)
			require.False(t, waits[0].MachineID.Valid)
		})
	}
}
