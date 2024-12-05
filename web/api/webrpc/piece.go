package webrpc

import (
	"context"
	"database/sql"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func (a *WebRPC) FindEntriesByDataURL(ctx context.Context, dataURL string) ([]PieceParkRefEntry, error) {
	var entries = []PieceParkRefEntry{}

	_, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		rows, err := tx.Query(`
			WITH combined AS (
				-- SDR Initial Pieces
				SELECT
					'sdr' AS table_name,
					sp_id,
					sector_number,
					piece_index,
					piece_cid,
					data_url,
					NULL::TEXT AS deal_uuid
				FROM sectors_sdr_initial_pieces
				WHERE data_url = $1

				UNION ALL

				-- Snap Initial Pieces
				SELECT
					'snap' AS table_name,
					sp_id,
					sector_number,
					piece_index,
					piece_cid,
					data_url,
					NULL::TEXT AS deal_uuid
				FROM sectors_snap_initial_pieces
				WHERE data_url = $1

				UNION ALL

				-- Open Sector Pieces
				SELECT
					'openpieces' AS table_name,
					sp_id,
					sector_number,
					piece_index,
					piece_cid,
					data_url,
					NULL::TEXT AS deal_uuid
				FROM open_sector_pieces
				WHERE data_url = $1

				UNION ALL

				-- Market MK12 Deal Pipeline
				SELECT
					'market' AS table_name,
					sp_id,
					NULL::BIGINT AS sector_number,
					NULL::BIGINT AS piece_index,
					piece_cid,
					url AS data_url,
					uuid AS deal_uuid
				FROM market_mk12_deal_pipeline
				WHERE url = $1
			)
			SELECT
				table_name,
				sp_id,
				sector_number,
				piece_index,
				piece_cid,
				deal_uuid
			FROM combined;
		`, dataURL)
		if err != nil {
			return false, err
		}
		defer rows.Close()

		for rows.Next() {
			var entry PieceParkRefEntry
			var sectorNumber sql.NullInt64
			var pieceIndex sql.NullInt64
			var dealUUID sql.NullString

			err = rows.Scan(&entry.TableName, &entry.SpID, &sectorNumber, &pieceIndex, &entry.PieceCID, &dealUUID)
			if err != nil {
				return false, err
			}
			if sectorNumber.Valid {
				entry.SectorNumber = &sectorNumber.Int64
			}
			if pieceIndex.Valid {
				entry.PieceIndex = &pieceIndex.Int64
			}
			if dealUUID.Valid {
				entry.DealUUID = &dealUUID.String
			}
			a, err := address.NewIDAddress(uint64(entry.SpID))
			if err != nil {
				return false, err
			}
			entry.Addr = a.String()

			entries = append(entries, entry)
		}
		if err = rows.Err(); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
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
