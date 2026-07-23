package message

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"database/sql"
	"errors"
	"fmt"
	mathbig "math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/golang/mock/gomock"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	filcrypto "github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/tasks/message/replace_mock"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build/buildconstants"
	"github.com/filecoin-project/lotus/chain/messagepool"
	ltypes "github.com/filecoin-project/lotus/chain/types"
)

type mockReplaceSigner struct {
	signCalls int
	sigByte   byte
	signErr   error
}

func (m *mockReplaceSigner) WalletSignMessage(ctx context.Context, addr address.Address, msg *ltypes.Message) (*ltypes.SignedMessage, error) {
	m.signCalls++
	if m.signErr != nil {
		return nil, m.signErr
	}

	m.sigByte++
	msgCopy := *msg
	return signedMessageWithByte(&msgCopy, m.sigByte), nil
}

type filecoinReplaceHarness struct {
	ctx         context.Context
	db          *harmonydb.DB
	api         *replace_mock.MockMessageReplaceAPI
	accountKeys map[string]address.Address
	actors      map[string]*ltypes.Actor
	signer      *mockReplaceSigner
	replacer    *messageReplacer
	pushedMsgs  []*ltypes.SignedMessage
	now         time.Time
}

func newFilecoinReplaceHarness(t *testing.T) *filecoinReplaceHarness {
	t.Helper()

	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	apiMock := replace_mock.NewMockMessageReplaceAPI(gomock.NewController(t))
	signer := &mockReplaceSigner{}
	h := &filecoinReplaceHarness{
		ctx:         ctx,
		db:          db,
		api:         apiMock,
		accountKeys: map[string]address.Address{},
		actors:      map[string]*ltypes.Actor{},
		signer:      signer,
		now:         time.Now().UTC().Truncate(time.Second),
	}
	h.replacer = &messageReplacer{
		db:               h.db,
		api:              h.api,
		signer:           h.signer,
		stuckForDuration: replaceTestStuckDuration(),
	}
	h.expectDefaultFilecoinAPIBehavior()
	return h
}

func (h *filecoinReplaceHarness) expectDefaultFilecoinAPIBehavior() {
	h.api.EXPECT().
		StateMinerInfo(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(api.MinerInfo{}, nil).
		AnyTimes()
	h.api.EXPECT().
		StateAccountKey(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, addr address.Address, tsk ltypes.TipSetKey) (address.Address, error) {
			if key, ok := h.accountKeys[addr.String()]; ok {
				return key, nil
			}
			return addr, nil
		}).
		AnyTimes()
	h.api.EXPECT().
		StateGetActor(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, actor address.Address, tsk ltypes.TipSetKey) (*ltypes.Actor, error) {
			if act, ok := h.actors[actor.String()]; ok {
				return act, nil
			}
			return nil, fmt.Errorf("actor %s not found", actor)
		}).
		AnyTimes()
	h.api.EXPECT().
		WalletBalance(gomock.Any(), gomock.Any()).
		Return(abi.NewTokenAmount(1_000_000_000_000_000_000), nil).
		AnyTimes()
}

func (h *filecoinReplaceHarness) expectGasEstimate(t *testing.T, estimate func(*ltypes.Message) *ltypes.Message) {
	t.Helper()

	h.api.EXPECT().
		GasEstimateMessageGas(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, msg *ltypes.Message, spec *api.MessageSendSpec, tsk ltypes.TipSetKey) (*ltypes.Message, error) {
			return estimate(msg), nil
		}).
		Times(1)
}

func (h *filecoinReplaceHarness) expectMpoolPush(t *testing.T, pushErr error) {
	t.Helper()

	h.api.EXPECT().
		MpoolPush(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, smsg *ltypes.SignedMessage) (cid.Cid, error) {
			h.pushedMsgs = append(h.pushedMsgs, smsg)
			if pushErr != nil {
				return cid.Undef, pushErr
			}
			return smsg.Cid(), nil
		}).
		Times(1)
}

func (h *filecoinReplaceHarness) run(t *testing.T) {
	t.Helper()

	tsk := ltypes.EmptyTSK
	require.NoError(t, h.replacer.runMessageReplacement(h.ctx, &tsk, 100, h.now))
}

