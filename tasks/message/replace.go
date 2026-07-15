package message

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	MessageReplaceGasBumpPercent     int64 = 120
	MessageReplaceMaxPerSenderPerRun int   = 10
)

var MessageReplaceMaxFee = abi.TokenAmount(types.MustParseFIL("1 FIL"))

//go:generate go run github.com/golang/mock/mockgen -source=replace.go -destination=replace_mock.go -package=message MessageReplaceAPI

type MessageReplaceAPI interface {
	StateMinerInfo(ctx context.Context, actor address.Address, tsk types.TipSetKey) (api.MinerInfo, error)
	StateAccountKey(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error)
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
	GasEstimateMessageGas(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec, tsk types.TipSetKey) (*types.Message, error)
	MpoolPush(ctx context.Context, smsg *types.SignedMessage) (cid.Cid, error)
	WalletBalance(ctx context.Context, addr address.Address) (big.Int, error)
}

type FilecoinReplacerConfig struct {
	API    MessageReplaceAPI
	Signer SignerAPI
}

type messageReplacer struct {
	db               *harmonydb.DB
	api              MessageReplaceAPI
	signer           SignerAPI
	stuckForDuration time.Duration
}

type messageReplaceSenderQueue struct {
	FromKey    string
	From       address.Address
	ActorNonce uint64
	Messages   []messageReplaceCandidate
}

type messageReplaceCandidate struct {
	ReplacementID int64  `db:"replacement_id"`
	ClaimID       string `db:"claim_id"`

	FromKey string `db:"from_key"`
	Nonce   uint64 `db:"nonce"`

	LatestSignedCID  string `db:"latest_signed_cid"`
	LatestSignedData []byte `db:"latest_signed_data"`

	RetrySigned       bool   `db:"retry_signed"`
	ClaimedSignedCID  string `db:"claimed_signed_cid"`
	ClaimedSignedData []byte `db:"claimed_signed_data"`

	DeleteSignedClaim bool
}

type signedMessageReplacement struct {
	Candidate     messageReplaceCandidate
	SignedMessage *types.SignedMessage
}

