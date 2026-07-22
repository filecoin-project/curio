package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
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
	receiptLanded receiptOutcome = iota
	receiptStuck
	receiptLost
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

// ConsiderOutstandingReceipts (prove phase 1): read-only vs chain. Materialize any
// Create/Add waits whose effects are already on chain so local state matches what we
// must prove against. Does not send transactions — chain state for this proof is fixed.
// Returns waits still outstanding for post-prove resolve (rebroadcast / mark lost).
func ConsiderOutstandingReceipts(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, dataSetId int64) ([]OutstandingReceipt, error) {
	waits, err := selectOutstandingReceipts(ctx, db, dataSetId)
	if err != nil || len(waits) == 0 {
		return waits, err
	}

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, eth)
	if err != nil {
		return nil, xerrors.Errorf("PDPVerifier: %w", err)
	}

	remaining := make([]OutstandingReceipt, 0, len(waits))
	for _, w := range waits {
		outcome, pieceIDs, _, evalErr := evaluateReceipt(ctx, db, eth, verifier, w)
		if evalErr != nil {
			log.Warnw("failed to evaluate receipt before prove", "txHash", w.TxHash, "error", evalErr)
			remaining = append(remaining, w)
			continue
		}
		if outcome != receiptLanded {
			remaining = append(remaining, w)
			continue
		}
		if applyErr := materializeLandedReceipt(ctx, db, w, pieceIDs); applyErr != nil {
			log.Warnw("failed to materialize landed receipt before prove", "txHash", w.TxHash, "error", applyErr)
			remaining = append(remaining, w)
		}
	}
	return remaining, nil
}

