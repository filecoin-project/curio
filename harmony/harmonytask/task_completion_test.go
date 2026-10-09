package harmonytask

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompletionIdentityCapturedAtAcquisition(t *testing.T) {
	ids := map[TaskID]int64{7: 0}
	store := harmonyTaskAttemptStore{owner: 101, generations: ids}
	identity := completionIdentityFor(store, 7, "original")
	require.True(t, identity.valid, "generation zero is valid")
	ids[7]++
	require.EqualValues(t, 0, identity.generation, "completion must retain acquisition identity")
	require.False(t, completionIdentityFor(store, 8, "original").valid)
	require.False(t, completionIdentityFor(store, 7, "").valid)
	require.False(t, completionIdentityFor(nil, 7, "original").valid)
}

func TestCompletionNotificationsRequireCommittedCurrentResult(t *testing.T) {
	for _, applied := range []bool{false, true} {
		for _, done := range []bool{false, true} {
			for _, doErr := range []error{nil, errors.New("worker result")} {
				calls := 0
				engine := &TaskEngine{completionCallbacks: map[string][]TaskCompleteFunc{}}
				engine.OnTaskComplete("TreeRC", func(context.Context, TaskID, bool) { calls++ })
				h := &taskTypeHandler{TaskEngine: engine, TaskTypeDetails: TaskTypeDetails{Name: "TreeRC"}}
				ch := make(chan schedulerEvent, 1)
				h.publishCompletion(taskCompletion{applied: applied}, 7, &completionMeta{}, done, doErr, eventEmitter{schedulerChannel: ch})
				if !applied {
					require.Zero(t, calls, "stale success cannot run the follower callback")
					require.Empty(t, ch, "stale completion cannot announce successful/current completion")
					continue
				}
				require.Len(t, ch, 1)
				event := <-ch
				require.Equal(t, schedulerSourceTaskCompleted, event.Source)
				require.Equal(t, done && doErr == nil, event.Success)
				if event.Success {
					require.Equal(t, 1, calls)
				} else {
					require.Zero(t, calls)
				}
			}
		}
	}
}
