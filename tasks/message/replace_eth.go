package message

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
)

const (
	TransactionReplaceGasBumpPercent     int64 = 120
	TransactionReplaceMaxPerSenderPerRun int   = 10
)

type EthReplacerConfig struct {
	Client ethchain.EthClient
}

type transactionReplacer struct {
	db               *harmonydb.DB
	client           ethchain.EthClient
	stuckForDuration time.Duration
}

type transactionReplaceSenderQueue struct {
	FromAddress  string
	From         common.Address
	AccountNonce uint64
	Transactions []transactionReplaceCandidate
}

type transactionReplaceCandidate struct {
	ReplacementID int64  `db:"replacement_id"`
	ClaimID       string `db:"claim_id"`

	FromAddress string `db:"from_address"`
	Nonce       uint64 `db:"nonce"`

	LatestSignedHash string `db:"latest_signed_hash"`
	LatestSignedTx   []byte `db:"latest_signed_tx"`

	RetrySigned       bool   `db:"retry_signed"`
	ClaimedSignedHash string `db:"claimed_signed_hash"`
	ClaimedSignedTx   []byte `db:"claimed_signed_tx"`

	DeleteSignedClaim bool
}

type signedTransactionReplacement struct {
	Candidate transactionReplaceCandidate
	SignedTx  *gethtypes.Transaction
}

func (t *transactionReplacer) runTransactionReplacement(ctx context.Context, height abi.ChainEpoch, tipSetTimeStamp time.Time) error {
	// This runs on the replacer goroutine, not on the chain scheduler callback path.
	//
	// Replacement run outline:
	// 1. Load stale successful Eth sends for local senders with nonce >= account nonce.
	// 2. Claim the current replacement chain tip for each nonce.
	// 3. Reuse an already-signed claim when retrying; otherwise prepare and sign a fee replacement.
	// 4. Record signed replacement data before sending it, then record the send result.
	// 5. Delete claims that should not be retried.

	stuckCutoff := tipSetTimeStamp.Add(-t.stuckForDuration)
	queues, err := t.loadTransactionCandidates(ctx, stuckCutoff, height)
	if err != nil {
		return err
	}

	replacements, claimsToDelete, err := t.signedTransactionReplacements(ctx, height, queues)
	if err != nil {
		return err
	}
	sendErr := t.sendAndRecordReplacementTransactions(ctx, replacements)
	deleteErr := t.deleteTransactionReplacementClaims(ctx, claimsToDelete)
	if sendErr != nil {
		if deleteErr != nil {
			return fmt.Errorf("sending transaction replacements: %w; deleting transaction replacement claims: %v", sendErr, deleteErr)
		}
		return sendErr
	}
	if deleteErr != nil {
		return deleteErr
	}

	count := 0
	for _, senderReplacements := range replacements {
		count += len(senderReplacements)
	}

	log.Debugw("transaction replacement candidates loaded", "senders", len(queues), "replacements", count, "deletedClaims", len(claimsToDelete), "height", height, "stuckCutoff", stuckCutoff)
	return nil
}

