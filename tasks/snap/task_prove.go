package snap

import (
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/tasks/seal"
)

type ProveTask struct {
	max int

	sc *ffi.SealCalls
	db *harmonydb.DB
}

func NewProveTask(sc *ffi.SealCalls, db *harmonydb.DB, max int) *ProveTask {
	return &ProveTask{
		max: max,

		sc: sc,
		db: db,
	}
}

func (p *ProveTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	//TODO implement me
	panic("implement me")
}

func (p *ProveTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (p *ProveTask) TypeDetails() harmonytask.TaskTypeDetails {
	gpu := 1.0
	if seal.IsDevnet {
		gpu = 0
	}
	return harmonytask.TaskTypeDetails{
		Max:  p.max,
		Name: "UpdateProve",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: gpu,
			Ram: 50 << 30, // todo correct value
		},
		MaxFailures: 3,
		IAmBored:    nil,
	}
}

func (p *ProveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	return
}

var _ harmonytask.TaskInterface = &ProveTask{}
