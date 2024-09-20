package winning

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"

	"github.com/filecoin-project/lotus/chain/types"
)

const InclusionCheckInterval = 5 * time.Minute
const MinInclusionEpochs = 5

type InclusionCheckNodeApi interface {
	ChainGetTipSetByHeight(context.Context, abi.ChainEpoch, types.TipSetKey) (*types.TipSet, error)
	ChainHead(context.Context) (*types.TipSet, error)
}

type InclusionCheckTask struct {
	db  *harmonydb.DB
	api InclusionCheckNodeApi
}

func NewInclusionCheckTask(db *harmonydb.DB, api InclusionCheckNodeApi) *InclusionCheckTask {
	return &InclusionCheckTask{db: db, api: api}
}

func (i *InclusionCheckTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var toCheck []struct {
		Epoch    int64  `db:"epoch"`
		MinedCID string `db:"mined_cid"`
	}

	err = i.db.Select(ctx, &toCheck, `SELECT epoch, mined_cid FROM mining_tasks WHERE won = true AND included IS NULL`)
	if err != nil {
		return false, err
	}

	head, err := i.api.ChainHead(ctx)
	if err != nil {
		return false, err

	}

	for _, check := range toCheck {
		var included bool
		if check.Epoch > int64(head.Height())-MinInclusionEpochs {
			continue
		}

		// Check if the block is included in the chain
		// If it is, update the included column in the database
		// If it is not, do nothing

		tsAt, err := i.api.ChainGetTipSetByHeight(ctx, abi.ChainEpoch(check.Epoch), types.EmptyTSK)
		if err != nil {
			return false, xerrors.Errorf("getting tipset at epoch %d: %w", check.Epoch, err)
		}

		for _, b := range tsAt.Blocks() {
			if b.Cid().String() == check.MinedCID {
				included = true
				break
			}
		}
		_, err = i.db.Exec(ctx, `UPDATE mining_tasks SET included = $1 WHERE epoch = $2`, included, check.Epoch)
		if err != nil {
			return false, xerrors.Errorf("updating included column: %w", err)
		}
	}

	return true, nil
}

func (i *InclusionCheckTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (i *InclusionCheckTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "WinInclCheck",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(InclusionCheckInterval, i),
	}
}

func (i *InclusionCheckTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ harmonytask.TaskInterface = &InclusionCheckTask{}
var _ = harmonytask.Reg(&InclusionCheckTask{})
