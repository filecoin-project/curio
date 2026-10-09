package seal

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cuzk"
	"github.com/filecoin-project/curio/lib/ffi"
)

func TestPoRepBackendAdmissionIsolation(t *testing.T) {
	p := &PoRepTask{enableRemoteProofs: true, paramsReady: func() (bool, error) { return true, nil }}
	require.False(t, p.TaskStartBlocked())
	_, _, ok := p.ReserveTaskStart(1)
	require.True(t, ok)
	for _, err := range []error{io.ErrUnexpectedEOF, errors.New("invalid sector"), errors.New("No CUDA devices available")} {
		require.Same(t, err, p.localBackendResult(p.health.Epoch(), err))
		require.False(t, p.TaskStartBlocked(), "untyped/remote errors cannot quarantine receiver")
	}
	var deferred *taskhelp.WorkerUnavailable
	require.ErrorAs(t, p.localBackendResult(p.health.Epoch(), &ffi.LocalPoRepBackendUnavailable{Cause: errors.New("No CUDA devices available")}), &deferred)
	require.True(t, p.TaskStartBlocked())
	ids, err := p.CanAccept([]harmonytask.TaskID{1}, nil)
	require.NoError(t, err)
	require.Empty(t, ids)
	_, _, ok = p.ReserveTaskStart(1)
	require.False(t, ok)
	require.False(t, new(PoRepTask).TaskStartBlocked(), "other instance is unaffected")
	p.cuzkClient = cuzk.NewClient("unix:///fixture-unused.sock", 1, 0) // lazy: no connection
	require.False(t, p.TaskStartBlocked(), "local backend gate must not disable CuZK")
	ids, err = p.CanAccept([]harmonytask.TaskID{1}, nil)
	require.NoError(t, err)
	require.Equal(t, []harmonytask.TaskID{1}, ids)
	start, cancel, ok := p.ReserveTaskStart(1)
	require.True(t, ok)
	require.Nil(t, start)
	require.Nil(t, cancel)
}