// ResolveRemainingReceipts (prove phase 2): after the proof, try to fix outstanding
// waits by sending messages — materialize if landed, rebroadcast if stuck, mark failed
// if lost past the proof-window age gate.
func ResolveRemainingReceipts(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, waits []OutstandingReceipt, proveAtEpoch *int64, height int64) error {
	if len(waits) == 0 {
		return nil
	}

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, eth)
	if err != nil {
		return xerrors.Errorf("PDPVerifier: %w", err)
	}

	var firstErr error
	for _, w := range waits {
		outcome, pieceIDs, signedTx, evalErr := evaluateReceipt(ctx, db, eth, verifier, w)
		if evalErr != nil {
			log.Warnw("failed to evaluate receipt after prove", "txHash", w.TxHash, "error", evalErr)
			if firstErr == nil {
				firstErr = evalErr
			}
			continue
		}

		switch outcome {
		case receiptLanded:
			if err := materializeLandedReceipt(ctx, db, w, pieceIDs); err != nil {
				log.Warnw("failed to materialize landed receipt after prove", "txHash", w.TxHash, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}

		case receiptStuck:
			if signedTx == nil {
				err := xerrors.Errorf("no signed tx available to rebroadcast for %s", w.TxHash)
				log.Warnw("failed to rebroadcast stuck receipt tx", "txHash", w.TxHash, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			sendErr := eth.SendTransaction(ctx, signedTx)
			if sendErr != nil {
				msg := strings.ToLower(sendErr.Error())
				if !strings.Contains(msg, "already known") &&
					!strings.Contains(msg, "nonce too low") &&
					!strings.Contains(msg, "known transaction") {
					log.Warnw("failed to rebroadcast stuck receipt tx", "txHash", w.TxHash, "error", sendErr)
					if firstErr == nil {
						firstErr = sendErr
					}
					continue
				}
				log.Infow("rebroadcast receipt tx already known to eth node", "txHash", w.TxHash, "error", sendErr)
			}
			log.Infow("rebroadcasted stuck receipt tx after prove", "txHash", w.TxHash)

		case receiptLost:
			if proveAtEpoch == nil || height < *proveAtEpoch+StaleReceiptWaitEpochs {
				log.Infow("receipt effects still missing; deferring lost mark",
					"txHash", w.TxHash, "height", height, "proveAtEpoch", proveAtEpoch)
				continue
			}
			n, err := db.Exec(ctx, `
				UPDATE message_waits_eth SET
					waiter_machine_id = NULL,
					tx_status = 'failed',
					tx_success = FALSE
				WHERE signed_tx_hash = $1 AND tx_status = 'pending'
			`, normalizeTxHash(w.TxHash))
			if err != nil {
				log.Warnw("failed to mark lost receipt as failed", "txHash", w.TxHash, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			} else if n > 1 {
				err = xerrors.Errorf("expected to update 0 or 1 wait rows for %s, updated %d", w.TxHash, n)
				log.Warnw("failed to mark lost receipt as failed", "txHash", w.TxHash, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			} else {
				log.Warnw("marked lost receipt as failed after prove",
					"txHash", w.TxHash, "height", height, "proveAtEpoch", *proveAtEpoch)
			}
		}
	}
	return firstErr
}

func selectOutstandingReceipts(ctx context.Context, db *harmonydb.DB, dataSetId int64) ([]OutstandingReceipt, error) {
	var rows []struct {
		TxHash    string         `db:"tx_hash"`
		DataSet   sql.NullInt64  `db:"data_set"`
		HasCreate bool           `db:"has_create"`
		Service   sql.NullString `db:"service"`
	}
	err := db.Select(ctx, &rows, `
		SELECT DISTINCT
			LOWER(TRIM(BOTH FROM a.add_message_hash)) AS tx_hash,
			a.data_set,
			EXISTS (
				SELECT 1 FROM pdp_data_set_creates c
				WHERE LOWER(TRIM(BOTH FROM c.create_message_hash)) = LOWER(TRIM(BOTH FROM a.add_message_hash))
				  AND c.data_set_created = FALSE AND c.ok IS NULL
			) AS has_create,
			(
				SELECT c.service FROM pdp_data_set_creates c
				WHERE LOWER(TRIM(BOTH FROM c.create_message_hash)) = LOWER(TRIM(BOTH FROM a.add_message_hash))
				LIMIT 1
			) AS service
		FROM pdp_data_set_piece_adds a
		INNER JOIN message_waits_eth mwe
			ON mwe.signed_tx_hash = LOWER(TRIM(BOTH FROM a.add_message_hash))
		WHERE a.data_set = $1
		  AND a.pieces_added = FALSE
		  AND a.add_message_ok IS NULL
		  AND mwe.tx_status = 'pending'
		ORDER BY tx_hash
	`, dataSetId)
	if err != nil {
		return nil, xerrors.Errorf("selecting outstanding receipts for data set %d: %w", dataSetId, err)
	}

	out := make([]OutstandingReceipt, 0, len(rows))
	for _, r := range rows {
		w := OutstandingReceipt{TxHash: r.TxHash, HasAddPiece: true, HasCreate: r.HasCreate, DataSet: r.DataSet}
		if r.Service.Valid {
			w.Service = r.Service.String
		}
		out = append(out, w)
	}
	return out, nil
}

// evaluateReceipt walks chain state for one wait:
// dataset live? → match piece CIDs on chain → landed; else check nonce → stuck or lost.
func evaluateReceipt(ctx context.Context, db *harmonydb.DB, eth ethchain.EthClient, verifier *contract.PDPVerifier, wait OutstandingReceipt) (receiptOutcome, map[uint64]int64, *types.Transaction, error) {
	if !wait.DataSet.Valid {
		return 0, nil, nil, xerrors.Errorf("outstanding receipt %s has no data set id", wait.TxHash)
	}
	dataSetId := wait.DataSet.Int64

	live, err := verifier.DataSetLive(contract.EthCallOpts(ctx), big.NewInt(dataSetId))
	if err != nil {
		return 0, nil, nil, xerrors.Errorf("DataSetLive(%d): %w", dataSetId, err)
	}
	if live {
		pieceIDs, allFound, matchErr := matchPieceAddsOnChain(ctx, db, verifier, dataSetId, wait.TxHash)
		if matchErr != nil {
			return 0, nil, nil, matchErr
		}
		if allFound {
			return receiptLanded, pieceIDs, nil, nil
		}
	}

	// Effects missing: stuck if we can still rebroadcast, otherwise lost.
	var fromAddress string
	var nonce sql.NullInt64
	var signedTxBytes []byte
	err = db.QueryRow(ctx, `
		SELECT from_address, nonce, signed_tx
		FROM message_sends_eth
		WHERE LOWER(TRIM(BOTH FROM signed_hash)) = $1
		ORDER BY send_time DESC NULLS LAST
		LIMIT 1
	`, normalizeTxHash(wait.TxHash)).Scan(&fromAddress, &nonce, &signedTxBytes)
	if errors.Is(err, sql.ErrNoRows) || len(signedTxBytes) == 0 || !nonce.Valid {
		return receiptLost, nil, nil, nil
	}
	if err != nil {
		return 0, nil, nil, xerrors.Errorf("loading signed receipt tx %s: %w", wait.TxHash, err)
	}

	signedTx := new(types.Transaction)
	if err := signedTx.UnmarshalBinary(signedTxBytes); err != nil {
		return 0, nil, nil, xerrors.Errorf("unmarshaling signed receipt tx %s: %w", wait.TxHash, err)
	}

	pendingNonce, err := eth.PendingNonceAt(ctx, common.HexToAddress(fromAddress))
	if err != nil {
		return 0, nil, nil, xerrors.Errorf("getting pending nonce for %s: %w", fromAddress, err)
	}
	if pendingNonce <= uint64(nonce.Int64) {
		return receiptStuck, nil, signedTx, nil
	}
	return receiptLost, nil, signedTx, nil
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
		log.Infow("materialized receipt from chain state during prove",
			"txHash", wait.TxHash, "dataSetId", dataSetId, "hasCreate", wait.HasCreate, "hasAddPiece", false)
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

	log.Infow("materialized receipt from chain state during prove",
		"txHash", wait.TxHash, "dataSetId", dataSetId,
		"hasCreate", wait.HasCreate, "hasAddPiece", wait.HasAddPiece)
	return nil
}
