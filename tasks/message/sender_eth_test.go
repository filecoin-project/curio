package message

import (
	"context"
	"crypto/ecdsa"
	"database/sql"
	"errors"
	mathbig "math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ethchain"

	"github.com/filecoin-project/lotus/build/buildconstants"
)

func TestSendTaskETHAmbiguousSendSucceedsAfterExactLookup(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000001")
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))

	taskID := harmonytask.TaskID(1)
	insertSendTaskETHTransaction(t, ctx, db, taskID, from, to)

	client := &sendTaskETHClient{
		pendingNonce:  11,
		sendErr:       errors.New(errMessageWithNonceExists),
		lookupSentTx:  true,
		lookupPending: true,
	}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, client.sendCalls)
	require.Equal(t, 1, client.txCalls)
	require.Len(t, client.sentTxs, 1)

	var signedHash string
	var sendSuccess bool
	var sendError string
	var sendTime sql.NullTime
	err = db.QueryRow(ctx, `
		SELECT signed_hash, send_success, send_error, send_time
		FROM message_sends_eth
		WHERE send_task_id = $1`, taskID).Scan(&signedHash, &sendSuccess, &sendError, &sendTime)
	require.NoError(t, err)
	require.Equal(t, client.sentTxs[0].Hash().Hex(), signedHash)
	require.True(t, sendSuccess)
	require.Empty(t, sendError)
	require.True(t, sendTime.Valid)
}

func TestSendTaskETHDoesNotTrustAmbiguousSendWithoutExactLookup(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000002")
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))

	taskID := harmonytask.TaskID(2)
	insertSendTaskETHTransaction(t, ctx, db, taskID, from, to)

	client := &sendTaskETHClient{
		pendingNonce: 12,
		sendErr:      errors.New(errMessageWithNonceExists),
	}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(taskID, func() bool { return true })
	require.Error(t, err)
	require.False(t, done)
	require.Equal(t, 1, client.sendCalls)
	require.Equal(t, 1, client.txCalls)
	require.Len(t, client.sentTxs, 1)

	var signedHash sql.NullString
	var sendSuccess sql.NullBool
	var sendError sql.NullString
	var sendTime sql.NullTime
	err = db.QueryRow(ctx, `
		SELECT signed_hash, send_success, send_error, send_time
		FROM message_sends_eth
		WHERE send_task_id = $1`, taskID).Scan(&signedHash, &sendSuccess, &sendError, &sendTime)
	require.NoError(t, err)
	require.True(t, signedHash.Valid)
	require.Equal(t, client.sentTxs[0].Hash().Hex(), signedHash.String)
	require.False(t, sendSuccess.Valid)
	require.False(t, sendError.Valid)
	require.False(t, sendTime.Valid)
}

func TestSendTaskETHFreshSendAssignsPendingNonce(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000003")
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))

	taskID := harmonytask.TaskID(3)
	insertSendTaskETHTransaction(t, ctx, db, taskID, from, to)

	client := &sendTaskETHClient{pendingNonce: 13}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, client.pendingNonceCalls)
	require.Equal(t, 1, client.networkIDCalls)
	require.Equal(t, 1, client.sendCalls)
	require.Equal(t, 0, client.txCalls)
	require.Len(t, client.sentTxs, 1)

	nonce, signedHash, signedTxData, sendSuccess := loadSendTaskETHFinalState(t, ctx, db, taskID)
	require.Equal(t, uint64(13), nonce)
	require.Equal(t, client.sentTxs[0].Hash().Hex(), signedHash)
	require.True(t, sendSuccess)

	signedTx := unmarshalSendTaskETHSignedTx(t, signedTxData)
	require.Equal(t, uint64(13), signedTx.Nonce())
	require.Equal(t, client.sentTxs[0].Hash(), signedTx.Hash())
}

func TestSendTaskETHFreshSendUsesDBNonceWhenItIsAhead(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000004")
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))
	insertSuccessfulSendTaskETHNonce(t, ctx, db, harmonytask.TaskID(40), from, to, privateKey, 20)

	taskID := harmonytask.TaskID(4)
	insertSendTaskETHTransaction(t, ctx, db, taskID, from, to)

	client := &sendTaskETHClient{pendingNonce: 13}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, client.pendingNonceCalls)
	require.Equal(t, 1, client.networkIDCalls)
	require.Equal(t, 1, client.sendCalls)
	require.Len(t, client.sentTxs, 1)

	nonce, signedHash, _, sendSuccess := loadSendTaskETHFinalState(t, ctx, db, taskID)
	require.Equal(t, uint64(21), nonce)
	require.Equal(t, client.sentTxs[0].Hash().Hex(), signedHash)
	require.True(t, sendSuccess)
	require.Equal(t, uint64(21), client.sentTxs[0].Nonce())
}

