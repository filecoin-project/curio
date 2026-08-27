package pdp

import (
	"context"
	"net/http"
	"time"

	logger "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

var log = logger.Logger("pdp")

type PDPNotifyTask struct {
	db     *harmonydb.DB
	client *http.Client
}

func NewPDPNotifyTask(db *harmonydb.DB) *PDPNotifyTask {
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}
	return &PDPNotifyTask{db: db, client: client}
}

func (t *PDPNotifyTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	return true, nil
}

func (t *PDPNotifyTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return []harmonytask.TaskID{}, nil
	}
	return ids, nil
}

func (t *PDPNotifyTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name:      tasknames.PDPNotify,
		MayFollow: []string{tasknames.PDPAddPiece},
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 128 << 20, // 128MB
		},
		MaxFailures: 14,
		RetryWait:   taskhelp.RetryWaitExp(5*time.Second, 2),
		IAmBored: passcall.Every(time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *PDPNotifyTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	return nil
}

func (t *PDPNotifyTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ = harmonytask.Reg(&PDPNotifyTask{})
var _ harmonytask.TaskInterface = &PDPNotifyTask{}
