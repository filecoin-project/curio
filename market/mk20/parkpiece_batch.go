package mk20

import (
	"context"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/parkpiece"
)

const parkedPieceDownloadBatchSize = 5000

// ParkedPieceDownloadRef is one download URL that must become exactly one
// parked_piece_refs row and exactly one appended ref_id in
// market_mk20_download_pipeline. Do not dedupe values of this type: duplicate
// URLs, duplicate headers, or duplicate piece keys are still distinct download
// refs that must be represented by distinct ref_ids.
type ParkedPieceDownloadRef struct {
	ID         string
	PieceCIDV2 string
	PieceCID   string
	PaddedSize int64
	RawSize    int64
	URL        string
	Headers    []byte
	LongTerm   bool
}

// InsertParkedPieceDownloadRefsBatch preserves the original per-URL semantics
// while still using pgx.Batch to reduce round trips. Each queued statement is
// one logical download source: it resolves or creates the parked_pieces row,
// inserts exactly one parked_piece_refs row, and appends that exact returned
// ref_id to market_mk20_download_pipeline. This deliberately avoids bulk ref
// insertion plus post-hoc ref_id remapping because (piece_id, data_url) is not
// a unique identity for a ref when duplicate URLs or headers are present.
func InsertParkedPieceDownloadRefsBatch(ctx context.Context, tx *harmonydb.Tx, product ProductName, refs []ParkedPieceDownloadRef) error {
	if len(refs) == 0 {
		return nil
	}

	indexValid, err := parkpiece.ActiveIndexValid(tx)
	if err != nil {
		return xerrors.Errorf("checking parked_pieces_active_piece_key: %w", err)
	}

	query := parkedPieceDownloadRefInsertSQL(indexValid)
	for start := 0; start < len(refs); start += parkedPieceDownloadBatchSize {
		end := min(start+parkedPieceDownloadBatchSize, len(refs))
		batch := &pgx.Batch{}
		for _, ref := range refs[start:end] {
			batch.Queue(query, ref.PieceCID, ref.PaddedSize, ref.RawSize, ref.LongTerm, ref.URL, ref.Headers, ref.ID, product, ref.PieceCIDV2)
		}

		res, err := tx.SendBatch(ctx, batch)
		if err != nil {
			return xerrors.Errorf("sending parked piece download ref batch: %w", err)
		}
		for i := 0; i < batch.Len(); i++ {
			if _, err := res.Exec(); err != nil {
				_ = res.Close()
				return xerrors.Errorf("executing parked piece download ref batch item %d: %w", start+i, err)
			}
		}
		if err := res.Close(); err != nil {
			return xerrors.Errorf("closing parked piece download ref batch: %w", err)
		}
	}
	return nil
}

// parkedPieceDownloadRefInsertSQL returns one self-contained SQL statement for
// one ParkedPieceDownloadRef. The valid-index statement uses the partial unique
// index for atomic parked_pieces upsert. The invalid/missing-index statement
// never references ON CONFLICT on parked_pieces, avoiding 42P10; it has the
// same degraded duplicate risk as parkpiece.Upsert fallback until
// FixParkPieceTask repairs the table/index. The CTEs here are statement-local
// only: they thread the exact inserted ref_id into the pipeline upsert for this
// one URL, and do not perform cross-row bridge/remap logic.
func parkedPieceDownloadRefInsertSQL(indexValid bool) string {
	if indexValid {
		return `
			WITH selected_piece AS (
				INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (piece_cid, piece_padded_size, long_term) WHERE cleanup_task_id IS NULL
				DO UPDATE SET piece_cid = parked_pieces.piece_cid
				RETURNING id
			),
			inserted_ref AS (
				INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
				SELECT id, $5, $6, $4 FROM selected_piece
				RETURNING ref_id
			)
			INSERT INTO market_mk20_download_pipeline (id, piece_cid_v2, product, ref_ids)
			VALUES ($7, $9, $8, ARRAY[(SELECT ref_id FROM inserted_ref)])
			ON CONFLICT (id, piece_cid_v2, product) DO UPDATE
			SET ref_ids = array_append(
				market_mk20_download_pipeline.ref_ids,
				(SELECT ref_id FROM inserted_ref)
			)
			WHERE NOT market_mk20_download_pipeline.ref_ids @> ARRAY[(SELECT ref_id FROM inserted_ref)]`
	}

	return `
		WITH existing_piece AS (
			SELECT id
			FROM parked_pieces
			WHERE piece_cid = $1
			  AND piece_padded_size = $2
			  AND long_term = $4
			  AND cleanup_task_id IS NULL
			ORDER BY id
			LIMIT 1
		),
		inserted_piece AS (
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			SELECT $1, $2, $3, $4
			WHERE NOT EXISTS (SELECT 1 FROM existing_piece)
			RETURNING id
		),
		selected_piece AS (
			SELECT id FROM existing_piece
			UNION ALL
			SELECT id FROM inserted_piece
			LIMIT 1
		),
		inserted_ref AS (
			INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
			SELECT id, $5, $6, $4 FROM selected_piece
			RETURNING ref_id
		)
		INSERT INTO market_mk20_download_pipeline (id, piece_cid_v2, product, ref_ids)
		VALUES ($7, $9, $8, ARRAY[(SELECT ref_id FROM inserted_ref)])
		ON CONFLICT (id, piece_cid_v2, product) DO UPDATE
		SET ref_ids = array_append(
			market_mk20_download_pipeline.ref_ids,
			(SELECT ref_id FROM inserted_ref)
		)
		WHERE NOT market_mk20_download_pipeline.ref_ids @> ARRAY[(SELECT ref_id FROM inserted_ref)]`
}