func (t *transactionReplacer) loadTransactionCandidates(ctx context.Context, stuckCutoff time.Time, height abi.ChainEpoch) ([]transactionReplaceSenderQueue, error) {
	var ethSenders []struct {
		FromAddress string `db:"from_address"`
	}
	if err := t.db.Select(ctx, &ethSenders, `
		SELECT address AS from_address
		FROM eth_keys`); err != nil {
		return nil, fmt.Errorf("selecting eth replacement senders: %w", err)
	}

	fromAddresses := make([]string, 0, len(ethSenders))
	senderQueues := make(map[string]*transactionReplaceSenderQueue, len(ethSenders))

	addSender := func(fromAddress string, from common.Address) {
		if _, ok := senderQueues[fromAddress]; ok {
			return
		}

		fromAddresses = append(fromAddresses, fromAddress)
		senderQueues[fromAddress] = &transactionReplaceSenderQueue{
			FromAddress: fromAddress,
			From:        from,
		}
	}

	for _, sender := range ethSenders {
		if !common.IsHexAddress(sender.FromAddress) {
			log.Warnw("skipping transaction replacement sender; invalid eth address", "from", sender.FromAddress)
			continue
		}

		from := common.HexToAddress(sender.FromAddress)
		addSender(from.Hex(), from)
	}

	activeFromAddresses := make([]string, 0, len(fromAddresses))
	accountNonces := make([]int64, 0, len(fromAddresses))
	for _, fromAddress := range fromAddresses {
		queue := senderQueues[fromAddress]
		ethCtx, cancel := context.WithTimeout(ctx, defaultEthCallTimeout)
		nonce, err := t.client.NonceAt(ethCtx, queue.From, big.NewInt(int64(height)))
		cancel()
		if err != nil {
			log.Warnw("skipping transaction replacement sender; account nonce lookup failed", "from", fromAddress, "error", err)
			continue
		}

		activeFromAddresses = append(activeFromAddresses, fromAddress)
		accountNonces = append(accountNonces, int64(nonce))
		queue.AccountNonce = nonce
	}

	deleted, err := t.db.Exec(ctx, `
		WITH sender_nonces AS (
			SELECT *
			FROM unnest($1::text[], $2::bigint[]) AS sn(from_address, account_nonce)
		)
		DELETE FROM message_send_eth_replacements mer
		USING sender_nonces sn
		WHERE mer.from_address = sn.from_address
			AND mer.nonce < sn.account_nonce
			AND (
				mer.send_success IS NOT NULL OR
				mer.claim_until < CURRENT_TIMESTAMP
			)`,
		activeFromAddresses, accountNonces)
	if err != nil {
		return nil, fmt.Errorf("deleting stale transaction replacement rows: %w", err)
	}
	if deleted > 0 {
		log.Debugw("deleted stale transaction replacement rows", "rows", deleted)
	}

	var rows []transactionReplaceCandidate
	claimID := uuid.NewString()
	// Load old successful Eth sends for active senders whose nonce has not
	// landed yet, resolve the current successful replacement chain tip for each
	// send, and claim the tip for this run. Expired unsent claims are reclaimed;
	// active claims and already-successful replacement rows are skipped. Returned
	// rows are only the claims owned by this run, including any previously signed
	// replacement transaction that should be retried instead of re-signed.
	err = t.db.Select(ctx, &rows, `
		WITH sender_nonces AS (
			SELECT *
			FROM unnest($1::text[], $2::bigint[]) AS sn(from_address, account_nonce)
		),
		candidate_sends AS (
			SELECT c.*
			FROM sender_nonces sn
			INNER JOIN LATERAL (
				SELECT
					mse.from_address,
					mse.nonce,
					mse.signed_hash,
					mse.signed_tx,
					mse.send_time
				FROM message_sends_eth mse
				WHERE mse.from_address = sn.from_address
					AND mse.send_success = TRUE
					AND mse.nonce IS NOT NULL
					AND mse.nonce >= sn.account_nonce
					AND mse.signed_hash IS NOT NULL
					AND mse.signed_tx IS NOT NULL
					AND mse.send_time IS NOT NULL
					AND mse.send_time <= $3
				ORDER BY mse.nonce
				LIMIT $4
			) c ON TRUE
		),
		eligible AS (
			SELECT
				cs.from_address,
				cs.nonce,
				cs.signed_hash AS original_signed_hash,
				COALESCE(latest.signed_hash, cs.signed_hash) AS latest_signed_hash,
				COALESCE(latest.signed_tx, cs.signed_tx) AS latest_signed_tx,
				COALESCE(latest.send_time, cs.send_time) AS latest_send_time
			FROM candidate_sends cs
			LEFT JOIN LATERAL (
				SELECT
					r.signed_hash,
					r.signed_tx,
					r.send_time
				FROM message_send_eth_replacements r
				WHERE r.from_address = cs.from_address
					AND r.nonce = cs.nonce
					AND r.send_success = TRUE
					AND r.signed_hash IS NOT NULL
					AND r.signed_tx IS NOT NULL
					AND NOT EXISTS (
						SELECT 1
						FROM message_send_eth_replacements next
						WHERE next.from_address = r.from_address
							AND next.nonce = r.nonce
							AND next.send_success = TRUE
							AND next.replaces_signed_hash = r.signed_hash
					)
				LIMIT 1
			) latest ON TRUE
			WHERE COALESCE(latest.send_time, cs.send_time) <= $3
		),
		claimed AS (
			INSERT INTO message_send_eth_replacements (
				from_address,
				nonce,
				original_signed_hash,
				replaces_signed_hash,
				claim_id
			)
			SELECT
				from_address,
				nonce,
				original_signed_hash,
				latest_signed_hash,
				$5
			FROM eligible
			ON CONFLICT (from_address, nonce, replaces_signed_hash)
				WHERE send_success IS NOT FALSE
			DO UPDATE SET
				claim_id = EXCLUDED.claim_id,
				claim_time = CURRENT_TIMESTAMP,
				claim_until = DEFAULT
			WHERE message_send_eth_replacements.send_success IS NULL
				AND message_send_eth_replacements.claim_until < CURRENT_TIMESTAMP
			RETURNING
				id AS replacement_id,
				from_address,
				nonce,
				replaces_signed_hash,
				claim_id,
				signed_hash,
				signed_tx
		)
		SELECT
			claimed.replacement_id,
			claimed.claim_id,
			eligible.from_address,
			eligible.nonce,

			eligible.latest_signed_hash,
			eligible.latest_signed_tx,

			(claimed.signed_hash IS NOT NULL AND claimed.signed_tx IS NOT NULL) AS retry_signed,
			COALESCE(claimed.signed_hash, '') AS claimed_signed_hash,
			claimed.signed_tx AS claimed_signed_tx
		FROM eligible
		INNER JOIN claimed
			ON claimed.from_address = eligible.from_address
			AND claimed.nonce = eligible.nonce
			AND claimed.replaces_signed_hash = eligible.latest_signed_hash
		ORDER BY eligible.from_address, eligible.nonce
	`, activeFromAddresses, accountNonces, stuckCutoff, TransactionReplaceMaxPerSenderPerRun, claimID)
	if err != nil {
		return nil, fmt.Errorf("selecting transaction replacement candidates: %w", err)
	}

	queues := make([]transactionReplaceSenderQueue, 0, len(senderQueues))

	for _, row := range rows {
		queue, ok := senderQueues[row.FromAddress]
		if !ok {
			return nil, fmt.Errorf("claimed transaction replacement for unknown sender %s", row.FromAddress)
		}
		queue.Transactions = append(queue.Transactions, row)
	}

	for _, fromAddress := range activeFromAddresses {
		queue := senderQueues[fromAddress]
		if queue != nil && len(queue.Transactions) > 0 {
			queues = append(queues, *queue)
		}
	}

	return queues, nil
}

