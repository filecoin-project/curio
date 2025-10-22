package pdp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

// Structures to represent database records
type DataSetPieceAdd struct {
	DataSet        sql.NullInt64 `db:"data_set"`
	AddMessageHash string        `db:"add_message_hash"`
}

// PieceAddEntry represents entries from pdp_data_set_piece_adds
type PieceAddEntry struct {
	DataSet         sql.NullInt64 `db:"data_set"`
	Piece           string        `db:"piece"`
	AddMessageHash  string        `db:"add_message_hash"`
	AddMessageIndex uint64        `db:"add_message_index"`
	SubPiece        string        `db:"sub_piece"`
	SubPieceOffset  int64         `db:"sub_piece_offset"`
	SubPieceSize    int64         `db:"sub_piece_size"`
	PDPPieceRefID   int64         `db:"pdp_pieceref"`
	AddMessageOK    *bool         `db:"add_message_ok"`
}

// processPendingDataSetPieceAdds processes piece additions that have been confirmed on-chain
// it is called from proofset_watch.go
func processPendingDataSetPieceAdds(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_data_set_piece_adds entries where add_message_ok = TRUE
	var pieceAdds []DataSetPieceAdd

	err := db.Select(ctx, &pieceAdds, `
        SELECT DISTINCT data_set, add_message_hash
        FROM pdp_data_set_piece_adds
        WHERE add_message_ok = TRUE AND pieces_added = FALSE
    `)
	if err != nil {
		return xerrors.Errorf("failed to select data set piece adds: %w", err)
	}

	if len(pieceAdds) == 0 {
		// No pending piece adds
		return nil
	}

	// Process each piece addition
	for _, pieceAdd := range pieceAdds {
		log.Infow("Processing piece add", "dataSet", pieceAdd.DataSet, "addMessageHash", pieceAdd.AddMessageHash)
		err := processDataSetPieceAdd(ctx, db, ethClient, pieceAdd)
		if err != nil {
			log.Warnf("Failed to process piece add for tx %s: %v", pieceAdd.AddMessageHash, err)
			continue
		}
	}

	return nil
}

