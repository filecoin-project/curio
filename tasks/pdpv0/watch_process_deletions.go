package pdpv0

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

const deletionDrainReconcileBatchLimit = 128

// NewProcessDeletionsWatcher reconciles confirmed processPieceDeletions
// transactions.
//
// Before FilOzone/pdp#297, nextProvingPeriod applied scheduled removals, so
// local removal tracking was reconciled off a confirmed nextProvingPeriod
// message. Removals are now applied by their own transaction, so that is the
// trigger. The reconciliation itself is unchanged and remains chain-authoritative
// (scheduled-removal queue membership, then pieceLive), which is why it stays
// correct against both contract versions and is still also driven from the
// proving-period watcher.
func NewProcessDeletionsWatcher(w *Watcher) {
	if err := w.AddWatcher(func(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, al curioalerting.AlertingInterface, revert, apply *chainTypes.TipSet) {
		dataSets, err := reconcileDrainMessages(ctx, db)
		if err != nil {
			log.Warnf("Failed to reconcile PDP removal drain messages: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProcessDeletions,
				Message:   fmt.Sprintf("failed to reconcile PDP removal drain messages: %s", err),
			})
			return
		}
		if len(dataSets) == 0 {
			return
		}

		if err := processPendingPieceDeletes(ctx, db, ethClient, dataSets); err != nil {
			log.Warnf("Failed to process pending PDP piece deletes: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProcessDeletions,
				Message:   fmt.Sprintf("failed to process pending PDP piece deletes: %s", err),
			})
		}
	}, WatcherOrderProcessDeletions); err != nil {
		panic(err)
	}
}

type drainMessage struct {
	DataSetID int64          `db:"data_set"`
	TxHash    string         `db:"msg_hash"`
	TxStatus  sql.NullString `db:"tx_status"`
	TxSuccess sql.NullBool   `db:"tx_success"`
}

// reconcileDrainMessages clears in-flight drain messages that have reached a
// final state, and returns the data sets whose local removal tracking should be
// re-examined.
//
// Clearing msg_hash is what lets the drain watcher pick the data set up again:
// one drain transaction handles at most one batch, so a deep queue needs several
// passes.
func reconcileDrainMessages(ctx context.Context, db *harmonydb.DB) ([]int64, error) {
	var messages []drainMessage
	err := db.Select(ctx, &messages, `
		SELECT d.data_set,
		       d.msg_hash,
		       mwe.tx_status,
		       mwe.tx_success
		FROM pdpv0_deletion_drain d
		LEFT JOIN message_waits_eth mwe ON mwe.signed_tx_hash = d.msg_hash
		WHERE d.msg_hash IS NOT NULL
		ORDER BY d.data_set
		LIMIT $1
	`, deletionDrainReconcileBatchLimit)
	if err != nil {
		return nil, xerrors.Errorf("failed to select in-flight removal drains: %w", err)
	}

	var settled []int64
	for _, msg := range messages {
		if !msg.TxStatus.Valid || msg.TxStatus.String != "confirmed" {
			continue
		}

		success := msg.TxSuccess.Valid && msg.TxSuccess.Bool
		if !success {
			// The drain did not apply. Clear the message so the data set is
			// retried rather than stalling behind a dead transaction, and bound
			// the retries.
			log.Errorw("PDP removal drain transaction failed", "dataSetId", msg.DataSetID, "txHash", msg.TxHash)
			if _, err := db.Exec(ctx, `
				UPDATE pdpv0_deletion_drain
				SET msg_hash = NULL, failures = failures + 1
				WHERE data_set = $1 AND msg_hash = $2
			`, msg.DataSetID, msg.TxHash); err != nil {
				return nil, xerrors.Errorf("failed to clear failed removal drain %s: %w", msg.TxHash, err)
			}
			continue
		}

		if _, err := db.Exec(ctx, `
			UPDATE pdpv0_deletion_drain
			SET msg_hash = NULL, blocked_at = NULL
			WHERE data_set = $1 AND msg_hash = $2
		`, msg.DataSetID, msg.TxHash); err != nil {
			return nil, xerrors.Errorf("failed to clear confirmed removal drain %s: %w", msg.TxHash, err)
		}

		log.Infow("PDP removal drain confirmed", "dataSetId", msg.DataSetID, "txHash", msg.TxHash)
		settled = append(settled, msg.DataSetID)
	}

	return settled, nil
}

type pendingPieceDelete struct {
	DataSetID int64          `db:"data_set"`
	PieceID   int64          `db:"piece_id"`
	TxHash    string         `db:"rm_message_hash"`
	TxStatus  sql.NullString `db:"tx_status"`
	TxSuccess sql.NullBool   `db:"tx_success"`
}

