package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"

	"github.com/filecoin-project/lotus/build"
)

// OutstandingReceipt is a pending CreateDataset and/or AddPiece wait for a dataset.
type OutstandingReceipt struct {
	TxHash      string
	HasCreate   bool
	HasAddPiece bool
	DataSet     sql.NullInt64
	Service     string
}

type receiptOutcome int

const (
	receiptStuck receiptOutcome = iota
	receiptLost
	receiptDeferred // leave pending; cannot safely conclude lost
)

type pieceAddIntentRow struct {
	AddMessageIndex uint64 `db:"add_message_index"`
	Piece           string `db:"piece"`
	SubPiece        string `db:"sub_piece"`
	SubPieceOffset  int64  `db:"sub_piece_offset"`
	SubPieceSize    int64  `db:"sub_piece_size"`
	PDPPieceRefID   int64  `db:"pdp_pieceref"`
	PieceRawSize    uint64 `db:"piece_raw_size"`
}

func normalizeTxHash(txHash string) string {
	return strings.ToLower(strings.TrimSpace(txHash))
}

// provingPeriodsPerDay is how many MaxProvingPeriod windows fit in one day,
// derived from contract PDP config (epochs/day ÷ MaxProvingPeriod).
func provingPeriodsPerDay(maxProvingPeriod uint64) uint64 {
	if maxProvingPeriod == 0 {
		return 1
	}
	epochsPerDay := uint64((24 * time.Hour / time.Second) / time.Duration(build.BlockDelaySecs))
	n := epochsPerDay / maxProvingPeriod
	if n == 0 {
		return 1 // proving period longer than a day
	}
	return n
}

// StaleReceiptAge is how old a Create/Add wait must be before the 8h chain-sync
// task reconciles it: 1/x of a day, where x is proving periods per day from the
// contract MaxProvingPeriod.
func StaleReceiptAge(maxProvingPeriod uint64) time.Duration {
	return (24 * time.Hour) / time.Duration(provingPeriodsPerDay(maxProvingPeriod))
}

