package pdp

import (
	"context"
	"fmt"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
)

// CheckIfIndexingNeeded checks if a data set has the withIPFSIndexing metadata flag.
// Returns true if indexing is needed, false otherwise.
// This is a read-only check that can be done outside a transaction for existing datasets.
func CheckIfIndexingNeeded(
	ctx context.Context,
	ethClient ethchain.EthClient,
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

// FWSSPayerFromExtraData extracts the FilecoinWarmStorageService payer from
// pull extraData. The expected format is the combined operation payload:
//
//	(bytes createPayload, bytes addPayload)
//
// where createPayload is:
//
//	(address payer, uint256 clientDataSetId, string[] keys, string[] values, bytes signature)
func FWSSPayerFromExtraData(extraData []byte) (common.Address, error) {
	payload, err := decodeFWSSCreatePayload(extraData)
	if err != nil {
		return common.Address{}, err
	}
	if payload.Payer == (common.Address{}) {
		return common.Address{}, fmt.Errorf("payer is zero address")
	}

	return payload.Payer, nil
}

type fwssCreatePayload struct {
	Payer        common.Address
	MetadataKeys []string
}

func decodeFWSSCreatePayload(extraData []byte) (*fwssCreatePayload, error) {
	if len(extraData) == 0 {
		return nil, fmt.Errorf("extraData is empty")
	}

	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create bytes ABI type: %w", err)
	}
	outerArgs := abi.Arguments{
		{Type: bytesType}, // createPayload
		{Type: bytesType}, // addPayload
	}

	decoded, err := outerArgs.Unpack(extraData)
	if err != nil {
		return nil, fmt.Errorf("decode combined extraData: %w", err)
	}
	if len(decoded) < 1 {
		return nil, fmt.Errorf("combined extraData missing createPayload")
	}

	createPayload, ok := decoded[0].([]byte)
	if !ok {
		return nil, fmt.Errorf("createPayload is not bytes")
	}

	addressType, err := abi.NewType("address", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create address ABI type: %w", err)
	}
	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create uint256 ABI type: %w", err)
	}
	stringArrayType, err := abi.NewType("string[]", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create string array ABI type: %w", err)
	}
	createArgs := abi.Arguments{
		{Type: addressType},     // payer
		{Type: uint256Type},     // clientDataSetId
		{Type: stringArrayType}, // keys
		{Type: stringArrayType}, // values
		{Type: bytesType},       // signature
	}

	createDecoded, err := createArgs.Unpack(createPayload)
	if err != nil {
		return nil, fmt.Errorf("decode createPayload: %w", err)
	}
	if len(createDecoded) < 3 {
		return nil, fmt.Errorf("createPayload missing metadata keys")
	}

	payer, ok := createDecoded[0].(common.Address)
	if !ok {
		return nil, fmt.Errorf("payer is not an address")
	}
	keys, ok := createDecoded[2].([]string)
	if !ok {
		return nil, fmt.Errorf("keys is not []string")
	}

	return &fwssCreatePayload{
		Payer:        payer,
		MetadataKeys: keys,
	}, nil
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
