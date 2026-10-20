package webrpc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/commcidv2"
	itype "github.com/filecoin-project/curio/market/ipni/types"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/tasks/indexing"
)

type MK20StorageDeal struct {
	Deal   *mk20.Deal `json:"deal"`
	DDOErr NullString `json:"ddoerr"`
	PDPErr NullString `json:"pdperr"`
	DDOId  NullInt64  `json:"ddoid"`
}

func (a *WebRPC) MK20DDOStorageDeal(ctx context.Context, id string) (*MK20StorageDeal, error) {
	pid, err := ulid.Parse(id)
	if err != nil {
		return nil, xerrors.Errorf("parsing deal ID: %w", err)
	}

	var dbDeals []mk20.DBDeal
	err = a.Deps.DB.Select(ctx, &dbDeals, `SELECT id, client, data, ddo_v1, retrieval_v1, pdp_v1
		FROM market_mk20_deal WHERE id = $1`, pid.String())
	if err != nil {
		return nil, xerrors.Errorf("getting deal from DB: %w", err)
	}
	if len(dbDeals) != 1 {
		return nil, xerrors.Errorf("expected 1 deal, got %d", len(dbDeals))
	}
	return mk20StorageDealFromDB(dbDeals[0])
}

func mk20StorageDealFromDB(dbDeal mk20.DBDeal) (*MK20StorageDeal, error) {
	deal, err := dbDeal.ToDeal()
	if err != nil {
		return nil, xerrors.Errorf("converting DB deal to struct: %w", err)
	}

	ret := &MK20StorageDeal{Deal: deal}

	if len(dbDeal.DDOv1) > 0 && string(dbDeal.DDOv1) != "null" {
		var dddov1 mk20.DBDDOV1
		if err := json.Unmarshal(dbDeal.DDOv1, &dddov1); err != nil {
			return nil, fmt.Errorf("unmarshal ddov1: %w", err)
		}
		if dddov1.Error != "" {
			ret.DDOErr = NullString{String: dddov1.Error, Valid: true}
		}
		if dddov1.DealID > 0 {
			ret.DDOId = NullInt64{Int64: dddov1.DealID, Valid: true}
		}
	}

	if len(dbDeal.PDPV1) > 0 && string(dbDeal.PDPV1) != "null" {
		var pdpv1 mk20.DBPDPV1
		if err := json.Unmarshal(dbDeal.PDPV1, &pdpv1); err != nil {
			return nil, fmt.Errorf("unmarshal pdpv1: %w", err)
		}
		if pdpv1.Error != "" {
			ret.PDPErr = NullString{String: pdpv1.Error, Valid: true}
		}
	}

	return ret, nil
}

type PieceDeal struct {
	// Cache line 1 (0-64 bytes): Hot path - identification fields used together
	ID    string `db:"id" json:"id"`       // 16 bytes - used with SpId (line 621-623)
	SpId  int64  `db:"sp_id" json:"sp_id"` // 8 bytes - checked early (line 614), used with ID (line 617)
	Miner string `json:"miner"`            // 16 bytes - set based on SpId (line 615, 625)
	// Cache line 2: Additional 8-byte types grouped together
	ChainDealId int64 `db:"chain_deal_id" json:"chain_deal_id"` // 8 bytes
	Sector      int64 `db:"sector_num" json:"sector"`           // 8 bytes
	Length      int64 `db:"piece_length" json:"length"`         // 8 bytes
	RawSize     int64 `db:"raw_size" json:"raw_size"`           // 8 bytes
	// NullInt64 (16 bytes)
	Offset NullInt64 `db:"piece_offset" json:"offset"` // 16 bytes
	// Cache line 3 (64+ bytes): Bools grouped together at the end to minimize padding
	MK20       bool `db:"-" json:"mk20"`                  // Used with ID check (line 621-623) - hot path
	BoostDeal  bool `db:"boost_deal" json:"boost_deal"`   // Less frequently accessed
	LegacyDeal bool `db:"legacy_deal" json:"legacy_deal"` // Less frequently accessed
}

type PieceInfo struct {
	// Cache line 1 (0-64 bytes): Hot path - piece identification used together
	PieceCidv2 string   `json:"piece_cid_v2"` // 16 bytes - used together with PieceCid (line 584)
	PieceCid   string   `json:"piece_cid"`    // 16 bytes - used with PieceCidv2 (line 584)
	Size       int64    `json:"size"`         // 8 bytes - used with PieceCid (line 587, 604)
	IPNIAd     []string `json:"ipni_ads"`     // 24 bytes - used for results
	// Cache line 2 (64+ bytes): Display
	CreatedAt time.Time `json:"created_at"` // 24 bytes - used for display
	IndexedAT time.Time `json:"indexed_at"` // 24 bytes - used for display
	Indexed   bool      `json:"indexed"`    // Used for display
	// Cache line 3
	Deals []PieceDeal `json:"deals"` // 24 bytes - used for results
}

