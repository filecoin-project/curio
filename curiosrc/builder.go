package curio

import (
	"context"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/tasks/message"
	window2 "github.com/filecoin-project/curio/tasks/window"
	"time"

	"github.com/filecoin-project/curio/curiosrc/multictladdr"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/lib/harmony/harmonydb"
	"github.com/filecoin-project/lotus/node/config"
	dtypes "github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/storage/paths"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

//var log = logging.Logger("provider")

func WindowPostScheduler(ctx context.Context, fc config.CurioFees, pc config.CurioProvingConfig,
	api api.FullNode, verif storiface.Verifier, sender *message.Sender, chainSched *chainsched.CurioChainSched,
	as *multictladdr.MultiAddressSelector, addresses map[dtypes.MinerAddress]bool, db *harmonydb.DB,
	stor paths.Store, idx paths.SectorIndex, max int) (*window2.WdPostTask, *window2.WdPostSubmitTask, *window2.WdPostRecoverDeclareTask, error) {

	// todo config
	ft := window2.NewSimpleFaultTracker(stor, idx, pc.ParallelCheckLimit, time.Duration(pc.SingleCheckTimeout), time.Duration(pc.PartitionCheckTimeout))

	computeTask, err := window2.NewWdPostTask(db, api, ft, stor, verif, chainSched, addresses, max, pc.ParallelCheckLimit, time.Duration(pc.SingleCheckTimeout))
	if err != nil {
		return nil, nil, nil, err
	}

	submitTask, err := window2.NewWdPostSubmitTask(chainSched, sender, db, api, fc.MaxWindowPoStGasFee, as)
	if err != nil {
		return nil, nil, nil, err
	}

	recoverTask, err := window2.NewWdPostRecoverDeclareTask(sender, db, api, ft, as, chainSched, fc.MaxWindowPoStGasFee, addresses)
	if err != nil {
		return nil, nil, nil, err
	}

	return computeTask, submitTask, recoverTask, nil
}
