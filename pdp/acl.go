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
	"github.com/filecoin-project/curio/pdp/pieceacl"
)

// MetadataKeyACL is the data set metadata key for read access.
const MetadataKeyACL = "read"

// DATASET_READ_RESTRICTED is the read-metadata value that marks an ACL data set.
const DATASET_READ_RESTRICTED = "restricted"

// ACL_SECURED is the pdp_data_sets.acl value for a read-restricted data set.
const ACL_SECURED = pieceacl.Secured

// ACL_PUBLIC is the pdp_data_sets.acl value once resolved and not restricted.
const ACL_PUBLIC = 0

// ACLFromExtraData reports whether create extraData sets read to restricted.
// known is false when extraData is empty or cannot be decoded as FWSS create metadata.
func ACLFromExtraData(extraData []byte) (known bool, acl bool) {
	if len(extraData) == 0 {
		return false, false
	}

	id, err := DecodeFWSSCreateIdentity(extraData)
	if err != nil {
		log.Debugw("Failed to decode extraData for ACL intent", "error", err)
		return false, false
	}

	return true, id.ReadRestricted()
}

// ResolveDatasetACL returns whether the data set is read-restricted (acl = 1).
// A NULL column is read from chain metadata and stored as 1 or 0. A chain or
// read failure is an error, so an unknown data set is not treated as public.
func ResolveDatasetACL(
	ctx context.Context,
	db *harmonydb.DB,
	ethClient ethchain.EthClient,
	dataSetId uint64,
) (bool, error) {
	var stored sql.NullInt32
	err := db.QueryRow(ctx, `SELECT acl FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, xerrors.Errorf("data set %d not found", dataSetId)
	}
	if err != nil {
		return false, xerrors.Errorf("reading pdp_data_sets.acl for %d: %w", dataSetId, err)
	}
	if stored.Valid {
		return int(stored.Int32) == ACL_SECURED, nil
	}

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
	isACL := exists && strings.EqualFold(strings.TrimSpace(value), DATASET_READ_RESTRICTED)
	if err := PersistDatasetACL(ctx, db, dataSetId, isACL); err != nil {
		return false, err
	}
	return isACL, nil
}

// PersistDatasetACL stores a resolved acl value as 1 or 0 when the column is still NULL.
func PersistDatasetACL(ctx context.Context, db *harmonydb.DB, dataSetId uint64, acl bool) error {
	value := ACL_PUBLIC
	if acl {
		value = ACL_SECURED
	}
	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_data_sets
			SET acl = $1
			WHERE id = $2 AND acl IS NULL
		`, value, dataSetId)
		if err != nil {
			return false, xerrors.Errorf("updating pdp_data_sets.acl for %d: %w", dataSetId, err)
		}
		if n == 0 {
			var stored sql.NullInt32
			if err := tx.QueryRow(`SELECT acl FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&stored); err != nil {
				return false, xerrors.Errorf("re-reading pdp_data_sets.acl for %d: %w", dataSetId, err)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())
	return err
}
