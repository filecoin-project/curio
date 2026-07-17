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

const alertNameProvingPeriod = "ProvingPeriod"

// NewProvingPeriodWatcher reconciles confirmed proving-period side effects
// before the prove watcher runs. nextProvingPeriod is what finally applies
// scheduled piece removals and can also clear PDPVerifier's next challenge when
// the dataset becomes empty, so both local states must be fixed before prove
// task scheduling looks at the dataset.
func NewProvingPeriodWatcher(w *Watcher) {
	if err := w.AddWatcher(func(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, al curioalerting.AlertingInterface, revert, apply *chainTypes.TipSet) {
		if err := processEmptyProvingPeriods(ctx, db, ethClient); err != nil {
			log.Warnf("Failed to process empty PDP proving periods: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProvingPeriod,
				Message:   fmt.Sprintf("failed to process empty PDP proving periods: %s", err),
			})
		}

		if err := processPendingPieceDeletes(ctx, db, ethClient); err != nil {
			log.Warnf("Failed to process pending PDP piece deletes: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProvingPeriod,
				Message:   fmt.Sprintf("failed to process pending PDP piece deletes: %s", err),
			})
		}
	}, WatcherOrderCleanupPieces); err != nil {
		panic(err)
	}
}

type confirmedProvingPeriod struct {
	DataSetID int64  `db:"id"`
	TxHash    string `db:"challenge_request_msg_hash"`
}

// processEmptyProvingPeriods reconciles datasets whose confirmed initPP/nextPP
// message left no next challenge on-chain. This happens when nextProvingPeriod
// removes the final piece: PDPVerifier clears the challenge, but Curio may have
// already stored the now-stale prove_at_epoch.
//
// Dropping that local period before piece-delete reconciliation is intentional.
// There is no proof to submit for a zero on-chain challenge epoch. Clearing the
// stale schedule first prevents the prove watcher from disabling proving after a
// new add already made the dataset ready again. The current on-chain leaf count
// then decides whether InitProvingPeriodTask should pick the dataset back up.
func processEmptyProvingPeriods(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient) error {
	var confirmed []confirmedProvingPeriod
	err := db.Select(ctx, &confirmed, `
		SELECT pds.id,
		       pds.challenge_request_msg_hash
		FROM pdp_data_sets pds
		INNER JOIN message_waits_eth mwe ON mwe.signed_tx_hash = pds.challenge_request_msg_hash
		WHERE pds.challenge_request_msg_hash IS NOT NULL
		  AND mwe.tx_status = 'confirmed'
		  AND mwe.tx_success = TRUE
		  AND pds.unrecoverable_proving_failure_epoch IS NULL
		ORDER BY pds.id
	`)
	if err != nil {
		return xerrors.Errorf("failed to select confirmed proving periods: %w", err)
	}
	if len(confirmed) == 0 {
		return nil
	}

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, period := range confirmed {
		dataSetID := big.NewInt(period.DataSetID)

		nextChallengeEpoch, err := verifier.GetNextChallengeEpoch(contract.EthCallOpts(ctx), dataSetID)
		if err != nil {
			return xerrors.Errorf("failed to get next challenge epoch for data set %d: %w", period.DataSetID, err)
		}
		if nextChallengeEpoch.Sign() != 0 {
			continue
		}

		leafCount, err := verifier.GetDataSetLeafCount(contract.EthCallOpts(ctx), dataSetID)
		if err != nil {
			return xerrors.Errorf("failed to get leaf count for data set %d: %w", period.DataSetID, err)
		}
		initReady := leafCount.Sign() > 0

		affected, err := db.Exec(ctx, `
			UPDATE pdp_data_sets
			SET challenge_request_msg_hash = NULL,
			    prove_at_epoch = NULL,
			    prev_challenge_request_epoch = NULL,
			    init_ready = $3
			WHERE id = $1
			  AND challenge_request_msg_hash = $2
			  AND unrecoverable_proving_failure_epoch IS NULL
		`, period.DataSetID, period.TxHash, initReady)
		if err != nil {
			return xerrors.Errorf("failed to reset empty proving period for data set %d: %w", period.DataSetID, err)
		}
		if affected > 1 {
			return xerrors.Errorf("expected to update at most 1 proving period row, updated %d", affected)
		}
		if affected == 1 {
			log.Infow("reset empty proving period",
				"dataSetId", period.DataSetID,
				"txHash", period.TxHash,
				"leafCount", leafCount.String(),
				"initReady", initReady)
		}
	}

	return nil
}

type pendingPieceDelete struct {
	DataSetID int64          `db:"data_set"`
	PieceID   int64          `db:"piece_id"`
	TxHash    string         `db:"rm_message_hash"`
	TxStatus  sql.NullString `db:"tx_status"`
	TxSuccess sql.NullBool   `db:"tx_success"`
}

// processPendingPieceDeletes reconciles local piece-removal rows after the
// schedulePieceDeletions transaction is confirmed. That transaction only queues
// removals on-chain; the piece is actually removed later by nextProvingPeriod,
// so this function must not mark a local row removed while PDPVerifier still
// reports the piece in GetScheduledRemovals.
func processPendingPieceDeletes(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient) error {
	var pendingDeletes []pendingPieceDelete
	err := db.Select(ctx, &pendingDeletes, `
		SELECT psp.data_set,
		       psp.piece_id,
		       psp.rm_message_hash,
		       mwe.tx_status,
		       mwe.tx_success
		FROM pdp_data_set_pieces psp
		LEFT JOIN message_waits_eth mwe ON mwe.signed_tx_hash = psp.rm_message_hash
		WHERE psp.rm_message_hash IS NOT NULL
		  AND psp.removed = FALSE
		ORDER BY psp.data_set, psp.piece_id
	`)
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
		// Still scheduled means nextProvingPeriod has not processed the removal
		// yet. Keep rm_message_hash set and leave removed=false.
		if _, ok := scheduled[piece.PieceID]; ok {
			continue
		}

		// Once it is no longer scheduled, PieceLive is the final authority for
		// whether nextProvingPeriod actually removed it.
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
