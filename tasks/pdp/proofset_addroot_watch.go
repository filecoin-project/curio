package pdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

// Structures to represent database records
type ProofSetRootAdd struct {
	ID              string `db:"id"`
	Client          string `db:"client"`
	PieceCID2       string `db:"piece_cid_v2"` // pieceCIDV2
	PieceCID        string `db:"piece_cid"`
	PieceSize       int64  `db:"piece_size"`
	RawSize         int64  `db:"raw_size"`
	ProofSet        uint64 `db:"proof_set_id"`
	PieceRef        int64  `db:"piece_ref"`
	AddMessageHash  string `db:"add_message_hash"`
	AddMessageIndex int64  `db:"add_message_index"`
}

// NewWatcherRootAdd sets up the watcher for proof set root additions
func NewWatcherRootAdd(db *harmonydb.DB, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingProofSetRootAdds(ctx, db)
		if err != nil {
			log.Errorf("Failed to process pending proof set root adds: %s", err)
		}

		return nil
	}); err != nil {
		panic(err)
	}
}

// processPendingProofSetRootAdds processes root additions that have been confirmed on-chain
func processPendingProofSetRootAdds(ctx context.Context, db *harmonydb.DB) error {
	// Query for pdp_proofset_root_adds entries where add_message_ok = TRUE
	var rootAdds []ProofSetRootAdd

	err := db.Select(ctx, &rootAdds, `
        SELECT id, client, piece_cid_v2, piece_cid, piece_size, raw_size, proof_set_id, piece_ref, add_message_hash, add_message_index 
        FROM pdp_pipeline
        WHERE after_add_root = TRUE AND after_add_root_msg = FALSE
    `)
	if err != nil {
		return xerrors.Errorf("failed to select proof set root adds: %w", err)
	}

	if len(rootAdds) == 0 {
		// No pending root adds
		return nil
	}

	// Process each root addition
	for _, rootAdd := range rootAdds {
		err := processProofSetRootAdd(ctx, db, rootAdd)
		if err != nil {
			log.Errorf("Failed to process root add for tx %s: %s", rootAdd.AddMessageHash, err)
			continue
		}
	}

	return nil
}

func processProofSetRootAdd(ctx context.Context, db *harmonydb.DB, rootAdd ProofSetRootAdd) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	var txSuccess bool
	err := db.QueryRow(ctx, `SELECT tx_success, tx_receipt FROM message_waits_eth WHERE signed_tx_hash = $1 
                                                       AND tx_success IS NOT NULL 
                                                       AND tx_receipt IS NOT NULL`, rootAdd.AddMessageHash).Scan(&txSuccess, &txReceiptJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return xerrors.Errorf("tx hash %s is either missing from watch table or is not yet processed by watcher", rootAdd.AddMessageHash)
		}
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", rootAdd.AddMessageHash, err)
	}

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", rootAdd.AddMessageHash, err)
	}

	if !txSuccess {
		// This means msg failed, we should let the user know
		// TODO: Review if error would be in receipt
		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			n, err := tx.Exec(`UPDATE market_mk20_deal
									SET pdp_v1 = jsonb_set(
													jsonb_set(pdp_v1, '{error}', to_jsonb($1::text), true),
													'{complete}', to_jsonb(true), true
												 )
									WHERE id = $2;`, "Transaction failed", rootAdd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}
			_, err = tx.Exec(`DELETE FROM pdp_pipeline WHERE id = $1`, rootAdd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to clean up pdp pipeline: %w", err)
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

	// Get the ABI from the contract metadata
	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return fmt.Errorf("failed to get PDP ABI: %w", err)
	}

	// Get the event definition
	event, exists := pdpABI.Events["RootsAdded"]
	if !exists {
		return fmt.Errorf("RootsAdded event not found in ABI")
	}

	var rootIds []uint64
	eventFound := false

	// Iterate over the logs in the receipt
	for _, vLog := range txReceipt.Logs {
		// Check if the log corresponds to the RootsAdded event
		if len(vLog.Topics) > 0 && vLog.Topics[0] == event.ID {
			// The setId is an indexed parameter in Topics[1], but we don't need it here
			// as we already have the proofset ID from the database

			// Parse the non-indexed parameter (rootIds array) from the data
			unpacked, err := event.Inputs.Unpack(vLog.Data)
			if err != nil {
				return fmt.Errorf("failed to unpack log data: %w", err)
			}

			// Extract the rootIds array
			if len(unpacked) == 0 {
				return fmt.Errorf("no unpacked data found in log")
			}

			// Convert the unpacked rootIds ([]interface{} containing *big.Int) to []uint64
			bigIntRootIds, ok := unpacked[0].([]*big.Int)
			if !ok {
				return fmt.Errorf("failed to convert unpacked data to array")
			}

			rootIds = make([]uint64, len(bigIntRootIds))
			for i := range bigIntRootIds {
				rootIds[i] = bigIntRootIds[i].Uint64()
			}

			eventFound = true
			// We found the event, so we can break the loop
			break
		}
	}

	if !eventFound {
		return fmt.Errorf("RootsAdded event not found in receipt")
	}

	rootId := rootIds[rootAdd.AddMessageIndex]

	// Insert into message_waits_eth and pdp_proofset_roots
	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Update proof set for initialization upon first add
		_, err = tx.Exec(`
			UPDATE pdp_proof_set SET init_ready = true
			WHERE id = $1 AND prev_challenge_request_epoch IS NULL AND challenge_request_msg_hash IS NULL AND prove_at_epoch IS NULL
			`, rootAdd.ProofSet)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
		}

		// Insert into pdp_proofset_roots
		n, err := tx.Exec(`
                  INSERT INTO pdp_proofset_root (
                      proof_set_id,
                      client,
                      piece_cid_v2,
					  piece_cid,
					  piece_size,
					  raw_size,
                      root,
                      piece_ref,
                      add_deal_id,
                      add_message_hash,
                      add_message_index
                  )
                  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
              `,
			rootAdd.ProofSet,
			rootAdd.Client,
			rootAdd.PieceCID2,
			rootAdd.PieceCID,
			rootAdd.PieceSize,
			rootAdd.RawSize,
			rootId,
			rootAdd.PieceRef,
			rootAdd.ID,
			rootAdd.AddMessageHash,
			rootAdd.AddMessageIndex,
		)
		if err != nil {
			return false, xerrors.Errorf("failed to insert into pdp_proofset_root: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("incorrect number of rows inserted for pdp_proofset_root: %d", n)
		}

		n, err = tx.Exec(`UPDATE pdp_pipeline SET after_add_root_msg = TRUE WHERE id = $1`, rootAdd.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("incorrect number of rows updated for pdp_pipeline: %d", n)
		}

		// Return true to commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("failed to save details to DB: %w", err)
	}

	if !comm {
		return xerrors.Errorf("failed to commit transaction")
	}

	return nil
}
