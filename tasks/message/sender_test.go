package message

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/tasks/tasknames"

	"github.com/filecoin-project/lotus/chain/types"
)

func TestSendTaskFinalizationReleasesLock(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := senderTestIDAddress(t, 1001)
	to := senderTestIDAddress(t, 1002)
	taskID := harmonytask.TaskID(1)
	insertSendTaskMessage(t, ctx, db, taskID, from, to)

	api := &senderTestAPI{nonce: 11}
	signer := &senderTestSigner{}
	task := &SendTask{
		api:    api,
		signer: signer,
		db:     db,
	}

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, api.nonceCalls)
	require.Equal(t, 1, api.pushCalls)
	require.Equal(t, 1, signer.calls)

	var sendSuccess sql.NullBool
	var sendTime sql.NullTime
	require.NoError(t, db.QueryRow(ctx, `
		SELECT send_success, send_time
		FROM message_sends
		WHERE send_task_id = $1`, taskID).Scan(&sendSuccess, &sendTime))
	require.Equal(t, sql.NullBool{Bool: true, Valid: true}, sendSuccess)
	require.True(t, sendTime.Valid)
	require.Zero(t, messageSendLockCount(t, ctx, db, from.String()))
}

func TestSendTaskPushFailureFinalizesAndReleasesLock(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := senderTestIDAddress(t, 1003)
	to := senderTestIDAddress(t, 1004)
	taskID := harmonytask.TaskID(2)
	insertSendTaskMessage(t, ctx, db, taskID, from, to)

	pushErr := errors.New("push failed")
	api := &senderTestAPI{nonce: 12, pushErr: pushErr}
	task := &SendTask{api: api, signer: &senderTestSigner{}, db: db}

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, api.pushCalls)

	var sendSuccess sql.NullBool
	var sendError sql.NullString
	var sendTime sql.NullTime
	require.NoError(t, db.QueryRow(ctx, `
		SELECT send_success, send_error, send_time
		FROM message_sends
		WHERE send_task_id = $1`, taskID).Scan(&sendSuccess, &sendError, &sendTime))
	require.Equal(t, sql.NullBool{Bool: false, Valid: true}, sendSuccess)
	require.Equal(t, sql.NullString{String: pushErr.Error(), Valid: true}, sendError)
	require.True(t, sendTime.Valid)
	require.Zero(t, messageSendLockCount(t, ctx, db, from.String()))
}

func TestSendTaskPreSendErrorReleasesLockWithoutFinalizing(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := senderTestIDAddress(t, 1005)
	to := senderTestIDAddress(t, 1006)
	taskID := harmonytask.TaskID(3)
	insertSendTaskMessage(t, ctx, db, taskID, from, to)

	api := &senderTestAPI{nonceErr: errors.New("nonce lookup failed")}
	signer := &senderTestSigner{}
	task := &SendTask{api: api, signer: signer, db: db}

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.ErrorContains(t, err, "nonce lookup failed")
	require.False(t, done)
	require.Equal(t, 1, api.nonceCalls)
	require.Zero(t, api.pushCalls)
	require.Zero(t, signer.calls)

	assertMessageSendPending(t, ctx, db, taskID)
	require.Zero(t, messageSendLockCount(t, ctx, db, from.String()))
}

func TestSendTaskDoesNotStealAnotherTasksLock(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := senderTestIDAddress(t, 1007)
	to := senderTestIDAddress(t, 1008)
	taskID := harmonytask.TaskID(4)
	holderTaskID := harmonytask.TaskID(40)
	insertSendTaskMessage(t, ctx, db, taskID, from, to)
	insertSenderHarmonyTask(t, ctx, db, holderTaskID, tasknames.SendMessage)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_locks (from_key, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from.String(), holderTaskID)
	require.NoError(t, err)

	api := &senderTestAPI{nonce: 14}
	signer := &senderTestSigner{}
	task := &SendTask{api: api, signer: signer, db: db}
	ownershipChecks := 0

	done, err := task.Do(ctx, taskID, func() bool {
		ownershipChecks++
		return ownershipChecks == 1
	})
	require.ErrorContains(t, err, "lost ownership")
	require.False(t, done)
	require.Equal(t, 2, ownershipChecks)
	require.Zero(t, api.nonceCalls)
	require.Zero(t, api.pushCalls)
	require.Zero(t, signer.calls)

	assertMessageSendPending(t, ctx, db, taskID)
	require.Equal(t, int64(holderTaskID), messageSendLockOwner(t, ctx, db, from.String()))
}