func (a *WebRPC) PieceInfo(ctx context.Context, pieceCid string) (*PieceInfo, error) {
	piece, err := cid.Parse(pieceCid)
	if err != nil {
		return nil, err
	}

	ret := PieceInfo{IPNIAd: []string{}}
	pcid := piece
	var rawSize uint64
	if commcidv2.IsPieceCidV2(piece) {
		pcid, rawSize, err = commcid.PieceCidV1FromV2(piece)
		if err != nil {
			return nil, xerrors.Errorf("failed to get piece CID v1 from piece CID v2: %w", err)
		}
		ret.PieceCidv2 = piece.String()
		ret.Size = int64(padreader.PaddedSize(rawSize).Padded())
	}
	ret.PieceCid = pcid.String()

	err = a.Deps.DB.QueryRow(ctx, `
		SELECT piece_size, created_at, indexed, indexed_at
		FROM market_piece_metadata
		WHERE piece_cid = $1 AND ($2::bigint = 0 OR piece_size = $2)
	`, ret.PieceCid, ret.Size).Scan(&ret.Size, &ret.CreatedAt, &ret.Indexed, &ret.IndexedAT)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("failed to get piece metadata: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// PDPv0 pieces are tracked in parked_pieces and pdp_piecerefs without
		// creating market_piece_metadata. Raw size also identifies their v2 CID.
		var parkedID int64
		err = a.Deps.DB.QueryRow(ctx, `
			SELECT id, piece_padded_size, piece_raw_size, created_at
			FROM parked_pieces
			WHERE piece_cid = $1 AND ($2::bigint = 0 OR piece_raw_size = $2)
			ORDER BY complete DESC, long_term DESC, created_at DESC, id DESC LIMIT 1
		`, ret.PieceCid, rawSize).Scan(&parkedID, &ret.Size, &rawSize, &ret.CreatedAt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, xerrors.Errorf("failed to get parked piece metadata: %w", err)
		}
		if err == nil {
			if ret.PieceCidv2 == "" && rawSize >= 127 {
				piece, err = commcid.PieceCidV2FromV1(pcid, rawSize)
				if err != nil {
					return nil, xerrors.Errorf("failed to get piece CID v2: %w", err)
				}
				ret.PieceCidv2 = piece.String()
			}
			var indexedAt sql.NullTime
			err = a.Deps.DB.QueryRow(ctx, `
				SELECT MAX(pr.indexed_at)
				FROM pdp_piecerefs pr
				JOIN parked_piece_refs r ON r.ref_id = pr.piece_ref
				WHERE r.piece_id = $1 AND pr.needs_indexing = FALSE
			`, parkedID).Scan(&indexedAt)
			if err != nil {
				return nil, xerrors.Errorf("failed to get PDP piece indexing state: %w", err)
			}
			ret.Indexed = indexedAt.Valid
			ret.IndexedAT = indexedAt.Time
		}
	}
	size := abi.PaddedPieceSize(ret.Size)

	if ret.PieceCidv2 != "" {
		for _, payload := range []bool{true, false} {
			pdpContext := itype.PdpIpniContext{PieceCID: piece, Payload: payload}
			contextID, err := pdpContext.Marshal()
			if err != nil {
				return nil, xerrors.Errorf("failed to marshal PDP piece info: %w", err)
			}
			var ad string
			err = a.Deps.DB.QueryRow(ctx, `SELECT ad_cid FROM ipni WHERE context_id = $1 ORDER BY order_number DESC LIMIT 1`, contextID).Scan(&ad)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, xerrors.Errorf("failed to get ad ID by piece CID for PDP: %w", err)
			}
			if err == nil {
				ret.IPNIAd = append(ret.IPNIAd, ad)
			}
		}
	}

	pieceDeals := []PieceDeal{}

	err = a.Deps.DB.Select(ctx, &pieceDeals, `SELECT 
														id, 
														boost_deal, 
														legacy_deal, 
														chain_deal_id, 
														sp_id, 
														sector_num, 
														piece_offset, 
														piece_length, 
														raw_size 
													FROM market_piece_deal
													WHERE piece_cid = $1`, ret.PieceCid)
	if err != nil {
		return nil, xerrors.Errorf("failed to get piece deals: %w", err)
	}

	for i := range pieceDeals {
		_, err := uuid.Parse(pieceDeals[i].ID)
		pieceDeals[i].MK20 = err != nil
		if pieceDeals[i].SpId == indexing.PDP_v1_SP_ID {
			pieceDeals[i].Miner = "PDP V1"
		} else {
			addr, err := address.NewIDAddress(uint64(pieceDeals[i].SpId))
			if err != nil {
				return nil, err
			}
			pieceDeals[i].Miner = addr.String()
		}
	}
	ret.Deals = pieceDeals

	b := new(bytes.Buffer)

	pi := abi.PieceInfo{
		PieceCID: pcid,
		Size:     size,
	}

	err = pi.MarshalCBOR(b)
	if err != nil {
		return nil, xerrors.Errorf("failed to marshal piece info: %w", err)
	}

	// Get only the latest Ad
	var ipniAd string
	err = a.Deps.DB.QueryRow(ctx, `SELECT ad_cid FROM ipni WHERE context_id = $1 ORDER BY order_number DESC LIMIT 1`, b.Bytes()).Scan(&ipniAd)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("failed to get ad ID by piece CID: %w", err)
	}

	if err == nil {
		ret.IPNIAd = append(ret.IPNIAd, ipniAd)
	}
	return &ret, nil
}