// SyncStaleCreateAddReceipts reconciles pending CreateDataSet / AddPiece waits older
// than 1/(proving periods per day) of a day by querying chain state (and rebroadcasting
// or marking lost when effects are still missing). Intended for the 8h chain-sync timer.
func SyncStaleCreateAddReceipts(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient) error {
	maxPeriod, err := readMaxProvingPeriod(ctx, eth)
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-StaleReceiptAge(maxPeriod))

	waits, err := selectStaleOutstandingReceipts(ctx, db, cutoff)
	if err != nil {
		return err
	}
	if len(waits) == 0 {
		log.Debugw("no stale Create/Add receipt waits to reconcile",
			"maxProvingPeriod", maxPeriod,
			"provingPeriodsPerDay", provingPeriodsPerDay(maxPeriod),
			"ageCutoff", cutoff)
		return nil
	}

	log.Infow("reconciling stale Create/Add receipt waits",
		"count", len(waits),
		"maxProvingPeriod", maxPeriod,
		"provingPeriodsPerDay", provingPeriodsPerDay(maxPeriod),
		"ageCutoff", cutoff)

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, eth)
	if err != nil {
		return xerrors.Errorf("PDPVerifier: %w", err)
	}

	var firstErr error
	for _, w := range waits {
		if err := reconcileStaleReceipt(ctx, db, eth, verifier, w); err != nil {
			log.Warnw("failed to reconcile stale receipt", "txHash", w.TxHash, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func readMaxProvingPeriod(ctx context.Context, eth ethchain.EthClient) (uint64, error) {
	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, sAddr, eth)
	if err != nil {
		return 0, xerrors.Errorf("resolving FWSS view address: %w", err)
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, eth)
	if err != nil {
		return 0, xerrors.Errorf("instantiating FWSS state view: %w", err)
	}
	config, err := fwssv.GetPDPConfig(contract.EthCallOpts(ctx))
	if err != nil {
		return 0, xerrors.Errorf("GetPDPConfig: %w", err)
	}
	if config.MaxProvingPeriod == 0 {
		return 0, xerrors.Errorf("contract MaxProvingPeriod is zero")
	}
	return config.MaxProvingPeriod, nil
}

func fwssStateView(ctx context.Context, eth ethchain.EthClient) (*FWSS.FilecoinWarmStorageServiceStateView, error) {
	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, sAddr, eth)
	if err != nil {
		return nil, xerrors.Errorf("resolving FWSS view address: %w", err)
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, eth)
	if err != nil {
		return nil, xerrors.Errorf("instantiating FWSS state view: %w", err)
	}
	return fwssv, nil
}

func reconcileStaleReceipt(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, verifier *contract.PDPVerifier, wait OutstandingReceipt) error {
	// Prefer chain state (clientNonces / piece live checks). Do not rely on
	// eth TransactionReceipt/TransactionByHash — many nodes prune those.
	landed, err := tryMaterializeFromChainState(ctx, db, eth, verifier, &wait)
	if err != nil {
		return err
	}
	if landed {
		return nil
	}

	outcome, signedTx, evalErr := classifyMissingReceipt(ctx, db, eth, wait)
	if evalErr != nil {
		return evalErr
	}
	switch outcome {
	case receiptStuck:
		return rebroadcastStuckReceipt(ctx, eth, wait, signedTx)
	case receiptLost:
		if wait.HasCreate {
			created, known, checkErr := checkCreateLandedViaClientNonces(ctx, db, eth, wait, signedTx)
			if checkErr != nil {
				return checkErr
			}
			if !known {
				log.Warnw("cannot confirm Create receipt lost (no create identity); leaving pending",
					"txHash", wait.TxHash)
				return nil
			}
			if created {
				log.Warnw("Create landed on chain (clientNonces) but not fully materialized; leaving pending",
					"txHash", wait.TxHash)
				return nil
			}
		}
		return markReceiptLost(ctx, db, wait)
	case receiptDeferred:
		return nil
	default:
		return nil
	}
}

func tryMaterializeFromChainState(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, verifier *contract.PDPVerifier, wait *OutstandingReceipt) (bool, error) {
	if wait.HasCreate && !wait.DataSet.Valid {
		id, found, err := discoverDataSetViaClientNonces(ctx, db, eth, wait)
		if err != nil {
			return false, err
		}
		if found {
			wait.DataSet = sql.NullInt64{Int64: id, Valid: true}
			live, liveErr := verifier.DataSetLive(contract.EthCallOpts(ctx), big.NewInt(id))
			if liveErr != nil {
				return false, xerrors.Errorf("DataSetLive(%d): %w", id, liveErr)
			}
			if !live {
				return false, nil
			}
			if err := ensureDataSetRow(ctx, db, eth, verifier, *wait, id); err != nil {
				return false, err
			}
			if !wait.HasAddPiece {
				if err := materializeLandedReceipt(ctx, db, *wait, nil); err != nil {
					return false, err
				}
				return true, nil
			}
			pieceIDs, allFound, matchErr := matchPieceAddsOnChain(ctx, db, verifier, id, wait.TxHash)
			if matchErr != nil {
				return false, matchErr
			}
			if !allFound {
				return false, nil
			}
			if err := materializeLandedReceipt(ctx, db, *wait, pieceIDs); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	if wait.HasAddPiece && wait.DataSet.Valid {
		pieceIDs, allFound, err := matchLivePieceAdds(ctx, db, verifier, wait.DataSet.Int64, wait.TxHash)
		if err != nil {
			return false, err
		}
		if !allFound {
			return false, nil
		}
		if err := materializeLandedReceipt(ctx, db, *wait, pieceIDs); err != nil {
			return false, err
		}
		return true, nil
	}

	if wait.HasCreate && !wait.HasAddPiece {
		// Create-only fallback: local data_set row already present (e.g. prior partial recovery).
		var id int64
		err := db.QueryRow(ctx, `
			SELECT id FROM pdp_data_sets
			WHERE LOWER(TRIM(BOTH FROM create_message_hash)) = $1
			LIMIT 1
		`, normalizeTxHash(wait.TxHash)).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, xerrors.Errorf("looking up created data set for %s: %w", wait.TxHash, err)
		}
		wait.DataSet = sql.NullInt64{Int64: id, Valid: true}
		live, liveErr := verifier.DataSetLive(contract.EthCallOpts(ctx), big.NewInt(id))
		if liveErr != nil {
			return false, xerrors.Errorf("DataSetLive(%d): %w", id, liveErr)
		}
		if !live {
			return false, nil
		}
		if err := materializeLandedReceipt(ctx, db, *wait, nil); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

var errNoCreateIdentity = xerrors.New("no create identity available")

func discoverDataSetViaClientNonces(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, wait *OutstandingReceipt) (int64, bool, error) {
	identity, err := resolveCreateIdentity(ctx, db, wait, nil)
	if err != nil {
		if errors.Is(err, errNoCreateIdentity) {
			log.Debugw("create identity unavailable for clientNonces lookup",
				"txHash", wait.TxHash, "error", err)
			return 0, false, nil
		}
		return 0, false, err
	}
	return lookupDataSetIdViaClientNonces(ctx, eth, identity)
}

func resolveCreateIdentity(ctx context.Context, db *harmonydb.DB, wait *OutstandingReceipt, signedTx *types.Transaction) (*pdp.FWSSCreateIdentity, error) {
	var extraData []byte
	err := db.QueryRow(ctx, `
		SELECT extra_data FROM pdp_data_set_creates
		WHERE LOWER(TRIM(BOTH FROM create_message_hash)) = $1
		LIMIT 1
	`, normalizeTxHash(wait.TxHash)).Scan(&extraData)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, xerrors.Errorf("loading create extra_data for %s: %w", wait.TxHash, err)
	}
	if len(extraData) > 0 {
		identity, decErr := pdp.DecodeFWSSCreateIdentityFromExtraData(extraData)
		if decErr == nil {
			return identity, nil
		}
		log.Debugw("failed to decode persisted create extra_data", "txHash", wait.TxHash, "error", decErr)
	}

	if signedTx == nil {
		var signedTxBytes []byte
		loadErr := db.QueryRow(ctx, `
			SELECT signed_tx
			FROM message_sends_eth
			WHERE LOWER(TRIM(BOTH FROM signed_hash)) = $1
			ORDER BY send_time DESC NULLS LAST
			LIMIT 1
		`, normalizeTxHash(wait.TxHash)).Scan(&signedTxBytes)
		if loadErr != nil && !errors.Is(loadErr, sql.ErrNoRows) {
			return nil, xerrors.Errorf("loading signed tx for %s: %w", wait.TxHash, loadErr)
		}
		if len(signedTxBytes) > 0 {
			tx := new(types.Transaction)
			if uerr := tx.UnmarshalBinary(signedTxBytes); uerr != nil {
				return nil, xerrors.Errorf("unmarshaling signed tx %s: %w", wait.TxHash, uerr)
			}
			signedTx = tx
		}
	}
	if signedTx == nil {
		return nil, errNoCreateIdentity
	}
	identity, decErr := pdp.CreateIdentityFromSignedTx(signedTx)
	if decErr != nil {
		return nil, errNoCreateIdentity
	}
	return identity, nil
}

func lookupDataSetIdViaClientNonces(ctx context.Context, eth ethchain.EthClient, identity *pdp.FWSSCreateIdentity) (int64, bool, error) {
	if identity == nil || identity.ClientDataSetId == nil {
		return 0, false, nil
	}
	if identity.Payer == (common.Address{}) {
		return 0, false, nil
	}
	fwssv, err := fwssStateView(ctx, eth)
	if err != nil {
		return 0, false, err
	}
	raw, err := fwssv.ClientNonces(contract.EthCallOpts(ctx), identity.Payer, identity.ClientDataSetId)
	if err != nil {
		return 0, false, xerrors.Errorf("clientNonces(%s, %s): %w", identity.Payer.Hex(), identity.ClientDataSetId.String(), err)
	}
	if raw == nil || raw.Sign() == 0 {
		return 0, false, nil
	}
	id := dataSetIdFromClientNonce(raw)
	if id <= 0 {
		return 0, false, nil
	}
	return id, true, nil
}

// dataSetIdFromClientNonce returns the data set id stored in clientNonces.
// Create stores the bare id; AddPieces packs nextPieceId in the upper 128 bits.
func dataSetIdFromClientNonce(v *big.Int) int64 {
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	id := new(big.Int).And(v, mask)
	if !id.IsInt64() {
		return 0
	}
	return id.Int64()
}

func checkCreateLandedViaClientNonces(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, wait OutstandingReceipt, signedTx *types.Transaction) (created bool, known bool, err error) {
	identity, idErr := resolveCreateIdentity(ctx, db, &wait, signedTx)
	if idErr != nil {
		if errors.Is(idErr, errNoCreateIdentity) {
			return false, false, nil
		}
		return false, false, idErr
	}
	_, found, lookErr := lookupDataSetIdViaClientNonces(ctx, eth, identity)
	if lookErr != nil {
		return false, false, lookErr
	}
	return found, true, nil
}

func ensureDataSetRow(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, verifier *contract.PDPVerifier, wait OutstandingReceipt, dataSetId int64) error {
	var existing int64
	err := db.QueryRow(ctx, `SELECT id FROM pdp_data_sets WHERE id = $1`, dataSetId).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return xerrors.Errorf("checking pdp_data_sets %d: %w", dataSetId, err)
	}

	service := wait.Service
	var extraData []byte
	var createService string
	lookupErr := db.QueryRow(ctx, `
		SELECT service, extra_data FROM pdp_data_set_creates
		WHERE LOWER(TRIM(BOTH FROM create_message_hash)) = $1
		LIMIT 1
	`, normalizeTxHash(wait.TxHash)).Scan(&createService, &extraData)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return xerrors.Errorf("loading create row for %s: %w", wait.TxHash, lookupErr)
	}
	if service == "" {
		service = createService
	}
	if service == "" {
		return xerrors.Errorf("no service for create tx %s when inserting data set %d", wait.TxHash, dataSetId)
	}

	listenerAddr, err := verifier.GetDataSetListener(contract.EthCallOpts(ctx), big.NewInt(dataSetId))
	if err != nil {
		return xerrors.Errorf("GetDataSetListener(%d): %w", dataSetId, err)
	}
	provingPeriod, challengeWindow, err := getProvingPeriodChallengeWindow(ctx, eth, listenerAddr)
	if err != nil {
		return xerrors.Errorf("proving schedule for data set %d: %w", dataSetId, err)
	}

	var ipfsIndexingArg any
	if known, ipfsIndexing := pdp.IPFSIndexingFromExtraData(extraData); known {
		ipfsIndexingArg = ipfsIndexing
	}

	n, err := db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, ipfs_indexing)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, dataSetId, normalizeTxHash(wait.TxHash), service, provingPeriod, challengeWindow, ipfsIndexingArg)
	if err != nil {
		return xerrors.Errorf("inserting pdp_data_sets %d: %w", dataSetId, err)
	}
	if n == 1 {
		log.Infow("inserted pdp_data_sets row from clientNonces recovery",
			"txHash", wait.TxHash, "dataSetId", dataSetId, "service", service, "ipfsIndexing", ipfsIndexingArg)
	}
	if ipfsIndexingArg == true {
		// Persist path also repairs missed needs_indexing flags for create-and-add races.
		if err := pdp.PersistDatasetIPFSIndexing(ctx, db, uint64(dataSetId), true); err != nil {
			return err
		}
	}
	return nil
}

