package harmonytask

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const PREPARE_TASK_ATTEMPT = `UPDATE harmony_task
SET attempt_id=$1, attempt_started_at=NULL, attempt_start_source='prepared'
WHERE id=$2 AND owner_id=$3 AND owner_generation=$4
  AND (attempt_id IS NULL OR attempt_id=$1)
  AND attempt_started_at IS NULL AND attempt_start_source IN ('claimed', 'prepared')`

const RECORD_TASK_ATTEMPT_START = `UPDATE harmony_task
SET attempt_started_at=$1, attempt_start_source='do_entry'
WHERE id=$2 AND owner_id=$3 AND attempt_id=$4
  AND attempt_started_at IS NULL AND attempt_start_source='prepared'`

type taskAttemptStore interface {
	prepare(context.Context, TaskID, string) error
	record(context.Context, TaskID, string, time.Time) (bool, error)
	releaseUnstarted(context.Context, TaskID) error
}

type harmonyTaskAttemptStore struct {
	db    *harmonydb.DB
	owner int
	// Populated by the claim/recovery statement, immutable before workers start.
	generations map[TaskID]int64
	token       string
}

func (s harmonyTaskAttemptStore) prepare(ctx context.Context, id TaskID, token string) error {
	generation, ok := s.generations[id]
	if !ok {
		return fmt.Errorf("missing acquisition generation for task %d", id)
	}
	n, err := s.db.Exec(ctx, PREPARE_TASK_ATTEMPT, token, id, s.owner, generation)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("attempt preparation affected %d task rows", n)
	}
	return nil
}

func (s harmonyTaskAttemptStore) record(ctx context.Context, id TaskID, token string, start time.Time) (bool, error) {
	n, err := s.db.Exec(ctx, RECORD_TASK_ATTEMPT_START, start.UTC(), id, s.owner, token)
	return n == 1, err
}

func (s harmonyTaskAttemptStore) releaseUnstarted(ctx context.Context, id TaskID) error {
	generation, ok := s.generations[id]
	if !ok {
		return fmt.Errorf("missing cleanup acquisition generation for task %d", id)
	}
	if s.token == "" {
		return fmt.Errorf("missing cleanup attempt token for task %d", id)
	}
	_, err := s.db.Exec(ctx, RELEASE_TASK_ACQUISITION, id, s.owner, generation, s.token)
	return err
}

const RELEASE_TASK_ACQUISITION = `UPDATE harmony_task SET owner_id=NULL
WHERE id=$1 AND owner_id=$2 AND owner_generation=$3
  AND (attempt_id IS NULL OR attempt_id=$4)
  AND attempt_started_at IS NULL AND attempt_start_source IN ('claimed', 'prepared')`

func bindAttemptToken(store taskAttemptStore, token string) taskAttemptStore {
	if s, ok := store.(harmonyTaskAttemptStore); ok {
		s.token = token
		return s
	}
	return store
}

func prepareTaskAttempts(ctx context.Context, store taskAttemptStore, ids []TaskID) ([]TaskID, map[TaskID]string) {
	prepareCtx, stopPrepare := context.WithTimeout(ctx, 5*time.Second)
	defer stopPrepare()
	prepared := make([]TaskID, 0, len(ids))
	tokens := make(map[TaskID]string, len(ids))
	failed := make([]TaskID, 0)
	for _, id := range ids {
		token := uuid.NewString()
		tokens[id] = token
		if err := store.prepare(prepareCtx, id, token); err != nil {
			log.Errorw("Could not prepare task attempt telemetry", "id", id, "error", err)
			failed = append(failed, id)
			continue
		}
		prepared = append(prepared, id)
		tokens[id] = token
	}
	// A single bounded release budget remains usable even if preparation timed
	// out. This uses the existing ownership CAS, never task deletion.
	releaseCtx, stopRelease := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopRelease()
	for _, id := range failed {
		if err := bindAttemptToken(store, tokens[id]).releaseUnstarted(releaseCtx, id); err != nil {
			log.Errorw("Could not release unstarted task", "id", id, "error", err)
		}
		delete(tokens, id)
	}
	return prepared, tokens
}

// runWithAttemptStart has one final entry gate. The optional hook commits a
// start reservation; after it succeeds there is no second cancellation exit
// before Do. Timestamp persistence runs concurrently so DB latency is not
// counted as pre-execution time. The bounded writer is joined even on panic.
func runWithAttemptStart(ctx context.Context, store taskAttemptStore, id TaskID, token string,
	beforeStart func(context.Context) error, now func() time.Time, onStart func(time.Time), do func() (bool, error),
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if beforeStart != nil {
		if err := beforeStart(ctx); err != nil {
			return false, err
		}
	}
	started := now()
	onStart(started)
	writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer func() {
			if failure := recover(); failure != nil {
				log.Errorw("Task execution start writer panicked; telemetry is unconfirmed", "id", id, "panic", failure)
			}
		}()
		ok, err := store.record(writeCtx, id, token, started)
		if err != nil || !ok {
			log.Warnw("Task execution start is unconfirmed", "id", id, "recorded", ok, "error", err)
		}
	}()
	defer func() { cancel(); <-finished }()
	return do()
}
