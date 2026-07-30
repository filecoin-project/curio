package message

import (
	"context"
	"sync"
	"time"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"

	"github.com/filecoin-project/lotus/chain/types"
)

const (
	ReplaceStuckEpochs abi.ChainEpoch = 10
)

type ReplacerConfig struct {
	DB         *harmonydb.DB
	ChainSched *chainsched.CurioChainSched

	Filecoin *FilecoinReplacerConfig
	Eth      *EthReplacerConfig
}

type Replacer struct {
	mr  *messageReplacer
	emr *ethMessageReplacer

	triggerCh     chan struct{}
	triggerMu     sync.Mutex
	latestTrigger replaceTrigger
}

type replaceTrigger struct {
	Height    abi.ChainEpoch
	Timestamp time.Time
	Key       *types.TipSetKey
}

// Removal tracked in https://github.com/filecoin-project/curio/issues/1386 complete
// Message replacement only safe to enable once synapse sdk caller can be made aware of 
// replacement flow with small curio api redesign.
const replaceByFeeEnabled = false

func NewMessageReplacer(ctx context.Context, cfg ReplacerConfig) error {
	if !replaceByFeeEnabled {
		return nil
	}

	stuckForDuration := time.Duration(ReplaceStuckEpochs) * time.Duration(build.BlockDelaySecs) * time.Second

	t := &Replacer{
		triggerCh: make(chan struct{}, 1),
	}

	if cfg.Filecoin != nil {
		t.mr = &messageReplacer{
			db:               cfg.DB,
			api:              cfg.Filecoin.API,
			signer:           cfg.Filecoin.Signer,
			stuckForDuration: stuckForDuration,
		}
	}

	if cfg.Eth != nil {
		t.emr = &ethMessageReplacer{
			db:               cfg.DB,
			client:           cfg.Eth.Client,
			stuckForDuration: stuckForDuration,
		}
	}

	if err := cfg.ChainSched.AddWatcher(t.processHeadChange); err != nil {
		return err
	}

	go t.run(ctx)

	return nil
}

func (t *Replacer) processHeadChange(ctx context.Context, revert *types.TipSet, apply *types.TipSet) error {
	if apply == nil {
		return nil
	}

	t.triggerMu.Lock()
	t.latestTrigger = replaceTrigger{
		Height:    apply.Height(),
		Timestamp: time.Unix(int64(apply.MinTimestamp()), 0).UTC(),
		Key:       new(apply.Key()),
	}
	t.triggerMu.Unlock()

	select {
	case t.triggerCh <- struct{}{}:
	default:
	}

	return nil
}

func (t *Replacer) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.triggerCh:
			trigger := t.currentTrigger()
			if trigger.Timestamp.IsZero() {
				continue
			}
			t.runReplacement(ctx, trigger)
		}
	}
}

func (t *Replacer) currentTrigger() replaceTrigger {
	t.triggerMu.Lock()
	defer t.triggerMu.Unlock()

	return t.latestTrigger
}

func (t *Replacer) runReplacement(ctx context.Context, trigger replaceTrigger) {
	if t.mr != nil {
		err := t.mr.runMessageReplacement(ctx, trigger.Key, trigger.Height, trigger.Timestamp)
		if err != nil {
			log.Errorw("message replacement failed", "error", err)
		}
	}

	if t.emr != nil {
		err := t.emr.runEthMessageReplacement(ctx, trigger.Height, trigger.Timestamp)
		if err != nil {
			log.Errorw("eth message replacement failed", "error", err)
		}
	}

}