func (h *filecoinReplaceHarness) oldSendTime() time.Time {
	return h.now.Add(-h.replacer.stuckForDuration - time.Minute)
}

func (h *filecoinReplaceHarness) youngSendTime() time.Time {
	return h.now.Add(-time.Second)
}

func (h *filecoinReplaceHarness) registerSender(t *testing.T, from address.Address, actorNonce uint64) {
	t.Helper()

	h.accountKeys[from.String()] = from
	h.actors[from.String()] = &ltypes.Actor{Nonce: actorNonce}

	var machineID int64
	err := h.db.QueryRow(h.ctx, `
		INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu)
		VALUES ('replace-test', 1, 1, 0)
		RETURNING id`).Scan(&machineID)
	require.NoError(t, err)

	_, err = h.db.Exec(h.ctx, `
		INSERT INTO harmony_machine_details (machine_id, layers)
		VALUES ($1, 'replace-test')`, machineID)
	require.NoError(t, err)

	_, err = h.db.Exec(h.ctx, `
		INSERT INTO harmony_config (title, config)
		VALUES ('replace-test', $1)
		ON CONFLICT (title) DO UPDATE SET config = EXCLUDED.config`,
		fmt.Sprintf(`
				[[Addresses]]
				PreCommitControl = ["%s"]
				CommitControl = []
				DealPublishControl = []
				TerminateControl = []
				MinerAddresses = []
				`, from.String()))
	require.NoError(t, err)
}

func (h *filecoinReplaceHarness) insertMessageSend(t *testing.T, smsg *ltypes.SignedMessage, sendTime time.Time, taskID int64) {
	t.Helper()

	signedData, err := smsg.Serialize()
	require.NoError(t, err)
	signedJSON, err := smsg.MarshalJSON()
	require.NoError(t, err)

	_, err = h.db.Exec(h.ctx, `
		INSERT INTO message_sends (
			from_key, to_addr, send_reason, send_task_id,
			unsigned_data, unsigned_cid,
			nonce, signed_data, signed_json, signed_cid,
			send_time, send_success
		)
		VALUES ($1, $2, 'replace-test', $3, $4, $5, $6, $7, $8, $9, $10, TRUE)`,
		smsg.Message.From.String(),
		smsg.Message.To.String(),
		taskID,
		[]byte("unsigned"),
		smsg.Message.Cid().String(),
		smsg.Message.Nonce,
		signedData,
		string(signedJSON),
		smsg.Cid().String(),
		sendTime)
	require.NoError(t, err)
}

func (h *filecoinReplaceHarness) insertExpiredSignedClaim(t *testing.T, originalCID string, replacesCID string, smsg *ltypes.SignedMessage) {
	t.Helper()

	signedData, err := smsg.Serialize()
	require.NoError(t, err)

	_, err = h.db.Exec(h.ctx, `
		INSERT INTO message_send_replacements (
			from_key, nonce, original_signed_cid, replaces_signed_cid,
			claim_id, claim_until, signed_cid, signed_data
		)
		VALUES ($1, $2, $3, $4, 'old-claim', CURRENT_TIMESTAMP - interval '1 minute', $5, $6)`,
		smsg.Message.From.String(),
		smsg.Message.Nonce,
		originalCID,
		replacesCID,
		smsg.Cid().String(),
		signedData)
	require.NoError(t, err)
}

func (h *filecoinReplaceHarness) insertSuccessfulReplacement(t *testing.T, originalCID string, replacesCID string, smsg *ltypes.SignedMessage, sendTime time.Time) {
	t.Helper()

	signedData, err := smsg.Serialize()
	require.NoError(t, err)

	_, err = h.db.Exec(h.ctx, `
		INSERT INTO message_send_replacements (
			from_key, nonce, original_signed_cid, replaces_signed_cid,
			claim_id, signed_cid, signed_data,
			send_time, send_success
		)
		VALUES ($1, $2, $3, $4, 'prior-success', $5, $6, $7, TRUE)`,
		smsg.Message.From.String(),
		smsg.Message.Nonce,
		originalCID,
		replacesCID,
		smsg.Cid().String(),
		signedData,
		sendTime)
	require.NoError(t, err)
}

