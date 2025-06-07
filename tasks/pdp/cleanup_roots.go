package pdp

import (
	"context"
	"database/sql"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

// CleanupScheduledRemovals processes any scheduled root removals for a proofset
// This should be called before nextProvingPeriod to ensure deletions aren't lost
func CleanupScheduledRemovals(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, proofSetID int64) error {
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	removals, err := pdpVerifier.GetScheduledRemovals(nil, big.NewInt(proofSetID))
	if err != nil {
		return xerrors.Errorf("failed to get scheduled removals: %w", err)
	}

	if len(removals) == 0 {
		// No removals to process
		return nil
	}

	// Execute cleanup in a transaction
	ok, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		for _, removeID := range removals {
			log.Debugw("cleanupScheduledRemovals", "proofSetID", proofSetID, "removeID", removeID)
			
			// Get the pdp_pieceref ID for the root before deleting
			var pdpPieceRefID int64
			err := tx.QueryRow(`
				SELECT pdp_pieceref 
				FROM pdp_proofset_roots 
				WHERE proofset = $1 AND root_id = $2
			`, proofSetID, removeID.Int64()).Scan(&pdpPieceRefID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// Root already deleted, skip
					continue
				}
				return false, xerrors.Errorf("failed to get piece ref for root %d: %w", removeID, err)
			}

			// Delete the parked piece ref, this will cascade to the pdp piece ref too
			_, err = tx.Exec(`
				DELETE FROM parked_piece_refs
				WHERE ref_id = $1
			`, pdpPieceRefID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete parked piece ref %d: %w", pdpPieceRefID, err)
			}

			// Delete the root entry
			_, err = tx.Exec(`
				DELETE FROM pdp_proofset_roots 
				WHERE proofset = $1 AND root_id = $2
			`, proofSetID, removeID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete root %d: %w", removeID, err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return xerrors.Errorf("failed to cleanup deleted roots: %w", err)
	}
	if !ok {
		return xerrors.Errorf("database delete not committed")
	}

	log.Infow("cleaned up scheduled removals", "proofSetID", proofSetID, "count", len(removals))
	return nil
}