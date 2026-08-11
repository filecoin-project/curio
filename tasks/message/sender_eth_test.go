package message

import (
	"context"
	"crypto/ecdsa"
	"database/sql"
	"errors"
	mathbig "math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/tasks/tasknames"

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

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, client.sendCalls)
	require.Equal(t, 1, client.txCalls)
	require.Equal(t, 1, client.pendingNonceCalls)
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
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
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

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.Error(t, err)
	require.False(t, done)
	require.Equal(t, 1, client.sendCalls)
	require.Equal(t, 1, client.txCalls)
	require.Equal(t, 1, client.pendingNonceCalls)
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
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
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

	done, err := task.Do(ctx, taskID, func() bool { return true })
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
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
}

func TestSendTaskETHFinalizationRollsBackWhenLockReleaseFails(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000008")
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))

	taskID := harmonytask.TaskID(8)
	insertSendTaskETHTransaction(t, ctx, db, taskID, from, to)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from.Hex(), taskID)
	require.NoError(t, err)
	blockEthSendLockDelete(t, ctx, db, from.Hex())

	client := &sendTaskETHClient{pendingNonce: 18}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.Error(t, err)
	require.False(t, done)
	require.Equal(t, 1, client.sendCalls)

	var sendSuccess sql.NullBool
	var sendError sql.NullString
	var sendTime sql.NullTime
	require.NoError(t, db.QueryRow(ctx, `
		SELECT send_success, send_error, send_time
		FROM message_sends_eth
		WHERE send_task_id = $1`, taskID).Scan(&sendSuccess, &sendError, &sendTime))
	require.False(t, sendSuccess.Valid)
	require.False(t, sendError.Valid)
	require.False(t, sendTime.Valid)
	require.Equal(t, int64(taskID), ethSendLockOwner(t, ctx, db, from.Hex()))

	_, err = db.Exec(ctx, `
		DELETE FROM message_send_eth_lock_delete_blocker
		WHERE from_address = $1`, from.Hex())
	require.NoError(t, err)

	done, err = task.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, client.pendingNonceCalls, "retry must reuse the stored nonce")
	require.Equal(t, 1, client.networkIDCalls, "retry must reuse the stored signature")
	require.Equal(t, 2, client.sendCalls)
	require.Len(t, client.sentTxs, 2)
	require.Equal(t, client.sentTxs[0].Hash(), client.sentTxs[1].Hash())

	require.NoError(t, db.QueryRow(ctx, `
		SELECT send_success, send_error, send_time
		FROM message_sends_eth
		WHERE send_task_id = $1`, taskID).Scan(&sendSuccess, &sendError, &sendTime))
	require.Equal(t, sql.NullBool{Bool: true, Valid: true}, sendSuccess)
	require.Equal(t, sql.NullString{String: "", Valid: true}, sendError)
	require.True(t, sendTime.Valid)
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
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

	done, err := task.Do(ctx, taskID, func() bool { return true })
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
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
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
	taskID := harmonytask.TaskID(5)
	insertSignedSendTaskETHTransaction(t, ctx, db, taskID, from, to, signedTx, signedTxData)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from.Hex(), taskID)
	require.NoError(t, err)

	client := &sendTaskETHClient{pendingNonce: 99}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 0, client.pendingNonceCalls)
	require.Equal(t, 0, client.networkIDCalls)
	require.Equal(t, 1, client.sendCalls)
	require.Len(t, client.sentTxs, 1)
	require.Equal(t, signedTx.Hash(), client.sentTxs[0].Hash())

	nonce, signedHash, _, sendSuccess := loadSendTaskETHFinalState(t, ctx, db, taskID)
	require.Equal(t, uint64(15), nonce)
	require.Equal(t, signedTx.Hash().Hex(), signedHash)
	require.True(t, sendSuccess)
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
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

	done, err := task.Do(ctx, taskID, func() bool { return true })
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
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
}

func TestSendTaskETHAlreadyFinalizedRowIsNotResent(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000007")
	taskID := harmonytask.TaskID(7)
	insertSuccessfulSendTaskETHNonce(t, ctx, db, taskID, from, to, privateKey, 17)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from.Hex(), taskID)
	require.NoError(t, err)

	client := &sendTaskETHClient{pendingNonce: 17}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 0, client.pendingNonceCalls)
	require.Equal(t, 0, client.networkIDCalls)
	require.Equal(t, 0, client.sendCalls)
	require.Equal(t, 0, client.txCalls)
	require.Equal(t, 1, ethSendLockCount(t, ctx, db, from.Hex()))

	// Harmony removes the completed task after Do returns. The task FK must
	// cascade that deletion to a lock stranded by an older sender version.
	_, err = db.Exec(ctx, `DELETE FROM harmony_task WHERE id = $1`, taskID)
	require.NoError(t, err)
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
}