func TestFilecoinReplacerReplacesFeeStuckMessage(t *testing.T) {
	h := newFilecoinReplaceHarness(t)
	from := testIDAddress(t, 1001)
	to := testIDAddress(t, 2001)
	nonce := uint64(7)
	h.registerSender(t, from, nonce)

	original := signedMessageWithByte(testMessage(from, to, nonce, 100, 10), 1)
	h.insertMessageSend(t, original, h.oldSendTime(), 1)

	h.expectGasEstimate(t, func(msg *ltypes.Message) *ltypes.Message {
		estimated := *msg
		estimated.GasLimit = 1000
		estimated.GasFeeCap = abi.NewTokenAmount(200)
		estimated.GasPremium = abi.NewTokenAmount(20)
		return &estimated
	})
	h.expectMpoolPush(t, nil)

	h.run(t)

	require.Equal(t, 1, h.signer.signCalls)
	require.Len(t, h.pushedMsgs, 1)
	require.Equal(t, nonce, h.pushedMsgs[0].Message.Nonce)
	require.True(t, h.pushedMsgs[0].Message.GasFeeCap.Equals(abi.NewTokenAmount(200)))
	require.True(t, h.pushedMsgs[0].Message.GasPremium.Equals(abi.NewTokenAmount(20)))

	var signedCID string
	var sendSuccess bool
	err := h.db.QueryRow(h.ctx, `
		SELECT signed_cid, send_success
		FROM message_send_replacements
		WHERE from_key = $1 AND nonce = $2`, from.String(), nonce).Scan(&signedCID, &sendSuccess)
	require.NoError(t, err)
	require.Equal(t, h.pushedMsgs[0].Cid().String(), signedCID)
	require.True(t, sendSuccess)
}

func TestFilecoinReplacerTreatsExistingNonceAsSuccess(t *testing.T) {
	h := newFilecoinReplaceHarness(t)
	from := testIDAddress(t, 1003)
	to := testIDAddress(t, 2003)
	nonce := uint64(9)
	h.registerSender(t, from, nonce)

	original := signedMessageWithByte(testMessage(from, to, nonce, 100, 10), 1)
	h.insertMessageSend(t, original, h.oldSendTime(), 1)
	h.expectGasEstimate(t, func(msg *ltypes.Message) *ltypes.Message {
		estimated := *msg
		estimated.GasLimit = 1000
		estimated.GasFeeCap = abi.NewTokenAmount(200)
		estimated.GasPremium = abi.NewTokenAmount(20)
		return &estimated
	})
	h.expectMpoolPush(t, messagepool.ErrExistingNonce)

	h.run(t)

	var sendSuccess bool
	var sendError string
	err := h.db.QueryRow(h.ctx, `
		SELECT send_success, send_error
		FROM message_send_replacements
		WHERE from_key = $1 AND nonce = $2`, from.String(), nonce).Scan(&sendSuccess, &sendError)
	require.NoError(t, err)
	require.True(t, sendSuccess)
	require.Empty(t, sendError)
}

func TestFilecoinReplacerRetriesSignedClaim(t *testing.T) {
	h := newFilecoinReplaceHarness(t)
	from := testIDAddress(t, 1002)
	to := testIDAddress(t, 2002)
	nonce := uint64(8)
	h.registerSender(t, from, nonce)

	original := signedMessageWithByte(testMessage(from, to, nonce, 100, 10), 1)
	retry := signedMessageWithByte(testMessage(from, to, nonce, 300, 30), 2)
	h.insertMessageSend(t, original, h.oldSendTime(), 1)
	h.insertExpiredSignedClaim(t, original.Cid().String(), original.Cid().String(), retry)
	h.expectMpoolPush(t, nil)

	h.run(t)

	require.Equal(t, 0, h.signer.signCalls)
	require.Len(t, h.pushedMsgs, 1)
	require.Equal(t, retry.Cid(), h.pushedMsgs[0].Cid())

	var sendSuccess bool
	err := h.db.QueryRow(h.ctx, `
		SELECT send_success
		FROM message_send_replacements
		WHERE from_key = $1 AND nonce = $2`, from.String(), nonce).Scan(&sendSuccess)
	require.NoError(t, err)
	require.True(t, sendSuccess)
}

