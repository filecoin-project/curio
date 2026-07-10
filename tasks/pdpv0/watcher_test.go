package pdpv0

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

func testWatcher() *Watcher {
	return &Watcher{
		chain:    make(chan *tipset, 1),
		watchers: make(map[WatcherOrder][]update),
	}
}

func testUpdate(fn func()) update {
	return func(context.Context, *harmonydb.DB, ethchain.EthClient, curioalerting.AlertingInterface, *chainTypes.TipSet, *chainTypes.TipSet) {
		fn()
	}
}

func TestWatcherExecuteRunsInPhaseOrder(t *testing.T) {
	w := testWatcher()

	var (
		lk   sync.Mutex
		seen []WatcherOrder
	)

	add := func(order WatcherOrder) {
		t.Helper()
		if err := w.AddWatcher(testUpdate(func() {
			lk.Lock()
			defer lk.Unlock()
			seen = append(seen, order)
		}), order); err != nil {
			t.Fatal(err)
		}
	}

	add(WatcherOrderProving)
	add(WatcherOrderCleanupPieces)
	add(WatcherOrderDelete)
	add(WatcherOrderPaymentSettle)
	add(WatcherOrderTerminate)
	add(WatcherOrderCreateAndAdd)

	w.execute(context.Background(), &tipset{})

	expected := []WatcherOrder{
		WatcherOrderCreateAndAdd,
		WatcherOrderTerminate,
		WatcherOrderPaymentSettle,
		WatcherOrderDelete,
		WatcherOrderCleanupPieces,
		WatcherOrderProving,
	}
	if !reflect.DeepEqual(seen, expected) {
		t.Fatalf("execution order = %v, want %v", seen, expected)
	}
}

func TestWatcherExecuteRunsSamePhaseTogether(t *testing.T) {
	w := testWatcher()

	startedA := make(chan struct{})
	startedB := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	if err := w.AddWatcher(testUpdate(func() {
		close(startedA)
		<-release
	}), WatcherOrderCreateAndAdd); err != nil {
		t.Fatal(err)
	}
	if err := w.AddWatcher(testUpdate(func() {
		close(startedB)
		<-release
	}), WatcherOrderCreateAndAdd); err != nil {
		t.Fatal(err)
	}

	go func() {
		w.execute(context.Background(), &tipset{})
		close(done)
	}()

	waitForClose(t, startedA, "first same-phase watcher did not start")
	waitForClose(t, startedB, "second same-phase watcher did not start")

	close(release)
	waitForClose(t, done, "same-phase watchers did not finish")
}

func TestWatcherAddWatcherAfterRunFails(t *testing.T) {
	w := testWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.Run(ctx)

	if err := w.AddWatcher(testUpdate(func() {}), WatcherOrderCreateAndAdd); err == nil {
		t.Fatal("expected AddWatcher after Run to fail")
	}
}

func waitForClose(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
}
