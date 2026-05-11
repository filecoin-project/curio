package pdpv0

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type mockEthReorg struct {
	block  *ethtypes.Block
	blkErr error
}

func (m *mockEthReorg) BlockByNumber(ctx context.Context, number *big.Int) (*ethtypes.Block, error) {
	if m.blkErr != nil {
		return nil, m.blkErr
	}
	return m.block, nil
}

func TestReorgCheckTask_txReorgedFromChain_HashMismatch(t *testing.T) {
	ctx := context.Background()
	hdr := &ethtypes.Header{Number: big.NewInt(100), GasLimit: 30_000_000}
	canonical := ethtypes.NewBlockWithHeader(hdr)
	storedBlockHash := common.Hash{1} // differs from canonical.Hash()
	rt := &ReorgCheckTask{eth: &mockEthReorg{block: canonical}}
	reorged, err := rt.TxReorgedFromChain(ctx, 100, storedBlockHash)
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
	rt := &ReorgCheckTask{eth: &mockEthReorg{block: blk}}
	reorged, err := rt.TxReorgedFromChain(ctx, 100, blk.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if reorged {
		t.Fatal("expected not reorged when canonical hash matches")
	}
}

func TestReorgCheckTask_txReorgedFromChain_BlockRPCError(t *testing.T) {
	ctx := context.Background()
	rt := &ReorgCheckTask{eth: &mockEthReorg{blkErr: errors.New("rpc down")}}
	_, err := rt.TxReorgedFromChain(ctx, 100, common.Hash{1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRollbackByReason_unknown(t *testing.T) {
	rt := &ReorgCheckTask{}
	_, err := rt.rollbackByReason(context.Background(), "unknown-reason", "0xabc")
	if err == nil {
		t.Fatal("expected error for unknown send_reason")
	}
}
