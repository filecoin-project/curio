package pdpv1

import (
	"context"
	"database/sql"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type DataSetDelete struct {
	DeleteMessageHash string `db:"tx_hash"`
	ID                string `db:"id"`
	PID               int64  `db:"set_id"`
}

func NewPDPv1DataSetDeleteWatcher(db *harmonydb.DB, pcs *chainsched.CurioChainSched, ethClient *ethclient.Client) {
	if err := pcs.AddHandler(chainsched.HandlerEntry{Fn: func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetDeletes(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process PDPv1 pending data set deletes: %s", err)
		}
		return nil
	}, Priority: chainsched.PriorityNormal}); err != nil {
		panic(err)
	}
}

func processPendingDataSetDeletes(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	var deletes []struct {
		ID      string       `db:"id"`
		PID     int64        `db:"set_id"`
		TxHash  string       `db:"delete_tx_hash"`
		Success sql.NullBool `db:"tx_success"`
	}

	err := db.Select(ctx, &deletes, `SELECT
    										pdds.id,
    										pdds.set_id,
    										pdds.tx_hash,
    										mwe.tx_success
										FROM pdp_data_set_delete pdds
										LEFT JOIN message_waits_eth mwe ON mwe.signed_tx_hash = pdds.tx_hash
										WHERE pdds.service_termination_epoch IS NOT NULL
										  AND pdds.terminated = FALSE
										  AND pdds.after_delete_data_set = TRUE
										  AND pdds.tx_hash IS NOT NULL`)
	if err != nil {
		return xerrors.Errorf("failed to select pending data sets: %w", err)
	}

	if len(deletes) == 0 {
		return nil
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, psd := range deletes {
		if !psd.Success.Valid {
			log.Debugw("data set delete tx not yet mined", "txHash", psd.TxHash, "dataSetId", psd.PID)
			continue
		}
		// Exit early if transaction executed with failure
		if !psd.Success.Bool {
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
				_, err = tx.Exec(`DELETE FROM pdp_data_set_delete WHERE id = $1`, psd.ID)
				if err != nil {
					return false, xerrors.Errorf("failed to delete row from pdp_data_set_delete: %w", err)
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

		live, err := verifier.DataSetLive(&bind.CallOpts{Context: ctx}, big.NewInt(psd.PID))
		if err != nil {
			return xerrors.Errorf("failed to check if data set is live: %w", err)
		}

		if live {
			return errors.New("data set is still live")
		}

		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			_, err = tx.Exec(`DELETE FROM pdp_data_set_delete WHERE id = $1`, psd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete row from pdp_data_set_delete: %w", err)
			}

			n, err := tx.Exec(`UPDATE market_mk20_deal
							SET pdp_v1 = jsonb_set(pdp_v1, '{complete}', 'true'::jsonb, true)
							WHERE id = $1;`, psd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}

			// Start piece cleanup tasks
			_, err = tx.Exec(`INSERT INTO piece_cleanup (id, piece_cid_v2, pdp, sp_id, sector_number, piece_ref)
								SELECT p.add_deal_id, p.piece_cid_v2, TRUE, -1, -1, p.piece_ref
								FROM pdp_dataset_piece AS p
								WHERE p.data_set_id = $1
									AND p.removed = FALSE
								ON CONFLICT (id, pdp) DO NOTHING;`, psd.PID)
			if err != nil {
				return false, xerrors.Errorf("failed to insert into piece_cleanup: %w", err)
			}

			n, err = tx.Exec(`UPDATE pdp_dataset_piece SET removed = TRUE, 
                         remove_deal_id = $1, 
                         remove_message_hash = $2 
                         WHERE data_set_id = $3`, psd.ID, psd.TxHash, psd.PID)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_dataset_piece: %w", err)
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
	}

	return nil
}
