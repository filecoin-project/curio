package pdp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type ProofSetRootDelete struct {
	ID       string  `db:"id"`
	ProofSet uint64  `db:"set_id"`
	Roots    []int64 `db:"roots"`
	Hash     string  `db:"tx_hash"`
}

func NewWatcherRootDelete(db *harmonydb.DB, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingProofSetRootDeletes(ctx, db)
		if err != nil {
			log.Errorf("Failed to process pending proof set creates: %s", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingProofSetRootDeletes(ctx context.Context, db *harmonydb.DB) error {
	var proofSetRootDeletes []ProofSetRootDelete
	err := db.Select(ctx, &proofSetRootDeletes, `
        SELECT id, tx_hash, roots, set_id FROM pdp_root_delete WHERE tx_hash IS NOT NULL`)
	if err != nil {
		return xerrors.Errorf("failed to select proof set root deletes: %w", err)
	}

	if len(proofSetRootDeletes) == 0 {
		return nil
	}

	for _, psd := range proofSetRootDeletes {
		err := processProofSetRootDelete(ctx, db, psd)
		if err != nil {
			log.Errorf("Failed to process proof set root delete for tx %s: %s", psd.Hash, err)
			continue
		}
	}

	return nil
}

func processProofSetRootDelete(ctx context.Context, db *harmonydb.DB, psd ProofSetRootDelete) error {
	var txReceiptJSON []byte
	var txSuccess bool
	err := db.QueryRow(ctx, `SELECT tx_receipt, tx_success FROM message_waits_eth WHERE signed_tx_hash = $1 
                                                       AND tx_success IS NOT NULL 
                                                       AND tx_receipt IS NOT NULL`, psd.Hash).Scan(&txReceiptJSON, &txSuccess)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return xerrors.Errorf("tx hash %s is either missing from watch table or is not yet processed by watcher", psd.Hash)
		}
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", psd.Hash, err)
	}

	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", psd.Hash, err)
	}

	if !txSuccess {
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
			_, err = tx.Exec(`DELETE FROM pdp_root_delete WHERE id = $1`, psd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete row from pdp_root_delete: %w", err)
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
		n, err := tx.Exec(`UPDATE pdp_proofset_root SET removed = TRUE, 
                         remove_deal_id = $1, 
                         remove_message_hash = $2 
                         WHERE proof_set_id = $3 AND root = ANY($4)`, psd.ID, psd.Hash, psd.ProofSet, psd.Roots)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_proofset_root: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
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
		_, err = tx.Exec(`DELETE FROM pdp_root_delete WHERE id = $1`, psd.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to delete row from pdp_root_delete: %w", err)
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
