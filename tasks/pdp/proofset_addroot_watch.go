package pdp

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

// Structures to represent database records
type DataSetRootAdd struct {
	DataSet        uint64 `db:"proofset"`
	AddMessageHash string `db:"add_message_hash"`
}

// RootAddEntry represents entries from pdp_proofset_root_adds
type RootAddEntry struct {
	DataSet         uint64 `db:"proofset"`
	Root            string `db:"root"`
	AddMessageHash  string `db:"add_message_hash"`
	AddMessageIndex uint64 `db:"add_message_index"`
	Subroot         string `db:"subroot"`
	SubrootOffset   int64  `db:"subroot_offset"`
	SubrootSize     int64  `db:"subroot_size"`
	PDPPieceRefID   int64  `db:"pdp_pieceref"`
	AddMessageOK    *bool  `db:"add_message_ok"`
	PDPDataSetID    uint64 `db:"proofset"`
}

// NewWatcherRootAdd sets up the watcher for proof set root additions
func NewWatcherRootAdd(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetRootAdds(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending proof set root adds: %v", err)
		}

		return nil
	}); err != nil {
		panic(err)
	}
}

// processPendingDataSetRootAdds processes root additions that have been confirmed on-chain
func processPendingDataSetRootAdds(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_proofset_root_adds entries where add_message_ok = TRUE
	var rootAdds []DataSetRootAdd

	err := db.Select(ctx, &rootAdds, `
        SELECT DISTINCT proofset, add_message_hash
        FROM pdp_proofset_root_adds
        WHERE add_message_ok = TRUE AND roots_added = FALSE
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
		err := processDataSetRootAdd(ctx, db, ethClient, rootAdd)
		if err != nil {
			log.Warnf("Failed to process root add for tx %s: %v", rootAdd.AddMessageHash, err)
			continue
		}
	}

	return nil
}

func processDataSetRootAdd(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, rootAdd DataSetRootAdd) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	err := db.QueryRow(ctx, `
        SELECT tx_receipt
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, rootAdd.AddMessageHash).Scan(&txReceiptJSON)
	if err != nil {
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", rootAdd.AddMessageHash, err)
	}

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", rootAdd.AddMessageHash, err)
	}

	// Parse the logs to extract root IDs and other data
	err = extractAndInsertRootsFromReceipt(ctx, db, &txReceipt, rootAdd)
	if err != nil {
		return xerrors.Errorf("failed to extract roots from receipt for tx %s: %w", rootAdd.AddMessageHash, err)
	}

	return nil
}

func extractAndInsertRootsFromReceipt(ctx context.Context, db *harmonydb.DB, receipt *types.Receipt, rootAdd DataSetRootAdd) error {
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
	for _, vLog := range receipt.Logs {
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
			bigIntPieceIds, ok := unpacked[0].([]*big.Int)
			if !ok {
				return fmt.Errorf("failed to convert unpacked data to array")
			}

			rootIds = make([]uint64, len(bigIntPieceIds))
			for i := range bigIntPieceIds {
				rootIds[i] = bigIntPieceIds[i].Uint64()
			}

			eventFound = true
			// We found the event, so we can break the loop
			break
		}
	}

	if !eventFound {
		return fmt.Errorf("RootsAdded event not found in receipt")
	}

	// Now we have the firstAdded rootId, proceed with database operations

	// Begin a database transaction
	_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Fetch the entries from pdp_proofset_root_adds
		var rootAddEntries []RootAddEntry
		err := tx.Select(&rootAddEntries, `
            SELECT proofset, root, add_message_hash, add_message_index, subroot, subroot_offset, subroot_size, pdp_pieceref
            FROM pdp_proofset_root_adds
            WHERE proofset = $1 AND add_message_hash = $2
            ORDER BY add_message_index ASC, subroot_offset ASC
        `, rootAdd.DataSet, rootAdd.AddMessageHash)
		if err != nil {
			return false, fmt.Errorf("failed to select from pdp_proofset_root_adds: %w", err)
		}

		// For each entry, use the corresponding rootId from the event
		for _, entry := range rootAddEntries {
			if entry.AddMessageIndex >= uint64(len(rootIds)) {
				return false, fmt.Errorf("index out of bounds: entry index %d exceeds rootIds length %d",
					entry.AddMessageIndex, len(rootIds))
			}

			rootId := rootIds[entry.AddMessageIndex]
			// Insert into pdp_proofset_roots
			_, err := tx.Exec(`
                INSERT INTO pdp_proofset_roots (
                    proofset,
                    root,
                    root_id,
                    subroot,
                    subroot_offset,
					subroot_size,
                    pdp_pieceref,
                    add_message_hash,
                    add_message_index
                ) VALUES (
                    $1, $2, $3, $4, $5, $6, $7, $8, $9
                )
            `, entry.DataSet, entry.Root, rootId, entry.Subroot, entry.SubrootOffset, entry.SubrootSize, entry.PDPPieceRefID, entry.AddMessageHash, entry.AddMessageIndex)
			if err != nil {
				return false, fmt.Errorf("failed to insert into pdp_proofset_roots: %w", err)
			}
		}

		// Mark as processed in pdp_proofset_root_adds (don't delete, for transaction tracking)
		rowsAffected, err := tx.Exec(`
                      UPDATE pdp_proofset_root_adds
                      SET roots_added = TRUE
                      WHERE proofset = $1 AND add_message_hash = $2 AND roots_added = FALSE
              `, rootAdd.DataSet, rootAdd.AddMessageHash)
		if err != nil {
			return false, fmt.Errorf("failed to update pdp_proofset_root_adds: %w", err)
		}

		if int(rowsAffected) != len(rootAddEntries) {
			return false, fmt.Errorf("expected to update %d rows in pdp_proofset_root_adds but updated %d", len(rootAddEntries), rowsAffected)
		}

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to process root additions in DB: %w", err)
	}

	return nil
}
