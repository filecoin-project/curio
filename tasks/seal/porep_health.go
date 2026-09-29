package seal

import (
	"context"
	"errors"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
)

func (p *PoRepTask) TaskStartBlocked() bool {
	if p.cuzkClient != nil && p.cuzkClient.Enabled() {
		return false
	}
	return p.health.Blocked()
}
func (p *PoRepTask) ReserveTaskStart(harmonytask.TaskID) (func(context.Context) error, func(), bool) {
	if p.cuzkClient != nil && p.cuzkClient.Enabled() {
		return nil, nil, true
	}
	return p.health.Reserve()
}
func (p *PoRepTask) localBackendResult(epoch uint64, err error) error {
	var unavailable *ffi.LocalPoRepBackendUnavailable
	if errors.As(err, &unavailable) {
		err = &taskhelp.WorkerUnavailable{Cause: err}
	}
	hold := p.health.Result(epoch, err)
	if unavailable != nil {
		log.Errorw("local PoRep backend unavailable; admission paused", "retry_after", hold, "error", err)
	}
	return err
}