func TestFilecoinReplacerIgnoresYoungMessage(t *testing.T) {
	h := newFilecoinReplaceHarness(t)
	from := testIDAddress(t, 1004)
	to := testIDAddress(t, 2004)
	nonce := uint64(10)
	h.registerSender(t, from, nonce)

	original := signedMessageWithByte(testMessage(from, to, nonce, 100, 10), 1)
	h.insertMessageSend(t, original, h.youngSendTime(), 1)

	h.run(t)

	require.Equal(t, 0, h.signer.signCalls)
	require.Empty(t, h.pushedMsgs)
	require.Equal(t, 0, messageReplacementRowCount(t, h.db, h.ctx))
}

func TestFilecoinReplacerDeletesNotFeeStuckClaim(t *testing.T) {
	h := newFilecoinReplaceHarness(t)
	from := testIDAddress(t, 1005)
	to := testIDAddress(t, 2005)
	nonce := uint64(11)
	h.registerSender(t, from, nonce)

	original := signedMessageWithByte(testMessage(from, to, nonce, 300, 30), 1)
	h.insertMessageSend(t, original, h.oldSendTime(), 1)
	h.expectGasEstimate(t, func(msg *ltypes.Message) *ltypes.Message {
		estimated := *msg
		estimated.GasLimit = 1000
		estimated.GasFeeCap = abi.NewTokenAmount(200)
		estimated.GasPremium = abi.NewTokenAmount(20)
		return &estimated
	})

	h.run(t)

	require.Equal(t, 0, h.signer.signCalls)
	require.Empty(t, h.pushedMsgs)
	require.Equal(t, 0, messageReplacementRowCount(t, h.db, h.ctx))
}

func TestFilecoinReplacerDeletesInvalidSignedClaim(t *testing.T) {
	h := newFilecoinReplaceHarness(t)
	from := testIDAddress(t, 1006)
	to := testIDAddress(t, 2006)
	nonce := uint64(12)
	h.registerSender(t, from, nonce)

	original := signedMessageWithByte(testMessage(from, to, nonce, 100, 10), 1)
	h.insertMessageSend(t, original, h.oldSendTime(), 1)
	_, err := h.db.Exec(h.ctx, `
		INSERT INTO message_send_replacements (
			from_key, nonce, original_signed_cid, replaces_signed_cid,
			claim_id, claim_until, signed_cid, signed_data
		)
		VALUES ($1, $2, $3, $3, 'old-claim', CURRENT_TIMESTAMP - interval '1 minute', $4, $5)`,
		from.String(),
		nonce,
		original.Cid().String(),
		original.Cid().String(),
		[]byte("not-cbor"))
	require.NoError(t, err)

	h.run(t)

	require.Equal(t, 0, h.signer.signCalls)
	require.Empty(t, h.pushedMsgs)
	require.Equal(t, 0, messageReplacementRowCount(t, h.db, h.ctx))
}

func TestFilecoinReplacerDeletesStaleRowsWhenActorNonceAdvanced(t *testing.T) {
	h := newFilecoinReplaceHarness(t)
	from := testIDAddress(t, 1007)
	to := testIDAddress(t, 2007)
	nonce := uint64(13)
	h.registerSender(t, from, nonce+1)

	original := signedMessageWithByte(testMessage(from, to, nonce, 100, 10), 1)
	prior := signedMessageWithByte(testMessage(from, to, nonce, 300, 30), 2)
	h.insertMessageSend(t, original, h.oldSendTime(), 1)
	h.insertSuccessfulReplacement(t, original.Cid().String(), original.Cid().String(), prior, h.oldSendTime())

	h.run(t)

	require.Equal(t, 0, messageReplacementRowCount(t, h.db, h.ctx))
}

type ethReplaceHarness struct {
	ctx      context.Context
	db       *harmonydb.DB
	client   *replaceEthClient
	replacer *transactionReplacer
	now      time.Time
}