func TestSendTaskETHPreSendErrorReleasesLockWithoutFinalizing(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x200000000000000000000000000000000000000a")
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))

	taskID := harmonytask.TaskID(10)
	insertSendTaskETHTransaction(t, ctx, db, taskID, from, to)
	client := &sendTaskETHClient{pendingNonceErr: errors.New("pending nonce lookup failed")}
	task := &SendTaskETH{client: client, db: db}

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.ErrorContains(t, err, "pending nonce lookup failed")
	require.False(t, done)
	require.Equal(t, 1, client.pendingNonceCalls)
	require.Zero(t, client.networkIDCalls)
	require.Zero(t, client.sendCalls)

	assertEthSendPending(t, ctx, db, taskID)
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
}

func TestSendTaskETHDoesNotStealAnotherTasksLock(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress("0x200000000000000000000000000000000000000b")
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))

	taskID := harmonytask.TaskID(11)
	holderTaskID := harmonytask.TaskID(110)
	insertSendTaskETHTransaction(t, ctx, db, taskID, from, to)
	insertSenderHarmonyTask(t, ctx, db, holderTaskID, tasknames.SendTransaction)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from.Hex(), holderTaskID)
	require.NoError(t, err)

	client := &sendTaskETHClient{pendingNonce: 21}
	task := &SendTaskETH{client: client, db: db}
	ownershipChecks := 0

	done, err := task.Do(ctx, taskID, func() bool {
		ownershipChecks++
		return ownershipChecks == 1
	})
	require.ErrorContains(t, err, "lost ownership")
	require.False(t, done)
	require.Equal(t, 2, ownershipChecks)
	require.Zero(t, client.pendingNonceCalls)
	require.Zero(t, client.networkIDCalls)
	require.Zero(t, client.sendCalls)

	assertEthSendPending(t, ctx, db, taskID)
	require.Equal(t, int64(holderTaskID), ethSendLockOwner(t, ctx, db, from.Hex()))
}

func TestSendTaskETHSerializesContendedSender(t *testing.T) {
	originalWait := SendLockedWait
	SendLockedWait = time.Millisecond
	t.Cleanup(func() { SendLockedWait = originalWait })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	privateKey, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(privateKey.PublicKey)
	insertSendTaskETHKey(t, ctx, db, from, gethcrypto.FromECDSA(privateKey))

	firstTaskID := harmonytask.TaskID(12)
	secondTaskID := harmonytask.TaskID(13)
	insertSendTaskETHTransaction(t, ctx, db, firstTaskID, from,
		common.HexToAddress("0x200000000000000000000000000000000000000c"))
	insertSendTaskETHTransaction(t, ctx, db, secondTaskID, from,
		common.HexToAddress("0x200000000000000000000000000000000000000d"))

	firstSendEntered := make(chan struct{})
	releaseFirstSend := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseFirstSend) }) })

	firstClient := &sendTaskETHClient{
		pendingNonce: 40,
		sendEntered:  firstSendEntered,
		sendRelease:  releaseFirstSend,
	}
	secondClient := &sendTaskETHClient{pendingNonce: 40}
	firstTask := &SendTaskETH{client: firstClient, db: db}
	secondTask := &SendTaskETH{client: secondClient, db: db}

	firstResult := make(chan senderTestResult, 1)
	go func() {
		done, err := firstTask.Do(ctx, firstTaskID, func() bool { return true })
		firstResult <- senderTestResult{done: done, err: err}
	}()
	waitForSenderTestSignal(t, ctx, firstSendEntered, "first ETH sender to enter SendTransaction")
	require.Equal(t, int64(firstTaskID), ethSendLockOwner(t, ctx, db, from.Hex()))

	secondContended := make(chan struct{})
	var secondOwnershipChecks atomic.Int32
	secondResult := make(chan senderTestResult, 1)
	go func() {
		done, err := secondTask.Do(ctx, secondTaskID, func() bool {
			if secondOwnershipChecks.Add(1) == 2 {
				close(secondContended)
			}
			return true
		})
		secondResult <- senderTestResult{done: done, err: err}
	}()
	waitForSenderTestSignal(t, ctx, secondContended, "second ETH sender to contend on the lock")
	require.Equal(t, int64(firstTaskID), ethSendLockOwner(t, ctx, db, from.Hex()))
	require.Zero(t, secondClient.sendCalls)

	releaseOnce.Do(func() { close(releaseFirstSend) })
	first := waitForSenderTestResult(t, ctx, firstResult, "first ETH sender")
	second := waitForSenderTestResult(t, ctx, secondResult, "second ETH sender")
	require.NoError(t, first.err)
	require.True(t, first.done)
	require.NoError(t, second.err)
	require.True(t, second.done)
	require.GreaterOrEqual(t, secondOwnershipChecks.Load(), int32(2))
	require.Equal(t, 1, firstClient.sendCalls)
	require.Equal(t, 1, secondClient.sendCalls)

	var firstNonce, secondNonce uint64
	var firstSuccess, secondSuccess bool
	require.NoError(t, db.QueryRow(ctx, `
		SELECT nonce, send_success FROM message_sends_eth WHERE send_task_id = $1`, firstTaskID).Scan(&firstNonce, &firstSuccess))
	require.NoError(t, db.QueryRow(ctx, `
		SELECT nonce, send_success FROM message_sends_eth WHERE send_task_id = $1`, secondTaskID).Scan(&secondNonce, &secondSuccess))
	require.Equal(t, uint64(40), firstNonce)
	require.Equal(t, uint64(41), secondNonce)
	require.True(t, firstSuccess)
	require.True(t, secondSuccess)
	require.Zero(t, ethSendLockCount(t, ctx, db, from.Hex()))
}

