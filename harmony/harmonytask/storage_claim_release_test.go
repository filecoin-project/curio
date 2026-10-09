package harmonytask

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStorageClaimReleaseSQLMatchesPreparedAttempt(t *testing.T) {
	// The adapter tests below model these predicates; keep the exact production
	// statement pinned so weakening the real query cannot leave the model green.
	want := `UPDATE harmony_task AS t SET owner_id = NULL
		FROM unnest($1::bigint[], $2::text[]) AS failed(id, attempt_id)
		WHERE t.id = failed.id AND t.owner_id = $3
		AND t.attempt_id = failed.attempt_id
		AND t.attempt_started_at IS NULL AND t.attempt_start_source = 'prepared'`
	if strings.Join(strings.Fields(releasePreparedTaskOwnershipSQL), " ") != strings.Join(strings.Fields(want), " ") {
		t.Fatal("storage-failure release must retain its batched owner/token/unstarted CAS")
	}
}

func TestStorageClaimReleasePreservesChangedOwnershipAndAttempts(t *testing.T) {
	type row struct {
		owner   int
		token   string
		started bool
		source  string
		retries int
	}
	rows := map[int64]row{
		1: {owner: 7, token: "old-1", source: "prepared", retries: 2},
		2: {owner: 8, token: "old-2", source: "prepared", retries: 3}, // another owner
		3: {owner: 7, token: "new-3", source: "prepared", retries: 4}, // same-owner ABA
		4: {owner: 7, token: "old-4", started: true, source: "do_entry", retries: 5},
		5: {owner: 7, token: "old-5", source: "claimed", retries: 6},
	}
	before := make(map[int64]row, len(rows))
	for id, r := range rows {
		before[id] = r
	}
	ids := []TaskID{3, 1, 5, 2, 4, 6} // unsorted; 6 disappeared before release
	tokens := map[TaskID]string{1: "old-1", 2: "old-2", 3: "old-3", 4: "old-4", 5: "old-5", 6: "old-6"}
	calls := 0
	err := releasePreparedTaskOwnership(ids, tokens, 7, func(ctx context.Context, gotIDs []int64, attempts []string, owner int) (int, error) {
		calls++
		if !reflect.DeepEqual(gotIDs, []int64{3, 1, 5, 2, 4, 6}) ||
			!reflect.DeepEqual(attempts, []string{"old-3", "old-1", "old-5", "old-2", "old-4", "old-6"}) || owner != 7 {
			t.Fatalf("release argument alignment lost: ids=%v tokens=%v owner=%d", gotIDs, attempts, owner)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second || ctx.Err() != nil {
			t.Fatal("cleanup must have a live bounded five-second context")
		}
		updated := 0
		// Memory-only adapter for the production statement pinned above. This is
		// not SQL execution and does not establish DB/trigger integration safety.
		for i, id := range gotIDs {
			r, exists := rows[id]
			if exists && r.owner == owner && r.token == attempts[i] && !r.started && r.source == "prepared" {
				r.owner = 0
				rows[id] = r
				updated++
			}
		}
		return updated, nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("batched release: err=%v calls=%d", err, calls)
	}
	wantReleased := before[1]
	wantReleased.owner = 0
	if rows[1] != wantReleased {
		t.Fatalf("matching unstarted attempt not released without retry mutation: %+v", rows[1])
	}
	for _, id := range []int64{2, 3, 4, 5} {
		if rows[id] != before[id] {
			t.Fatalf("stale cleanup changed task %d: before=%+v after=%+v", id, before[id], rows[id])
		}
	}
	if _, exists := rows[6]; exists {
		t.Fatal("release recreated a missing task")
	}
}

func TestStorageClaimReleaseFailsClosedWithoutToken(t *testing.T) {
	calls := 0
	exec := func(context.Context, []int64, []string, int) (int, error) { calls++; return 0, nil }
	if err := releasePreparedTaskOwnership([]TaskID{1, 2}, map[TaskID]string{1: "old-1"}, 7, exec); err == nil || calls != 0 {
		t.Fatalf("missing token must not reach DB adapter: err=%v calls=%d", err, calls)
	}
	if err := releasePreparedTaskOwnership(nil, nil, 7, exec); err != nil || calls != 0 {
		t.Fatalf("empty release must be a no-op: err=%v calls=%d", err, calls)
	}
}

func TestStorageClaimReleasePropagatesFailureAndCancelsContext(t *testing.T) {
	for _, failure := range []error{errors.New("synthetic transient SQL failure"), context.Canceled, context.DeadlineExceeded} {
		var releaseCtx context.Context
		calls := 0
		err := releasePreparedTaskOwnership([]TaskID{1}, map[TaskID]string{1: "old-1"}, 7,
			func(ctx context.Context, _ []int64, _ []string, _ int) (int, error) {
				calls++
				releaseCtx = ctx
				return 0, failure
			})
		if !errors.Is(err, failure) || calls != 1 || releaseCtx.Err() != context.Canceled {
			t.Fatalf("failure=%v returned=%v calls=%d cleanup_context=%v", failure, err, calls, releaseCtx.Err())
		}
	}
}
