// Package parkpiece provides race-safe upsert helpers for the parked_pieces
// table.
package parkpiece

import (
	"errors"
	"sync/atomic"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var activePieceIndexKnownValid atomic.Bool

// Upsert returns the id for the active parked_pieces row matching
// (piece_cid, piece_padded_size, long_term), inserting it when no active row is
// present. When parked_pieces_active_piece_key is known valid, this uses the
// partial unique index as the concurrency guard and performs a no-op conflict
// update only so RETURNING can report the existing row id. When the index is
// missing or INVALID, this avoids ON CONFLICT entirely and uses the degraded
// check-then-insert fallback; that fallback can race and create duplicates
// until FixParkPieceTask repairs the table/index.
func Upsert(tx *harmonydb.Tx, pieceCID string, paddedSize, rawSize int64, longTerm bool) (int64, error) {
	indexValid, err := ActiveIndexValid(tx)
	if err != nil {
		return 0, xerrors.Errorf("checking parked_pieces_active_piece_key: %w", err)
	}

	if indexValid {
		var id int64
		err = tx.QueryRow(`
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL
			-- no-op SET so RETURNING fires on the conflict path (DO NOTHING returns no rows)
			DO UPDATE SET piece_cid = parked_pieces.piece_cid
			RETURNING id`, pieceCID, paddedSize, rawSize, longTerm).Scan(&id)
		if err != nil {
			return 0, xerrors.Errorf("upsert parked_pieces: %w", err)
		}
		return id, nil
	}

	return upsertFallback(tx, pieceCID, paddedSize, rawSize, longTerm, nil)
}

// UpsertSkip is Upsert plus a skip value for newly inserted rows. The skip
// flag is intentionally insert-only: if the piece already exists, both the
// valid-index path and fallback path return the existing id without changing
// the existing row's skip or raw-size metadata.
func UpsertSkip(tx *harmonydb.Tx, pieceCID string, paddedSize, rawSize int64, longTerm, skip bool) (int64, error) {
	indexValid, err := ActiveIndexValid(tx)
	if err != nil {
		return 0, xerrors.Errorf("checking parked_pieces_active_piece_key: %w", err)
	}

	if indexValid {
		var id int64
		err = tx.QueryRow(`
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL
			-- no-op SET so RETURNING fires on the conflict path (DO NOTHING returns no rows)
			DO UPDATE SET piece_cid = parked_pieces.piece_cid
			RETURNING id`, pieceCID, paddedSize, rawSize, longTerm, skip).Scan(&id)
		if err != nil {
			return 0, xerrors.Errorf("upsert parked_pieces: %w", err)
		}
		return id, nil
	}

	return upsertFallback(tx, pieceCID, paddedSize, rawSize, longTerm, &skip)
}

// upsertFallback is the non-atomic check-then-insert path used while the
// partial unique index cannot be bound by ON CONFLICT. It first reuses the
// lowest-id active row for the key, and inserts only when no active row exists.
// There is deliberately no lock here; callers accept the same temporary
// duplicate risk that the cleanup task is responsible for repairing. skip is
// applied only to the inserted row.
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

// ActiveIndexValid returns whether parked_pieces_active_piece_key is present,
// valid, and shaped exactly as the upsert helpers require. It caches only a
// positive answer: once this process has observed the index as valid, later
// calls skip the catalog lookup. Missing, invalid, or wrongly-shaped states are
// rechecked on every call so a repair that drops/recreates the index can be
// observed without restarting the process.
func ActiveIndexValid(tx *harmonydb.Tx) (bool, error) {
	if activePieceIndexKnownValid.Load() {
		return true, nil
	}

	return RefreshActiveIndexValid(tx)
}

// RefreshActiveIndexValid checks the catalog even if this process previously
// cached the index as valid. Repair code uses this path because it must be
// authoritative when deciding whether to drop/recreate the index. The result
// refreshes the positive cache; a false result clears a stale positive cache
// but is still not a negative cache because ActiveIndexValid will requery while
// the flag is false.
func RefreshActiveIndexValid(tx *harmonydb.Tx) (bool, error) {
	var exists bool
	err := tx.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_index ix
			JOIN pg_catalog.pg_class idx ON idx.oid = ix.indexrelid
			JOIN pg_catalog.pg_class tbl ON tbl.oid = ix.indrelid
			JOIN pg_catalog.pg_namespace ns ON ns.oid = tbl.relnamespace
			JOIN pg_catalog.pg_am am ON am.oid = idx.relam
			WHERE ns.nspname = current_schema()
			  AND idx.relnamespace = ns.oid
			  AND tbl.relname = 'parked_pieces'
			  AND idx.relname = 'parked_pieces_active_piece_key'
			  AND idx.relkind = 'i'
			  AND ix.indisunique
			  AND ix.indisvalid
			  AND ix.indisready
			  AND ix.indislive
			  AND ix.indnkeyatts = 3
			  AND ix.indnatts = 3
			  AND ix.indexprs IS NULL
			  AND ARRAY(
				  SELECT pg_catalog.pg_get_indexdef(ix.indexrelid, n, true)
				  FROM generate_series(1, ix.indnkeyatts) AS n
				  ORDER BY n
			  ) = ARRAY['piece_cid', 'piece_padded_size', 'long_term']
			  AND pg_catalog.pg_get_expr(ix.indpred, ix.indrelid, false) = '(cleanup_task_id IS NULL)'
		)`).Scan(&exists)
	if err != nil {
		return false, err
	}
	activePieceIndexKnownValid.Store(exists)
	return exists, nil
}

// ResetActiveIndexValidCacheForTest clears the process-local valid-index cache.
// Production code should not call this; it exists so tests that drop or corrupt
// the index are not order-dependent.
func ResetActiveIndexValidCacheForTest() {
	activePieceIndexKnownValid.Store(false)
}
