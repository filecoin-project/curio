package webrpc

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/market/mk20"
)

type MK20StorageDeal struct {
	Deal       *mk20.Deal     `json:"deal"`
	Error      sql.NullString `json:"error"`
	PieceCidV2 string         `json:"piece_cid_v2"`
}

func (a *WebRPC) MK20DDOStorageDeal(ctx context.Context, id string) (*MK20StorageDeal, error) {
	pid, err := ulid.Parse(id)
	if err != nil {
		return nil, xerrors.Errorf("parsing deal ID: %w", err)
	}

	var dbDeal []mk20.DBDeal
	err = a.deps.DB.Select(ctx, &dbDeal, `SELECT id, 
													piece_cid, 
													piece_size, 
													format, 
													source_http, 
													source_aggregate, 
													source_offline, 
													source_http_put, 
													ddo_v1,
													error FROM market_mk20_deal WHERE id = $1`, pid.String())
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

	pi := abi.PieceInfo{
		PieceCID: deal.Data.PieceCID,
		Size:     deal.Data.Size,
	}

	commp, err := commcidv2.CommPFromPieceInfo(pi)
	if err != nil {
		return nil, xerrors.Errorf("failed to get commp: %w", err)
	}

	return &MK20StorageDeal{Deal: deal, Error: dbDeal[0].Error, PieceCidV2: commp.PCidV2().String()}, nil
}

func (a *WebRPC) MK20DDOStorageDeals(ctx context.Context, limit int, offset int) ([]*StorageDealList, error) {
	var mk20Summaries []*StorageDealList

	err := a.deps.DB.Select(ctx, &mk20Summaries, `SELECT
													  d.id AS uuid,
													  d.piece_cid,
													  d.piece_size,
													  d.created_at,
													  d.sp_id,
													  d.error,
													  CASE
														WHEN EXISTS (
														  SELECT 1 FROM market_mk20_pipeline_waiting w
														  WHERE w.id = d.id
														) THEN FALSE
														WHEN EXISTS (
														  SELECT 1 FROM market_mk20_pipeline p
														  WHERE p.id = d.id AND p.complete = FALSE
														) THEN FALSE
														ELSE TRUE
													  END AS processed
													FROM market_mk20_deal d
													WHERE d.ddo_v1 IS NOT NULL AND d.ddo_v1 != 'null'
													ORDER BY d.created_at DESC
													LIMIT $1 OFFSET $2;
													`, limit, offset)
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