func newEthReplaceHarness(t *testing.T) *ethReplaceHarness {
	t.Helper()

	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	client := &replaceEthClient{
		header: &gethtypes.Header{
			Number:  mathbig.NewInt(100),
			BaseFee: mathbig.NewInt(150),
		},
		gasTipCap: mathbig.NewInt(20),
	}
	return &ethReplaceHarness{
		ctx:    ctx,
		db:     db,
		client: client,
		replacer: &transactionReplacer{
			db:               db,
			client:           client,
			stuckForDuration: replaceTestStuckDuration(),
		},
		now: time.Now().UTC().Truncate(time.Second),
	}
}

func (h *ethReplaceHarness) run(t *testing.T) {
	t.Helper()

	require.NoError(t, h.replacer.runTransactionReplacement(h.ctx, 100, h.now))
}

func (h *ethReplaceHarness) oldSendTime() time.Time {
	return h.now.Add(-h.replacer.stuckForDuration - time.Minute)
}

func (h *ethReplaceHarness) youngSendTime() time.Time {
	return h.now.Add(-time.Second)
}

func (h *ethReplaceHarness) insertKey(t *testing.T, from common.Address, privateKey []byte) {
	t.Helper()

	_, err := h.db.Exec(h.ctx, `
		INSERT INTO eth_keys (address, private_key, role)
		VALUES ($1, $2, 'replace-test')`, from.Hex(), privateKey)
	require.NoError(t, err)
}

func (h *ethReplaceHarness) insertTransactionSend(t *testing.T, from common.Address, to common.Address, tx *gethtypes.Transaction, txData []byte, sendTime time.Time) {
	t.Helper()

	_, err := h.db.Exec(h.ctx, `
		INSERT INTO message_sends_eth (
			from_address, to_address, send_reason,
			unsigned_tx, unsigned_hash,
			nonce, signed_tx, signed_hash,
			send_time, send_success
		)
		VALUES ($1, $2, 'replace-eth-test', $3, $4, $5, $6, $7, $8, TRUE)`,
		from.Hex(),
		to.Hex(),
		[]byte("unsigned-eth"),
		tx.Hash().Hex(),
		tx.Nonce(),
		txData,
		tx.Hash().Hex(),
		sendTime)
	require.NoError(t, err)
}

func (h *ethReplaceHarness) insertExpiredSignedClaim(t *testing.T, from common.Address, nonce uint64, originalHash string, replacesHash string, signedHash string, signedTx []byte) {
	t.Helper()

	_, err := h.db.Exec(h.ctx, `
		INSERT INTO message_send_eth_replacements (
			from_address, nonce, original_signed_hash, replaces_signed_hash,
			claim_id, claim_until, signed_hash, signed_tx
		)
		VALUES ($1, $2, $3, $4, 'old-claim', CURRENT_TIMESTAMP - interval '1 minute', $5, $6)`,
		from.Hex(),
		nonce,
		originalHash,
		replacesHash,
		signedHash,
		signedTx)
	require.NoError(t, err)
}

func (h *ethReplaceHarness) insertSuccessfulReplacement(t *testing.T, from common.Address, nonce uint64, originalHash string, replacesHash string, signedHash string, signedTx []byte, sendTime time.Time) {
	t.Helper()

	_, err := h.db.Exec(h.ctx, `
		INSERT INTO message_send_eth_replacements (
			from_address, nonce, original_signed_hash, replaces_signed_hash,
			claim_id, signed_hash, signed_tx,
			send_time, send_success
		)
		VALUES ($1, $2, $3, $4, 'prior-success', $5, $6, $7, TRUE)`,
		from.Hex(),
		nonce,
		originalHash,
		replacesHash,
		signedHash,
		signedTx,
		sendTime)
	require.NoError(t, err)
}