type ParkedPieceState struct {
	ID              int64            `db:"id" json:"id"`
	PieceCID        string           `db:"piece_cid" json:"piece_cid"`
	PiecePaddedSize int64            `db:"piece_padded_size" json:"piece_padded_size"`
	PieceRawSize    int64            `db:"piece_raw_size" json:"piece_raw_size"`
	Complete        bool             `db:"complete" json:"complete"`
	CreatedAt       time.Time        `db:"created_at" json:"created_at"`
	TaskID          sql.NullInt64    `db:"task_id" json:"task_id"`
	CleanupTaskID   sql.NullInt64    `db:"cleanup_task_id" json:"cleanup_task_id"`
	Refs            []ParkedPieceRef `json:"refs"`
}

type ParkedPieceRef struct {
	RefID       int64           `db:"ref_id" json:"ref_id"`
	PieceID     int64           `db:"piece_id" json:"piece_id"`
	DataURL     sql.NullString  `db:"data_url" json:"data_url"`
	DataHeaders json.RawMessage `db:"data_headers" json:"data_headers"`
}

// PieceParkStates retrieves the park states for a given piece CID
func (a *WebRPC) PieceParkStates(ctx context.Context, pieceCID string) (*ParkedPieceState, error) {
	pcid2, err := cid.Parse(pieceCID)
	if err != nil {
		return nil, err
	}

	var pcid1 cid.Cid
	var rawSize uint64

	if commcidv2.IsPieceCidV2(pcid2) {
		pcid, size, err := commcid.PieceCidV1FromV2(pcid2)
		if err != nil {
			return nil, xerrors.Errorf("failed to get piece CID v1 from piece CID v2: %w", err)
		}
		pcid1 = pcid
		rawSize = size
	} else {
		pcid1 = pcid2
	}

	pps := ParkedPieceState{Refs: []ParkedPieceRef{}}

	// Query the parked_pieces table
	err = a.Deps.DB.QueryRow(ctx, `
        SELECT id, created_at, piece_cid, piece_padded_size, piece_raw_size, complete, task_id, cleanup_task_id
        FROM parked_pieces WHERE piece_cid = $1 AND ($2::bigint = 0 OR piece_raw_size = $2)
        ORDER BY complete DESC, long_term DESC, created_at DESC, id DESC LIMIT 1
    `, pcid1.String(), rawSize).Scan(
		&pps.ID, &pps.CreatedAt, &pps.PieceCID, &pps.PiecePaddedSize, &pps.PieceRawSize,
		&pps.Complete, &pps.TaskID, &pps.CleanupTaskID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query parked piece: %w", err)
	}

	// Query the parked_piece_refs table for references
	rows, err := a.Deps.DB.Query(ctx, `
        SELECT ref_id, piece_id, data_url, data_headers
        FROM parked_piece_refs WHERE piece_id = $1
    `, pps.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query parked piece refs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ref ParkedPieceRef
		var dataHeaders []byte
		err := rows.Scan(&ref.RefID, &ref.PieceID, &ref.DataURL, &dataHeaders)
		if err != nil {
			return nil, fmt.Errorf("failed to scan parked piece ref: %w", err)
		}
		ref.DataHeaders = json.RawMessage(dataHeaders)
		pps.Refs = append(pps.Refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over parked piece refs: %w", err)
	}

	return &pps, nil
}

type PieceParkRefEntry struct {
	TableName    string  `json:"table_name"`
	SpID         int64   `json:"sp_id"`
	SectorNumber *int64  `json:"sector_number,omitempty"`
	PieceIndex   *int64  `json:"piece_index,omitempty"`
	PieceCID     string  `json:"piece_cid"`
	DealUUID     *string `json:"deal_uuid,omitempty"`

	Addr string `json:"addr"`
}
