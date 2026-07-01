package pdp

import (
	"context"
	"errors"
	"fmt"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var (
	// ErrDataSetNotFound indicates the data set does not exist or does not belong to the service.
	ErrDataSetNotFound = errors.New("data set not found")
	// ErrDataSetTerminated indicates the data set was terminated due to unrecoverable proving failure.
	ErrDataSetTerminated = errors.New("data set has been terminated due to unrecoverable proving failure")
)

// verifyDataSetForService checks that dataSetId exists in pdp_data_sets, belongs to service,
// and has not been terminated due to unrecoverable proving failure or client/FWSS termination.
func verifyDataSetForService(ctx context.Context, db *harmonydb.DB, service string, dataSetId uint64) error {
	var dataSetService string
	var unrecoverable *int64
	var terminatedAt *int64
	err := db.QueryRow(ctx, `
		SELECT service, unrecoverable_proving_failure_epoch, terminated_at_epoch
		FROM pdp_data_sets
		WHERE id = $1
	`, dataSetId).Scan(&dataSetService, &unrecoverable, &terminatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDataSetNotFound
		}
		return fmt.Errorf("failed to retrieve data set: %w", err)
	}

	if dataSetService != service {
		return ErrDataSetNotFound
	}

	if unrecoverable != nil || terminatedAt != nil {
		return ErrDataSetTerminated
	}

	return nil
}

// discardOrphanPiecrefsForSubPieces removes unreferenced pdp_piecerefs for the
// given subPiece CIDs. Called when addPieces is rejected for a missing or
// terminated data set, so notify-created piecerefs do not linger.
func discardOrphanPiecrefsForSubPieces(ctx context.Context, db *harmonydb.DB, service string, subPieceCidV1List []string) error {
	if len(subPieceCidV1List) == 0 {
		return nil
	}

	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			WITH doomed AS (
				SELECT pr.id, pr.piece_ref
				FROM pdp_piecerefs pr
				WHERE pr.service = $1
				  AND pr.piece_cid = ANY($2)
				  AND pr.data_set_refcount = 0
				  AND NOT EXISTS (
					SELECT 1 FROM pdp_data_set_piece_adds a
					WHERE a.pdp_pieceref = pr.id
					  AND a.pieces_added = FALSE
					  AND (a.add_message_ok IS NULL OR a.add_message_ok = TRUE)
				  )
			),
			deleted AS (
				DELETE FROM pdp_piecerefs pr
				USING doomed d
				WHERE pr.id = d.id
				RETURNING d.piece_ref AS piece_ref
			)
			DELETE FROM parked_piece_refs ppr
			USING deleted d
			WHERE ppr.ref_id = d.piece_ref
			  AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.piece_ref = ppr.ref_id)
		`, service, subPieceCidV1List)
		if err != nil {
			return false, fmt.Errorf("discard orphan piecerefs: %w", err)
		}
		if n > 0 {
			log.Infow("discarded orphan PDP piecerefs after bad data set addPieces",
				"service", service,
				"subPieceCount", len(subPieceCidV1List),
				"parkedRefsRemoved", n)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	return err
}
