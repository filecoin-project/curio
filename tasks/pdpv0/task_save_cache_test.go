package pdpv0

import (
	"context"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
)

func TestSaveCacheScheduleStopsWhenAddTaskDoesNotRunCallback(t *testing.T) {
	done := make(chan struct{})

	go func() {
		defer close(done)
		task := &TaskPDPSaveCache{}
		_ = task.schedule(context.Background(), func(func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("schedule did not stop after AddTask declined to run its callback")
	}
}
