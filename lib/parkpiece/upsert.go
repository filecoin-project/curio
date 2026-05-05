// Package parkpiece provides race-safe upsert helpers for the parked_pieces
// table.
package parkpiece

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// Upsert atomically inserts a parked_pieces row for the given
// (piece_cid, piece_padded_size, long_term) or returns the id of the existing
// live row. Safe under concurrent writers.
//
// Must be called inside a transaction.
func Upsert(tx *harmonydb.Tx, pieceCID string, paddedSize, rawSize int64, longTerm bool) (int64, error) {
	var id int64
	err := tx.QueryRow(`
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL
		-- no-op SET so RETURNING fires on the conflict path (DO NOTHING returns no rows)
		DO UPDATE SET piece_raw_size = EXCLUDED.piece_raw_size
		RETURNING id`, pieceCID, paddedSize, rawSize, longTerm).Scan(&id)
	if err != nil {
		return 0, xerrors.Errorf("upsert parked_pieces: %w", err)
	}
	return id, nil
}

// UpsertSkip is like Upsert but inserts the row with the given skip value when
// new. The existing row's skip value is preserved on the conflict path -
// callers that need to flip skip on an existing row must do so separately.
//
// Must be called inside a transaction.
func UpsertSkip(tx *harmonydb.Tx, pieceCID string, paddedSize, rawSize int64, longTerm, skip bool) (int64, error) {
	var id int64
	err := tx.QueryRow(`
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL
		-- no-op SET so RETURNING fires on the conflict path (DO NOTHING returns no rows)
		DO UPDATE SET piece_raw_size = EXCLUDED.piece_raw_size
		RETURNING id`, pieceCID, paddedSize, rawSize, longTerm, skip).Scan(&id)
	if err != nil {
		return 0, xerrors.Errorf("upsert parked_pieces: %w", err)
	}
	return id, nil
}
