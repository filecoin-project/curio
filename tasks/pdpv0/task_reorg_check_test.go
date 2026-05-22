package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"

	"github.com/filecoin-project/lotus/chain/actors/policy"
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