func matchLivePieceAdds(ctx context.Context, db *harmonydb.DB, verifier *contract.PDPVerifier, dataSetId int64, txHash string) (map[uint64]int64, bool, error) {
	live, err := verifier.DataSetLive(contract.EthCallOpts(ctx), big.NewInt(dataSetId))
	if err != nil {
		return nil, false, xerrors.Errorf("DataSetLive(%d): %w", dataSetId, err)
	}
	if !live {
		return nil, false, nil
	}
	return matchPieceAddsOnChain(ctx, db, verifier, dataSetId, txHash)
}

func rebroadcastStuckReceipt(ctx context.Context, eth ethchain.EthClient, wait OutstandingReceipt, signedTx *types.Transaction) error {
	if signedTx == nil {
		return xerrors.Errorf("no signed tx available to rebroadcast for %s", wait.TxHash)
	}
	sendErr := eth.SendTransaction(ctx, signedTx)
	if sendErr != nil {
		msg := strings.ToLower(sendErr.Error())
		if !strings.Contains(msg, "already known") &&
			!strings.Contains(msg, "nonce too low") &&
			!strings.Contains(msg, "known transaction") {
			return xerrors.Errorf("rebroadcasting stuck receipt %s: %w", wait.TxHash, sendErr)
		}
		log.Infow("rebroadcast receipt tx already known to eth node", "txHash", wait.TxHash, "error", sendErr)
	}
	log.Infow("rebroadcasted stuck Create/Add receipt tx", "txHash", wait.TxHash)
	return nil
}

