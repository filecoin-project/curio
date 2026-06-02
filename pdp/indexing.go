package pdp

import (
	"context"
	"math/big"
	"slices"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

// CheckIfIndexingNeeded checks if a data set has the withIPFSIndexing metadata flag.
// Returns true if indexing is needed, false otherwise.
// This is a read-only check that can be done outside a transaction for existing datasets.
func CheckIfIndexingNeeded(
	ctx context.Context,
	ethClient api.EthClientInterface,
	dataSetId uint64,
) (bool, error) {
	// Get the PDPVerifier contract instance
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		log.Errorw("Failed to instantiate PDPVerifier contract", "error", err, "dataSetId", dataSetId)
		return false, err
	}

	// Get the listener (record keeper) address for this data set
	listenerAddr, err := pdpVerifier.GetDataSetListener(contract.EthCallOpts(ctx), new(big.Int).SetUint64(dataSetId))
	if err != nil {
		log.Errorw("Failed to get listener address for data set", "error", err, "dataSetId", dataSetId)
		return false, err
	}

	// Check if the withIPFSIndexing metadata exists
	mustIndex, _, err := contract.GetDataSetMetadataAtKey(ctx, listenerAddr, ethClient, new(big.Int).SetUint64(dataSetId), "withIPFSIndexing")
	if err != nil {
		// Hard to differentiate between unsupported listener type OR internal error
		// So we log and skip indexing attempt
		log.Warnw("Failed to get data set metadata, skipping indexing", "error", err, "dataSetId", dataSetId)
		return false, nil
	}

	return mustIndex, nil
}

// CheckIfIndexingNeededFromExtraData checks if extraData contains withIPFSIndexing metadata.
// This is used for the CreateDataSet+AddPieces combined operation where the dataset doesn't exist yet.
// The extraData format for combined operations is: (bytes createPayload, bytes addPayload)
// We attempt to decode createPayload according to the
// FilecoinWarmStorageService format:
//
//	(address payer, uint256 clientDataSetId, string[] keys, string[] values, bytes signature)
func CheckIfIndexingNeededFromExtraData(extraData []byte) (bool, error) {
	if len(extraData) == 0 {
		return false, nil
	}

	payload, err := decodeFWSSCreatePayload(extraData)
	if err != nil {
		log.Debugw("Failed to decode extraData as combined operation, not an error", "error", err)
		return false, nil
	}

	// Look for withIPFSIndexing in the keys array
	if slices.Contains(payload.MetadataKeys, "withIPFSIndexing") {
		log.Debugw("Found withIPFSIndexing in extraData metadata keys")
		return true, nil
	}

	return false, nil
}

// EnableIndexingForPiecesInTx marks the specified piecerefs as needing indexing within a transaction.
func EnableIndexingForPiecesInTx(
	tx *harmonydb.Tx,
	serviceLabel string,
	subPieceRefIDs []int64,
) error {
	log.Debugw("Marking subpieces as needing indexing (in transaction)",
		"serviceLabel", serviceLabel,
		"subPieceCount", len(subPieceRefIDs))

	_, err := tx.Exec(`
		UPDATE pdp_piecerefs
		SET needs_indexing = TRUE
		WHERE service = $1
			AND id = ANY($2)
			AND needs_indexing = FALSE
	`, serviceLabel, subPieceRefIDs)
	return err
}