func TestSendTasksSerializeContendedSender(t *testing.T) {
	originalWait := SendLockedWait
	SendLockedWait = time.Millisecond
	t.Cleanup(func() { SendLockedWait = originalWait })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := senderTestIDAddress(t, 1021)
	firstTaskID := harmonytask.TaskID(7)
	secondTaskID := harmonytask.TaskID(8)
	insertSendTaskMessage(t, ctx, db, firstTaskID, from, senderTestIDAddress(t, 1022))
	insertSendTaskMessage(t, ctx, db, secondTaskID, from, senderTestIDAddress(t, 1023))

	firstPushEntered := make(chan struct{})
	releaseFirstPush := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseFirstPush) }) })

	firstAPI := &senderTestAPI{
		nonce:       30,
		pushEntered: firstPushEntered,
		pushRelease: releaseFirstPush,
	}
	secondAPI := &senderTestAPI{nonce: 30}
	firstTask := &SendTask{api: firstAPI, signer: &senderTestSigner{}, db: db}
	secondTask := &SendTask{api: secondAPI, signer: &senderTestSigner{}, db: db}

	firstResult := make(chan senderTestResult, 1)
	go func() {
		done, err := firstTask.Do(ctx, firstTaskID, func() bool { return true })
		firstResult <- senderTestResult{done: done, err: err}
	}()
	waitForSenderTestSignal(t, ctx, firstPushEntered, "first sender to enter MpoolPush")
	require.Equal(t, int64(firstTaskID), messageSendLockOwner(t, ctx, db, from.String()))

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
	waitForSenderTestSignal(t, ctx, secondContended, "second sender to contend on the lock")
	require.Equal(t, int64(firstTaskID), messageSendLockOwner(t, ctx, db, from.String()))
	require.Zero(t, secondAPI.pushCalls)

	releaseOnce.Do(func() { close(releaseFirstPush) })
	first := waitForSenderTestResult(t, ctx, firstResult, "first sender")
	second := waitForSenderTestResult(t, ctx, secondResult, "second sender")
	require.NoError(t, first.err)
	require.True(t, first.done)
	require.NoError(t, second.err)
	require.True(t, second.done)
	require.GreaterOrEqual(t, secondOwnershipChecks.Load(), int32(2))
	require.Equal(t, 1, firstAPI.pushCalls)
	require.Equal(t, 1, secondAPI.pushCalls)

	var firstNonce, secondNonce uint64
	var firstSuccess, secondSuccess bool
	require.NoError(t, db.QueryRow(ctx, `
		SELECT nonce, send_success FROM message_sends WHERE send_task_id = $1`, firstTaskID).Scan(&firstNonce, &firstSuccess))
	require.NoError(t, db.QueryRow(ctx, `
		SELECT nonce, send_success FROM message_sends WHERE send_task_id = $1`, secondTaskID).Scan(&secondNonce, &secondSuccess))
	require.Equal(t, uint64(30), firstNonce)
	require.Equal(t, uint64(31), secondNonce)
	require.True(t, firstSuccess)
	require.True(t, secondSuccess)
	require.Zero(t, messageSendLockCount(t, ctx, db, from.String()))
}

func TestSendTaskFinalizationRollsBackAndRetryReleasesLock(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := senderTestIDAddress(t, 1011)
	to := senderTestIDAddress(t, 1012)
	taskID := harmonytask.TaskID(6)
	insertSendTaskMessage(t, ctx, db, taskID, from, to)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_locks (from_key, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from.String(), taskID)
	require.NoError(t, err)
	blockMessageSendLockDelete(t, ctx, db, from.String())

	api := &senderTestAPI{nonce: 16}
	signer := &senderTestSigner{}
	task := &SendTask{
		api:    api,
		signer: signer,
		db:     db,
	}

	done, err := task.Do(ctx, taskID, func() bool { return true })
	require.Error(t, err)
	require.False(t, done)
	require.Equal(t, 1, api.nonceCalls)
	require.Equal(t, 1, api.pushCalls)
	require.Equal(t, 1, signer.calls)
	require.Len(t, api.pushedMessages, 1)

	var sendSuccess sql.NullBool
	var sendError sql.NullString
	var sendTime sql.NullTime
	require.NoError(t, db.QueryRow(ctx, `
		SELECT send_success, send_error, send_time
		FROM message_sends
		WHERE send_task_id = $1`, taskID).Scan(&sendSuccess, &sendError, &sendTime))
	require.False(t, sendSuccess.Valid)
	require.False(t, sendError.Valid)
	require.False(t, sendTime.Valid)
	require.Equal(t, int64(taskID), messageSendLockOwner(t, ctx, db, from.String()))

	_, err = db.Exec(ctx, `
		DELETE FROM message_send_lock_delete_blocker
		WHERE from_key = $1`, from.String())
	require.NoError(t, err)

	done, err = task.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 1, api.nonceCalls, "retry must reuse the stored nonce")
	require.Equal(t, 2, api.pushCalls)
	require.Equal(t, 1, signer.calls, "retry must reuse the stored signature")
	require.Len(t, api.pushedMessages, 2)
	require.Equal(t, api.pushedMessages[0].Cid(), api.pushedMessages[1].Cid())

	require.NoError(t, db.QueryRow(ctx, `
		SELECT send_success, send_error, send_time
		FROM message_sends
		WHERE send_task_id = $1`, taskID).Scan(&sendSuccess, &sendError, &sendTime))
	require.Equal(t, sql.NullBool{Bool: true, Valid: true}, sendSuccess)
	require.Equal(t, sql.NullString{String: "", Valid: true}, sendError)
	require.True(t, sendTime.Valid)
	require.Zero(t, messageSendLockCount(t, ctx, db, from.String()))
}

