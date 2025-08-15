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

type DataSetPieceDelete struct {
	ID      string  `db:"id"`
	DataSet uint64  `db:"set_id"`
	Pieces  []int64 `db:"pieces"`
	Hash    string  `db:"tx_hash"`
}

func NewWatcherPieceDelete(db *harmonydb.DB, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetPieceDeletes(ctx, db)
		if err != nil {
			log.Errorf("Failed to process pending data set creates: %s", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingDataSetPieceDeletes(ctx context.Context, db *harmonydb.DB) error {
	var dataSetPieceDeletes []DataSetPieceDelete
	err := db.Select(ctx, &dataSetPieceDeletes, `
        SELECT id, tx_hash, pieces, set_id FROM pdp_piece_delete WHERE tx_hash IS NOT NULL`)
	if err != nil {
		return xerrors.Errorf("failed to select data set piece deletes: %w", err)
	}

	if len(dataSetPieceDeletes) == 0 {
		return nil
	}

	for _, psd := range dataSetPieceDeletes {
		err := processDataSetPieceDelete(ctx, db, psd)
		if err != nil {
			log.Errorf("Failed to process data set piece delete for tx %s: %s", psd.Hash, err)
			continue
		}
	}

	return nil
}

func processDataSetPieceDelete(ctx context.Context, db *harmonydb.DB, psd DataSetPieceDelete) error {
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
			_, err = tx.Exec(`DELETE FROM pdp_piece_delete WHERE id = $1`, psd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete row from pdp_piece_delete: %w", err)
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
		n, err := tx.Exec(`UPDATE pdp_dataset_piece SET removed = TRUE, 
                         remove_deal_id = $1, 
                         remove_message_hash = $2 
                         WHERE data_set_id = $3 AND piece = ANY($4)`, psd.ID, psd.Hash, psd.DataSet, psd.Pieces)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_dataset_piece: %w", err)
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
		_, err = tx.Exec(`DELETE FROM pdp_piece_delete WHERE id = $1`, psd.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to delete row from pdp_piece_delete: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return xerrors.Errorf("failed to commit transaction: %w", err)
	}
	if !comm {
		return xerrors.Errorf("failed to commit transaction")
	}
	return nil
}