func (t *messageReplacer) runMessageReplacement(ctx context.Context, tsk *types.TipSetKey, height abi.ChainEpoch, tipSetTimeStamp time.Time) error {
	// This runs on the replacer goroutine, not on the chain scheduler callback path.
	//
	// Replacement run outline:
	// 1. Load stale successful Filecoin sends for local senders with nonce >= actor nonce.
	// 2. Claim the current replacement chain tip for each nonce.
	// 3. Reuse an already-signed claim when retrying; otherwise prepare and sign a fee replacement.
	// 4. Record signed replacement data before pushing it to Lotus, then record the send result.
	// 5. Delete claims that should not be retried.

	stuckCutoff := tipSetTimeStamp.Add(-t.stuckForDuration)
	queues, err := t.loadMessageCandidates(ctx, tsk, stuckCutoff)
	if err != nil {
		return err
	}

	replacements, claimsToDelete, err := t.signedMessageReplacements(ctx, tsk, queues)
	if err != nil {
		return err
	}
	sendErr := t.sendAndRecordReplacementMessages(ctx, replacements)
	deleteErr := t.deleteMessageReplacementClaims(ctx, claimsToDelete)
	if sendErr != nil {
		if deleteErr != nil {
			return fmt.Errorf("sending replacements: %w; deleting replacement claims: %v", sendErr, deleteErr)
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

	log.Debugw("message replacement candidates loaded", "senders", len(queues), "replacements", count, "deletedClaims", len(claimsToDelete), "height", height, "stuckCutoff", stuckCutoff)
	return nil
}

func (t *messageReplacer) loadMessageCandidates(ctx context.Context, tsk *types.TipSetKey, stuckCutoff time.Time) ([]messageReplaceSenderQueue, error) {
	// Get all wallets from the config. This includes all the control address for miners
	wallets, _, err := config.GetAddressesFromConfig(ctx, t.db, t.api)
	if err != nil {
		return nil, err
	}

	// Create a queue per sender
	fromKeys := make([]string, 0, len(wallets))
	senderQueues := make(map[string]*messageReplaceSenderQueue, len(wallets))

	addSender := func(fromKey string, from address.Address) {
		if _, ok := senderQueues[fromKey]; ok {
			return
		}

		fromKeys = append(fromKeys, fromKey)
		senderQueues[fromKey] = &messageReplaceSenderQueue{
			FromKey: fromKey,
			From:    from,
		}
	}

	// Verify wallet actor on chain
	for _, wallet := range wallets {
		from, err := t.api.StateAccountKey(ctx, wallet, *tsk)
		if err != nil {
			log.Warnw("skipping message replacement sender; account key lookup failed", "wallet", wallet, "error", err)
			continue
		}

		addSender(from.String(), from)
	}

	// Get nonce from the chain for each actor
	// Let's also create nonce and key arrays for easy DB look-ups. Maps are easy to read in Go but
	// arrays are better for SQL.
	activeFromKeys := make([]string, 0, len(fromKeys))
	actorNonces := make([]int64, 0, len(fromKeys))
	for _, fromKey := range fromKeys {
		queue := senderQueues[fromKey]
		act, err := t.api.StateGetActor(ctx, queue.From, *tsk)
		if err != nil {
			log.Warnw("skipping message replacement sender; actor lookup failed", "from", fromKey, "error", err)
			continue
		}

		activeFromKeys = append(activeFromKeys, fromKey)
		actorNonces = append(actorNonces, int64(act.Nonce))
		queue.ActorNonce = act.Nonce
	}

	// Delete stale rows
	deleted, err := t.db.Exec(ctx, `
		WITH sender_nonces AS (
			SELECT *
			FROM unnest($1::text[], $2::bigint[]) AS sn(from_key, actor_nonce)
		)
		DELETE FROM message_send_replacements msr
		USING sender_nonces sn
		WHERE msr.from_key = sn.from_key
			AND msr.nonce < sn.actor_nonce
			AND (
				msr.send_success IS NOT NULL OR
				msr.claim_until < CURRENT_TIMESTAMP
			)`,
		activeFromKeys, actorNonces)
	if err != nil {
		return nil, fmt.Errorf("deleting stale message replacement rows: %w", err)
	}
	if deleted > 0 {
		log.Debugw("deleted stale message replacement rows", "rows", deleted)
	}

	var rows []messageReplaceCandidate
	claimID := uuid.NewString()
	// Load old successful Filecoin sends for active senders whose nonce has not
	// landed yet, resolve the current successful replacement chain tip for each
	// send, and claim the tip for this run. Expired unsent claims are reclaimed;
	// active claims and already-successful replacement rows are skipped. Returned
	// rows are only the claims owned by this run, including any previously signed
	// replacement data that should be retried instead of re-signed.
	err = t.db.Select(ctx, &rows, `
					WITH sender_nonces AS (
						SELECT *
						FROM unnest($1::text[], $2::bigint[]) AS sn(from_key, actor_nonce)
					),
					candidate_sends AS (
						SELECT c.*
						FROM sender_nonces sn
						INNER JOIN LATERAL (
							SELECT
								ms.from_key,
								ms.nonce,
								ms.signed_cid,
								ms.signed_data,
								ms.send_time
							FROM message_sends ms
							WHERE ms.from_key = sn.from_key
								AND ms.send_success = TRUE
								AND ms.nonce IS NOT NULL
								AND ms.nonce >= sn.actor_nonce
								AND ms.signed_cid IS NOT NULL
								AND ms.signed_data IS NOT NULL
								AND ms.send_time IS NOT NULL
								AND ms.send_time <= $3
							ORDER BY ms.nonce
							LIMIT $4
						) c ON TRUE
					),
					eligible AS (
						SELECT
							cs.from_key,
							cs.nonce,
							cs.signed_cid AS original_signed_cid,
							COALESCE(latest.signed_cid, cs.signed_cid) AS latest_signed_cid,
							COALESCE(latest.signed_data, cs.signed_data) AS latest_signed_data,
							COALESCE(latest.send_time, cs.send_time) AS latest_send_time
						FROM candidate_sends cs
						LEFT JOIN LATERAL (
							SELECT
								r.signed_cid,
								r.signed_data,
								r.send_time
							FROM message_send_replacements r
							WHERE r.from_key = cs.from_key
								AND r.nonce = cs.nonce
								AND r.send_success = TRUE
								AND r.signed_cid IS NOT NULL
								AND r.signed_data IS NOT NULL
								AND NOT EXISTS (
									SELECT 1
									FROM message_send_replacements next
									WHERE next.from_key = r.from_key
										AND next.nonce = r.nonce
										AND next.send_success = TRUE
										AND next.replaces_signed_cid = r.signed_cid
								)
							LIMIT 1
						) latest ON TRUE
						WHERE COALESCE(latest.send_time, cs.send_time) <= $3
					),
					claimed AS (
						INSERT INTO message_send_replacements (
							from_key,
							nonce,
							original_signed_cid,
							replaces_signed_cid,
							claim_id
						)
						SELECT
							from_key,
							nonce,
							original_signed_cid,
							latest_signed_cid,
							$5
						FROM eligible
						ON CONFLICT (from_key, nonce, replaces_signed_cid)
							WHERE send_success IS NOT FALSE
						DO UPDATE SET
							claim_id = EXCLUDED.claim_id,
							claim_time = CURRENT_TIMESTAMP,
							claim_until = DEFAULT
						WHERE message_send_replacements.send_success IS NULL
							AND message_send_replacements.claim_until < CURRENT_TIMESTAMP
						RETURNING
							id AS replacement_id,
							from_key,
							nonce,
							replaces_signed_cid,
							claim_id,
							signed_cid,
							signed_data
					)
					SELECT
						claimed.replacement_id,
						claimed.claim_id,
						eligible.from_key,
						eligible.nonce,
				
						eligible.latest_signed_cid,
						eligible.latest_signed_data,
				
						(claimed.signed_cid IS NOT NULL AND claimed.signed_data IS NOT NULL) AS retry_signed,
						COALESCE(claimed.signed_cid, '') AS claimed_signed_cid,
						claimed.signed_data AS claimed_signed_data
					FROM eligible
					INNER JOIN claimed
						ON claimed.from_key = eligible.from_key
						AND claimed.nonce = eligible.nonce
						AND claimed.replaces_signed_cid = eligible.latest_signed_cid
					ORDER BY eligible.from_key, eligible.nonce
				`, activeFromKeys, actorNonces, stuckCutoff, MessageReplaceMaxPerSenderPerRun, claimID)
	if err != nil {
		return nil, xerrors.Errorf("selecting message candidates: %w", err)
	}

	queues := make([]messageReplaceSenderQueue, 0, len(senderQueues))

	// Attach claimed rows back to their sender queues. The SQL returns a flat
	// list across all active senders; queues preserve the sender address and
	// per-sender nonce order needed by the signing/sending step.
	for _, row := range rows {
		queue, ok := senderQueues[row.FromKey]
		if !ok {
			return nil, fmt.Errorf("claimed replacement for unknown sender %s", row.FromKey)
		}
		queue.Messages = append(queue.Messages, row)
	}

	// Return only active senders with work, keeping the same sender order used
	// to build the SQL inputs.
	for _, fromKey := range activeFromKeys {
		queue := senderQueues[fromKey]
		if queue != nil && len(queue.Messages) > 0 {
			queues = append(queues, *queue)
		}
	}

	return queues, nil
}

func (t *messageReplacer) signedMessageReplacements(ctx context.Context, tsk *types.TipSetKey, queue []messageReplaceSenderQueue) (map[address.Address][]signedMessageReplacement, []messageReplaceCandidate, error) {
	// Signed message map per sender
	replacements := make(map[address.Address][]signedMessageReplacement)

	// Claims to delete from the message_send_replacements
	var claimsToDelete []messageReplaceCandidate

	for _, q := range queue {
		// Let's create an array per sender capped at MessageReplaceMaxPerSenderPerRun
		signedPerSender := make([]signedMessageReplacement, 0, min(len(q.Messages), MessageReplaceMaxPerSenderPerRun))
		for _, candidate := range q.Messages {
			if len(signedPerSender) >= MessageReplaceMaxPerSenderPerRun {
				// If queue is full then let's delete the remaining claims so next run can pick them up cleanly
				claimsToDelete = append(claimsToDelete, candidate)
				continue
			}

			// If we already created a replacement message but failed to send it then let's reuse it
			if candidate.RetrySigned {
				signedReplacement, err := signedReplacementRetry(candidate)
				if err != nil {
					log.Warnw("skipping message replacement candidate; loading signed claim failed", "from", candidate.FromKey, "nonce", candidate.Nonce, "cid", candidate.ClaimedSignedCID, "error", err)
					candidate.DeleteSignedClaim = true
					claimsToDelete = append(claimsToDelete, candidate)
					continue
				}

				signedPerSender = append(signedPerSender, signedReplacement)
				continue
			}

			replacement, feeStuck, err := t.prepareFeeReplacementMessage(ctx, *tsk, q.From, candidate)
			if err != nil {
				log.Warnw("skipping message replacement candidate; preparing replacement failed", "from", candidate.FromKey, "nonce", candidate.Nonce, candidate.LatestSignedCID, "error", err)
				claimsToDelete = append(claimsToDelete, candidate)
				continue
			}
			if !feeStuck {
				claimsToDelete = append(claimsToDelete, candidate)
				continue
			}

			var smsg *types.SignedMessage

			smsg, err = t.signer.WalletSignMessage(ctx, q.From, replacement)
			if err != nil {
				log.Warnw("skipping message replacement candidate; signing replacement failed", "from", candidate.FromKey, "nonce", candidate.Nonce, "cid", candidate.LatestSignedCID, "error", err)
				claimsToDelete = append(claimsToDelete, candidate)
				continue
			}

			signedPerSender = append(signedPerSender, signedMessageReplacement{
				Candidate:     candidate,
				SignedMessage: smsg,
			})
		}
		if len(signedPerSender) > 0 {
			replacements[q.From] = signedPerSender
		}
	}
	return replacements, claimsToDelete, nil
}

func signedReplacementRetry(candidate messageReplaceCandidate) (signedMessageReplacement, error) {
	smsg := new(types.SignedMessage)

	// Unmarshal the previously created replacement
	if err := smsg.UnmarshalCBOR(bytes.NewReader(candidate.ClaimedSignedData)); err != nil {
		return signedMessageReplacement{}, fmt.Errorf("unmarshaling claimed signed message %s: %w", candidate.ClaimedSignedCID, err)
	}
	if smsg.Cid().String() != candidate.ClaimedSignedCID {
		return signedMessageReplacement{}, fmt.Errorf("claimed signed message decoded to cid %s, expected %s", smsg.Cid(), candidate.ClaimedSignedCID)
	}
	if smsg.Message.From.String() != candidate.FromKey {
		return signedMessageReplacement{}, fmt.Errorf("claimed replacement sender %s does not match candidate sender %s", smsg.Message.From, candidate.FromKey)
	}
	if smsg.Message.Nonce != candidate.Nonce {
		return signedMessageReplacement{}, fmt.Errorf("claimed replacement nonce %d does not match candidate nonce %d", smsg.Message.Nonce, candidate.Nonce)
	}

	return signedMessageReplacement{
		Candidate:     candidate,
		SignedMessage: smsg,
	}, nil
}

func (t *messageReplacer) prepareFeeReplacementMessage(ctx context.Context, tsk types.TipSetKey, from address.Address, candidate messageReplaceCandidate) (*types.Message, bool, error) {
	latest := new(types.SignedMessage)
	if err := latest.UnmarshalCBOR(bytes.NewReader(candidate.LatestSignedData)); err != nil {
		return nil, false, fmt.Errorf("unmarshaling latest signed message %s: %w", candidate.LatestSignedCID, err)
	}

	// Let's double check in case cbor unmarshal has any issue
	if latest.Message.From != from {
		return nil, false, fmt.Errorf("latest signed message sender %s does not match candidate sender %s", latest.Message.From, from)
	}
	if latest.Message.Nonce != candidate.Nonce {
		return nil, false, fmt.Errorf("latest signed message nonce %d does not match candidate nonce %d", latest.Message.Nonce, candidate.Nonce)
	}

	replacement := latest.Message
	replacement.GasFeeCap = abi.NewTokenAmount(0)
	replacement.GasPremium = abi.NewTokenAmount(0)

	spec := &api.MessageSendSpec{MaxFee: MessageReplaceMaxFee}
	estimated, err := t.api.GasEstimateMessageGas(ctx, &replacement, spec, tsk)
	if err != nil {
		return nil, false, fmt.Errorf("estimating replacement gas for %s nonce %d: %w", from, candidate.Nonce, err)
	}

	feeStuck := latest.Message.GasFeeCap.LessThan(estimated.GasFeeCap) ||
		latest.Message.GasPremium.LessThan(estimated.GasPremium)

	if !feeStuck {
		return nil, false, nil
	}

	replacement = *estimated
	rbfPremium := messagepool.ComputeRBF(latest.Message.GasPremium, types.Percent(MessageReplaceGasBumpPercent))
	replacement.GasPremium = big.Max(replacement.GasPremium, rbfPremium)
	replacement.GasFeeCap = big.Max(replacement.GasFeeCap, replacement.GasPremium)

	if replacement.GasLimit <= 0 {
		return nil, false, fmt.Errorf("estimated replacement gas limit is %d for %s nonce %d", replacement.GasLimit, from, candidate.Nonce)
	}

	messagepool.CapGasFee(func() (abi.TokenAmount, error) {
		return MessageReplaceMaxFee, nil
	}, &replacement, spec)

	if replacement.GasFeeCap.LessThan(estimated.GasFeeCap) {
		return nil, false, nil
	}
	if replacement.GasPremium.LessThan(rbfPremium) {
		return nil, false, nil
	}

	return &replacement, true, nil
}

// sendAndRecordReplacementMessages stores each signed replacement before pushing it,
// then records the mpool push result. Saving the signed bytes first lets a later run
// retry the same replacement if this task exits before send_success is recorded.
func (t *messageReplacer) sendAndRecordReplacementMessages(ctx context.Context, replacements map[address.Address][]signedMessageReplacement) error {
	for _, senderReplacements := range replacements {
		for _, replacement := range senderReplacements {
			smsg := replacement.SignedMessage
			if smsg == nil {
				return fmt.Errorf("signed replacement is nil for %s nonce %d", replacement.Candidate.FromKey, replacement.Candidate.Nonce)
			}

			signedData, err := smsg.Serialize()
			if err != nil {
				return fmt.Errorf("serializing replacement for %s nonce %d: %w", replacement.Candidate.FromKey, replacement.Candidate.Nonce, err)
			}

			n, err := t.db.Exec(ctx, `
				UPDATE message_send_replacements
				SET signed_cid = $1,
					signed_data = $2,
					claim_until = DEFAULT
				WHERE id = $3
					AND claim_id = $4
					AND send_success IS NULL`,
				smsg.Cid().String(),
				signedData,
				replacement.Candidate.ReplacementID,
				replacement.Candidate.ClaimID,
			)
			if err != nil {
				return fmt.Errorf("recording signed replacement for %s nonce %d: %w", replacement.Candidate.FromKey, replacement.Candidate.Nonce, err)
			}
			if n == 0 {
				log.Warnw("message replacement claim lost before push", "from", replacement.Candidate.FromKey, "nonce", replacement.Candidate.Nonce, "replacement_id", replacement.Candidate.ReplacementID)
				continue
			}
			if n != 1 {
				return fmt.Errorf("recording signed replacement for %s nonce %d: expected 1 row, got %d", replacement.Candidate.FromKey, replacement.Candidate.Nonce, n)
			}

			_, pushErr := t.api.MpoolPush(ctx, smsg)
			if isMpoolExistingNonceError(pushErr) {
				log.Infow("message replacement already in mpool", "from", replacement.Candidate.FromKey, "nonce", replacement.Candidate.Nonce, "cid", smsg.Cid())
				pushErr = nil
			}
			sendSuccess := pushErr == nil
			sendError := ""
			if pushErr != nil {
				sendError = pushErr.Error()
				log.Warnw("message replacement push failed", "from", replacement.Candidate.FromKey, "nonce", replacement.Candidate.Nonce, "cid", smsg.Cid(), "error", pushErr)
			}

			n, err = t.db.Exec(ctx, `
				UPDATE message_send_replacements
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
				return fmt.Errorf("recording replacement for %s nonce %d: %w", replacement.Candidate.FromKey, replacement.Candidate.Nonce, err)
			}
			if n != 1 {
				if sendSuccess {
					return fmt.Errorf("recording successful replacement for %s nonce %d: expected 1 row, got %d", replacement.Candidate.FromKey, replacement.Candidate.Nonce, n)
				}
				log.Warnw("message replacement failure record lost", "from", replacement.Candidate.FromKey, "nonce", replacement.Candidate.Nonce, "replacement_id", replacement.Candidate.ReplacementID, "rows", n)
			}
		}
	}

	return nil
}

func (t *messageReplacer) deleteMessageReplacementClaims(ctx context.Context, candidates []messageReplaceCandidate) error {
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
		DELETE FROM message_send_replacements msr
		USING (
			SELECT *
			FROM unnest($1::bigint[], $2::text[], $3::boolean[]) AS c(id, claim_id, delete_signed_claim)
		) c
		WHERE msr.id = c.id
			AND msr.claim_id = c.claim_id
			AND msr.send_success IS NULL
			AND (
				msr.signed_cid IS NULL OR
				c.delete_signed_claim
			)`,
		ids, claimIDs, deleteSignedClaims)
	if err != nil {
		return fmt.Errorf("deleting message replacement claims: %w", err)
	}

	return nil
}

func isMpoolExistingNonceError(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, messagepool.ErrExistingNonce) ||
		strings.Contains(err.Error(), messagepool.ErrExistingNonce.Error())
}