func TestSendLocksCascadeWhenHarmonyTaskIsDeleted(t *testing.T) {
	t.Run("Filecoin", func(t *testing.T) {
		ctx := t.Context()
		db, err := harmonydb.NewFromConfigWithITestID(t)
		require.NoError(t, err)

		taskID := harmonytask.TaskID(9001)
		from := "f1cascade-lock"
		insertSenderHarmonyTask(t, ctx, db, taskID, tasknames.SendMessage)
		_, err = db.Exec(ctx, `
			INSERT INTO message_send_locks (from_key, task_id, claimed_at)
			VALUES ($1, $2, CURRENT_TIMESTAMP)`, from, taskID)
		require.NoError(t, err)

		_, err = db.Exec(ctx, `DELETE FROM harmony_task WHERE id = $1`, taskID)
		require.NoError(t, err)
		require.Zero(t, messageSendLockCount(t, ctx, db, from))
	})

	t.Run("ETH", func(t *testing.T) {
		ctx := t.Context()
		db, err := harmonydb.NewFromConfigWithITestID(t)
		require.NoError(t, err)

		taskID := harmonytask.TaskID(9002)
		from := "0xcascade-lock"
		insertSenderHarmonyTask(t, ctx, db, taskID, tasknames.SendTransaction)
		_, err = db.Exec(ctx, `
			INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
			VALUES ($1, $2, CURRENT_TIMESTAMP)`, from, taskID)
		require.NoError(t, err)

		_, err = db.Exec(ctx, `DELETE FROM harmony_task WHERE id = $1`, taskID)
		require.NoError(t, err)
		require.Zero(t, ethSendLockCount(t, ctx, db, from))
	})
}

func TestSendLocksRejectMissingHarmonyTask(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_locks (from_key, task_id, claimed_at)
		VALUES ('f1missing-task', 9101, CURRENT_TIMESTAMP)`)
	require.Error(t, err)
	require.Zero(t, messageSendLockCount(t, ctx, db, "f1missing-task"))

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ('0xmissing-task', 9102, CURRENT_TIMESTAMP)`)
	require.Error(t, err)
	require.Zero(t, ethSendLockCount(t, ctx, db, "0xmissing-task"))
}

func TestSendLocksSurviveHarmonyMachineDeletion(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	var machineID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu)
		VALUES ('sender-lock-machine', 1, 1, 0)
		RETURNING id`).Scan(&machineID))

	messageTaskID := harmonytask.TaskID(9201)
	ethTaskID := harmonytask.TaskID(9202)
	_, err = db.Exec(ctx, `
		INSERT INTO harmony_task (id, posted_time, owner_id, added_by, name)
		VALUES
			($1, CURRENT_TIMESTAMP, $3, $3, $4),
			($2, CURRENT_TIMESTAMP, $3, $3, $5)`,
		messageTaskID, ethTaskID, machineID, tasknames.SendMessage, tasknames.SendTransaction)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_locks (from_key, task_id, claimed_at)
		VALUES ('f1machine-loss', $1, CURRENT_TIMESTAMP)`, messageTaskID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ('0xmachine-loss', $1, CURRENT_TIMESTAMP)`, ethTaskID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `DELETE FROM harmony_machines WHERE id = $1`, machineID)
	require.NoError(t, err)

	var messageOwner, ethOwner sql.NullInt64
	require.NoError(t, db.QueryRow(ctx, `SELECT owner_id FROM harmony_task WHERE id = $1`, messageTaskID).Scan(&messageOwner))
	require.NoError(t, db.QueryRow(ctx, `SELECT owner_id FROM harmony_task WHERE id = $1`, ethTaskID).Scan(&ethOwner))
	require.False(t, messageOwner.Valid)
	require.False(t, ethOwner.Valid)
	require.Equal(t, 1, messageSendLockCount(t, ctx, db, "f1machine-loss"))
	require.Equal(t, 1, ethSendLockCount(t, ctx, db, "0xmachine-loss"))
}

func insertSendTaskMessage(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, from, to address.Address) {
	t.Helper()
	insertSenderHarmonyTask(t, ctx, db, taskID, tasknames.SendMessage)

	msg := &types.Message{
		Version:    0,
		To:         to,
		From:       from,
		Value:      abi.NewTokenAmount(0),
		GasLimit:   1000,
		GasFeeCap:  abi.NewTokenAmount(100),
		GasPremium: abi.NewTokenAmount(10),
	}
	unsignedData, err := msg.Serialize()
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO message_sends (
			from_key, to_addr, send_reason, send_task_id,
			unsigned_data, unsigned_cid
		)
		VALUES ($1, $2, 'send-task-test', $3, $4, $5)`,
		from.String(), to.String(), taskID, unsignedData, msg.Cid().String())
	require.NoError(t, err)
}

