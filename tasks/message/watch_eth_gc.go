package message

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

const (
	messageWaitsEthGCRetention = int64(builtin.EpochsInDay * 30) // 30 days
)

type ethMsgWaitsGC interface {
	BlockNumber(ctx context.Context) (uint64, error)
}

type EthMessageWaitsGCTask struct {
	db  *harmonydb.DB
	eth ethMsgWaitsGC
}

func NewMessageWaitsEthGCTask(db *harmonydb.DB, eth ethMsgWaitsGC) *EthMessageWaitsGCTask {
	return &EthMessageWaitsGCTask{
		db:  db,
		eth: eth,
	}
}

func (t *EthMessageWaitsGCTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	head, err := t.eth.BlockNumber(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting latest eth head: %w", err)
	}

	cutoffBlock := int64(head) - messageWaitsEthGCRetention
	if cutoffBlock <= 0 {
		log.Debugw("skipping ETH message wait GC before retention window", "head", head, "cutoffBlock", cutoffBlock)
		return true, nil
	}

	deleted, err := t.db.Exec(ctx, `
		DELETE FROM message_waits_eth
		WHERE tx_status = 'confirmed'
		  AND confirmed_block_number IS NOT NULL
		  AND confirmed_block_number < $1
	`, cutoffBlock)
	if err != nil {
		return false, xerrors.Errorf("pruning old eth message waits: %w", err)
	}

	if deleted > 0 {
		log.Infow("pruned old ETH message waits", "count", deleted, "head", head, "cutoffBlock", cutoffBlock)
	}
	return true, nil
}

func (t *EthMessageWaitsGCTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *EthMessageWaitsGCTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "EthMessageWaitGC",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(time.Hour*24, t),
	}
}

func (t *EthMessageWaitsGCTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&EthMessageWaitsGCTask{})
var _ harmonytask.TaskInterface = &EthMessageWaitsGCTask{}