func TestTransactionReplacerReplacesFeeStuckTransaction(t *testing.T) {
	h := newEthReplaceHarness(t)
	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000001")
	nonce := uint64(11)
	h.client.nonce = nonce
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
	h.insertTransactionSend(t, from, to, original, originalData, h.oldSendTime())

	h.run(t)

	require.Len(t, h.client.sentTxs, 1)
	replacement := h.client.sentTxs[0]
	require.Equal(t, nonce, replacement.Nonce())
	require.Equal(t, uint64(21000), replacement.Gas())
	require.Equal(t, 0, replacement.GasFeeCap().Cmp(mathbig.NewInt(170)))
	require.Equal(t, 0, replacement.GasTipCap().Cmp(mathbig.NewInt(20)))
	require.NotEqual(t, original.Hash(), replacement.Hash())

	var signedHash string
	var signedTx []byte
	var sendSuccess bool
	err = h.db.QueryRow(h.ctx, `
		SELECT signed_hash, signed_tx, send_success
		FROM message_send_eth_replacements
		WHERE from_address = $1 AND nonce = $2`, from.Hex(), nonce).Scan(&signedHash, &signedTx, &sendSuccess)
	require.NoError(t, err)
	require.Equal(t, replacement.Hash().Hex(), signedHash)
	require.NotEmpty(t, signedTx)
	require.True(t, sendSuccess)
}

func TestTransactionReplacerAmbiguousSendSucceedsAfterExactLookup(t *testing.T) {
	h := newEthReplaceHarness(t)
	h.client.sendErr = errors.New(errMessageWithNonceExists)
	h.client.lookupSentTx = true

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000004")
	nonce := uint64(14)
	h.client.nonce = nonce
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
	h.insertTransactionSend(t, from, to, original, originalData, h.oldSendTime())

	h.run(t)

	require.Equal(t, 1, h.client.txCalls)

	var sendSuccess bool
	var sendError string
	err = h.db.QueryRow(h.ctx, `
		SELECT send_success, send_error
		FROM message_send_eth_replacements
		WHERE from_address = $1 AND nonce = $2`, from.Hex(), nonce).Scan(&sendSuccess, &sendError)
	require.NoError(t, err)
	require.True(t, sendSuccess)
	require.Empty(t, sendError)
}

func TestTransactionReplacerDoesNotTrustAmbiguousSendWithoutExactLookup(t *testing.T) {
	h := newEthReplaceHarness(t)
	h.client.sendErr = errors.New(errMessageWithNonceExists)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000009")
	nonce := uint64(19)
	h.client.nonce = nonce
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
	h.insertTransactionSend(t, from, to, original, originalData, h.oldSendTime())

	h.run(t)

	require.Equal(t, 1, h.client.txCalls)

	var signedHash sql.NullString
	var sendSuccess sql.NullBool
	var sendTime sql.NullTime
	var sendError sql.NullString
	err = h.db.QueryRow(h.ctx, `
		SELECT signed_hash, send_success, send_time, send_error
		FROM message_send_eth_replacements
		WHERE from_address = $1 AND nonce = $2`, from.Hex(), nonce).Scan(&signedHash, &sendSuccess, &sendTime, &sendError)
	require.NoError(t, err)
	require.True(t, signedHash.Valid)
	require.Equal(t, h.client.sentTxs[0].Hash().Hex(), signedHash.String)
	require.False(t, sendSuccess.Valid)
	require.False(t, sendTime.Valid)
	require.False(t, sendError.Valid)
}

func TestTransactionReplacerRetriesSignedClaim(t *testing.T) {
	h := newEthReplaceHarness(t)
	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000002")
	nonce := uint64(12)
	h.client.nonce = nonce
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
	retry, retryData := signedDynamicTx(t, privateKey, to, nonce, 300, 30)
	h.insertTransactionSend(t, from, to, original, originalData, h.oldSendTime())
	h.insertExpiredSignedClaim(t, from, nonce, original.Hash().Hex(), original.Hash().Hex(), retry.Hash().Hex(), retryData)

	h.run(t)

	require.Len(t, h.client.sentTxs, 1)
	require.Equal(t, retry.Hash(), h.client.sentTxs[0].Hash())
	require.Zero(t, h.client.headerCalls)
	require.Zero(t, h.client.tipCapCalls)

	var sendSuccess bool
	err = h.db.QueryRow(h.ctx, `
		SELECT send_success
		FROM message_send_eth_replacements
		WHERE from_address = $1 AND nonce = $2`, from.Hex(), nonce).Scan(&sendSuccess)
	require.NoError(t, err)
	require.True(t, sendSuccess)
}