func markReceiptLost(ctx context.Context, db *harmonydb.DB, wait OutstandingReceipt) error {
	n, err := db.Exec(ctx, `
		UPDATE message_waits_eth SET
			waiter_machine_id = NULL,
			tx_status = 'failed',
			tx_success = FALSE
		WHERE signed_tx_hash = $1 AND tx_status = 'pending'
	`, normalizeTxHash(wait.TxHash))
	if err != nil {
		return xerrors.Errorf("marking lost receipt %s failed: %w", wait.TxHash, err)
	}
	if n > 1 {
		return xerrors.Errorf("expected to update 0 or 1 wait rows for %s, updated %d", wait.TxHash, n)
	}
	if n == 1 {
		log.Warnw("marked lost Create/Add receipt as failed", "txHash", wait.TxHash)
	}
	return nil
}

func selectStaleOutstandingReceipts(ctx context.Context, db *harmonydb.DB, olderThan time.Time) ([]OutstandingReceipt, error) {
	var rows []struct {
		TxHash      string         `db:"tx_hash"`
		DataSet     sql.NullInt64  `db:"data_set"`
		HasCreate   bool           `db:"has_create"`
		HasAddPiece bool           `db:"has_add_piece"`
		Service     sql.NullString `db:"service"`
	}
	err := db.Select(ctx, &rows, `
		SELECT tx_hash, data_set, has_create, has_add_piece, service FROM (
			SELECT DISTINCT
				LOWER(TRIM(BOTH FROM a.add_message_hash)) AS tx_hash,
				a.data_set,
				EXISTS (
					SELECT 1 FROM pdp_data_set_creates c
					WHERE LOWER(TRIM(BOTH FROM c.create_message_hash)) = LOWER(TRIM(BOTH FROM a.add_message_hash))
					  AND c.data_set_created = FALSE AND c.ok IS NULL
				) AS has_create,
				TRUE AS has_add_piece,
				(
					SELECT c.service FROM pdp_data_set_creates c
					WHERE LOWER(TRIM(BOTH FROM c.create_message_hash)) = LOWER(TRIM(BOTH FROM a.add_message_hash))
					LIMIT 1
				) AS service,
				mse.send_time
			FROM pdp_data_set_piece_adds a
			INNER JOIN message_waits_eth mwe
				ON mwe.signed_tx_hash = LOWER(TRIM(BOTH FROM a.add_message_hash))
			INNER JOIN message_sends_eth mse
				ON LOWER(TRIM(BOTH FROM mse.signed_hash)) = LOWER(TRIM(BOTH FROM a.add_message_hash))
			WHERE a.pieces_added = FALSE
			  AND a.add_message_ok IS NULL
			  AND mwe.tx_status = 'pending'
			  AND mse.send_time IS NOT NULL

			UNION ALL

			SELECT DISTINCT
				LOWER(TRIM(BOTH FROM c.create_message_hash)) AS tx_hash,
				NULL::bigint AS data_set,
				TRUE AS has_create,
				FALSE AS has_add_piece,
				c.service AS service,
				mse.send_time
			FROM pdp_data_set_creates c
			INNER JOIN message_waits_eth mwe
				ON mwe.signed_tx_hash = LOWER(TRIM(BOTH FROM c.create_message_hash))
			INNER JOIN message_sends_eth mse
				ON LOWER(TRIM(BOTH FROM mse.signed_hash)) = LOWER(TRIM(BOTH FROM c.create_message_hash))
			WHERE c.data_set_created = FALSE
			  AND c.ok IS NULL
			  AND mwe.tx_status = 'pending'
			  AND mse.send_time IS NOT NULL
			  AND NOT EXISTS (
				SELECT 1 FROM pdp_data_set_piece_adds a
				WHERE LOWER(TRIM(BOTH FROM a.add_message_hash)) = LOWER(TRIM(BOTH FROM c.create_message_hash))
			  )
		) pending
		WHERE send_time < $1
		ORDER BY tx_hash
	`, olderThan)
	if err != nil {
		return nil, xerrors.Errorf("selecting stale Create/Add receipts: %w", err)
	}

	out := make([]OutstandingReceipt, 0, len(rows))
	for _, r := range rows {
		w := OutstandingReceipt{
			TxHash:      r.TxHash,
			HasCreate:   r.HasCreate,
			HasAddPiece: r.HasAddPiece,
			DataSet:     r.DataSet,
		}
		if r.Service.Valid {
			w.Service = r.Service.String
		}
		out = append(out, w)
	}
	return out, nil
}

