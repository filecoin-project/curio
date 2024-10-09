package pdp

import (
	"context"
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
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
	// Load the ABI of the PDPService contract
	pdpServiceABI, err := contract.PDPRecordKeeperMetaData.GetAbi()
	if err != nil {
		return xerrors.Errorf("failed to get PDPService ABI: %w", err)
	}

	// Define the event we're interested in
	eventName := "RecordAdded"
	event, exists := pdpServiceABI.Events[eventName]
	if !exists {
		return xerrors.Errorf("event %s not found in PDPService ABI", eventName)
	}

	// Iterate over the logs to find the event
	for _, vLog := range receipt.Logs {
		if len(vLog.Topics) == 0 {
			continue
		}
		if vLog.Topics[0] != event.ID {
			continue
		}

		// Parse the event log to get ProofSetId, Epoch, OperationType, ExtraData
		var eventData struct {
			Epoch         uint64
			OperationType uint8
			ExtraData     []byte
		}

		// Set ProofSetId from indexed topic
		proofSetIdBigInt := new(big.Int).SetBytes(vLog.Topics[1].Bytes())
		proofSetId := proofSetIdBigInt.Uint64()

		// Unpack non-indexed event data
		err := pdpServiceABI.UnpackIntoInterface(&eventData, eventName, vLog.Data)
		if err != nil {
			return xerrors.Errorf("failed to unpack event data: %w", err)
		}

		const OperationTypeAdd = 2

		if eventData.OperationType != OperationTypeAdd {
			continue // Not an ADD operation
		}

		// We expect the ProofSetId to match
		if proofSetId != rootAdd.ProofSet {
			continue // Not matching proofset
		}

		// Decode FirstAdded from ExtraData
		firstAdded, err := decodeAddRootsExtraData(eventData.ExtraData)
		if err != nil {
			return xerrors.Errorf("failed to decode FirstAdded from ExtraData: %w", err)
		}

		// Now we have the firstAdded rootId

		// Begin a database transaction
		_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// Fetch the entries from pdp_proofset_root_adds
			var rootAddEntries []RootAddEntry
			err := tx.Select(&rootAddEntries, `
                SELECT proofset, root, add_message_hash, add_message_index, subroot, subroot_offset, pdp_pieceref
                FROM pdp_proofset_root_adds
                WHERE proofset = $1 AND add_message_hash = $2
                ORDER BY add_message_index ASC, subroot_offset ASC
            `, rootAdd.ProofSet, rootAdd.AddMessageHash)
			if err != nil {
				return false, xerrors.Errorf("failed to select from pdp_proofset_root_adds: %w", err)
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
                        pdp_pieceref,
                        add_message_hash,
                        add_message_index
                    ) VALUES (
                        $1, $2, $3, $4, $5, $6, $7, $8
                    )
                `, entry.ProofSet, entry.Root, rootId, entry.Subroot, entry.SubrootOffset, entry.PDPPieceRefID, entry.AddMessageHash, entry.AddMessageIndex)
				if err != nil {
					return false, xerrors.Errorf("failed to insert into pdp_proofset_roots: %w", err)
				}
			}

			// Delete from pdp_proofset_root_adds
			_, err = tx.Exec(`
                DELETE FROM pdp_proofset_root_adds
                WHERE proofset = $1 AND add_message_hash = $2
            `, rootAdd.ProofSet, rootAdd.AddMessageHash)
			if err != nil {
				return false, xerrors.Errorf("failed to delete from pdp_proofset_root_adds: %w", err)
			}

			return true, nil
		})
		if err != nil {
			return xerrors.Errorf("failed to process root additions in DB: %w", err)
		}

		// Only process the first matching event
		break
	}

	return nil
}

func decodeAddRootsExtraData(data []byte) (*big.Int, error) {
	// Define the ABI for the extraData
	// extraData is expected to be an encoded (uint256)
	firstAddedType, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, xerrors.Errorf("failed to create ABI type for firstAdded: %w", err)
	}

	extraDataArguments := abi.Arguments{
		{
			Name: "firstAdded",
			Type: firstAddedType,
		},
	}

	// Unpack the extraData
	extraDecodedValues, err := extraDataArguments.Unpack(data)
	if err != nil {
		return nil, xerrors.Errorf("failed to unpack extra data: %w", err)
	}

	if len(extraDecodedValues) != 1 {
		return nil, xerrors.Errorf("unexpected number of decoded values: got %d, want 1", len(extraDecodedValues))
	}

	// Extract FirstAdded
	firstAdded, ok := extraDecodedValues[0].(*big.Int)
	if !ok {
		return nil, xerrors.Errorf("expected firstAdded to be *big.Int, got %T", extraDecodedValues[0])
	}

	return firstAdded, nil
}