func TestTransactionReplacerIgnoresYoungTransaction(t *testing.T) {
	h := newEthReplaceHarness(t)
	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000005")
	nonce := uint64(15)
	h.client.nonce = nonce
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
	h.insertTransactionSend(t, from, to, original, originalData, h.youngSendTime())

	h.run(t)

	require.Empty(t, h.client.sentTxs)
	require.Equal(t, 0, ethReplacementRowCount(t, h.db, h.ctx))
}

func TestTransactionReplacerDeletesNotFeeStuckClaim(t *testing.T) {
	h := newEthReplaceHarness(t)
	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000006")
	nonce := uint64(16)
	h.client.nonce = nonce
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 300, 30)
	h.insertTransactionSend(t, from, to, original, originalData, h.oldSendTime())

	h.run(t)

	require.Empty(t, h.client.sentTxs)
	require.Equal(t, 0, ethReplacementRowCount(t, h.db, h.ctx))
}

func TestTransactionReplacerDeletesInvalidSignedClaim(t *testing.T) {
	h := newEthReplaceHarness(t)
	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000007")
	nonce := uint64(17)
	h.client.nonce = nonce
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
	h.insertTransactionSend(t, from, to, original, originalData, h.oldSendTime())
	h.insertExpiredSignedClaim(t, from, nonce, original.Hash().Hex(), original.Hash().Hex(), original.Hash().Hex(), []byte("not-rlp"))

	h.run(t)

	require.Empty(t, h.client.sentTxs)
	require.Equal(t, 0, ethReplacementRowCount(t, h.db, h.ctx))
}

func TestTransactionReplacerDeletesStaleRowsWhenAccountNonceAdvanced(t *testing.T) {
	h := newEthReplaceHarness(t)
	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000008")
	nonce := uint64(18)
	h.client.nonce = nonce + 1
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
	prior, priorData := signedDynamicTx(t, privateKey, to, nonce, 300, 30)
	h.insertTransactionSend(t, from, to, original, originalData, h.oldSendTime())
	h.insertSuccessfulReplacement(t, from, nonce, original.Hash().Hex(), original.Hash().Hex(), prior.Hash().Hex(), priorData, h.oldSendTime())

	h.run(t)

	require.Equal(t, 0, ethReplacementRowCount(t, h.db, h.ctx))
}

func TestTransactionReplacerLeavesSignedClaimOnSendTimeout(t *testing.T) {
	h := newEthReplaceHarness(t)
	h.client.sendErr = context.DeadlineExceeded

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x1000000000000000000000000000000000000003")
	nonce := uint64(13)
	h.client.nonce = nonce
	h.insertKey(t, from, gethcrypto.FromECDSA(privateKey))

	original, originalData := signedDynamicTx(t, privateKey, to, nonce, 100, 10)
	h.insertTransactionSend(t, from, to, original, originalData, h.oldSendTime())

	h.run(t)

	require.Len(t, h.client.sentTxs, 1)
	require.Equal(t, 1, h.client.txCalls)

	var signedHash sql.NullString
	var sendSuccess sql.NullBool
	var sendTime sql.NullTime
	var sendError sql.NullString
	err = h.db.QueryRow(h.ctx, `
		SELECT signed_hash, send_success, send_time, send_error
		FROM message_send_eth_replacements
		WHERE from_address = $1 AND nonce = $2`, from.Hex(), nonce).Scan(&signedHash, &sendSuccess, &sendTime, &sendError)
	require.NoError(t, err)
	require.True(t, signedHash.Valid)
	require.Equal(t, h.client.sentTxs[0].Hash().Hex(), signedHash.String)
	require.False(t, sendSuccess.Valid)
	require.False(t, sendTime.Valid)
	require.False(t, sendError.Valid)
}

type replaceEthClient struct {
	ethchain.EthClient

	nonce         uint64
	nonceErr      error
	header        *gethtypes.Header
	headerErr     error
	gasTipCap     *mathbig.Int
	tipCapErr     error
	sendErr       error
	lookupSentTx  bool
	lookupPending bool
	lookupErr     error

	nonceCalls  int
	headerCalls int
	tipCapCalls int
	sendCalls   int
	txCalls     int
	sentTxs     []*gethtypes.Transaction
}