func TestSendTaskETHRetryUsesStoredSignedTransaction(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000005")
	signedTx, signedTxData := signedSendTaskETHTransaction(t, privateKey, to, 15)
	insertSignedSendTaskETHTransaction(t, ctx, db, harmonytask.TaskID(5), from, to, signedTx, signedTxData)

	client := &sendTaskETHClient{pendingNonce: 99}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(harmonytask.TaskID(5), func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 0, client.pendingNonceCalls)
	require.Equal(t, 0, client.networkIDCalls)
	require.Equal(t, 1, client.sendCalls)
	require.Len(t, client.sentTxs, 1)
	require.Equal(t, signedTx.Hash(), client.sentTxs[0].Hash())

	nonce, signedHash, _, sendSuccess := loadSendTaskETHFinalState(t, ctx, db, harmonytask.TaskID(5))
	require.Equal(t, uint64(15), nonce)
	require.Equal(t, signedTx.Hash().Hex(), signedHash)
	require.True(t, sendSuccess)
}

func TestSendTaskETHDefinitiveSendFailureFinalizesRow(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000006")
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))

	taskID := harmonytask.TaskID(6)
	insertSendTaskETHTransaction(t, ctx, db, taskID, from, to)

	client := &sendTaskETHClient{
		pendingNonce: 16,
		sendErr:      errors.New("gas fee cap too low"),
	}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, client.sendCalls)
	require.Equal(t, 0, client.txCalls)

	var sendSuccess bool
	var sendError string
	var sendTime sql.NullTime
	err = db.QueryRow(ctx, `
		SELECT send_success, send_error, send_time
		FROM message_sends_eth
		WHERE send_task_id = $1`, taskID).Scan(&sendSuccess, &sendError, &sendTime)
	require.NoError(t, err)
	require.False(t, sendSuccess)
	require.Contains(t, sendError, "gas fee cap too low")
	require.True(t, sendTime.Valid)
}

func TestSendTaskETHAlreadyFinalizedRowIsNotResent(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000007")
	insertSuccessfulSendTaskETHNonce(t, ctx, db, harmonytask.TaskID(7), from, to, privateKey, 17)

	client := &sendTaskETHClient{pendingNonce: 17}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(harmonytask.TaskID(7), func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 0, client.pendingNonceCalls)
	require.Equal(t, 0, client.networkIDCalls)
	require.Equal(t, 0, client.sendCalls)
	require.Equal(t, 0, client.txCalls)
}

func insertSendTaskETHKey(t *testing.T, ctx context.Context, db *harmonydb.DB, from common.Address, privateKey []byte) {
	t.Helper()

	_, err := db.Exec(ctx, `
		INSERT INTO eth_keys (address, private_key, role)
		VALUES ($1, $2, 'send-task-eth-test')`, from.Hex(), privateKey)
	require.NoError(t, err)
}

func insertSendTaskETHTransaction(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, from common.Address, to common.Address) {
	t.Helper()

	tx := unsignedSendTaskETHTransaction(to)
	unsignedTx, err := tx.MarshalBinary()
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO message_sends_eth (
			from_address, to_address, send_reason, send_task_id,
			unsigned_tx, unsigned_hash
		)
		VALUES ($1, $2, 'send-task-eth-test', $3, $4, $5)`,
		from.Hex(), to.Hex(), taskID, unsignedTx, tx.Hash().Hex())
	require.NoError(t, err)
}

func insertSignedSendTaskETHTransaction(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, from common.Address, to common.Address, signedTx *gethtypes.Transaction, signedTxData []byte) {
	t.Helper()

	unsignedTx := unsignedSendTaskETHTransaction(to)
	unsignedTxData, err := unsignedTx.MarshalBinary()
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO message_sends_eth (
			from_address, to_address, send_reason, send_task_id,
			unsigned_tx, unsigned_hash,
			nonce, signed_tx, signed_hash
		)
		VALUES ($1, $2, 'send-task-eth-test', $3, $4, $5, $6, $7, $8)`,
		from.Hex(),
		to.Hex(),
		taskID,
		unsignedTxData,
		unsignedTx.Hash().Hex(),
		signedTx.Nonce(),
		signedTxData,
		signedTx.Hash().Hex())
	require.NoError(t, err)
}

