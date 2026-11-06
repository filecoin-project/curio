package seal

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cuzk"
	"github.com/filecoin-project/curio/lib/ffi"
)

func TestLocalPoRepHoldProbe(t *testing.T) {
	now := time.Now()
	p := &PoRepTask{enableRemoteProofs: true, paramsReady: func() (bool, error) { return true, nil }}
	p.health.now = func() time.Time { return now }
	noCUDA := &ffi.LocalPoRepBackendUnavailable{Cause: errors.New("No CUDA devices available")}
	epoch, _, ok := p.health.reserve()
	require.True(t, ok)
	for _, err := range []error{errors.New("transport"), errors.New("No CUDA devices available"), context.Canceled} {
		require.Same(t, err, p.health.result(epoch, err))
		require.False(t, p.health.blocked())
	}
	var unavailable *taskhelp.WorkerUnavailable
	require.ErrorAs(t, p.health.result(epoch, noCUDA), &unavailable)
	require.Equal(t, 2*time.Minute, p.health.delay)
	require.NoError(t, p.health.result(epoch, nil)) // an older success cannot undo the newer failure
	require.True(t, p.health.blocked())
	ids, err := p.CanAccept([]harmonytask.TaskID{1}, nil)
	require.NoError(t, err)
	require.Empty(t, ids)
	_, err = p.Do(context.Background(), 1, func() bool { return true }) // cached acceptance rechecked before any DB/native access
	require.ErrorAs(t, err, &unavailable)
	for _, delay := range []time.Duration{4, 8, 16, 30, 30} {
		now = p.health.until
		epoch, release, accepted := p.health.reserve()
		require.True(t, accepted)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, _, ok := p.health.reserve(); require.False(t, ok) }()
		}
		wg.Wait()
		require.ErrorAs(t, p.health.result(epoch, noCUDA), &unavailable)
		release()
		require.Equal(t, delay*time.Minute, p.health.delay)
	}
	now = p.health.until
	_, cancel, ok := p.health.reserve()
	require.True(t, ok)
	cancel() // pre-native failure returns the exact probe reservation
	epoch, release, ok := p.health.reserve()
	require.True(t, ok)
	cancel() // stale release must not free a newer probe
	require.True(t, p.health.blocked())
	require.NoError(t, p.health.result(epoch, nil))
	release()
	require.False(t, p.health.blocked())
	require.False(t, new(PoRepTask).health.blocked())
	require.ErrorAs(t, p.health.result(epoch, noCUDA), &unavailable)
	p.cuzkClient = cuzk.NewClient("unix:///test-unused.sock", 1, 0)
	ids, err = p.CanAccept([]harmonytask.TaskID{1}, nil)
	require.NoError(t, err)
	require.Len(t, ids, 1)
}