func (m *replaceEthClient) NonceAt(ctx context.Context, account common.Address, blockNumber *mathbig.Int) (uint64, error) {
	m.nonceCalls++
	if m.nonceErr != nil {
		return 0, m.nonceErr
	}
	return m.nonce, nil
}

func (m *replaceEthClient) HeaderByNumber(ctx context.Context, number *mathbig.Int) (*gethtypes.Header, error) {
	m.headerCalls++
	if m.headerErr != nil {
		return nil, m.headerErr
	}
	return m.header, nil
}

func (m *replaceEthClient) SuggestGasTipCap(ctx context.Context) (*mathbig.Int, error) {
	m.tipCapCalls++
	if m.tipCapErr != nil {
		return nil, m.tipCapErr
	}
	return new(mathbig.Int).Set(m.gasTipCap), nil
}

func (m *replaceEthClient) SendTransaction(ctx context.Context, tx *gethtypes.Transaction) error {
	m.sendCalls++
	m.sentTxs = append(m.sentTxs, tx)
	return m.sendErr
}

func (m *replaceEthClient) TransactionByHash(ctx context.Context, hash common.Hash) (*gethtypes.Transaction, bool, error) {
	m.txCalls++
	if m.lookupErr != nil {
		return nil, false, m.lookupErr
	}
	if m.lookupSentTx {
		for _, tx := range m.sentTxs {
			if tx.Hash() == hash {
				return tx, m.lookupPending, nil
			}
		}
	}
	return nil, false, ethereum.NotFound
}

func replaceTestStuckDuration() time.Duration {
	return time.Duration(ReplaceStuckEpochs) * time.Duration(build.BlockDelaySecs) * time.Second
}

func messageReplacementRowCount(t *testing.T, db *harmonydb.DB, ctx context.Context) int {
	t.Helper()

	var count int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM message_send_replacements`).Scan(&count)
	require.NoError(t, err)
	return count
}

func ethReplacementRowCount(t *testing.T, db *harmonydb.DB, ctx context.Context) int {
	t.Helper()

	var count int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM message_send_eth_replacements`).Scan(&count)
	require.NoError(t, err)
	return count
}

func signedDynamicTx(t *testing.T, privateKey *ecdsa.PrivateKey, to common.Address, nonce uint64, gasFeeCap int64, gasTipCap int64) (*gethtypes.Transaction, []byte) {
	t.Helper()

	chainID := mathbig.NewInt(int64(buildconstants.Eip155ChainId))
	tx := gethtypes.NewTx(&gethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: mathbig.NewInt(gasTipCap),
		GasFeeCap: mathbig.NewInt(gasFeeCap),
		Gas:       21000,
		To:        &to,
		Value:     mathbig.NewInt(123),
		Data:      []byte{1, 2, 3},
	})

	signedTx, err := gethtypes.SignTx(tx, gethtypes.LatestSignerForChainID(chainID), privateKey)
	require.NoError(t, err)
	data, err := signedTx.MarshalBinary()
	require.NoError(t, err)

	return signedTx, data
}

func signedMessageWithByte(msg *ltypes.Message, sigByte byte) *ltypes.SignedMessage {
	return &ltypes.SignedMessage{
		Message: *msg,
		Signature: filcrypto.Signature{
			Type: filcrypto.SigTypeSecp256k1,
			Data: bytes.Repeat([]byte{sigByte}, 65),
		},
	}
}

func testIDAddress(t *testing.T, id uint64) address.Address {
	t.Helper()

	addr, err := address.NewIDAddress(id)
	require.NoError(t, err)
	return addr
}

func testMessage(from address.Address, to address.Address, nonce uint64, gasFeeCap int64, gasPremium int64) *ltypes.Message {
	return &ltypes.Message{
		Version:    0,
		To:         to,
		From:       from,
		Nonce:      nonce,
		Value:      abi.NewTokenAmount(0),
		GasLimit:   1000,
		GasFeeCap:  abi.NewTokenAmount(gasFeeCap),
		GasPremium: abi.NewTokenAmount(gasPremium),
		Method:     abi.MethodNum(0),
	}
}
