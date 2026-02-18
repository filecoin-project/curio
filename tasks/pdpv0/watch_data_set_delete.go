package pdp

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

func NewDataSetDeleteWatcher(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDeletes(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending data set delete: %s", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingDeletes(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	var deletes []struct {
		ID      int64        `db:"id"`
		TxHash  string       `db:"delete_tx_hash"`
		Success sql.NullBool `db:"tx_success"`
	}

	err := db.Select(ctx, &deletes, `SELECT
    										pdds.id,
    										pdds.delete_tx_hash,
    										mwe.tx_success
										FROM pdp_delete_data_set pdds
										LEFT JOIN message_waits_eth mwe ON mwe.signed_tx_hash = pdds.delete_tx_hash
										WHERE pdds.service_termination_epoch IS NOT NULL
										  AND pdds.terminated = FALSE
										  AND pdds.after_delete_data_set = TRUE
										  AND pdds.delete_tx_hash IS NOT NULL`)
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

	for _, detail := range deletes {
		if !detail.Success.Valid {
			log.Debugw("data set delete tx not yet mined", "txHash", detail.TxHash, "dataSetId", detail.ID)
			continue
		}

		if !detail.Success.Bool {
			return xerrors.Errorf("data set delete tx %s failed for data set %d", detail.TxHash, detail.ID)
		}

		live, err := verifier.DataSetLive(&bind.CallOpts{Context: ctx}, big.NewInt(detail.ID))
		if err != nil {
			return xerrors.Errorf("failed to check if data set is live: %w", err)
		}

		if live {
			return errors.New("data set is still live")
		}

		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			// Using a transaction as there are foreign key constraints and triggers.

			// Delete all piece refs for this data set
			/*
				pdp_data_sets (id)
				│   ON DELETE CASCADE
				├── pdp_data_set_pieces.data_set           -- CASCADE
				│      ├─(TRIGGER) increment/decrement_data_set_refcount()
				│      └─ pdp_data_set_pieces.pdp_pieceref → pdp_piecerefs(id)  -- ON DELETE SET NULL
				│
				├── pdp_data_set_piece_adds.data_set       -- CASCADE
				│
				└── pdp_prove_tasks.data_set               -- CASCADE

				What this means at delete time:
				1. We run:
				 "DELETE FROM curio.pdp_data_sets WHERE id = $1;"

				2. Postgres automatically:
					a. Deletes all matching rows in pdp_data_set_pieces (CASCADE).
					b. Deletes all matching rows in pdp_data_set_piece_adds (CASCADE).
					c. Deletes all matching rows in pdp_prove_tasks (CASCADE).

				3. While removing pdp_data_set_pieces rows:
					a. The row’s FK pdp_data_set_pieces.pdp_pieceref → pdp_piecerefs(id) is ON DELETE SET NULL (so we do not delete pdp_piecerefs).
					b. Triggers on pdp_data_set_pieces
						pdp_data_set_piece_insert (increments refcount)
						pdp_data_set_piece_delete (decrements refcount)
						pdp_data_set_piece_update (adjusts)
					update pdp_piecerefs.data_set_refcount accordingly, so refcounts drop when pieces are removed.

				pdp_pieceRefs will be cleaned up by watch_piece_delete.go process. It will also remove index entries and publish IPNI announcements.
			*/

			_, err = tx.Exec(`DELETE FROM pdp_data_sets WHERE id = $1`, detail.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete data set %d: %w", detail.ID, err)
			}

			_, err = tx.Exec(`DELETE FROM  pdp_delete_data_set WHERE id = $1 AND delete_tx_hash = $2`, detail.ID, detail.TxHash)
			if err != nil {
				return false, xerrors.Errorf("failed to delete row from pdp_delete_data_set: %w", err)
			}

			return true, nil
		}, harmonydb.OptionRetry())

		if err != nil {
			return xerrors.Errorf("failed to commit transaction: %w", err)
		}
		if !comm {
			return xerrors.Errorf("failed to commit transaction")
		}
	}

	return nil
}
