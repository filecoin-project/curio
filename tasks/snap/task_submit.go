package snap

import (
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
)

type SubmitTask struct {
	sc *ffi.SealCalls
	db *harmonydb.DB
}

func NewSubmitTask(sc *ffi.SealCalls, db *harmonydb.DB) *SubmitTask {
	return &SubmitTask{
		sc: sc,
		db: db,
	}
}

func (s *SubmitTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	//TODO implement me
	panic("implement me")
}

func (s *SubmitTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SubmitTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "UpdateSubmit",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored:    nil,
	}
}

func (s *SubmitTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	return
}

var _ harmonytask.TaskInterface = &SubmitTask{}
