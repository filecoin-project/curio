package pdpv0

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

const datasetIdxInterval = 15 * time.Minute
const datasetIdxBatchSize = 100

// DatasetIdxTask backfills pdp_data_sets.ipfs_indexing for rows still NULL via chain reads,
// and repairs missed needs_indexing flags when intent resolves to true.
type DatasetIdxTask struct {
	db        *harmonydb.DB
	ethClient ethchain.EthClient
}

// FUTURE: This is a "run once" task. It should be removed once we are confident that the indexing intent is being correctly set.
func NewDatasetIdxTask(db *harmonydb.DB, ethClient ethchain.EthClient) *DatasetIdxTask {
	return &DatasetIdxTask{db: db, ethClient: ethClient}
}

func (t *DatasetIdxTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	if !stillOwned() {
		return false, nil
	}

	var rows []struct {
		ID uint64 `db:"id"`
	}
	err = t.db.Select(ctx, &rows, `
		SELECT id
		FROM pdp_data_sets
		WHERE ipfs_indexing IS NULL
		ORDER BY id
		LIMIT $1
	`, datasetIdxBatchSize)
	if err != nil {
		return false, xerrors.Errorf("selecting data sets with null ipfs_indexing: %w", err)
	}
	if len(rows) == 0 {
		if err := harmonytask.DisableSingleton(ctx, t.db, tasknames.PDPv0_DatasetIdx, "no data sets left with null ipfs_indexing"); err != nil {
			return false, xerrors.Errorf("disabling completed repair task: %w", err)
		}
		log.Infow("no data sets left with null ipfs_indexing; disabling PDPv0_DatasetIdx")
		return true, nil
	}

	var resolveErrs int
	for _, row := range rows {
		if !stillOwned() {
			return false, nil
		}
		mustIndex, err := pdp.ResolveDatasetIPFSIndexing(ctx, t.db, t.ethClient, row.ID)
		if err != nil {
			log.Warnw("failed to resolve IPFS indexing intent", "dataSetId", row.ID, "error", err)
			resolveErrs++
			continue
		}
		log.Infow("resolved IPFS indexing intent", "dataSetId", row.ID, "ipfsIndexing", mustIndex)
	}

	if resolveErrs > 0 {
		return false, xerrors.Errorf("failed to resolve IPFS indexing intent for %d of %d data sets", resolveErrs, len(rows))
	}
	return true, nil
}

func (t *DatasetIdxTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *DatasetIdxTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: tasknames.PDPv0_DatasetIdx,
		Max:  taskhelp.Max(1),
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 64 << 20,
			Gpu: 0,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(datasetIdxInterval, t),
		CanDisable:  true,
	}
}

func (t *DatasetIdxTask) Adder(_ harmonytask.AddTaskFunc) {}

var (
	_                           = harmonytask.Reg(&DatasetIdxTask{})
	_ harmonytask.TaskInterface = &DatasetIdxTask{}
)
