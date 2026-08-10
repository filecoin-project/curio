package message

import (
	"context"
	"database/sql"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
)

const sendLockReleaseTimeout = 15 * time.Second

// SendLockStaleTTL is how old a lock must be before it may be stolen from a
// holder whose harmony_task row still exists but has no live owning machine.
// Missing harmony_task rows may be stolen immediately. Overridable in tests.
var SendLockStaleTTL = 2 * time.Minute

func releaseLockCtx() (context.Context, context.CancelFunc) {
	// Must not use the task ctx: preemption/cancel would make DELETE fail and
	// leave an orphan lock that wedges all subsequent sends for the address.
	return context.WithTimeout(context.Background(), sendLockReleaseTimeout)
}

func releaseMessageSendLock(db *harmonydb.DB, fromKey string, taskID harmonytask.TaskID) error {
	ctx, cancel := releaseLockCtx()
	defer cancel()
	_, err := db.Exec(ctx, `
		DELETE FROM message_send_locks WHERE from_key = $1 AND task_id = $2`, fromKey, taskID)
	return err
}

func releaseEthSendLock(db *harmonydb.DB, fromAddress string, taskID harmonytask.TaskID) error {
	ctx, cancel := releaseLockCtx()
	defer cancel()
	_, err := db.Exec(ctx, `
		DELETE FROM message_send_eth_locks WHERE from_address = $1 AND task_id = $2`, fromAddress, taskID)
	return err
}

// tryAcquireMessageSendLock claims message_send_locks for fromKey.
// Succeeds when
// - the row is free,
// - already held by taskID,
// - the holding task is gone from harmony_task, or
// - it's after SendLockStaleTTL & the holder's harmony_machines TTL expired.
func tryAcquireMessageSendLock(ctx context.Context, db *harmonydb.DB, fromKey string, taskID harmonytask.TaskID) (bool, error) {
	var prevTask sql.NullInt64
	_ = db.QueryRow(ctx, `SELECT task_id FROM message_send_locks WHERE from_key = $1`, fromKey).Scan(&prevTask)

	cn, err := db.Exec(ctx, `
		INSERT INTO message_send_locks (from_key, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (from_key) DO UPDATE
		SET task_id = EXCLUDED.task_id, claimed_at = CURRENT_TIMESTAMP
		WHERE message_send_locks.task_id = $2
		   OR NOT EXISTS (
		        SELECT 1 FROM harmony_task ht WHERE ht.id = message_send_locks.task_id
		   )
		   OR (
		        message_send_locks.claimed_at < CURRENT_TIMESTAMP - ($3::bigint * INTERVAL '1 millisecond')
		        AND NOT EXISTS (
		            SELECT 1
		            FROM harmony_task ht
		            JOIN harmony_machines hm ON hm.id = ht.owner_id
		            WHERE ht.id = message_send_locks.task_id
		              AND hm.last_contact > CURRENT_TIMESTAMP - ($4::bigint * INTERVAL '1 millisecond')
		        )
		   )`,
		fromKey, taskID, SendLockStaleTTL.Milliseconds(), resources.LOOKS_DEAD_TIMEOUT.Milliseconds())
	if err != nil {
		return false, err
	}
	if cn == 1 {
		if prevTask.Valid && prevTask.Int64 != int64(taskID) {
			log.Infow("stole stale message send lock",
				"from", fromKey, "old_task", prevTask.Int64, "new_task", taskID)
		}
		return true, nil
	}
	return false, nil
}

// tryAcquireEthSendLock claims message_send_eth_locks for fromAddress.
// See tryAcquireMessageSendLock for steal conditions.
func tryAcquireEthSendLock(ctx context.Context, db *harmonydb.DB, fromAddress string, taskID harmonytask.TaskID) (bool, error) {
	var prevTask sql.NullInt64
	_ = db.QueryRow(ctx, `SELECT task_id FROM message_send_eth_locks WHERE from_address = $1`, fromAddress).Scan(&prevTask)

	cn, err := db.Exec(ctx, `
		INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (from_address) DO UPDATE
		SET task_id = EXCLUDED.task_id, claimed_at = CURRENT_TIMESTAMP
		WHERE message_send_eth_locks.task_id = $2
		   OR NOT EXISTS (
		        SELECT 1 FROM harmony_task ht WHERE ht.id = message_send_eth_locks.task_id
		   )
		   OR (
		        message_send_eth_locks.claimed_at < CURRENT_TIMESTAMP - ($3::bigint * INTERVAL '1 millisecond')
		        AND NOT EXISTS (
		            SELECT 1
		            FROM harmony_task ht
		            JOIN harmony_machines hm ON hm.id = ht.owner_id
		            WHERE ht.id = message_send_eth_locks.task_id
		              AND hm.last_contact > CURRENT_TIMESTAMP - ($4::bigint * INTERVAL '1 millisecond')
		        )
		   )`,
		fromAddress, taskID, SendLockStaleTTL.Milliseconds(), resources.LOOKS_DEAD_TIMEOUT.Milliseconds())
	if err != nil {
		return false, err
	}
	if cn == 1 {
		if prevTask.Valid && prevTask.Int64 != int64(taskID) {
			log.Infow("stole stale eth send lock",
				"from", fromAddress, "old_task", prevTask.Int64, "new_task", taskID)
		}
		return true, nil
	}
	return false, nil
}
