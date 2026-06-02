package pdpv0

import (
	"context"
	"database/sql"
	"errors"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

func NewCleanupPiecesWatcher(db *harmonydb.DB, ethClient ethchain.EthClient, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingCleanupPieces(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending PDP piece cleanup: %s", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

type pendingCleanupPieces struct {
	ID     int64  `db:"id"`
	TxHash string `db:"cleanup_pieces_tx_hash"`
}

type cleanupPiecesMessageWait struct {
	TxHash  string       `db:"signed_tx_hash"`
	Status  string       `db:"tx_status"`
	Success sql.NullBool `db:"tx_success"`
}

func processPendingCleanupPieces(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient) error {
	var pending []pendingCleanupPieces
	err := db.Select(ctx, &pending, `
		SELECT id, cleanup_pieces_tx_hash
		FROM pdp_delete_data_set
		WHERE service_termination_epoch IS NOT NULL
		  AND terminated = FALSE
		  AND after_delete_data_set = TRUE
		  AND cleanup_pieces_tx_hash IS NOT NULL
	`)
	if err != nil {
		return xerrors.Errorf("failed to select pending PDP cleanup txs: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	byHash := make(map[string]pendingCleanupPieces, len(pending))
	hashes := make([]string, 0, len(pending))
	for _, detail := range pending {
		hashes = append(hashes, detail.TxHash)
		byHash[detail.TxHash] = detail
	}

	var waits []cleanupPiecesMessageWait
	err = db.Select(ctx, &waits, `
		SELECT signed_tx_hash, tx_status, tx_success
		FROM message_waits_eth
		WHERE signed_tx_hash = ANY($1)
	`, hashes)
	if err != nil {
		return xerrors.Errorf("failed to select cleanupPieces message waits: %w", err)
	}

	seen := map[string]struct{}{}
	var successes []pendingCleanupPieces
	var failures []pendingCleanupPieces
	for _, wait := range waits {
		seen[wait.TxHash] = struct{}{}
		detail, ok := byHash[wait.TxHash]
		if !ok {
			continue
		}

		if wait.Status == "confirmed" && wait.Success.Valid && wait.Success.Bool {
			successes = append(successes, detail)
			continue
		}

		if wait.Status == "failed" || (wait.Status == "confirmed" && wait.Success.Valid && !wait.Success.Bool) {
			failures = append(failures, detail)
		}
	}

	for _, detail := range pending {
		if _, ok := seen[detail.TxHash]; ok {
			continue
		}
		log.Warnw("cleanupPieces tx missing message_waits_eth row", "txHash", detail.TxHash, "dataSetId", detail.ID)
	}

	successErr := processSuccessfulCleanupPieces(ctx, db, ethClient, successes)
	failureErr := processFailedCleanupPieces(ctx, db, ethClient, failures)
	if successErr != nil || failureErr != nil {
		return xerrors.Errorf("failed to process PDP cleanup txs: %w", errors.Join(successErr, failureErr))
	}
	return nil
}

func processSuccessfulCleanupPieces(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, successes []pendingCleanupPieces) error {
	for _, detail := range successes {
		state, err := readDataSetCleanupState(ctx, ethClient, detail.ID)
		if err != nil {
			return xerrors.Errorf("failed to read PDP cleanup state for data set %d: %w", detail.ID, err)
		}

		if state.Finalized() {
			if err := cleanupFinalizedDataSet(ctx, db, detail.ID); err != nil {
				return err
			}
			continue
		}

		if state.CleanupMode {
			if err := clearCleanupTxHash(ctx, db, detail.ID, detail.TxHash); err != nil {
				return err
			}
			log.Infow("PDP cleanupPieces batch confirmed",
				"dataSetId", detail.ID,
				"txHash", detail.TxHash,
				"remainingPieceSlots", state.NextPieceID)
			continue
		}

		if state.Live {
			if err := resetCleanupTxToDelete(ctx, db, detail.ID, detail.TxHash); err != nil {
				return err
			}
			log.Warnw("cleanupPieces tx confirmed but data set is live; reset to deleteDataSet stage",
				"dataSetId", detail.ID,
				"txHash", detail.TxHash)
			continue
		}

		return xerrors.Errorf("data set %d is not live, in cleanup mode, or finalized", detail.ID)
	}
	return nil
}

func processFailedCleanupPieces(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, failures []pendingCleanupPieces) error {
	for _, detail := range failures {
		state, err := readDataSetCleanupState(ctx, ethClient, detail.ID)
		if err == nil {
			if state.Finalized() {
				if err := cleanupFinalizedDataSet(ctx, db, detail.ID); err != nil {
					return err
				}
				continue
			}
			if state.Live {
				if err := resetCleanupTxToDelete(ctx, db, detail.ID, detail.TxHash); err != nil {
					return err
				}
				log.Warnw("cleanupPieces tx failed and data set is live; reset to deleteDataSet stage",
					"dataSetId", detail.ID,
					"txHash", detail.TxHash)
				continue
			}
		} else {
			log.Warnw("failed to read PDP cleanup state after failed cleanupPieces tx",
				"dataSetId", detail.ID,
				"txHash", detail.TxHash,
				"error", err)
		}

		if err := clearCleanupTxHash(ctx, db, detail.ID, detail.TxHash); err != nil {
			return err
		}
		log.Warnw("reset failed cleanupPieces tx for retry", "dataSetId", detail.ID, "txHash", detail.TxHash)
	}
	return nil
}

func clearCleanupTxHash(ctx context.Context, db *harmonydb.DB, dataSetID int64, txHash string) error {
	n, err := db.Exec(ctx, `
		UPDATE pdp_delete_data_set
		SET cleanup_pieces_tx_hash = NULL,
		    cleanup_pieces_task_id = NULL
		WHERE id = $1
		  AND cleanup_pieces_tx_hash = $2
		  AND after_delete_data_set = TRUE
		  AND service_termination_epoch IS NOT NULL
		  AND terminated = FALSE
	`, dataSetID, txHash)
	if err != nil {
		return xerrors.Errorf("failed to clear cleanupPieces tx hash for data set %d: %w", dataSetID, err)
	}
	if n > 1 {
		return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", dataSetID, n)
	}
	return nil
}

func resetCleanupTxToDelete(ctx context.Context, db *harmonydb.DB, dataSetID int64, txHash string) error {
	n, err := db.Exec(ctx, `
		UPDATE pdp_delete_data_set
		SET cleanup_pieces_tx_hash = NULL,
		    cleanup_pieces_task_id = NULL,
		    after_delete_data_set = FALSE,
		    delete_tx_hash = NULL,
		    delete_data_set_task_id = NULL
		WHERE id = $1
		  AND cleanup_pieces_tx_hash = $2
		  AND service_termination_epoch IS NOT NULL
		  AND terminated = FALSE
	`, dataSetID, txHash)
	if err != nil {
		return xerrors.Errorf("failed to reset cleanupPieces tx to delete stage for data set %d: %w", dataSetID, err)
	}
	if n > 1 {
		return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", dataSetID, n)
	}
	return nil
}
