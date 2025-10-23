package pdp

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
)

type dataSetDelete struct {
	ID     int64  `db:"id"`
	TxHash string `db:"delete_tx_hash"`
}

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
	var deletes []dataSetDelete

	err := db.Select(ctx, &deletes, `SELECT id, delete_tx_hash FROM pdp_delete_data_set WHERE termination_epoch IS NOT NULL AND terminated = FALSE`)
	if err != nil {
		return xerrors.Errorf("failed to select pending data sets: %w", err)
	}

	for _, detail := range deletes {
		err := processDataSetDelete(ctx, db, ethClient, detail)
		if err != nil {
			return xerrors.Errorf("failed to process pending data set delete: %w", err)
		}
	}

	return nil
}

func processDataSetDelete(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, detail dataSetDelete) error {
	var success bool
	err := db.QueryRow(ctx, `
        SELECT tx_success
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
        AND tx_success IS NOT NULL
    `, detail.TxHash).Scan(&success)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", detail.TxHash, err)
	}

	if !success {
		return xerrors.Errorf("tx %s failed", detail.TxHash)
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	live, err := verifier.DataSetLive(&bind.CallOpts{Context: ctx}, big.NewInt(detail.ID))
	if err != nil {
		return xerrors.Errorf("failed to check if data set is live: %w", err)
	}

	if live {
		return errors.New("data set is still live")
	}

	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
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
		*/

		_, err = tx.Exec(`DELETE FROM pdp_data_sets WHERE id = $1`, detail.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to delete data set %d: %w", detail.ID, err)
		}

		_, err = tx.Exec(`WITH refs AS (
								  SELECT COALESCE(array_agg(piece_ref), '{}'::bigint[]) AS ref_ids
								  FROM curio.pdp_piecerefs
								  WHERE data_set_refcount = 0
								)
								DELETE FROM curio.parked_piece_refs
								WHERE ref_id = ANY ((SELECT ref_ids FROM refs));`)
		if err != nil {
			return false, xerrors.Errorf("failed to delete parked piece refs: %w", err)
		}

		_, err = tx.Exec(`DELETE FROM pdp_piecerefs WHERE data_set_refcount = 0`)
		if err != nil {
			return false, xerrors.Errorf("failed to delete pdp_piecerefs: %w", err)
		}

		n, err := tx.Exec(`UPDATE pdp_delete_data_set SET terminated = TRUE WHERE id = $1 AND delete_tx_hash = $2`, detail.ID, detail.TxHash)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
		}

		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 row but got %d", n)
		}

		// TODO: Indexing and IPNI cleanup

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