func (t *transactionReplacer) signedTransactionReplacements(ctx context.Context, height abi.ChainEpoch, queue []transactionReplaceSenderQueue) (map[common.Address][]signedTransactionReplacement, []transactionReplaceCandidate, error) {
	replacements := make(map[common.Address][]signedTransactionReplacement)

	var claimsToDelete []transactionReplaceCandidate

	for _, q := range queue {
		signedPerSender := make([]signedTransactionReplacement, 0, min(len(q.Transactions), TransactionReplaceMaxPerSenderPerRun))
		for _, candidate := range q.Transactions {
			if len(signedPerSender) >= TransactionReplaceMaxPerSenderPerRun {
				claimsToDelete = append(claimsToDelete, candidate)
				continue
			}

			if candidate.RetrySigned {
				signedReplacement, err := signedTransactionRetry(candidate)
				if err != nil {
					log.Warnw("skipping transaction replacement candidate; loading signed claim failed", "from", candidate.FromAddress, "nonce", candidate.Nonce, "hash", candidate.ClaimedSignedHash, "error", err)
					candidate.DeleteSignedClaim = true
					claimsToDelete = append(claimsToDelete, candidate)
					continue
				}

				signedPerSender = append(signedPerSender, signedReplacement)
				continue
			}

			replacement, feeStuck, err := t.prepareFeeReplacementTransaction(ctx, q.From, height, candidate)
			if err != nil {
				log.Warnw("skipping transaction replacement candidate; preparing replacement failed", "from", candidate.FromAddress, "nonce", candidate.Nonce, "hash", candidate.LatestSignedHash, "error", err)
				claimsToDelete = append(claimsToDelete, candidate)
				continue
			}
			if !feeStuck {
				claimsToDelete = append(claimsToDelete, candidate)
				continue
			}

			signedTx, err := t.signTransaction(ctx, q.From, replacement)
			if err != nil {
				log.Warnw("skipping transaction replacement candidate; signing replacement failed", "from", candidate.FromAddress, "nonce", candidate.Nonce, "hash", candidate.LatestSignedHash, "error", err)
				claimsToDelete = append(claimsToDelete, candidate)
				continue
			}

			signedPerSender = append(signedPerSender, signedTransactionReplacement{
				Candidate: candidate,
				SignedTx:  signedTx,
			})
		}
		if len(signedPerSender) > 0 {
			replacements[q.From] = signedPerSender
		}
	}
	return replacements, claimsToDelete, nil
}

