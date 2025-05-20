package webrpc

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"
)

type MK20StorageDeal struct {
	Deal  *mk20.Deal     `json:"deal"`
	Error sql.NullString `json:"error"`
}

func (a *WebRPC) MK20DDOStorageDeal(ctx context.Context, idStr string) (*MK20StorageDeal, error) {
	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, xerrors.Errorf("parsing deal ID: %w", err)
	}

	var dbDeal []mk20.DBDeal
	err = a.deps.DB.Select(ctx, &dbDeal, `SELECT * FROM market_mk20_deal WHERE id = $1`, id.String())
	if err != nil {
		return nil, xerrors.Errorf("getting deal from DB: %w", err)
	}
	if len(dbDeal) != 1 {
		return nil, xerrors.Errorf("expected 1 deal, got %d", len(dbDeal))
	}
	deal, err := dbDeal[0].ToDeal()
	if err != nil {
		return nil, xerrors.Errorf("converting DB deal to struct: %w", err)
	}

	return &MK20StorageDeal{Deal: deal, Error: dbDeal[0].Error}, nil
}

func (a *WebRPC) MK20DDOStorageDeals(ctx context.Context, limit int, offset int) ([]*StorageDealList, error) {
	var mk20Summaries []*StorageDealList

	err := a.deps.DB.Select(ctx, &mk20Summaries, `SELECT
															d.id as uuid,
															d.piece_cid,
															d.size AS piece_size,
															d.created_at,
															d.sp_id,
															d.error,
															CASE
																WHEN w.id IS NOT NULL THEN FALSE
																WHEN p.id IS NOT NULL THEN p.complete
																ELSE TRUE
															END AS processed
														FROM market_mk20_deal d
														LEFT JOIN market_mk20_pipeline_waiting w ON d.id = w.id
														LEFT JOIN market_mk20_pipeline p ON d.id = p.id
														WHERE d.ddo_v1 IS NOT NULL AND d.ddo_v1 != 'null'
														ORDER BY d.created_at DESC
														LIMIT $1 OFFSET $2;`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal list: %w", err)
	}

	for i := range mk20Summaries {
		addr, err := address.NewIDAddress(uint64(mk20Summaries[i].MinerID))
		if err != nil {
			return nil, err
		}
		mk20Summaries[i].Miner = addr.String()
		pcid, err := cid.Parse(mk20Summaries[i].PieceCidV1)
		if err != nil {
			return nil, xerrors.Errorf("failed to parse v1 piece CID: %w", err)
		}
		commp, err := commcidv2.CommPFromPieceInfo(abi.PieceInfo{
			PieceCID: pcid,
			Size:     abi.PaddedPieceSize(mk20Summaries[i].PieceSize),
		})
		if err != nil {
			return nil, xerrors.Errorf("failed to get commP from piece info: %w", err)
		}
		mk20Summaries[i].PieceCidV2 = commp.PCidV2().String()
	}
	return mk20Summaries, nil
}
