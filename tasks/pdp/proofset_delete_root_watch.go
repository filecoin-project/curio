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

type ProofSetRootDelete struct {
	ID       string `db:"id"`
	ProofSet uint64 `db:"set_id"`
	Roots    int64  `db:"roots"`
	Hash     string `db:"tx_hash"`
}

func NewWatcherRootDelete(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingProofSetRootDeletes(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending proof set creates: %v", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingProofSetRootDeletes(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	var proofSetRootDeletes []ProofSetRootDelete
	err := db.Select(ctx, &proofSetRootDeletes, `
        SELECT id, tx_hash, roots, set_id
        FROM pdp_delete_root
        WHERE tx_hash IS NOT NULL`)
	if err != nil {
		return xerrors.Errorf("failed to select proof set deletes: %w", err)
	}

	if len(proofSetRootDeletes) == 0 {
		return nil
	}

	for _, psd := range proofSetRootDeletes {
		err := processProofSetRootDelete(ctx, db, psd, ethClient)
		if err != nil {
			log.Warnf("Failed to process proof set root delete for tx %s: %v", psd.Hash, err)
			continue
		}
	}

	return nil
}

func processProofSetRootDelete(ctx context.Context, db *harmonydb.DB, psd ProofSetRootDelete, ethClient *ethclient.Client) error {
	var txReceiptJSON []byte
	var txSuccess bool
	err := db.QueryRow(ctx, `
        SELECT tx_success, tx_receipt 
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, psd.Hash).Scan(&txReceiptJSON, &txSuccess)
	if err != nil {
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
			_, err = tx.Exec(`DELETE FROM pdp_delete_root WHERE id = $1`, psd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete row from pdp_delete_root: %w", err)
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
                         WHERE id = $3 AND root = ANY($4)`, psd.ID, psd.Hash, psd.ProofSet, psd.Roots)
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