func classifyMissingReceipt(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, wait OutstandingReceipt) (receiptOutcome, *types.Transaction, error) {
	var fromAddress string
	var nonce sql.NullInt64
	var signedTxBytes []byte
	err := db.QueryRow(ctx, `
		SELECT from_address, nonce, signed_tx
		FROM message_sends_eth
		WHERE LOWER(TRIM(BOTH FROM signed_hash)) = $1
		ORDER BY send_time DESC NULLS LAST
		LIMIT 1
	`, normalizeTxHash(wait.TxHash)).Scan(&fromAddress, &nonce, &signedTxBytes)
	if errors.Is(err, sql.ErrNoRows) || len(signedTxBytes) == 0 || !nonce.Valid {
		// Without send metadata we cannot safely rebroadcast or conclude lost.
		return receiptDeferred, nil, nil
	}
	if err != nil {
		return 0, nil, xerrors.Errorf("loading signed receipt tx %s: %w", wait.TxHash, err)
	}

	signedTx := new(types.Transaction)
	if err := signedTx.UnmarshalBinary(signedTxBytes); err != nil {
		return 0, nil, xerrors.Errorf("unmarshaling signed receipt tx %s: %w", wait.TxHash, err)
	}

	pendingNonce, err := eth.PendingNonceAt(ctx, common.HexToAddress(fromAddress))
	if err != nil {
		return 0, nil, xerrors.Errorf("getting pending nonce for %s: %w", fromAddress, err)
	}
	if pendingNonce <= uint64(nonce.Int64) {
		return receiptStuck, signedTx, nil
	}
	return receiptLost, signedTx, nil
}

