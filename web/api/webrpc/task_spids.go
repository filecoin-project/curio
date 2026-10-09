package webrpc

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
)

const CLUSTER_TASK_SPID_BATCH_SIZE = 10_000

type BatchSpidGetter interface {
	GetSpids(context.Context, *harmonydb.DB, []int64) ([]harmonytask.TaskSPID, error)
}

func makeBatchTaskSPIDs(legacy map[string]SpidGetter) map[string]BatchSpidGetter {
	batch := make(map[string]BatchSpidGetter)
	for name, getter := range legacy {
		if g, ok := getter.(BatchSpidGetter); ok {
			batch[name] = g
		}
	}
	return batch
}
