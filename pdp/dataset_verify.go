package pdp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/tasks/openpieces"
)

var (
	// ErrDataSetNotFound indicates the data set does not exist or does not belong to the service.
	ErrDataSetNotFound = errors.New("data set not found")
	// ErrDataSetTerminated indicates the data set was terminated due to unrecoverable proving failure.
	ErrDataSetTerminated = errors.New("data set has been terminated due to unrecoverable proving failure")
)

// verifyDataSetForService checks that dataSetId exists in pdp_data_sets, belongs to service,
// and has not been terminated due to unrecoverable proving failure.
func verifyDataSetForService(ctx context.Context, db *harmonydb.DB, service string, dataSetId uint64) error {
	var dataSetService string
	var unrecoverable *int64
	err := db.QueryRow(ctx, `
		SELECT service, unrecoverable_proving_failure_epoch
		FROM pdp_data_sets
		WHERE id = $1
	`, dataSetId).Scan(&dataSetService, &unrecoverable)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDataSetNotFound
		}
		return fmt.Errorf("failed to retrieve data set: %w", err)
	}

	if dataSetService != service {
		return ErrDataSetNotFound
	}

	if unrecoverable != nil {
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

	_, err := harmonytask.TxWithTask(ctx, db, openpieces.DropAdder(), func(tx *harmonydb.Tx, dropTask harmonytask.TaskID) (bool, error) {
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
		if _, err := tx.Exec(`
			DELETE FROM hash_space_place hp
			WHERE hp.pdp_piece_cid = ANY($1)
			  AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.id = hp.pdp_pieceref)
		`, subPieceCidV1List); err != nil {
			return false, fmt.Errorf("discard orphan open-pieces placements: %w", err)
		}
		// queueOpenPieceDeletes: queue removal of CIDs that no longer have a PDP
		// ref. Hash spaces are keyed by piece CID v2, from the parked raw size.
		var gone []struct {
			PieceCID string `db:"piece_cid"`
			RawSize  int64  `db:"piece_raw_size"`
		}
		if err := tx.Select(&gone, `
			SELECT DISTINCT pp.piece_cid, pp.piece_raw_size
			FROM parked_pieces pp
			WHERE pp.piece_cid = ANY($1)
			  AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.piece_cid = pp.piece_cid)
		`, subPieceCidV1List); err != nil {
			return false, fmt.Errorf("find pieces without PDP refs: %w", err)
		}
		for _, g := range gone {
			v1, err := cid.Parse(g.PieceCID)
			if err != nil {
				return false, fmt.Errorf("parse piece cid %s: %w", g.PieceCID, err)
			}
			v2, err := commcid.PieceCidV2FromV1(v1, uint64(g.RawSize))
			if err != nil {
				return false, fmt.Errorf("piece cid v2 for %s: %w", g.PieceCID, err)
			}
			if err := openpieces.QueueDrop(tx, dropTask, v2.String()); err != nil {
				return false, err
			}
		}
		if n > 0 {
			log.Infow("discarded orphan PDP piecerefs after bad data set addPieces",
				"service", service,
				"subPieceCount", len(subPieceCidV1List),
				"parkedRefsRemoved", n)
		}
		return true, nil
	})
	return err
}
