package pdpv0

import (
	"context"
	"fmt"
	"sync"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/paths/alertinginterface"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

// WatcherOrder groups PDPv0 callbacks into phases for one tipset. The sequence
// reconciles chain-derived state before proving callbacks inspect the DB; callbacks
// in the same phase run together after prior phases finish.
type WatcherOrder uint8

const (
	WatcherOrderCreateAndAdd WatcherOrder = iota
	WatcherOrderTerminate
	WatcherOrderPaymentSettle
	WatcherOrderDelete
	WatcherOrderCleanupPieces
	WatcherOrderProving
)

// watcherOrders is the canonical PDPv0 phase order used by execute.
var watcherOrders = []WatcherOrder{
	WatcherOrderCreateAndAdd,
	WatcherOrderTerminate,
	WatcherOrderPaymentSettle,
	WatcherOrderDelete,
	WatcherOrderCleanupPieces,
	WatcherOrderProving,
}

type Watcher struct {
	db       *harmonydb.DB
	eth      ethchain.EthClient
	pcs      *chainsched.CurioChainSched
	al       alertinginterface.AlertingInterface
	chain    chan *tipset
	watchers map[WatcherOrder][]update
	started  bool
	lk       sync.Mutex
}

type tipset struct {
	revert *chainTypes.TipSet
	apply  *chainTypes.TipSet
}

type update func(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, al alertinginterface.AlertingInterface, revert, apply *chainTypes.TipSet)

func NewPDPv0Watcher(db *harmonydb.DB, ethClient ethchain.EthClient, pcs *chainsched.CurioChainSched, al alertinginterface.AlertingInterface) *Watcher {
	w := &Watcher{
		db:       db,
		eth:      ethClient,
		pcs:      pcs,
		al:       al,
		chain:    make(chan *tipset, 100),
		watchers: make(map[WatcherOrder][]update),
	}

	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		t := tipset{revert: revert, apply: apply}
		w.chain <- &t
		return nil
	}); err != nil {
		panic(err)
	}
	return w
}

func (w *Watcher) AddWatcher(wf update, order WatcherOrder) error {
	w.lk.Lock()
	defer w.lk.Unlock()
	if w.started {
		return fmt.Errorf("pdpv0 watcher already started")
	}
	existing, ok := w.watchers[order]
	if !ok {
		w.watchers[order] = []update{wf}
	} else {
		w.watchers[order] = append(existing, wf)
	}

	return nil
}

func (w *Watcher) Run(ctx context.Context) {
	w.lk.Lock()
	defer w.lk.Unlock()
	w.started = true
	go w.start(ctx)
}

func (w *Watcher) start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ts := <-w.chain:
			w.execute(ctx, ts)
		}
	}
}

func (w *Watcher) execute(ctx context.Context, ts *tipset) {
	w.lk.Lock()
	// Snapshot watchers in phase order so execution is independent of map or registration order.
	owfs := make([][]update, 0, len(w.watchers))
	for _, order := range watcherOrders {
		wfs, ok := w.watchers[order]
		if !ok {
			continue
		}
		if len(wfs) == 0 {
			continue
		}
		owfs = append(owfs, wfs)
	}
	w.lk.Unlock()

	for _, wfs := range owfs {
		wg := &sync.WaitGroup{}
		for _, wf := range wfs {
			wg.Add(1)
			wg.Go(func() {
				defer wg.Done()
				wf(ctx, w.db, w.eth, w.al, ts.revert, ts.apply)
			})
		}
		wg.Wait()
	}
}
