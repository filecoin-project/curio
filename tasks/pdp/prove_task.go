package pdp

import (
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"golang.org/x/xerrors"
)

type ProveTask struct {
	chainSched *chainsched.CurioChainSched
	addFunc    promise.Promise[harmonytask.AddTaskFunc]
}

func (p *ProveTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	return false, xerrors.Errorf("not implemented")
}

func (p *ProveTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (p *ProveTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPProve",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 128 << 20,
		},
		MaxFailures: 5,
	}
}

func (p *ProveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.addFunc.Set(taskFunc)
}

var _ = harmonytask.Reg(&ProveTask{})
var _ harmonytask.TaskInterface = &ProveTask{}
