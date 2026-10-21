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

// MetadataKeyACL is the data set metadata key whose value "true" marks an ACL data set.
const MetadataKeyACL = "foc/acl"

// ACLFromExtraData reports whether create extraData sets foc/acl to true.
// known is false when extraData is empty or cannot be decoded as FWSS create metadata.
func ACLFromExtraData(extraData []byte) (known bool, acl bool) {
	if len(extraData) == 0 {
		return false, false
	}

	payload, err := DecodeFWSSCreateIdentityFromExtraData(extraData)
	if err != nil {
		log.Debugw("Failed to decode extraData for ACL intent", "error", err)
		return false, false
	}

	return true, metadataValueTrue(payload, MetadataKeyACL)
}

// metadataValueTrue reports whether key's paired metadata value is the string "true".
// The caller has already decoded the create payload.
func metadataValueTrue(payload *FWSSCreateIdentity, key string) bool {
	for i, k := range payload.MetadataKeys {
		if k != key || i >= len(payload.MetadataValues) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(payload.MetadataValues[i]), "true") {
			return true
		}
	}
	return false
}

// ResolveDatasetACL returns the cached foc/acl intent for a data set.
// If pdp_data_sets.acl is NULL, it reads chain metadata and persists the result.
// Chain/read failures return an error so callers do not treat an unknown data set as public.
func ResolveDatasetACL(
	ctx context.Context,
	db *harmonydb.DB,
	ethClient ethchain.EthClient,
	dataSetId uint64,
) (bool, error) {
	acl, err := readDatasetACL(ctx, db, dataSetId)
	if err != nil {
		return false, err
	}
	if acl != nil {
		return *acl, nil
	}

	isACL, err := fetchFromChainACL(ctx, ethClient, dataSetId)
	if err != nil {
		return false, err
	}
	if err := PersistDatasetACL(ctx, db, dataSetId, isACL); err != nil {
		return false, err
	}
	return isACL, nil
}

// readDatasetACL returns the stored flag. A nil bool means the column is still NULL.
func readDatasetACL(ctx context.Context, db *harmonydb.DB, dataSetId uint64) (*bool, error) {
	var acl sql.NullBool
	err := db.QueryRow(ctx, `SELECT acl FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&acl)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("data set %d not found", dataSetId)
	}
	if err != nil {
		return nil, xerrors.Errorf("reading pdp_data_sets.acl for %d: %w", dataSetId, err)
	}
	if !acl.Valid {
		return nil, nil
	}
	v := acl.Bool
	return &v, nil
}

// fetchFromChainACL reads foc/acl from the data set listener. A missing key is false.
func fetchFromChainACL(ctx context.Context, ethClient ethchain.EthClient, dataSetId uint64) (bool, error) {
	pdpVerifier, err := contract.NewPDPVerifierCaller(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return false, xerrors.Errorf("instantiate PDPVerifier: %w", err)
	}

	setID := new(big.Int).SetUint64(dataSetId)
	listenerAddr, err := pdpVerifier.GetDataSetListener(contract.EthCallOpts(ctx), setID)
	if err != nil {
		return false, xerrors.Errorf("GetDataSetListener(%d): %w", dataSetId, err)
	}

	exists, value, err := contract.GetDataSetMetadataAtKey(ctx, listenerAddr, ethClient, setID, MetadataKeyACL)
	if err != nil {
		return false, xerrors.Errorf("GetDataSetMetadataAtKey(%d, %s): %w", dataSetId, MetadataKeyACL, err)
	}
	return exists && strings.EqualFold(strings.TrimSpace(value), "true"), nil
}

// PersistDatasetACL stores a resolved acl value when the column is still NULL.
func PersistDatasetACL(ctx context.Context, db *harmonydb.DB, dataSetId uint64, acl bool) error {
	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_data_sets
			SET acl = $1
			WHERE id = $2 AND acl IS NULL
		`, acl, dataSetId)
		if err != nil {
			return false, xerrors.Errorf("updating pdp_data_sets.acl for %d: %w", dataSetId, err)
		}
		if n == 0 {
			var stored sql.NullBool
			if err := tx.QueryRow(`SELECT acl FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&stored); err != nil {
				return false, xerrors.Errorf("re-reading pdp_data_sets.acl for %d: %w", dataSetId, err)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())
	return err
}
