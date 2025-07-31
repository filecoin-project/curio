package pdp

import (
	"context"
	"encoding/json"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type ProofSetDelete struct {
	DeleteMessageHash string `db:"tx_hash"`
	ID                string `db:"id"`
	PID               int64  `db:"set_id"`
}

func NewWatcherDelete(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingProofSetDeletes(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending proof set creates: %v", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingProofSetDeletes(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_proof_set_delete where txHash is not NULL
	var proofSetDeletes []ProofSetDelete

	err := db.Select(ctx, &proofSetDeletes, `
        SELECT id, client, tx_hash
        FROM pdp_proof_set_delete
        WHERE tx_hash IS NOT NULL`)
	if err != nil {
		return xerrors.Errorf("failed to select proof set deletes: %w", err)
	}

	if len(proofSetDeletes) == 0 {
		// No pending proof set creates
		return nil
	}

	// Process each proof set delete
	for _, psd := range proofSetDeletes {
		err := processProofSetDelete(ctx, db, psd, ethClient)
		if err != nil {
			log.Warnf("Failed to process proof set delete for tx %s: %v", psd.DeleteMessageHash, err)
			continue
		}
	}

	return nil
}

func processProofSetDelete(ctx context.Context, db *harmonydb.DB, psd ProofSetDelete, ethClient *ethclient.Client) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	var txSuccess bool
	err := db.QueryRow(ctx, `
        SELECT tx_success, tx_receipt 
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, psd.DeleteMessageHash).Scan(&txReceiptJSON, &txSuccess)
	if err != nil {
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", psd.DeleteMessageHash, err)
	}

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", psd.DeleteMessageHash, err)
	}

	// Exit early if transaction executed with failure
	if !txSuccess {
		// This means msg failed, we should let the user know
		// TODO: Review if error would be in receipt
		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			n, err := tx.Exec(`UPDATE market_mk20_deal
									SET pdp_v1 = jsonb_set(
													jsonb_set(pdp_v1, '{error}', to_jsonb($1::text), true),
													'{complete}', to_jsonb(true), true
												 )
									WHERE id = $2;`, "Transaction failed", psd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}
			_, err = tx.Exec(`DELETE FROM pdp_proof_set_delete WHERE id = $1`, psd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete row from pdp_proof_set_delete: %w", err)
			}
			return true, nil
		})
		if err != nil {
			return xerrors.Errorf("failed to commit transaction: %w", err)
		}
		if !comm {
			return xerrors.Errorf("failed to commit transaction")
		}
		return nil
	}

	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE pdp_proof_set SET removed = TRUE, 
                         remove_deal_id = $1, 
                         remove_message_hash = $2 
                         WHERE id = $3`, psd.ID, psd.DeleteMessageHash, psd.PID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_proof_set: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
		}
		_, err = tx.Exec(`DELETE FROM pdp_proof_set_delete WHERE id = $1`, psd.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to delete row from pdp_proof_set_delete: %w", err)
		}
		n, err = tx.Exec(`UPDATE market_mk20_deal
							SET pdp_v1 = jsonb_set(pdp_v1, '{complete}', 'true'::jsonb, true)
							WHERE id = $1;`, psd.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
		}
		n, err = tx.Exec(`UPDATE pdp_proofset_root SET removed = TRUE, 
                         remove_deal_id = $1, 
                         remove_message_hash = $2 
                         WHERE id = $3`, psd.ID, psd.DeleteMessageHash, psd.PID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_proofset_root: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
		}
		return true, nil
	})

	if err != nil {
		return xerrors.Errorf("failed to commit transaction: %w", err)
	}
	if !comm {
		return xerrors.Errorf("failed to commit transaction")
	}

	return nil
}
