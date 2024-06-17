package webrpc

import (
	"context"
	"github.com/filecoin-project/lotus/chain/types"
	"time"
)

type OpenDealInfo struct {
	Actor        int64     `db:"sp_id"`
	SectorNumber uint64    `db:"sector_number"`
	PieceCID     string    `db:"piece_cid"`
	PieceSize    uint64    `db:"piece_size"`
	CreatedAt    time.Time `db:"created_at"`
	SnapDeals    bool      `db:"is_snap"`

	PieceSizeStr string `db:"-"`
	CreatedAtStr string `db:"-"`
}

func (a *WebRPC) DealsPending(ctx context.Context) ([]OpenDealInfo, error) {
	var deals []OpenDealInfo
	err := a.deps.DB.Select(ctx, &deals, `SELECT sp_id, sector_number, piece_cid, piece_size, created_at, is_snap FROM open_sector_pieces ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}

	for i, deal := range deals {
		deals[i].PieceSizeStr = types.SizeStr(types.NewInt(deal.PieceSize))
		deals[i].CreatedAtStr = deal.CreatedAt.Format("2006-01-02 15:04:05")
	}

	return deals, nil
}

func (a *WebRPC) DealsSealNow(ctx context.Context, spId, sectorNumber uint64) error {
	panic("todo")
}