func processDataSetPieceAdd(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, pieceAdd DataSetPieceAdd) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	err := db.QueryRow(ctx, `
        SELECT tx_receipt
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, pieceAdd.AddMessageHash).Scan(&txReceiptJSON)
	if err != nil {
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", pieceAdd.AddMessageHash, err)
	}

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", pieceAdd.AddMessageHash, err)
	}

	// Parse the logs to extract piece IDs and other data
	err = extractAndInsertPiecesFromReceipt(ctx, db, &txReceipt, pieceAdd)
	if err != nil {
		return xerrors.Errorf("failed to extract pieces from receipt for tx %s: %w", pieceAdd.AddMessageHash, err)
	}

	return nil
}

func extractAndInsertPiecesFromReceipt(ctx context.Context, db *harmonydb.DB, receipt *types.Receipt, pieceAdd DataSetPieceAdd) error {
	if !pieceAdd.DataSet.Valid {
		var err error
		pieceAdd.DataSet.Int64, err = extractDataSetIdFromReceipt(receipt)
		if err != nil {
			return fmt.Errorf("expeted to find dataSetId in receipt but failed to extract: %w", err)
		}
		pieceAdd.DataSet.Valid = true
		var exists bool
		// we check if the dataset exists already to avoid foreign key violation
		err = db.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pdp_data_sets
				WHERE id = $1
			)`, pieceAdd.DataSet.Int64).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check if data set exists: %w", err)
		}
		if !exists {
			// this is a rare case where the transaction is marked as complete between create_watch being called and this function
			// if that happens, we return an error which will get logged and ignored
			// piece addition will get picked up in the next run of the watcher
			return fmt.Errorf("data set %d not found in pdp_data_sets", pieceAdd.DataSet.Int64)
		}
	}
	// Get the ABI from the contract metadata
	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return fmt.Errorf("failed to get PDP ABI: %w", err)
	}

	// Get the event definition
	event, exists := pdpABI.Events["PiecesAdded"]
	if !exists {
		return fmt.Errorf("PiecesAdded event not found in ABI")
	}

	var pieceIds []uint64
	eventFound := false

	// Iterate over the logs in the receipt
	for _, vLog := range receipt.Logs {
		// Check if the log corresponds to the PiecesAdded event
		if len(vLog.Topics) > 0 && vLog.Topics[0] == event.ID {
			// The setId is an indexed parameter in Topics[1], but we don't need it here
			// as we already have the data set ID from the database

			// Parse the non-indexed parameter (pieceIds array) from the data
			unpacked, err := event.Inputs.Unpack(vLog.Data)
			if err != nil {
				return fmt.Errorf("failed to unpack log data: %w", err)
			}

			// Extract the pieceIds array
			if len(unpacked) == 0 {
				return fmt.Errorf("no unpacked data found in log")
			}

			// Convert the unpacked pieceIds ([]interface{} containing *big.Int) to []uint64
			bigIntPieceIds, ok := unpacked[0].([]*big.Int)
			if !ok {
				return fmt.Errorf("failed to convert unpacked data to array")
			}

			pieceIds = make([]uint64, len(bigIntPieceIds))
			for i := range bigIntPieceIds {
				pieceIds[i] = bigIntPieceIds[i].Uint64()
			}

			eventFound = true
			// We found the event, so we can break the loop
			break
		}
	}

	if !eventFound {
		return fmt.Errorf("PiecesAdded event not found in receipt")
	}

	// Now we have the firstAdded pieceId, proceed with database operations

	// Begin a database transaction
	_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Fetch the entries from pdp_data_set_piece_adds
		var pieceAddEntries []PieceAddEntry
		err := tx.Select(&pieceAddEntries, `
            SELECT data_set, piece, add_message_hash, add_message_index, sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref
            FROM pdp_data_set_piece_adds
            WHERE add_message_hash = $1
            ORDER BY add_message_index ASC, sub_piece_offset ASC
        `, pieceAdd.AddMessageHash)
		if err != nil {
			return false, fmt.Errorf("failed to select from pdp_data_set_piece_adds: %w", err)
		}

		if len(pieceAddEntries) == 0 {
			return false, fmt.Errorf("no entries found for piece add %s", pieceAdd.AddMessageHash)
		}

		// For each entry, use the corresponding pieceId from the event
		for _, entry := range pieceAddEntries {
			if entry.AddMessageIndex >= uint64(len(pieceIds)) {
				return false, fmt.Errorf("index out of bounds: entry index %d exceeds pieceIds length %d",
					entry.AddMessageIndex, len(pieceIds))
			}
			if entry.DataSet.Valid && entry.DataSet.Int64 != pieceAdd.DataSet.Int64 {
				return false, fmt.Errorf("data set mismatch: expected %d but got %d", pieceAdd.DataSet.Int64, entry.DataSet.Int64)
			}
			entry.DataSet = pieceAdd.DataSet // just so we don't use wrong value accidentally

			pieceId := pieceIds[entry.AddMessageIndex]
			// Insert into pdp_data_set_pieces
			_, err := tx.Exec(`
                INSERT INTO pdp_data_set_pieces (
                    data_set,
                    piece,
                    piece_id,
                    sub_piece,
                    sub_piece_offset,
                    sub_piece_size,
                    pdp_pieceref,
                    add_message_hash,
                    add_message_index
                ) VALUES (
                    $1, $2, $3, $4, $5, $6, $7, $8, $9
                )
            `, pieceAdd.DataSet, entry.Piece, pieceId, entry.SubPiece, entry.SubPieceOffset, entry.SubPieceSize, entry.PDPPieceRefID, entry.AddMessageHash, entry.AddMessageIndex)
			if err != nil {
				return false, fmt.Errorf("failed to insert into pdp_data_set_pieces: %w", err)
			}
		}

		// Mark as processed in pdp_data_set_piece_adds (don't delete, for transaction tracking)
		rowsAffected, err := tx.Exec(`
                      UPDATE pdp_data_set_piece_adds
                      SET pieces_added = TRUE, data_set = $1
                      WHERE add_message_hash = $2 AND pieces_added = FALSE
              `, pieceAdd.DataSet, pieceAdd.AddMessageHash)
		if err != nil {
			return false, fmt.Errorf("failed to update pdp_data_set_piece_adds: %w", err)
		}

		if int(rowsAffected) != len(pieceAddEntries) {
			return false, fmt.Errorf("expected to update %d rows in pdp_data_set_piece_adds but updated %d", len(pieceAddEntries), rowsAffected)
		}

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to process piece additions in DB: %w", err)
	}

	return nil
}