func signedTransactionRetry(candidate transactionReplaceCandidate) (signedTransactionReplacement, error) {
	tx := new(gethtypes.Transaction)
	if err := tx.UnmarshalBinary(candidate.ClaimedSignedTx); err != nil {
		return signedTransactionReplacement{}, fmt.Errorf("unmarshaling claimed signed transaction %s: %w", candidate.ClaimedSignedHash, err)
	}
	if !strings.EqualFold(tx.Hash().Hex(), candidate.ClaimedSignedHash) {
		return signedTransactionReplacement{}, fmt.Errorf("claimed signed transaction decoded to hash %s, expected %s", tx.Hash().Hex(), candidate.ClaimedSignedHash)
	}
	if tx.Nonce() != candidate.Nonce {
		return signedTransactionReplacement{}, fmt.Errorf("claimed replacement nonce %d does not match candidate nonce %d", tx.Nonce(), candidate.Nonce)
	}

	from, err := transactionSender(tx)
	if err != nil {
		return signedTransactionReplacement{}, fmt.Errorf("recovering claimed transaction sender %s: %w", candidate.ClaimedSignedHash, err)
	}
	if !strings.EqualFold(from.Hex(), candidate.FromAddress) {
		return signedTransactionReplacement{}, fmt.Errorf("claimed replacement sender %s does not match candidate sender %s", from.Hex(), candidate.FromAddress)
	}

	return signedTransactionReplacement{
		Candidate: candidate,
		SignedTx:  tx,
	}, nil
}

func (t *transactionReplacer) prepareFeeReplacementTransaction(ctx context.Context, from common.Address, height abi.ChainEpoch, candidate transactionReplaceCandidate) (*gethtypes.Transaction, bool, error) {
	latest := new(gethtypes.Transaction)
	if err := latest.UnmarshalBinary(candidate.LatestSignedTx); err != nil {
		return nil, false, fmt.Errorf("unmarshaling latest signed transaction %s: %w", candidate.LatestSignedHash, err)
	}
	if !strings.EqualFold(latest.Hash().Hex(), candidate.LatestSignedHash) {
		return nil, false, fmt.Errorf("latest signed transaction decoded to hash %s, expected %s", latest.Hash().Hex(), candidate.LatestSignedHash)
	}
	if latest.Nonce() != candidate.Nonce {
		return nil, false, fmt.Errorf("latest signed transaction nonce %d does not match candidate nonce %d", latest.Nonce(), candidate.Nonce)
	}
	if latest.Type() != gethtypes.DynamicFeeTxType {
		return nil, false, fmt.Errorf("unsupported latest transaction type %d", latest.Type())
	}

	txFrom, err := gethtypes.Sender(gethtypes.LatestSignerForChainID(latest.ChainId()), latest)
	if err != nil {
		return nil, false, fmt.Errorf("recovering latest transaction sender %s: %w", candidate.LatestSignedHash, err)
	}
	if txFrom != from {
		return nil, false, fmt.Errorf("latest signed transaction sender %s does not match candidate sender %s", txFrom.Hex(), from.Hex())
	}

	ethCtx, cancel := context.WithTimeout(ctx, defaultEthCallTimeout)
	defer cancel()

	header, err := t.client.HeaderByNumber(ethCtx, big.NewInt(int64(height)))
	if err != nil {
		return nil, false, fmt.Errorf("getting latest eth header for %s nonce %d: %w", from.Hex(), candidate.Nonce, err)
	}
	if header.BaseFee == nil {
		return nil, false, fmt.Errorf("base fee is not available for %s nonce %d", from.Hex(), candidate.Nonce)
	}

	gasTipCap, err := t.client.SuggestGasTipCap(ethCtx)
	if err != nil {
		return nil, false, fmt.Errorf("estimating gas tip cap for %s nonce %d: %w", from.Hex(), candidate.Nonce, err)
	}
	gasFeeCap := new(big.Int).Add(header.BaseFee, gasTipCap)

	feeStuck := latest.GasFeeCap().Cmp(gasFeeCap) < 0 ||
		latest.GasTipCap().Cmp(gasTipCap) < 0
	if !feeStuck {
		return nil, false, nil
	}

	rbfTipCap := bumpInt(latest.GasTipCap(), TransactionReplaceGasBumpPercent)
	rbfFeeCap := bumpInt(latest.GasFeeCap(), TransactionReplaceGasBumpPercent)
	gasTipCap = maxInt(gasTipCap, rbfTipCap)
	gasFeeCap = maxInt(gasFeeCap, rbfFeeCap)
	if gasFeeCap.Cmp(gasTipCap) < 0 {
		gasFeeCap = new(big.Int).Set(gasTipCap)
	}
	if latest.Gas() == 0 {
		return nil, false, fmt.Errorf("latest signed transaction gas limit is zero for %s nonce %d", from.Hex(), candidate.Nonce)
	}

	return gethtypes.NewTx(&gethtypes.DynamicFeeTx{
		ChainID:    new(big.Int).Set(latest.ChainId()),
		Nonce:      latest.Nonce(),
		GasTipCap:  new(big.Int).Set(gasTipCap),
		GasFeeCap:  new(big.Int).Set(gasFeeCap),
		Gas:        latest.Gas(),
		To:         latest.To(),
		Value:      new(big.Int).Set(latest.Value()),
		Data:       append([]byte(nil), latest.Data()...),
		AccessList: append(gethtypes.AccessList(nil), latest.AccessList()...),
	}), true, nil
}

