package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/actors/policy"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type mockEthReorg struct {
	byNumber map[uint64]*ethtypes.Block
	byHash   map[common.Hash]*ethtypes.Block
	head     *ethtypes.Block
	receipt  *ethtypes.Receipt
	blkErr   error
	rcptErr  error
}

func (m *mockEthReorg) BlockByNumber(_ context.Context, number *big.Int) (*ethtypes.Block, error) {
	if m.blkErr != nil {
		return nil, m.blkErr
	}
	if number == nil {
		return m.head, nil
	}
	if m.byNumber != nil {
		if blk, ok := m.byNumber[number.Uint64()]; ok {
			return blk, nil
		}
	}
	return nil, ethereum.NotFound
}

func (m *mockEthReorg) BlockByHash(_ context.Context, hash common.Hash) (*ethtypes.Block, error) {
	if m.blkErr != nil {
		return nil, m.blkErr
	}
	if m.byHash != nil {
		if blk, ok := m.byHash[hash]; ok {
			return blk, nil
		}
	}
	return nil, ethereum.NotFound
}

func (m *mockEthReorg) TransactionReceipt(_ context.Context, _ common.Hash) (*ethtypes.Receipt, error) {
	if m.rcptErr != nil {
		return nil, m.rcptErr
	}
	return m.receipt, nil
}

func testBlockChain(t *testing.T, heights []uint64, txByHeight map[uint64]*ethtypes.Transaction) (*ethtypes.Block, map[common.Hash]*ethtypes.Block) {
	t.Helper()
	byHash := make(map[common.Hash]*ethtypes.Block, len(heights))
	var head *ethtypes.Block
	var parentHash common.Hash
	for _, h := range heights {
		hdr := &ethtypes.Header{
			Number:     big.NewInt(int64(h)),
			GasLimit:   30_000_000,
			ParentHash: parentHash,
		}
		var txs ethtypes.Transactions
		if tx := txByHeight[h]; tx != nil {
			txs = ethtypes.Transactions{tx}
		}
		blk := ethtypes.NewBlock(hdr, &ethtypes.Body{Transactions: txs}, nil, trie.NewStackTrie(nil))
		byHash[blk.Hash()] = blk
		head = blk
		parentHash = blk.Hash()
	}
	return head, byHash
}

