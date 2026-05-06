// Package parkpiece provides race-safe upsert helpers for the parked_pieces
// table.
package parkpiece

import (
	"errors"

	"github.com/jackc/pgerrcode"
	"github.com/yugabyte/pgx/v5"
	"github.com/yugabyte/pgx/v5/pgconn"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// Upsert returns the id of the existing live row, or inserts and returns a
// new one. Race-safe under the parked_pieces_active_piece_key partial unique
// index. Falls back to check-then-insert (not race-safe) if that index is
// missing or INVALID. Must run inside a transaction.
func Upsert(tx *harmonydb.Tx, pieceCID string, paddedSize, rawSize int64, longTerm bool) (int64, error) {
	var id int64
	err := tx.QueryRow(`
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL
		-- no-op SET so RETURNING fires on the conflict path (DO NOTHING returns no rows)
		DO UPDATE SET piece_raw_size = EXCLUDED.piece_raw_size
		RETURNING id`, pieceCID, paddedSize, rawSize, longTerm).Scan(&id)
	if err == nil {
		return id, nil
	}
	if isErrInferenceUnmatched(err) {
		return upsertFallback(tx, pieceCID, paddedSize, rawSize, longTerm, nil)
	}
	return 0, xerrors.Errorf("upsert parked_pieces: %w", err)
}

// UpsertSkip is Upsert with an explicit skip value for new rows. Existing
// rows keep their skip value.
func UpsertSkip(tx *harmonydb.Tx, pieceCID string, paddedSize, rawSize int64, longTerm, skip bool) (int64, error) {
	var id int64
	err := tx.QueryRow(`
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL
		-- no-op SET so RETURNING fires on the conflict path (DO NOTHING returns no rows)
		DO UPDATE SET piece_raw_size = EXCLUDED.piece_raw_size
		RETURNING id`, pieceCID, paddedSize, rawSize, longTerm, skip).Scan(&id)
	if err == nil {
		return id, nil
	}
	if isErrInferenceUnmatched(err) {
		return upsertFallback(tx, pieceCID, paddedSize, rawSize, longTerm, &skip)
	}
	return 0, xerrors.Errorf("upsert parked_pieces: %w", err)
}

// upsertFallback is the non-atomic check-then-insert path used when the
// partial unique index isn't bindable. skip is applied only on insert.
func upsertFallback(tx *harmonydb.Tx, pieceCID string, paddedSize, rawSize int64, longTerm bool, skip *bool) (int64, error) {
	var id int64
	err := tx.QueryRow(`
		SELECT id FROM parked_pieces
		WHERE piece_cid = $1 AND piece_padded_size = $2 AND long_term = $3 AND cleanup_task_id IS NULL
		ORDER BY id LIMIT 1`, pieceCID, paddedSize, longTerm).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, xerrors.Errorf("upsert parked_pieces (fallback select): %w", err)
	}
	if skip != nil {
		err = tx.QueryRow(`
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			pieceCID, paddedSize, rawSize, longTerm, *skip).Scan(&id)
	} else {
		err = tx.QueryRow(`
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			VALUES ($1, $2, $3, $4) RETURNING id`,
			pieceCID, paddedSize, rawSize, longTerm).Scan(&id)
	}
	if err != nil {
		return 0, xerrors.Errorf("upsert parked_pieces (fallback insert): %w", err)
	}
	return id, nil
}

// isErrInferenceUnmatched is PG 42P10 - ON CONFLICT inference could not bind
// to any unique constraint or VALID unique index.
func isErrInferenceUnmatched(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.InvalidColumnReference
}