// sendAndRecordReplacementTransactions stores each signed replacement before sending it,
// then records the Eth send result. Saving the signed bytes first lets a later run
// retry the same replacement if this task exits before send_success is recorded.
func (t *transactionReplacer) sendAndRecordReplacementTransactions(ctx context.Context, replacements map[common.Address][]signedTransactionReplacement) error {
	for _, senderReplacements := range replacements {
		for _, replacement := range senderReplacements {
			tx := replacement.SignedTx
			if tx == nil {
				return fmt.Errorf("signed replacement transaction is nil for %s nonce %d", replacement.Candidate.FromAddress, replacement.Candidate.Nonce)
			}

			signedTx, err := tx.MarshalBinary()
			if err != nil {
				return fmt.Errorf("serializing replacement transaction for %s nonce %d: %w", replacement.Candidate.FromAddress, replacement.Candidate.Nonce, err)
			}

			n, err := t.db.Exec(ctx, `
				UPDATE message_send_eth_replacements
				SET signed_hash = $1,
					signed_tx = $2,
					claim_until = DEFAULT
				WHERE id = $3
					AND claim_id = $4
					AND send_success IS NULL`,
				tx.Hash().Hex(),
				signedTx,
				replacement.Candidate.ReplacementID,
				replacement.Candidate.ClaimID,
			)
			if err != nil {
				return fmt.Errorf("recording signed transaction replacement for %s nonce %d: %w", replacement.Candidate.FromAddress, replacement.Candidate.Nonce, err)
			}
			if n == 0 {
				log.Warnw("transaction replacement claim lost before send", "from", replacement.Candidate.FromAddress, "nonce", replacement.Candidate.Nonce, "replacement_id", replacement.Candidate.ReplacementID)
				continue
			}
			if n != 1 {
				return fmt.Errorf("recording signed transaction replacement for %s nonce %d: expected 1 row, got %d", replacement.Candidate.FromAddress, replacement.Candidate.Nonce, n)
			}

			ethCtx, cancel := context.WithTimeout(ctx, defaultEthCallTimeout)
			sendErr := t.client.SendTransaction(ethCtx, tx)
			cancel()
			if isEthTransactionAlreadyKnownError(sendErr) {
				log.Infow("transaction replacement already known", "from", replacement.Candidate.FromAddress, "nonce", replacement.Candidate.Nonce, "hash", tx.Hash().Hex())
				sendErr = nil
			}
			if errors.Is(sendErr, context.Canceled) || errors.Is(sendErr, context.DeadlineExceeded) {
				log.Warnw("transaction replacement send timed out; leaving signed claim for retry", "from", replacement.Candidate.FromAddress, "nonce", replacement.Candidate.Nonce, "hash", tx.Hash().Hex(), "error", sendErr)
				continue
			}
			sendSuccess := sendErr == nil
			sendError := ""
			if sendErr != nil {
				sendError = sendErr.Error()
				log.Warnw("transaction replacement send failed", "from", replacement.Candidate.FromAddress, "nonce", replacement.Candidate.Nonce, "hash", tx.Hash().Hex(), "error", sendErr)
			}

			n, err = t.db.Exec(ctx, `
				UPDATE message_send_eth_replacements
				SET send_time = CURRENT_TIMESTAMP,
					send_success = $1,
					send_error = $2
				WHERE id = $3
					AND claim_id = $4
					AND send_success IS NULL`,
				sendSuccess,
				sendError,
				replacement.Candidate.ReplacementID,
				replacement.Candidate.ClaimID,
			)
			if err != nil {
				return fmt.Errorf("recording transaction replacement for %s nonce %d: %w", replacement.Candidate.FromAddress, replacement.Candidate.Nonce, err)
			}
			if n != 1 {
				if sendSuccess {
					return fmt.Errorf("recording successful transaction replacement for %s nonce %d: expected 1 row, got %d", replacement.Candidate.FromAddress, replacement.Candidate.Nonce, n)
				}
				log.Warnw("transaction replacement failure record lost", "from", replacement.Candidate.FromAddress, "nonce", replacement.Candidate.Nonce, "replacement_id", replacement.Candidate.ReplacementID, "rows", n)
			}
		}
	}

	return nil
}