func TestTxsNotIncludedInCanonicalChain_includedInStoredBlock(t *testing.T) {
	ctx := context.Background()
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 1, Gas: 21_000})
	head, byHash := testBlockChain(t, []uint64{99, 100}, map[uint64]*ethtypes.Transaction{100: tx})

	rt := &ReorgCheckTask{eth: &mockEthReorg{head: head, byHash: byHash}}
	notIncluded, err := rt.txsNotIncludedInCanonicalChain(ctx, []reorgInclusionCheck{{
		TxHash:        tx.Hash(),
		ConfirmHeight: 100,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if notIncluded[tx.Hash()] {
		t.Fatal("expected tx to be included in canonical chain")
	}
}

func TestTxsNotIncludedInCanonicalChain_relocatedStillIncluded(t *testing.T) {
	ctx := context.Background()
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 2, Gas: 21_000})
	head, byHash := testBlockChain(t, []uint64{99, 100, 101}, map[uint64]*ethtypes.Transaction{
		99:  ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 99, Gas: 21_000}),
		101: tx,
	})

	rt := &ReorgCheckTask{eth: &mockEthReorg{head: head, byHash: byHash}}
	notIncluded, err := rt.txsNotIncludedInCanonicalChain(ctx, []reorgInclusionCheck{{
		TxHash:        tx.Hash(),
		ConfirmHeight: 100,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if notIncluded[tx.Hash()] {
		t.Fatal("expected tx still included after moving to a later canonical block")
	}
}

func TestTxsNotIncludedInCanonicalChain_absentFromChain(t *testing.T) {
	ctx := context.Background()
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 3, Gas: 21_000})
	head, byHash := testBlockChain(t, []uint64{99, 100}, nil)

	rt := &ReorgCheckTask{eth: &mockEthReorg{head: head, byHash: byHash}}
	notIncluded, err := rt.txsNotIncludedInCanonicalChain(ctx, []reorgInclusionCheck{{
		TxHash:        tx.Hash(),
		ConfirmHeight: 100,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !notIncluded[tx.Hash()] {
		t.Fatal("expected tx not included when absent from canonical chain")
	}
}

func TestTxsNotIncludedInCanonicalChain_batchSharesWalk(t *testing.T) {
	ctx := context.Background()
	tx1 := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 4, Gas: 21_000})
	tx2 := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 5, Gas: 21_000})
	head, byHash := testBlockChain(t, []uint64{98, 99, 100}, map[uint64]*ethtypes.Transaction{
		99: tx1,
	})

	rt := &ReorgCheckTask{eth: &mockEthReorg{head: head, byHash: byHash}}
	notIncluded, err := rt.txsNotIncludedInCanonicalChain(ctx, []reorgInclusionCheck{
		{TxHash: tx1.Hash(), ConfirmHeight: 99},
		{TxHash: tx2.Hash(), ConfirmHeight: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if notIncluded[tx1.Hash()] {
		t.Fatal("tx1 should be included")
	}
	if !notIncluded[tx2.Hash()] {
		t.Fatal("tx2 should not be included")
	}
}

func TestTxNotIncludedInChain_BlockRPCError(t *testing.T) {
	ctx := context.Background()
	rt := &ReorgCheckTask{eth: &mockEthReorg{blkErr: errors.New("rpc down")}}
	_, err := rt.TxNotIncludedInChain(ctx, common.Hash{1}, 100)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRollbackByReason_unknown(t *testing.T) {
	rt := &ReorgCheckTask{}
	_, err := rt.rollbackByReasonTx(context.Background(), nil, "unknown-reason", "0xabc")
	if err == nil {
		t.Fatal("expected error for unknown send_reason")
	}
}

func TestConfirmationForCheck_fromWaitRow(t *testing.T) {
	rt := &ReorgCheckTask{eth: &mockEthReorg{}}
	epoch, hash, ready, dropped, err := rt.confirmationForCheck(context.Background(), reorgCheckCandidate{
		ConfirmEpoch:    sql.NullInt64{Int64: 100, Valid: true},
		StoredBlockHash: "0x" + common.Hash{1}.Hex()[2:],
	}, 100+int64(policy.ChainFinality))
	if err != nil || !ready || dropped {
		t.Fatalf("ready=%v dropped=%v err=%v", ready, dropped, err)
	}
	if epoch != 100 || hash != (common.Hash{1}) {
		t.Fatalf("unexpected epoch/hash %d %s", epoch, hash)
	}
}

func TestConfirmationForCheck_sendOnlyReceipt(t *testing.T) {
	blkNum := int64(200)
	hdr := &ethtypes.Header{Number: big.NewInt(blkNum), GasLimit: 30_000_000}
	blk := ethtypes.NewBlockWithHeader(hdr)
	receipt := &ethtypes.Receipt{BlockNumber: big.NewInt(blkNum), BlockHash: blk.Hash()}
	rt := &ReorgCheckTask{eth: &mockEthReorg{receipt: receipt}}
	epoch, hash, ready, dropped, err := rt.confirmationForCheck(context.Background(), reorgCheckCandidate{
		SendTime: time.Now().Add(-48 * time.Hour),
	}, blkNum+int64(policy.ChainFinality))
	if err != nil || !ready || dropped {
		t.Fatalf("ready=%v dropped=%v err=%v", ready, dropped, err)
	}
	if epoch != blkNum || hash != blk.Hash() {
		t.Fatalf("unexpected epoch/hash %d %s", epoch, hash)
	}
}

func TestConfirmationForCheck_sendOnlyNotFound(t *testing.T) {
	rt := &ReorgCheckTask{eth: &mockEthReorg{rcptErr: ethereum.NotFound}}
	_, _, ready, dropped, err := rt.confirmationForCheck(context.Background(), reorgCheckCandidate{
		SendTime: time.Now().Add(-48 * time.Hour),
	}, 1000+int64(policy.ChainFinality))
	if err != nil || !ready || !dropped {
		t.Fatalf("ready=%v dropped=%v err=%v", ready, dropped, err)
	}
}

func TestConfirmationForCheck_sendOnlyTooRecent(t *testing.T) {
	rt := &ReorgCheckTask{eth: &mockEthReorg{}}
	_, _, ready, _, err := rt.confirmationForCheck(context.Background(), reorgCheckCandidate{
		SendTime: time.Now(),
	}, 1000+int64(policy.ChainFinality))
	if err != nil || ready {
		t.Fatalf("expected not ready, ready=%v err=%v", ready, err)
	}
}

type replacementReorgEth struct {
	*mockEthReorg
	receipts     map[common.Hash]*ethtypes.Receipt
	receiptCalls []common.Hash
	headCalls    int
}

func (m *replacementReorgEth) TransactionReceipt(_ context.Context, hash common.Hash) (*ethtypes.Receipt, error) {
	m.receiptCalls = append(m.receiptCalls, hash)
	if receipt, ok := m.receipts[hash]; ok {
		return receipt, nil
	}
	return nil, ethereum.NotFound
}

func (m *replacementReorgEth) BlockByNumber(ctx context.Context, number *big.Int) (*ethtypes.Block, error) {
	if number == nil {
		m.headCalls++
	}
	return m.mockEthReorg.BlockByNumber(ctx, number)
}

type replacementReorgChain struct {
	head *chainTypes.TipSet
}

func (m replacementReorgChain) ChainHead(context.Context) (*chainTypes.TipSet, error) {
	return m.head, nil
}

func TestReorgCheckReplacementHashes(t *testing.T) {
	const confirmHeight = uint64(100)
	headHeight := confirmHeight + uint64(policy.ChainFinality)
	txs := []*ethtypes.Transaction{
		ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 42, Gas: 21_000, GasPrice: big.NewInt(1)}),
		ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 42, Gas: 21_000, GasPrice: big.NewInt(2)}),
		ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 42, Gas: 21_000, GasPrice: big.NewInt(3)}),
	}

	for _, tc := range []struct {
		name               string
		wait               bool
		confirmedTx        int
		legacyWait         bool
		replacementHistory bool
		missingBlockHash   bool
		chainTx            int
		absent             bool
		rollback           bool
	}{
		{name: "confirmed replacement after history cleanup", wait: true, confirmedTx: 1, chainTx: 1},
		{name: "missing replacement rolls back original wait", wait: true, confirmedTx: 1, chainTx: 1, absent: true, rollback: true},
		{name: "confirmed replacement overrides newer replacement", wait: true, confirmedTx: 1, replacementHistory: true, chainTx: 1},
		{name: "receipt fallback queries confirmed replacement", wait: true, confirmedTx: 1, missingBlockHash: true, chainTx: 1},
		{name: "send only resolves latest replacement", replacementHistory: true, chainTx: 2},
		{name: "missing send only replacement records original event", replacementHistory: true, chainTx: 2, absent: true, rollback: true},
		{name: "legacy wait falls back to original", wait: true, legacyWait: true, chainTx: 0},
		{name: "unreplaced send falls back to original", chainTx: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)

			now := time.Now().UTC()
			sendTime := now.Add(-time.Duration(policy.ChainFinality+1) * time.Duration(build.BlockDelaySecs) * time.Second)
			from := common.HexToAddress("0x1234").Hex()
			originalHash := txs[0].Hash().Hex()
			_, err = db.Exec(ctx, `
				INSERT INTO message_sends_eth (
					from_address, to_address, send_reason, unsigned_tx, unsigned_hash,
					nonce, signed_hash, send_time, send_success
				) VALUES ($1, $1, $2, $3, $4, 42, $4, $5, TRUE)`,
				from, reasonPDPProve, []byte{1}, originalHash, sendTime)
			require.NoError(t, err)

			if tc.replacementHistory {
				_, err = db.Exec(ctx, `
					INSERT INTO message_send_eth_replacements (
						from_address, nonce, original_signed_hash, replaces_signed_hash,
						claim_id, signed_hash, send_time, send_success
					) VALUES
						($1, 42, $2, $2, 'first-replacement', $3, $5, TRUE),
						($1, 42, $2, $3, 'latest-replacement', $4, $5, TRUE)`,
					from, originalHash, txs[1].Hash().Hex(), txs[2].Hash().Hex(), sendTime)
				require.NoError(t, err)
			}

			canonicalTxs := make(map[uint64]*ethtypes.Transaction)
			if !tc.absent {
				canonicalTxs[confirmHeight] = txs[tc.chainTx]
			}
			head, byHash := testBlockChain(t, []uint64{confirmHeight, headHeight}, canonicalTxs)
			confirmationBlock := byHash[head.ParentHash()]
			receipt := &ethtypes.Receipt{
				TxHash:      txs[tc.chainTx].Hash(),
				BlockNumber: new(big.Int).SetUint64(confirmHeight),
				BlockHash:   confirmationBlock.Hash(),
				Status:      ethtypes.ReceiptStatusSuccessful,
			}
			if tc.wait {
				var confirmedHash any
				if !tc.legacyWait {
					confirmedHash = txs[tc.confirmedTx].Hash().Hex()
				}
				storedReceipt := fmt.Sprintf(`{"blockHash":%q}`, receipt.BlockHash.Hex())
				if tc.missingBlockHash {
					storedReceipt = `{}`
				}
				_, err = db.Exec(ctx, `
					INSERT INTO message_waits_eth (
						signed_tx_hash, tx_status, tx_success, confirmed_block_number, confirmed_tx_hash, tx_receipt
					) VALUES ($1, 'confirmed', TRUE, $2, $3, $4::jsonb)`,
					originalHash, confirmHeight, confirmedHash, storedReceipt)
				require.NoError(t, err)
			}

			eth := &replacementReorgEth{
				mockEthReorg: &mockEthReorg{head: head, byHash: byHash},
				receipts:     make(map[common.Hash]*ethtypes.Receipt),
			}
			if !tc.absent {
				eth.receipts[receipt.TxHash] = receipt
			}
			task := NewReorgCheckTask(db, eth, replacementReorgChain{head: replacementReorgTipSet(t, headHeight, now)})
			done, err := task.Do(ctx, 1, func() bool { return true })
			require.NoError(t, err)
			require.True(t, done)

			if !tc.wait || tc.missingBlockHash {
				require.Equal(t, []common.Hash{txs[tc.chainTx].Hash()}, eth.receiptCalls)
			} else {
				require.Empty(t, eth.receiptCalls)
			}
			if tc.wait || !tc.absent {
				require.Equal(t, 1, eth.headCalls, "candidate must reach the canonical inclusion check")
			}

			var events []struct {
				TxHash string `db:"tx_hash"`
			}
			require.NoError(t, db.Select(ctx, &events, `SELECT tx_hash FROM pdpv0_reorg_events`))
			if tc.rollback {
				require.Len(t, events, 1)
				require.Equal(t, originalHash, events[0].TxHash)
			} else {
				require.Empty(t, events)
			}

			if tc.wait {
				var status string
				var success sql.NullBool
				var confirmed sql.NullString
				var height sql.NullInt64
				var hasReceipt bool
				err = db.QueryRow(ctx, `
					SELECT tx_status, tx_success, confirmed_tx_hash, confirmed_block_number, tx_receipt IS NOT NULL
					FROM message_waits_eth WHERE signed_tx_hash = $1`, originalHash).
					Scan(&status, &success, &confirmed, &height, &hasReceipt)
				require.NoError(t, err)
				if tc.rollback {
					require.Equal(t, "reorged", status)
					require.False(t, success.Valid)
					require.False(t, confirmed.Valid)
					require.False(t, height.Valid)
					require.False(t, hasReceipt)
				} else {
					require.Equal(t, "confirmed", status)
					require.Equal(t, sql.NullBool{Bool: true, Valid: true}, success)
					require.Equal(t, sql.NullInt64{Int64: int64(confirmHeight), Valid: true}, height)
					require.True(t, hasReceipt)
					if tc.legacyWait {
						require.False(t, confirmed.Valid)
					} else {
						require.Equal(t, sql.NullString{String: txs[tc.confirmedTx].Hash().Hex(), Valid: true}, confirmed)
					}
				}
			}
		})
	}
}

func replacementReorgTipSet(t *testing.T, height uint64, now time.Time) *chainTypes.TipSet {
	t.Helper()
	miner, err := address.NewIDAddress(1)
	require.NoError(t, err)
	root, err := cid.Decode("bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4")
	require.NoError(t, err)
	head, err := chainTypes.NewTipSet([]*chainTypes.BlockHeader{{
		Miner:                 miner,
		Ticket:                &chainTypes.Ticket{VRFProof: []byte{1}},
		Height:                abi.ChainEpoch(height),
		ParentStateRoot:       root,
		Messages:              root,
		ParentMessageReceipts: root,
		BlockSig:              &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		BLSAggregate:          &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		Timestamp:             uint64(now.Unix()),
		ParentBaseFee:         chainTypes.NewInt(100),
	}})
	require.NoError(t, err)
	return head
}
