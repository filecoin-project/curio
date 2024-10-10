package pdp

import (
	"context"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
)

type ProveTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

func NewProveTask(chainSched *chainsched.CurioChainSched, db *harmonydb.DB, ethClient *ethclient.Client) *ProveTask {
	pt := &ProveTask{
		db:        db,
		ethClient: ethClient,
	}

	err := chainSched.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		if apply == nil {
			return nil
		}

		more := true
		for more {
			pt.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// select pdp_proof_sets where next_challenge_epoch not null and next_challenge_epoch > apply.Height and next_challenge_possible = true limit 2
				// if zero rows, return shouldCommit = false

				// more = len(rows) == 2

				// todo = rows[0]

				// insert into pdp_prove_tasks proofset_id = todo.id, challenge_epoch = todo.next_challenge_epoch, task_id = id

				// more = more & affected == 1

			})
		}

		return nil
	})
	if err != nil {
		// can only error if the handler is added after the scheduler has started
		panic(err)
	}

	return pt
}

func (p *ProveTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	// select proofset_id, challenge_epoch from pdp_prove_tasks where task_id = taskID

	// query contract for required info to create the proof

	// todo later - create the proof (just write this as a semi-stub)

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
