package message

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
)

func TestReleaseSendLockIgnoresCanceledTaskContext(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := "0xlock-release-cancel"
	taskID := harmonytask.TaskID(9001)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from, taskID)
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	// Bug we fixed: release must not use the canceled task ctx.
	_, err = db.Exec(canceled,
		`DELETE FROM message_send_eth_locks WHERE from_address = $1 AND task_id = $2`, from, taskID)
	require.Error(t, err)

	require.NoError(t, releaseEthSendLock(db, from, taskID))

	var n int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT count(*) FROM message_send_eth_locks WHERE from_address = $1`, from).Scan(&n))
	require.Equal(t, 0, n)
}

func TestEthSendLockStealMissingTask(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := "0xlock-steal-missing"
	holder := harmonytask.TaskID(9101)
	waiter := harmonytask.TaskID(9102)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from, holder)
	require.NoError(t, err)

	got, err := tryAcquireEthSendLock(ctx, db, from, waiter)
	require.NoError(t, err)
	require.True(t, got, "missing harmony_task row should allow immediate steal")

	var holding int64
	require.NoError(t, db.QueryRow(ctx,
		`SELECT task_id FROM message_send_eth_locks WHERE from_address = $1`, from).Scan(&holding))
	require.Equal(t, int64(waiter), holding)
}

func TestEthSendLockNoStealLiveOwner(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	oldTTL := SendLockStaleTTL
	SendLockStaleTTL = time.Millisecond
	t.Cleanup(func() { SendLockStaleTTL = oldTTL })

	from := "0xlock-no-steal-live"
	machineID := insertLiveMachine(t, ctx, db, "lock-live-owner")
	holder := insertOwnedHarmonyTask(t, ctx, db, "SendTransaction", machineID)
	waiter := harmonytask.TaskID(9202)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP - INTERVAL '1 hour')`, from, holder)
	require.NoError(t, err)

	got, err := tryAcquireEthSendLock(ctx, db, from, waiter)
	require.NoError(t, err)
	require.False(t, got, "must not steal from a live owning machine")
}

func TestEthSendLockStealDeadOwnerAfterTTL(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	oldTTL := SendLockStaleTTL
	oldDead := resources.LOOKS_DEAD_TIMEOUT
	SendLockStaleTTL = time.Millisecond
	resources.LOOKS_DEAD_TIMEOUT = time.Minute
	t.Cleanup(func() {
		SendLockStaleTTL = oldTTL
		resources.LOOKS_DEAD_TIMEOUT = oldDead
	})

	from := "0xlock-steal-dead"
	machineID := insertStaleMachine(t, ctx, db, "lock-dead-owner", 2*time.Hour)
	holder := insertOwnedHarmonyTask(t, ctx, db, "SendTransaction", machineID)
	waiter := harmonytask.TaskID(9302)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP - INTERVAL '1 hour')`, from, holder)
	require.NoError(t, err)

	got, err := tryAcquireEthSendLock(ctx, db, from, waiter)
	require.NoError(t, err)
	require.True(t, got, "TTL + dead harmony_machines owner should allow steal")

	var holding int64
	require.NoError(t, db.QueryRow(ctx,
		`SELECT task_id FROM message_send_eth_locks WHERE from_address = $1`, from).Scan(&holding))
	require.Equal(t, int64(waiter), holding)
}

func TestEthSendLockStealNullOwnerAfterTTL(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	oldTTL := SendLockStaleTTL
	SendLockStaleTTL = time.Millisecond
	t.Cleanup(func() { SendLockStaleTTL = oldTTL })

	from := "0xlock-steal-null-owner"
	machineID := insertLiveMachine(t, ctx, db, "lock-null-owner-machine")
	holder := insertOwnedHarmonyTask(t, ctx, db, "SendTransaction", machineID)
	_, err = db.Exec(ctx, `UPDATE harmony_task SET owner_id = NULL WHERE id = $1`, holder)
	require.NoError(t, err)
	waiter := harmonytask.TaskID(9352)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP - INTERVAL '1 hour')`, from, holder)
	require.NoError(t, err)

	got, err := tryAcquireEthSendLock(ctx, db, from, waiter)
	require.NoError(t, err)
	require.True(t, got, "TTL + disowned harmony_task should allow steal")
}

func TestEthSendLockNoStealDeadOwnerBeforeTTL(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	oldTTL := SendLockStaleTTL
	oldDead := resources.LOOKS_DEAD_TIMEOUT
	SendLockStaleTTL = time.Hour
	resources.LOOKS_DEAD_TIMEOUT = time.Minute
	t.Cleanup(func() {
		SendLockStaleTTL = oldTTL
		resources.LOOKS_DEAD_TIMEOUT = oldDead
	})

	from := "0xlock-no-steal-young"
	machineID := insertStaleMachine(t, ctx, db, "lock-dead-young", 2*time.Hour)
	holder := insertOwnedHarmonyTask(t, ctx, db, "SendTransaction", machineID)
	waiter := harmonytask.TaskID(9402)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from, holder)
	require.NoError(t, err)

	got, err := tryAcquireEthSendLock(ctx, db, from, waiter)
	require.NoError(t, err)
	require.False(t, got, "dead owner alone is not enough before SendLockStaleTTL")
}

func TestMessageSendLockStealMissingTask(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	from := "f1lockstealmissing"
	holder := harmonytask.TaskID(9501)
	waiter := harmonytask.TaskID(9502)

	_, err = db.Exec(ctx, `
		INSERT INTO message_send_locks (from_key, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)`, from, holder)
	require.NoError(t, err)

	got, err := tryAcquireMessageSendLock(ctx, db, from, waiter)
	require.NoError(t, err)
	require.True(t, got)

	var holding int64
	require.NoError(t, db.QueryRow(ctx,
		`SELECT task_id FROM message_send_locks WHERE from_key = $1`, from).Scan(&holding))
	require.Equal(t, int64(waiter), holding)
}

func insertLiveMachine(t *testing.T, ctx context.Context, db *harmonydb.DB, host string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(ctx, `
		INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu, last_contact)
		VALUES ($1, 1, 1, 0, CURRENT_TIMESTAMP)
		RETURNING id`, host).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertStaleMachine(t *testing.T, ctx context.Context, db *harmonydb.DB, host string, age time.Duration) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(ctx, `
		INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu, last_contact)
		VALUES ($1, 1, 1, 0, CURRENT_TIMESTAMP - ($2::bigint * INTERVAL '1 millisecond'))
		RETURNING id`, host, age.Milliseconds()).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertOwnedHarmonyTask(t *testing.T, ctx context.Context, db *harmonydb.DB, name string, ownerID int64) harmonytask.TaskID {
	t.Helper()
	var id int64
	err := db.QueryRow(ctx, `
		INSERT INTO harmony_task (posted_time, owner_id, added_by, name)
		VALUES (CURRENT_TIMESTAMP, $1, $1, $2)
		RETURNING id`, ownerID, name).Scan(&id)
	require.NoError(t, err)
	return harmonytask.TaskID(id)
}