// processPendingPieceDeletes reconciles local piece-removal rows against
// PDPVerifier. A confirmed schedulePieceDeletions transaction only records
// delete intent; the piece must not be marked removed locally while PDPVerifier
// still reports it as scheduled or live, because piece GC keys off that flag and
// deletes the underlying data.
func processPendingPieceDeletes(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, dataSets []int64) error {
	var pendingDeletes []pendingPieceDelete
	err := db.Select(ctx, &pendingDeletes, `
		SELECT psp.data_set,
		       psp.piece_id,
		       psp.rm_message_hash,
		       mwe.tx_status,
		       mwe.tx_success
		FROM pdp_data_set_pieces psp
		LEFT JOIN message_waits_eth mwe ON mwe.signed_tx_hash = psp.rm_message_hash
		WHERE psp.data_set = ANY($1::bigint[])
		  AND psp.rm_message_hash IS NOT NULL
		  AND psp.removed = FALSE
		ORDER BY psp.data_set, psp.piece_id
	`, dataSets)
	if err != nil {
		return xerrors.Errorf("failed to select pending piece deletes: %w", err)
	}
	if len(pendingDeletes) == 0 {
		return nil
	}

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	scheduledByDataSet := map[int64]map[int64]struct{}{}
	for _, piece := range pendingDeletes {
		// Wait until the schedulePieceDeletions send has a final watcher result.
		if !piece.TxStatus.Valid || piece.TxStatus.String != "confirmed" {
			continue
		}
		// A confirmed row without tx_success is malformed for our purposes; clear
		// the local delete intent so operators can resubmit cleanly.
		if !piece.TxSuccess.Valid {
			log.Errorf("invalid message_waits_eth state for piece delete tx %s", piece.TxHash)
			if err := clearPendingPieceDelete(ctx, db, piece); err != nil {
				return err
			}
			continue
		}
		// The schedule transaction failed, so no on-chain removal is pending.
		if !piece.TxSuccess.Bool {
			log.Errorf("failed to process pending piece delete as transaction %s failed", piece.TxHash)
			if err := clearPendingPieceDelete(ctx, db, piece); err != nil {
				return err
			}
			continue
		}

		scheduled, ok := scheduledByDataSet[piece.DataSetID]
		if !ok {
			scheduled, err = getScheduledRemovalSet(ctx, verifier, piece.DataSetID)
			if err != nil {
				return err
			}
			scheduledByDataSet[piece.DataSetID] = scheduled
		}
		// Still scheduled means the removal has not been processed yet. Keep
		// rm_message_hash set and leave removed=false.
		if _, ok := scheduled[piece.PieceID]; ok {
			continue
		}

		// Once it is no longer scheduled, PieceLive is the final authority for
		// whether the removal actually applied.
		pieceID := big.NewInt(piece.PieceID)
		live, err := verifier.PieceLive(contract.EthCallOpts(ctx), big.NewInt(piece.DataSetID), pieceID)
		if err != nil {
			return xerrors.Errorf("failed to check if piece is live: %w", err)
		}
		if !live {
			if err := markPendingPieceRemoved(ctx, db, piece); err != nil {
				return err
			}
			log.Infow("piece removed on-chain, marking as removed in DB", "dataSetId", piece.DataSetID, "pieceID", piece.PieceID, "txHash", piece.TxHash)
			continue
		}

		log.Warnw("piece is live and not scheduled despite successful delete tx; clearing stale delete tracking",
			"dataSetId", piece.DataSetID, "pieceID", piece.PieceID, "txHash", piece.TxHash)
		if err := clearPendingPieceDelete(ctx, db, piece); err != nil {
			return err
		}
	}

	return nil
}

func getScheduledRemovalSet(ctx context.Context, verifier *contract.PDPVerifier, dataSetID int64) (map[int64]struct{}, error) {
	removals, err := verifier.GetScheduledRemovals(contract.EthCallOpts(ctx), big.NewInt(dataSetID))
	if err != nil {
		return nil, xerrors.Errorf("failed to get scheduled removals: %w", err)
	}

	out := make(map[int64]struct{}, len(removals))
	for _, removal := range removals {
		if removal.IsInt64() {
			out[removal.Int64()] = struct{}{}
		}
	}
	return out, nil
}

func clearPendingPieceDelete(ctx context.Context, db *harmonydb.DB, piece pendingPieceDelete) error {
	_, err := db.Exec(ctx, `
		UPDATE pdp_data_set_pieces
		SET rm_message_hash = NULL
		WHERE data_set = $1
		  AND piece_id = $2
		  AND rm_message_hash = $3
		  AND removed = FALSE
	`, piece.DataSetID, piece.PieceID, piece.TxHash)
	if err != nil {
		return xerrors.Errorf("failed to clear pending piece delete %s: %w", piece.TxHash, err)
	}
	return nil
}

func markPendingPieceRemoved(ctx context.Context, db *harmonydb.DB, piece pendingPieceDelete) error {
	affected, err := db.Exec(ctx, `
		UPDATE pdp_data_set_pieces
		SET removed = TRUE
		WHERE data_set = $1
		  AND piece_id = $2
		  AND rm_message_hash = $3
		  AND removed = FALSE
	`, piece.DataSetID, piece.PieceID, piece.TxHash)
	if err != nil {
		return xerrors.Errorf("failed to mark piece removed: %w", err)
	}
	if affected > 1 {
		return xerrors.Errorf("expected to update at most 1 piece delete row, updated %d", affected)
	}
	return nil
}