func matchPieceAddsOnChain(ctx context.Context, db *harmonydb.DB, verifier *contract.PDPVerifier, dataSetId int64, txHash string) (map[uint64]int64, bool, error) {
	var rows []pieceAddIntentRow
	err := db.Select(ctx, &rows, `
		SELECT a.add_message_index, a.piece, a.sub_piece, a.sub_piece_offset, a.sub_piece_size,
		       a.pdp_pieceref, pp.piece_raw_size
		FROM pdp_data_set_piece_adds a
		JOIN pdp_piecerefs ppr ON ppr.id = a.pdp_pieceref
		JOIN parked_piece_refs pprf ON pprf.ref_id = ppr.piece_ref
		JOIN parked_pieces pp ON pp.id = pprf.piece_id
		WHERE LOWER(TRIM(BOTH FROM a.add_message_hash)) = $1
		ORDER BY a.add_message_index ASC, a.sub_piece_offset ASC
	`, normalizeTxHash(txHash))
	if err != nil {
		return nil, false, xerrors.Errorf("loading piece add intents for %s: %w", txHash, err)
	}
	if len(rows) == 0 {
		return nil, false, xerrors.Errorf("no piece_adds rows for tx %s", txHash)
	}

	var existingIDs []int64
	if err := db.Select(ctx, &existingIDs, `SELECT piece_id FROM pdp_data_set_pieces WHERE data_set = $1`, dataSetId); err != nil {
		return nil, false, xerrors.Errorf("loading existing piece ids for data set %d: %w", dataSetId, err)
	}
	usedIDs := make(map[int64]bool, len(existingIDs))
	for _, id := range existingIDs {
		usedIDs[id] = true
	}

	type pieceGroup struct {
		Piece   string
		RawSize uint64
	}
	groups := make(map[uint64]*pieceGroup)
	order := make([]uint64, 0)
	for _, r := range rows {
		g, ok := groups[r.AddMessageIndex]
		if !ok {
			g = &pieceGroup{Piece: r.Piece}
			groups[r.AddMessageIndex] = g
			order = append(order, r.AddMessageIndex)
		}
		g.RawSize += r.PieceRawSize
	}

	assigned := make(map[uint64]int64, len(order))
	for _, idx := range order {
		g := groups[idx]
		v1, err := cid.Parse(g.Piece)
		if err != nil {
			return nil, false, xerrors.Errorf("building piece CID v2 for tx %s index %d: %w", txHash, idx, err)
		}
		v2, err := commcid.PieceCidV2FromV1(v1, g.RawSize)
		if err != nil {
			return nil, false, xerrors.Errorf("building piece CID v2 for tx %s index %d: %w", txHash, idx, err)
		}

		ids, err := verifier.FindPieceIdsByCid(contract.EthCallOpts(ctx), big.NewInt(dataSetId), contract.CidsCid{Data: v2.Bytes()}, big.NewInt(0), big.NewInt(256))
		if err != nil {
			return nil, false, xerrors.Errorf("FindPieceIdsByCid for tx %s index %d: %w", txHash, idx, err)
		}

		matched := int64(-1)
		for _, idBig := range ids {
			if idBig == nil || !idBig.IsInt64() {
				continue
			}
			id := idBig.Int64()
			if usedIDs[id] {
				continue
			}
			pieceLive, liveErr := verifier.PieceLive(contract.EthCallOpts(ctx), big.NewInt(dataSetId), big.NewInt(id))
			if liveErr != nil {
				return nil, false, xerrors.Errorf("PieceLive(%d,%d): %w", dataSetId, id, liveErr)
			}
			if !pieceLive {
				continue
			}
			matched = id
			break
		}
		if matched < 0 {
			return assigned, false, nil
		}
		assigned[idx] = matched
		usedIDs[matched] = true
	}
	return assigned, true, nil
}

