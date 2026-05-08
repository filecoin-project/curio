package pdpv0

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type mockEthReorg struct {
	receipt *ethtypes.Receipt
	recErr  error
	block   *ethtypes.Block
	blkErr  error
}

func (m *mockEthReorg) TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethtypes.Receipt, error) {
	if m.recErr != nil {
		return nil, m.recErr
	}
	return m.receipt, nil
}

func (m *mockEthReorg) BlockByNumber(ctx context.Context, number *big.Int) (*ethtypes.Block, error) {
	if m.blkErr != nil {
		return nil, m.blkErr
	}
	return m.block, nil
}

func TestReorgCheckTask_txReorgedFromChain_NotFound(t *testing.T) {
	ctx := context.Background()
	rt := &ReorgCheckTask{eth: &mockEthReorg{recErr: ethereum.NotFound}}
	reorged, err := rt.txReorgedFromChain(ctx, common.Hash{42})
	if err != nil {
		t.Fatal(err)
	}
	if !reorged {
		t.Fatal("expected reorged when receipt not found")
	}
}

func TestReorgCheckTask_txReorgedFromChain_HashMismatch(t *testing.T) {
	ctx := context.Background()
	hdr := &ethtypes.Header{Number: big.NewInt(100), GasLimit: 30_000_000}
	canonical := ethtypes.NewBlockWithHeader(hdr)
	r := &ethtypes.Receipt{
		Status:           ethtypes.ReceiptStatusSuccessful,
		BlockNumber:      big.NewInt(100),
		BlockHash:        common.Hash{1}, // not equal to canonical.Hash()
		TransactionIndex: 0,
	}
	rt := &ReorgCheckTask{eth: &mockEthReorg{receipt: r, block: canonical}}
	reorged, err := rt.txReorgedFromChain(ctx, common.Hash{9})
	if err != nil {
		t.Fatal(err)
	}
	if !reorged {
		t.Fatal("expected reorged on block hash mismatch")
	}
}

func TestReorgCheckTask_txReorgedFromChain_OK(t *testing.T) {
	ctx := context.Background()
	hdr := &ethtypes.Header{Number: big.NewInt(100), GasLimit: 30_000_000}
	blk := ethtypes.NewBlockWithHeader(hdr)
	r := &ethtypes.Receipt{
		Status:           ethtypes.ReceiptStatusSuccessful,
		BlockNumber:      big.NewInt(100),
		BlockHash:        blk.Hash(),
		TransactionIndex: 0,
	}
	rt := &ReorgCheckTask{eth: &mockEthReorg{receipt: r, block: blk}}
	reorged, err := rt.txReorgedFromChain(ctx, common.Hash{9})
	if err != nil {
		t.Fatal(err)
	}
	if reorged {
		t.Fatal("expected not reorged when canonical hash matches")
	}
}

func TestReorgCheckTask_txReorgedFromChain_FailedReceipt(t *testing.T) {
	ctx := context.Background()
	r := &ethtypes.Receipt{
		Status:           ethtypes.ReceiptStatusFailed,
		BlockNumber:      big.NewInt(100),
		BlockHash:        common.Hash{1},
		TransactionIndex: 0,
	}
	rt := &ReorgCheckTask{eth: &mockEthReorg{receipt: r, block: nil}}
	reorged, err := rt.txReorgedFromChain(ctx, common.Hash{9})
	if err != nil {
		t.Fatal(err)
	}
	if reorged {
		t.Fatal("failed on-chain tx should not be treated as reorged")
	}
}

func TestRollbackByReason_unknown(t *testing.T) {
	rt := &ReorgCheckTask{}
	_, err := rt.rollbackByReason(context.Background(), "unknown-reason", "0xabc")
	if err == nil {
		t.Fatal("expected error for unknown send_reason")
	}
}

func TestReorgCheckTask_txReorgedFromChain_BlockRPCError(t *testing.T) {
	ctx := context.Background()
	r := &ethtypes.Receipt{
		Status:           ethtypes.ReceiptStatusSuccessful,
		BlockNumber:      big.NewInt(100),
		BlockHash:        common.Hash{1},
		TransactionIndex: 0,
	}
	rt := &ReorgCheckTask{eth: &mockEthReorg{receipt: r, blkErr: errors.New("rpc down")}}
	_, err := rt.txReorgedFromChain(ctx, common.Hash{9})
	if err == nil {
		t.Fatal("expected error")
	}
}