func insertSendTaskETHKey(t *testing.T, ctx context.Context, db *harmonydb.DB, from common.Address, privateKey []byte) {
	t.Helper()

	_, err := db.Exec(ctx, `
		INSERT INTO eth_keys (address, private_key, role)
		VALUES ($1, $2, 'send-task-eth-test')`, from.Hex(), privateKey)
	require.NoError(t, err)
}

func blockEthSendLockDelete(t *testing.T, ctx context.Context, db *harmonydb.DB, from string) {
	t.Helper()

	_, err := db.Exec(ctx, `
		CREATE TABLE message_send_eth_lock_delete_blocker (
			from_address TEXT PRIMARY KEY REFERENCES message_send_eth_locks (from_address)
		)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_lock_delete_blocker (from_address)
		VALUES ($1)`, from)
	require.NoError(t, err)
}

func ethSendLockCount(t *testing.T, ctx context.Context, db *harmonydb.DB, from string) int {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRow(ctx, `
		SELECT count(*) FROM message_send_eth_locks WHERE from_address = $1`, from).Scan(&count))
	return count
}

func ethSendLockOwner(t *testing.T, ctx context.Context, db *harmonydb.DB, from string) int64 {
	t.Helper()

	var taskID int64
	require.NoError(t, db.QueryRow(ctx, `
		SELECT task_id FROM message_send_eth_locks WHERE from_address = $1`, from).Scan(&taskID))
	return taskID
}

func assertEthSendPending(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID) {
	t.Helper()

	var nonce sql.NullInt64
	var sendSuccess sql.NullBool
	var sendError sql.NullString
	var sendTime sql.NullTime
	require.NoError(t, db.QueryRow(ctx, `
		SELECT nonce, send_success, send_error, send_time
		FROM message_sends_eth
		WHERE send_task_id = $1`, taskID).Scan(&nonce, &sendSuccess, &sendError, &sendTime))
	require.False(t, nonce.Valid)
	require.False(t, sendSuccess.Valid)
	require.False(t, sendError.Valid)
	require.False(t, sendTime.Valid)
}

func insertSendTaskETHTransaction(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, from common.Address, to common.Address) {
	t.Helper()
	insertSenderHarmonyTask(t, ctx, db, taskID, tasknames.SendTransaction)

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
	insertSenderHarmonyTask(t, ctx, db, taskID, tasknames.SendTransaction)

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

	pendingNonce    uint64
	pendingNonceErr error
	networkID       *mathbig.Int
	sendErr         error

	lookupSentTx  bool
	lookupPending bool
	lookupErr     error

	pendingNonceCalls int
	networkIDCalls    int
	sendCalls         int
	txCalls           int
	sentTxs           []*gethtypes.Transaction
	sendEntered       chan struct{}
	sendRelease       <-chan struct{}
	sendEnterOnce     sync.Once
}

func (m *sendTaskETHClient) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	m.pendingNonceCalls++
	return m.pendingNonce, m.pendingNonceErr
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
	if m.sendEntered != nil {
		m.sendEnterOnce.Do(func() { close(m.sendEntered) })
	}
	if m.sendRelease != nil {
		select {
		case <-m.sendRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
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