func insertSuccessfulSendTaskETHNonce(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, from common.Address, to common.Address, privateKey *ecdsa.PrivateKey, nonce uint64) {
	t.Helper()

	signedTx, signedTxData := signedSendTaskETHTransaction(t, privateKey, to, nonce)
	insertSignedSendTaskETHTransaction(t, ctx, db, taskID, from, to, signedTx, signedTxData)

	_, err := db.Exec(ctx, `
		UPDATE message_sends_eth
		SET send_success = TRUE, send_error = '', send_time = CURRENT_TIMESTAMP
		WHERE send_task_id = $1`, taskID)
	require.NoError(t, err)
}

func loadSendTaskETHFinalState(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID) (uint64, string, []byte, bool) {
	t.Helper()

	var nonce uint64
	var signedHash string
	var signedTx []byte
	var sendSuccess bool
	err := db.QueryRow(ctx, `
		SELECT nonce, signed_hash, signed_tx, send_success
		FROM message_sends_eth
		WHERE send_task_id = $1`, taskID).Scan(&nonce, &signedHash, &signedTx, &sendSuccess)
	require.NoError(t, err)
	return nonce, signedHash, signedTx, sendSuccess
}

func signedSendTaskETHTransaction(t *testing.T, privateKey *ecdsa.PrivateKey, to common.Address, nonce uint64) (*gethtypes.Transaction, []byte) {
	t.Helper()

	chainID := mathbig.NewInt(int64(buildconstants.Eip155ChainId))
	tx := gethtypes.NewTx(&gethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: mathbig.NewInt(10),
		GasFeeCap: mathbig.NewInt(100),
		Gas:       21000,
		To:        &to,
		Value:     mathbig.NewInt(123),
		Data:      []byte{1, 2, 3},
	})

	signedTx, err := gethtypes.SignTx(tx, gethtypes.LatestSignerForChainID(chainID), privateKey)
	require.NoError(t, err)
	signedTxData, err := signedTx.MarshalBinary()
	require.NoError(t, err)

	return signedTx, signedTxData
}

func unmarshalSendTaskETHSignedTx(t *testing.T, data []byte) *gethtypes.Transaction {
	t.Helper()

	tx := new(gethtypes.Transaction)
	require.NoError(t, tx.UnmarshalBinary(data))
	return tx
}

func unsignedSendTaskETHTransaction(to common.Address) *gethtypes.Transaction {
	return gethtypes.NewTx(&gethtypes.DynamicFeeTx{
		ChainID:   mathbig.NewInt(int64(buildconstants.Eip155ChainId)),
		Nonce:     0,
		GasTipCap: mathbig.NewInt(10),
		GasFeeCap: mathbig.NewInt(100),
		Gas:       21000,
		To:        &to,
		Value:     mathbig.NewInt(123),
		Data:      []byte{1, 2, 3},
	})
}

type sendTaskETHClient struct {
	ethchain.EthClient

	pendingNonce uint64
	networkID    *mathbig.Int
	sendErr      error

	lookupSentTx  bool
	lookupPending bool
	lookupErr     error

	pendingNonceCalls int
	networkIDCalls    int
	sendCalls         int
	txCalls           int
	sentTxs           []*gethtypes.Transaction
}

func (m *sendTaskETHClient) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	m.pendingNonceCalls++
	return m.pendingNonce, nil
}

func (m *sendTaskETHClient) NetworkID(ctx context.Context) (*mathbig.Int, error) {
	m.networkIDCalls++
	if m.networkID != nil {
		return new(mathbig.Int).Set(m.networkID), nil
	}
	return mathbig.NewInt(int64(buildconstants.Eip155ChainId)), nil
}

func (m *sendTaskETHClient) SendTransaction(ctx context.Context, tx *gethtypes.Transaction) error {
	m.sendCalls++
	m.sentTxs = append(m.sentTxs, tx)
	return m.sendErr
}

func (m *sendTaskETHClient) TransactionByHash(ctx context.Context, hash common.Hash) (*gethtypes.Transaction, bool, error) {
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