func (t *transactionReplacer) deleteTransactionReplacementClaims(ctx context.Context, candidates []transactionReplaceCandidate) error {
	if len(candidates) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(candidates))
	claimIDs := make([]string, 0, len(candidates))
	deleteSignedClaims := make([]bool, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ReplacementID)
		claimIDs = append(claimIDs, candidate.ClaimID)
		deleteSignedClaims = append(deleteSignedClaims, candidate.DeleteSignedClaim)
	}

	_, err := t.db.Exec(ctx, `
		DELETE FROM message_send_eth_replacements mer
		USING (
			SELECT *
			FROM unnest($1::bigint[], $2::text[], $3::boolean[]) AS c(id, claim_id, delete_signed_claim)
		) c
		WHERE mer.id = c.id
			AND mer.claim_id = c.claim_id
			AND mer.send_success IS NULL
			AND (
				mer.signed_hash IS NULL OR
				c.delete_signed_claim
			)`,
		ids, claimIDs, deleteSignedClaims)
	if err != nil {
		return fmt.Errorf("deleting transaction replacement claims: %w", err)
	}

	return nil
}

func (t *transactionReplacer) signTransaction(ctx context.Context, from common.Address, tx *gethtypes.Transaction) (*gethtypes.Transaction, error) {
	var privateKeyData []byte
	err := t.db.QueryRow(ctx, `SELECT private_key FROM eth_keys WHERE address = $1`, from.Hex()).Scan(&privateKeyData)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("eth key %s not found", from.Hex())
		}
		return nil, fmt.Errorf("fetching eth key %s: %w", from.Hex(), err)
	}

	privateKey, err := crypto.ToECDSA(privateKeyData)
	if err != nil {
		return nil, fmt.Errorf("converting eth key %s: %w", from.Hex(), err)
	}

	return gethtypes.SignTx(tx, gethtypes.LatestSignerForChainID(tx.ChainId()), privateKey)
}

func transactionSender(tx *gethtypes.Transaction) (common.Address, error) {
	return gethtypes.Sender(gethtypes.LatestSignerForChainID(tx.ChainId()), tx)
}

func bumpInt(v *big.Int, percent int64) *big.Int {
	if v == nil {
		v = big.NewInt(0)
	}
	out := new(big.Int).Mul(v, big.NewInt(percent))
	out.Div(out, big.NewInt(100))
	out.Add(out, big.NewInt(1))
	return out
}

func maxInt(a, b *big.Int) *big.Int {
	if a == nil {
		return new(big.Int).Set(b)
	}
	if b == nil || a.Cmp(b) >= 0 {
		return new(big.Int).Set(a)
	}
	return new(big.Int).Set(b)
}

func isEthTransactionAlreadyKnownError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already known") ||
		strings.Contains(msg, "already imported") ||
		strings.Contains(msg, "already in the txpool")
}
