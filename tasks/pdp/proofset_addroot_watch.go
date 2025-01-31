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
type ProofSetRootAdd struct {
	ProofSet       uint64 `db:"proofset"`
	AddMessageHash string `db:"add_message_hash"`
}

// RootAddEntry represents entries from pdp_proofset_root_adds
type RootAddEntry struct {
	ProofSet        uint64 `db:"proofset"`
	Root            string `db:"root"`
	AddMessageHash  string `db:"add_message_hash"`
	AddMessageIndex uint64 `db:"add_message_index"`
	Subroot         string `db:"subroot"`
	SubrootOffset   int64  `db:"subroot_offset"`
	SubrootSize     int64  `db:"subroot_size"`
	PDPPieceRefID   int64  `db:"pdp_pieceref"`
	AddMessageOK    *bool  `db:"add_message_ok"`
	PDPProofSetID   uint64 `db:"proofset"`
}

// NewWatcherRootAdd sets up the watcher for proof set root additions
func NewWatcherRootAdd(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingProofSetRootAdds(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending proof set root adds: %v", err)
		}

		return nil
	}); err != nil {
		panic(err)
	}
}

// processPendingProofSetRootAdds processes root additions that have been confirmed on-chain
func processPendingProofSetRootAdds(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_proofset_root_adds entries where add_message_ok = TRUE
	var rootAdds []ProofSetRootAdd

	err := db.Select(ctx, &rootAdds, `
        SELECT DISTINCT proofset, add_message_hash
        FROM pdp_proofset_root_adds
        WHERE add_message_ok = TRUE
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
		err := processProofSetRootAdd(ctx, db, ethClient, rootAdd)
		if err != nil {
			log.Warnf("Failed to process root add for tx %s: %v", rootAdd.AddMessageHash, err)
			continue
		}
	}

	return nil
}

func processProofSetRootAdd(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, rootAdd ProofSetRootAdd) error {
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

func extractAndInsertRootsFromReceipt(ctx context.Context, db *harmonydb.DB, receipt *types.Receipt, rootAdd ProofSetRootAdd) error {
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

	var firstAdded *big.Int
	eventFound := false

	// Iterate over the logs in the receipt
	for _, vLog := range receipt.Logs {
		// Check if the log corresponds to the RootsAdded event
		if len(vLog.Topics) > 0 && vLog.Topics[0] == event.ID {
			// Since 'firstAdded' is an indexed parameter, it's in Topics[1]
			if len(vLog.Topics) < 2 {
				return fmt.Errorf("log does not contain firstAdded topic")
			}

			// Convert the topic to a big.Int
			firstAdded = new(big.Int).SetBytes(vLog.Topics[1].Bytes())
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
        `, rootAdd.ProofSet, rootAdd.AddMessageHash)
		if err != nil {
			return false, fmt.Errorf("failed to select from pdp_proofset_root_adds: %w", err)
		}

		// For each entry, calculate root_id and insert into pdp_proofset_roots
		for _, entry := range rootAddEntries {
			rootId := firstAdded.Uint64() + entry.AddMessageIndex

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
            `, entry.ProofSet, entry.Root, rootId, entry.Subroot, entry.SubrootOffset, entry.SubrootSize, entry.PDPPieceRefID, entry.AddMessageHash, entry.AddMessageIndex)
			if err != nil {
				return false, fmt.Errorf("failed to insert into pdp_proofset_roots: %w", err)
			}
		}

		// Delete from pdp_proofset_root_adds
		_, err = tx.Exec(`
            DELETE FROM pdp_proofset_root_adds
            WHERE proofset = $1 AND add_message_hash = $2
        `, rootAdd.ProofSet, rootAdd.AddMessageHash)
		if err != nil {
			return false, fmt.Errorf("failed to delete from pdp_proofset_root_adds: %w", err)
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("failed to process root additions in DB: %w", err)
	}

	return nil
}