func insertSenderHarmonyTask(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, name string) {
	t.Helper()

	_, err := db.Exec(ctx, `
		INSERT INTO harmony_task (id, posted_time, added_by, name)
		VALUES ($1, CURRENT_TIMESTAMP, 0, $2)`, taskID, name)
	require.NoError(t, err)
}

func blockMessageSendLockDelete(t *testing.T, ctx context.Context, db *harmonydb.DB, from string) {
	t.Helper()

	_, err := db.Exec(ctx, `
		CREATE TABLE message_send_lock_delete_blocker (
			from_key TEXT PRIMARY KEY REFERENCES message_send_locks (from_key)
		)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO message_send_lock_delete_blocker (from_key)
		VALUES ($1)`, from)
	require.NoError(t, err)
}

func messageSendLockCount(t *testing.T, ctx context.Context, db *harmonydb.DB, from string) int {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRow(ctx, `
		SELECT count(*) FROM message_send_locks WHERE from_key = $1`, from).Scan(&count))
	return count
}

func messageSendLockOwner(t *testing.T, ctx context.Context, db *harmonydb.DB, from string) int64 {
	t.Helper()

	var taskID int64
	require.NoError(t, db.QueryRow(ctx, `
		SELECT task_id FROM message_send_locks WHERE from_key = $1`, from).Scan(&taskID))
	return taskID
}

func assertMessageSendPending(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID) {
	t.Helper()

	var nonce sql.NullInt64
	var sendSuccess sql.NullBool
	var sendError sql.NullString
	var sendTime sql.NullTime
	require.NoError(t, db.QueryRow(ctx, `
		SELECT nonce, send_success, send_error, send_time
		FROM message_sends
		WHERE send_task_id = $1`, taskID).Scan(&nonce, &sendSuccess, &sendError, &sendTime))
	require.False(t, nonce.Valid)
	require.False(t, sendSuccess.Valid)
	require.False(t, sendError.Valid)
	require.False(t, sendTime.Valid)
}

func senderTestIDAddress(t *testing.T, id uint64) address.Address {
	t.Helper()

	addr, err := address.NewIDAddress(id)
	require.NoError(t, err)
	return addr
}

type senderTestAPI struct {
	SenderAPI

	nonce          uint64
	nonceErr       error
	nonceCalls     int
	pushErr        error
	pushCalls      int
	pushedMessages []*types.SignedMessage
	pushEntered    chan struct{}
	pushRelease    <-chan struct{}
	pushEnterOnce  sync.Once
}

func (s *senderTestAPI) MpoolGetNonce(context.Context, address.Address) (uint64, error) {
	s.nonceCalls++
	return s.nonce, s.nonceErr
}

func (s *senderTestAPI) MpoolPush(ctx context.Context, msg *types.SignedMessage) (cid.Cid, error) {
	s.pushCalls++
	s.pushedMessages = append(s.pushedMessages, msg)
	if s.pushEntered != nil {
		s.pushEnterOnce.Do(func() { close(s.pushEntered) })
	}
	if s.pushRelease != nil {
		select {
		case <-s.pushRelease:
		case <-ctx.Done():
			return cid.Undef, ctx.Err()
		}
	}
	if s.pushErr != nil {
		return cid.Undef, s.pushErr
	}
	return msg.Cid(), nil
}

type senderTestResult struct {
	done bool
	err  error
}

func waitForSenderTestSignal(t *testing.T, ctx context.Context, signal <-chan struct{}, description string) {
	t.Helper()

	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s: %s", description, ctx.Err())
	}
}

func waitForSenderTestResult(t *testing.T, ctx context.Context, result <-chan senderTestResult, description string) senderTestResult {
	t.Helper()

	select {
	case res := <-result:
		return res
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s: %s", description, ctx.Err())
		return senderTestResult{}
	}
}

type senderTestSigner struct {
	calls int
}

func (s *senderTestSigner) WalletSignMessage(_ context.Context, _ address.Address, msg *types.Message) (*types.SignedMessage, error) {
	s.calls++
	return &types.SignedMessage{
		Message: *msg,
		Signature: crypto.Signature{
			Type: crypto.SigTypeSecp256k1,
			Data: make([]byte, 65),
		},
	}, nil
}
