package pdp

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"slices"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
)

// IPFSIndexingFromExtraData returns whether create extraData encodes withIPFSIndexing.
// known is false when extraData is empty or cannot be decoded as FWSS create metadata.
func IPFSIndexingFromExtraData(extraData []byte) (known bool, ipfsIndexing bool) {
	if len(extraData) == 0 {
		return false, false
	}

	payload, err := DecodeFWSSCreateIdentityFromExtraData(extraData)
	if err != nil {
		log.Debugw("Failed to decode extraData for IPFS indexing intent", "error", err)
		return false, false
	}

	return true, slices.Contains(payload.MetadataKeys, "withIPFSIndexing")
}

// CheckIfIndexingNeededFromExtraData checks if extraData contains withIPFSIndexing metadata.
// Used for CreateDataSet+AddPieces where the data set row does not exist yet.
func CheckIfIndexingNeededFromExtraData(extraData []byte) (bool, error) {
	known, ipfsIndexing := IPFSIndexingFromExtraData(extraData)
	if !known {
		return false, nil
	}
	if ipfsIndexing {
		log.Debugw("Found withIPFSIndexing in extraData metadata keys")
	}
	return ipfsIndexing, nil
}

// ResolveDatasetIPFSIndexing returns the cached withIPFSIndexing intent for a data set.
// If pdp_data_sets.ipfs_indexing is NULL, it reads chain metadata, persists the result, and
// when true repairs any pieces that were never marked for indexing.
// Chain/read failures return an error so callers do not soft-fail into a permanent opt-out.
func ResolveDatasetIPFSIndexing(
	ctx context.Context,
	db *harmonydb.DB,
	ethClient ethchain.EthClient,
	dataSetId uint64,
) (bool, error) {
	ipfsIndexing, err := readDatasetIPFSIndexing(ctx, db, dataSetId)
	if err != nil {
		return false, err
	}
	if ipfsIndexing != nil {
		return *ipfsIndexing, nil
	}

	mustIndex, err := fetchFromChainIPFSIndexing(ctx, ethClient, dataSetId)
	if err != nil {
		return false, err
	}

	if err := PersistDatasetIPFSIndexing(ctx, db, dataSetId, mustIndex); err != nil {
		return false, err
	}
	return mustIndex, nil
}

func readDatasetIPFSIndexing(ctx context.Context, db *harmonydb.DB, dataSetId uint64) (*bool, error) {
	var ipfsIndexing sql.NullBool
	err := db.QueryRow(ctx, `SELECT ipfs_indexing FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&ipfsIndexing)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("data set %d not found", dataSetId)
	}
	if err != nil {
		return nil, xerrors.Errorf("reading pdp_data_sets.ipfs_indexing for %d: %w", dataSetId, err)
	}
	if !ipfsIndexing.Valid {
		return nil, nil
	}
	v := ipfsIndexing.Bool
	return &v, nil
}

func fetchFromChainIPFSIndexing(ctx context.Context, ethClient ethchain.EthClient, dataSetId uint64) (bool, error) {
	pdpVerifier, err := contract.NewPDPVerifierCaller(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return false, xerrors.Errorf("instantiate PDPVerifier: %w", err)
	}

	setID := new(big.Int).SetUint64(dataSetId)
	listenerAddr, err := pdpVerifier.GetDataSetListener(contract.EthCallOpts(ctx), setID)
	if err != nil {
		return false, xerrors.Errorf("GetDataSetListener(%d): %w", dataSetId, err)
	}

	mustIndex, _, err := contract.GetDataSetMetadataAtKey(ctx, listenerAddr, ethClient, setID, "withIPFSIndexing")
	if err != nil {
		return false, xerrors.Errorf("GetDataSetMetadataAtKey(%d, withIPFSIndexing): %w", dataSetId, err)
	}
	return mustIndex, nil
}

// PersistDatasetIPFSIndexing stores a resolved ipfs_indexing value when still NULL.
// When ipfsIndexing is true, marks unindexed piecerefs for the data set as needing indexing.
func PersistDatasetIPFSIndexing(ctx context.Context, db *harmonydb.DB, dataSetId uint64, ipfsIndexing bool) error {
	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_data_sets
			SET ipfs_indexing = $1
			WHERE id = $2 AND ipfs_indexing IS NULL
		`, ipfsIndexing, dataSetId)
		if err != nil {
			return false, xerrors.Errorf("updating pdp_data_sets.ipfs_indexing for %d: %w", dataSetId, err)
		}
		if n == 0 {
			// Already resolved concurrently; still repair if the stored value is true.
			var stored sql.NullBool
			if err := tx.QueryRow(`SELECT ipfs_indexing FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&stored); err != nil {
				return false, xerrors.Errorf("re-reading pdp_data_sets.ipfs_indexing for %d: %w", dataSetId, err)
			}
			if !stored.Valid || !stored.Bool {
				return true, nil
			}
			ipfsIndexing = true
		}

		if ipfsIndexing {
			if err := RepairIndexingForDataSetInTx(tx, dataSetId); err != nil {
				return false, err
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())
	return err
}

// RepairIndexingForDataSetInTx marks piecerefs belonging to the data set that look like
// a missed indexing opt-in (needs_indexing=false, indexed_at IS NULL).
func RepairIndexingForDataSetInTx(tx *harmonydb.Tx, dataSetId uint64) error {
	_, err := tx.Exec(`
		UPDATE pdp_piecerefs pr
		SET needs_indexing = TRUE
		WHERE pr.needs_indexing = FALSE
		  AND pr.indexed_at IS NULL
		  AND pr.id IN (
			SELECT pdp_pieceref FROM pdp_data_set_pieces WHERE data_set = $1 AND pdp_pieceref IS NOT NULL
			UNION
			SELECT pdp_pieceref FROM pdp_data_set_piece_adds WHERE data_set = $1 AND pdp_pieceref IS NOT NULL
			UNION
			SELECT dspa.pdp_pieceref
			FROM pdp_data_set_piece_adds dspa
			JOIN pdp_data_sets ds ON ds.create_message_hash = dspa.add_message_hash
			WHERE ds.id = $1 AND dspa.pdp_pieceref IS NOT NULL
		  )
	`, dataSetId)
	if err != nil {
		return xerrors.Errorf("repairing indexing flags for data set %d: %w", dataSetId, err)
	}
	return nil
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