func materializeLandedReceipt(ctx context.Context, db *harmonydb.DB, wait OutstandingReceipt, pieceIDByIndex map[uint64]int64) error {
	if !wait.DataSet.Valid {
		return xerrors.Errorf("cannot materialize receipt %s without data set id", wait.TxHash)
	}
	dataSetId := wait.DataSet.Int64
	txHash := normalizeTxHash(wait.TxHash)

	n, err := db.Exec(ctx, `
		UPDATE message_waits_eth SET
			waiter_machine_id = NULL,
			tx_status = 'confirmed',
			tx_success = TRUE
		WHERE signed_tx_hash = $1 AND tx_status = 'pending'
	`, txHash)
	if err != nil {
		return xerrors.Errorf("confirming receipt %s: %w", wait.TxHash, err)
	}
	if n > 1 {
		return xerrors.Errorf("expected to update 0 or 1 wait rows for %s, updated %d", wait.TxHash, n)
	}

	if wait.HasCreate {
		if _, err := db.Exec(ctx, `
			UPDATE pdp_data_set_creates
			SET data_set_created = TRUE, ok = TRUE
			WHERE LOWER(TRIM(BOTH FROM create_message_hash)) = $1 AND data_set_created = FALSE
		`, txHash); err != nil {
			return xerrors.Errorf("marking create materialized for %s: %w", wait.TxHash, err)
		}
	}

	if !wait.HasAddPiece {
		log.Infow("materialized Create receipt from chain state",
			"txHash", wait.TxHash, "dataSetId", dataSetId, "hasCreate", wait.HasCreate)
		return nil
	}

	var rows []pieceAddIntentRow
	err = db.Select(ctx, &rows, `
		SELECT a.add_message_index, a.piece, a.sub_piece, a.sub_piece_offset, a.sub_piece_size,
		       a.pdp_pieceref, pp.piece_raw_size
		FROM pdp_data_set_piece_adds a
		JOIN pdp_piecerefs ppr ON ppr.id = a.pdp_pieceref
		JOIN parked_piece_refs pprf ON pprf.ref_id = ppr.piece_ref
		JOIN parked_pieces pp ON pp.id = pprf.piece_id
		WHERE LOWER(TRIM(BOTH FROM a.add_message_hash)) = $1
		ORDER BY a.add_message_index ASC, a.sub_piece_offset ASC
	`, txHash)
	if err != nil {
		return xerrors.Errorf("loading piece add intents for %s: %w", wait.TxHash, err)
	}
	if len(rows) == 0 {
		return xerrors.Errorf("no piece_adds rows for tx %s", wait.TxHash)
	}
	for _, r := range rows {
		if _, ok := pieceIDByIndex[r.AddMessageIndex]; !ok {
			return xerrors.Errorf("missing on-chain piece id for tx %s index %d", wait.TxHash, r.AddMessageIndex)
		}
	}

	_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if _, err := tx.Exec(`
			UPDATE pdp_data_sets SET init_ready = true
			WHERE id = $1 AND prev_challenge_request_epoch IS NULL
			  AND challenge_request_msg_hash IS NULL AND prove_at_epoch IS NULL
		`, dataSetId); err != nil {
			return false, err
		}

		for _, entry := range rows {
			pieceId := pieceIDByIndex[entry.AddMessageIndex]
			if _, err := tx.Exec(`
				INSERT INTO pdp_data_set_pieces (
					data_set, piece, piece_id, sub_piece, sub_piece_offset, sub_piece_size,
					pdp_pieceref, add_message_hash, add_message_index
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT DO NOTHING
			`, dataSetId, entry.Piece, pieceId, entry.SubPiece, entry.SubPieceOffset, entry.SubPieceSize,
				entry.PDPPieceRefID, txHash, entry.AddMessageIndex); err != nil {
				return false, xerrors.Errorf("inserting piece id %d: %w", pieceId, err)
			}
		}

		n, err := tx.Exec(`
			UPDATE pdp_data_set_piece_adds
			SET pieces_added = TRUE, data_set = $1, add_message_ok = TRUE
			WHERE LOWER(TRIM(BOTH FROM add_message_hash)) = $2 AND pieces_added = FALSE
		`, dataSetId, txHash)
		if err != nil {
			return false, xerrors.Errorf("updating piece_adds: %w", err)
		}
		if n == 0 {
			return false, xerrors.Errorf("no piece_adds rows updated for %s", wait.TxHash)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}

	log.Infow("materialized Create/Add receipt from chain state",
		"txHash", wait.TxHash, "dataSetId", dataSetId,
		"hasCreate", wait.HasCreate, "hasAddPiece", wait.HasAddPiece)
	return nil
}
