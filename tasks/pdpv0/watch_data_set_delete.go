package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"math/big"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

func NewDataSetDeleteWatcher(db *harmonydb.DB, ethClient ethchain.EthClient, pcs *chainsched.CurioChainSched) {
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

type pendingDataSetDelete struct {
	ID     int64  `db:"id"`
	TxHash string `db:"delete_tx_hash"`
}

type dataSetDeleteMessageWait struct {
	TxHash  string       `db:"signed_tx_hash"`
	Status  string       `db:"tx_status"`
	Success sql.NullBool `db:"tx_success"`
}

func processPendingDeletes(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient) error {
	var pending []pendingDataSetDelete
	err := db.Select(ctx, &pending, `
		SELECT id, delete_tx_hash
		FROM pdp_delete_data_set
		WHERE service_termination_epoch IS NOT NULL
		  AND terminated = FALSE
		  AND after_delete_data_set = TRUE
		  AND delete_tx_hash IS NOT NULL
	`)
	if err != nil {
		return xerrors.Errorf("failed to select pending data sets: %w", err)
	}

	if len(pending) == 0 {
		return nil
	}

	byHash := make(map[string]pendingDataSetDelete, len(pending))
	hashes := make([]string, 0, len(pending))
	for _, detail := range pending {
		hashes = append(hashes, detail.TxHash)
		byHash[detail.TxHash] = detail
	}

	var waits []dataSetDeleteMessageWait
	err = db.Select(ctx, &waits, `
		SELECT signed_tx_hash, tx_status, tx_success
		FROM message_waits_eth
		WHERE signed_tx_hash = ANY($1)
	`, hashes)
	if err != nil {
		return xerrors.Errorf("failed to select data set delete message waits: %w", err)
	}

	seen := map[string]struct{}{}
	var successes []pendingDataSetDelete
	var failures []pendingDataSetDelete
	for _, wait := range waits {
		seen[wait.TxHash] = struct{}{}
		detail, ok := byHash[wait.TxHash]
		if !ok {
			continue
		}

		if wait.Status == "confirmed" && wait.Success.Valid && wait.Success.Bool {
			successes = append(successes, detail)
			continue
		}

		if wait.Status == "failed" || (wait.Status == "confirmed" && wait.Success.Valid && !wait.Success.Bool) {
			failures = append(failures, detail)
		}
	}

	for _, detail := range pending {
		if _, ok := seen[detail.TxHash]; ok {
			continue
		}
		log.Warnw("data set delete tx missing message_waits_eth row", "txHash", detail.TxHash, "dataSetId", detail.ID)
	}

	successErr := processSuccessfulDeletes(ctx, db, ethClient, successes)
	failureErr := processFailedDeletes(ctx, db, failures)
	if successErr != nil || failureErr != nil {
		return xerrors.Errorf("failed to delete data sets: %w", errors.Join(successErr, failureErr))
	}
	return nil
}

func processSuccessfulDeletes(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, successes []pendingDataSetDelete) error {
	if len(successes) == 0 {
		return nil
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, detail := range successes {
		live, err := verifier.DataSetLive(contract.EthCallOpts(ctx), big.NewInt(detail.ID))
		if err != nil {
			return xerrors.Errorf("failed to check if data set is live: %w", err)
		}

		if live {
			return errors.New("data set is still live")
		}

		if err := cleanupDeletedDataSet(ctx, db, detail.ID, detail.TxHash); err != nil {
			return err
		}
	}

	return nil
}

func processFailedDeletes(ctx context.Context, db *harmonydb.DB, failures []pendingDataSetDelete) error {
	for _, detail := range failures {
		_, err := db.Exec(ctx, `
			UPDATE pdp_delete_data_set
			SET delete_tx_hash = NULL,
			    after_delete_data_set = FALSE,
			    delete_data_set_task_id = NULL
			WHERE id = $1
			  AND delete_tx_hash = $2
			  AND after_delete_data_set = TRUE
			  AND service_termination_epoch IS NOT NULL
			  AND terminated = FALSE
		`, detail.ID, detail.TxHash)
		if err != nil {
			return xerrors.Errorf("failed to reset failed data set delete for data set %d: %w", detail.ID, err)
		}

		log.Warnw("reset failed data set delete for retry", "dataSetId", detail.ID, "txHash", detail.TxHash)
	}

	return nil
}

func cleanupDeletedDataSet(ctx context.Context, db *harmonydb.DB, dataSetID int64, deleteTxHash string) error {
	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// TODO: When the post-delete cleanup stage lands, route successful
		// deleteDataSet confirmations there instead of removing local rows here.
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

		_, err = tx.Exec(`DELETE FROM pdp_data_sets WHERE id = $1`, dataSetID)
		if err != nil {
			return false, xerrors.Errorf("failed to delete data set %d: %w", dataSetID, err)
		}

		_, err = tx.Exec(`DELETE FROM  pdp_delete_data_set WHERE id = $1 AND delete_tx_hash = $2`, dataSetID, deleteTxHash)
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

	return nil
}
