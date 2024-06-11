package snap

import (
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
)

type SubmitTask struct {
	max int

	sc *ffi.SealCalls
	db *harmonydb.DB
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
		Max:  s.max,
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
