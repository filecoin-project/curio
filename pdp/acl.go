package pdp

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strings"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
)

// MetadataKeyKeyspace is the data set metadata key for the FOC keyspace id.
const MetadataKeyKeyspace = "foc/ks"

// MetadataKeyRead is the piece-upload JSON field for read access intent.
const MetadataKeyRead = "read"

// PieceReadRestricted is the read value that marks a piece for acl-pieces.
const PieceReadRestricted = "restricted"

// pieceRestrictedFromRead reports whether a piece upload's read field is restricted.
func pieceRestrictedFromRead(read string) bool {
	return strings.EqualFold(strings.TrimSpace(read), PieceReadRestricted)
}

// KeyspaceFromExtraData reports whether create extraData includes a non-empty foc/ks value.
// known is false when extraData is empty or cannot be decoded as FWSS create metadata.
// hasKeyspace is true when foc/ks is present and non-empty after trim.
func KeyspaceFromExtraData(extraData []byte) (known bool, hasKeyspace bool) {
	ks, known := keyspaceValueFromExtraData(extraData)
	return known, ks != ""
}

// keyspaceValueFromExtraData returns the foc/ks metadata value from create extraData.
// known is false when extraData is empty or cannot be decoded.
func keyspaceValueFromExtraData(extraData []byte) (keyspace string, known bool) {
	if len(extraData) == 0 {
		return "", false
	}

	payload, err := DecodeFWSSCreateIdentityFromExtraData(extraData)
	if err != nil {
		log.Debugw("Failed to decode extraData for keyspace", "error", err)
		return "", false
	}

	return metadataValue(payload, MetadataKeyKeyspace), true
}

// metadataValue returns the trimmed metadata value for key, or "" when absent.
// The caller has already decoded the create payload.
func metadataValue(payload *FWSSCreateIdentity, key string) string {
	for i, k := range payload.MetadataKeys {
		if k != key || i >= len(payload.MetadataValues) {
			continue
		}
		return strings.TrimSpace(payload.MetadataValues[i])
	}
	return ""
}

// ResolveDatasetHasKeyspace returns the cached has_keyspace flag for a data set.
// If pdp_data_sets.has_keyspace is NULL, it reads chain metadata and persists the result.
// Chain/read failures return an error so callers do not treat an unknown data set as public-only.
func ResolveDatasetHasKeyspace(
	ctx context.Context,
	db *harmonydb.DB,
	ethClient ethchain.EthClient,
	dataSetId uint64,
) (bool, error) {
	cached, err := readDatasetHasKeyspace(ctx, db, dataSetId)
	if err != nil {
		return false, err
	}
	if cached != nil {
		return *cached, nil
	}

	hasKS, err := fetchHasKeyspaceFromChain(ctx, ethClient, dataSetId)
	if err != nil {
		return false, err
	}
	if err := PersistDatasetHasKeyspace(ctx, db, dataSetId, hasKS); err != nil {
		return false, err
	}
	return hasKS, nil
}

// ResolveDatasetKeyspace returns the foc/ks value for a data set from chain metadata.
// An empty string means the data set has no keyspace.
func ResolveDatasetKeyspace(
	ctx context.Context,
	ethClient ethchain.EthClient,
	dataSetId uint64,
) (string, error) {
	return fetchKeyspaceFromChain(ctx, ethClient, dataSetId)
}

// readDatasetHasKeyspace returns the stored flag. A nil bool means the column is still NULL.
func readDatasetHasKeyspace(ctx context.Context, db *harmonydb.DB, dataSetId uint64) (*bool, error) {
	var hasKS sql.NullBool
	err := db.QueryRow(ctx, `SELECT has_keyspace FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&hasKS)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("data set %d not found", dataSetId)
	}
	if err != nil {
		return nil, xerrors.Errorf("reading pdp_data_sets.has_keyspace for %d: %w", dataSetId, err)
	}
	if !hasKS.Valid {
		return nil, nil
	}
	v := hasKS.Bool
	return &v, nil
}

// fetchHasKeyspaceFromChain reports whether the data set listener has a non-empty foc/ks value.
func fetchHasKeyspaceFromChain(ctx context.Context, ethClient ethchain.EthClient, dataSetId uint64) (bool, error) {
	ks, err := fetchKeyspaceFromChain(ctx, ethClient, dataSetId)
	if err != nil {
		return false, err
	}
	return ks != "", nil
}

// fetchKeyspaceFromChain reads the foc/ks metadata key from the data set listener.
func fetchKeyspaceFromChain(ctx context.Context, ethClient ethchain.EthClient, dataSetId uint64) (string, error) {
	pdpVerifier, err := contract.NewPDPVerifierCaller(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return "", xerrors.Errorf("instantiate PDPVerifier: %w", err)
	}

	setID := new(big.Int).SetUint64(dataSetId)
	listenerAddr, err := pdpVerifier.GetDataSetListener(contract.EthCallOpts(ctx), setID)
	if err != nil {
		return "", xerrors.Errorf("GetDataSetListener(%d): %w", dataSetId, err)
	}

	exists, value, err := contract.GetDataSetMetadataAtKey(ctx, listenerAddr, ethClient, setID, MetadataKeyKeyspace)
	if err != nil {
		return "", xerrors.Errorf("GetDataSetMetadataAtKey(%d, %s): %w", dataSetId, MetadataKeyKeyspace, err)
	}
	if !exists {
		return "", nil
	}
	return strings.TrimSpace(value), nil
}

// PersistDatasetHasKeyspace stores a resolved has_keyspace value when the column is still NULL.
func PersistDatasetHasKeyspace(ctx context.Context, db *harmonydb.DB, dataSetId uint64, hasKeyspace bool) error {
	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_data_sets
			SET has_keyspace = $1
			WHERE id = $2 AND has_keyspace IS NULL
		`, hasKeyspace, dataSetId)
		if err != nil {
			return false, xerrors.Errorf("updating pdp_data_sets.has_keyspace for %d: %w", dataSetId, err)
		}
		if n == 0 {
			var stored sql.NullBool
			if err := tx.QueryRow(`SELECT has_keyspace FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&stored); err != nil {
				return false, xerrors.Errorf("re-reading pdp_data_sets.has_keyspace for %d: %w", dataSetId, err)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())
	return err
}
