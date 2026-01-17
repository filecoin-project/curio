package window

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

type SimpleFaultTracker struct {
	storage paths.Store
	index   paths.SectorIndex

	parallelCheckLimit    int // todo live config?
	singleCheckTimeout    time.Duration
	partitionCheckTimeout time.Duration
}

func NewSimpleFaultTracker(storage paths.Store, index paths.SectorIndex,
	parallelCheckLimit int, singleCheckTimeout time.Duration, partitionCheckTimeout time.Duration) *SimpleFaultTracker {
	return &SimpleFaultTracker{
		storage: storage,
		index:   index,

		parallelCheckLimit:    parallelCheckLimit,
		singleCheckTimeout:    singleCheckTimeout,
		partitionCheckTimeout: partitionCheckTimeout,
	}
}

func (m *SimpleFaultTracker) CheckProvable(ctx context.Context, pp abi.RegisteredPoStProof, sectors []storiface.SectorRef, rg storiface.RGetter) (map[abi.SectorID]string, error) {
	checkStartTime := time.Now()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if rg == nil {
		return nil, xerrors.Errorf("rg is nil")
	}

	var bad = make(map[abi.SectorID]string)
	var badLk sync.Mutex
	var pendingCount int32 // Track how many goroutines are waiting for throttle slot

	var postRand abi.PoStRandomness = make([]byte, abi.RandomnessLength)
	_, _ = rand.Read(postRand)
	postRand[31] &= 0x3f

	limit := m.parallelCheckLimit
	if limit <= 0 {
		limit = len(sectors)
	}
	throttle := make(chan struct{}, limit)

	log.Infow("CheckProvable starting",
		"sectors", len(sectors),
		"parallelLimit", limit,
		"singleCheckTimeout", m.singleCheckTimeout,
		"partitionCheckTimeout", m.partitionCheckTimeout)

	addBad := func(s abi.SectorID, reason string) {
		badLk.Lock()
		bad[s] = reason
		badLk.Unlock()
	}

	if m.partitionCheckTimeout > 0 {
		var cancel2 context.CancelFunc
		ctx, cancel2 = context.WithTimeout(ctx, m.partitionCheckTimeout)
		defer cancel2()
	}

	var wg sync.WaitGroup
	wg.Add(len(sectors))

	for sectorIdx, sector := range sectors {
		queueStartTime := time.Now()

		badLk.Lock()
		pendingCount++
		currentPending := pendingCount
		badLk.Unlock()

		select {
		case throttle <- struct{}{}:
			queueWaitTime := time.Since(queueStartTime)
			if queueWaitTime > 5*time.Second {
				log.Warnw("CheckProvable throttle slot acquisition was slow",
					"sector", sector.ID.Number,
					"sectorIdx", sectorIdx,
					"totalSectors", len(sectors),
					"queueWaitTime", queueWaitTime,
					"pendingWhenQueued", currentPending)
			}
		case <-ctx.Done():
			log.Warnw("CheckProvable context done while waiting for throttle",
				"sector", sector.ID.Number,
				"sectorIdx", sectorIdx,
				"totalSectors", len(sectors),
				"pendingWhenQueued", currentPending,
				"timeSinceStart", time.Since(checkStartTime),
				"error", ctx.Err())
			addBad(sector.ID, fmt.Sprintf("waiting for check worker (idx %d/%d, pending %d): %s",
				sectorIdx, len(sectors), currentPending, ctx.Err()))
			wg.Done()

			badLk.Lock()
			pendingCount--
			badLk.Unlock()
			continue
		}

		badLk.Lock()
		pendingCount--
		badLk.Unlock()

		go func(sector storiface.SectorRef, sectorIdx int, dispatchTime time.Time) {
			defer wg.Done()
			defer func() {
				<-throttle
			}()
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			sectorStartTime := time.Now()

			commr, update, err := rg(ctx, sector.ID)
			rgDuration := time.Since(sectorStartTime)

			if err != nil {
				log.Warnw("CheckProvable Sector FAULT: getting commR",
					"sector", sector.ID.Number,
					"sectorIdx", sectorIdx,
					"rgDuration", rgDuration,
					"err", err)
				addBad(sector.ID, fmt.Sprintf("getting commR: %s", err))
				return
			}

			toLock := storiface.FTSealed | storiface.FTCache
			if update {
				toLock = storiface.FTUpdate | storiface.FTUpdateCache
			}

			lockStart := time.Now()
			locked, err := m.index.StorageTryLock(ctx, sector.ID, toLock, storiface.FTNone)
			lockDuration := time.Since(lockStart)

			if err != nil {
				log.Warnw("CheckProvable Sector FAULT: tryLock error",
					"sector", sector.ID.Number,
					"sectorIdx", sectorIdx,
					"lockDuration", lockDuration,
					"error", err)
				addBad(sector.ID, fmt.Sprintf("tryLock error: %s", err))
				return
			}

			if !locked {
				log.Warnw("CheckProvable Sector FAULT: can't acquire read lock",
					"sector", sector.ID.Number,
					"sectorIdx", sectorIdx,
					"lockDuration", lockDuration)
				addBad(sector.ID, "can't acquire read lock")
				return
			}

			challengeStart := time.Now()
			ch, err := ffi.GeneratePoStFallbackSectorChallenges(pp, sector.ID.Miner, postRand, []abi.SectorNumber{
				sector.ID.Number,
			})
			challengeDuration := time.Since(challengeStart)

			if err != nil {
				log.Warnw("CheckProvable Sector FAULT: generating challenges",
					"sector", sector.ID.Number,
					"sectorIdx", sectorIdx,
					"challengeDuration", challengeDuration,
					"err", err)
				addBad(sector.ID, fmt.Sprintf("generating fallback challenges: %s", err))
				return
			}

			vctx := ctx

			if m.singleCheckTimeout > 0 {
				var cancel2 context.CancelFunc
				vctx, cancel2 = context.WithTimeout(ctx, m.singleCheckTimeout)
				defer cancel2()
			}

			proofStart := time.Now()
			_, err = m.storage.GenerateSingleVanillaProof(vctx, sector.ID.Miner, storiface.PostSectorChallenge{
				SealProof:    sector.ProofType,
				SectorNumber: sector.ID.Number,
				SealedCID:    commr,
				Challenge:    ch.Challenges[sector.ID.Number],
				Update:       update,
			}, pp)
			proofDuration := time.Since(proofStart)
			totalSectorDuration := time.Since(sectorStartTime)

			if err != nil {
				log.Warnw("CheckProvable Sector FAULT: generating vanilla proof",
					"sector", sector.ID.Number,
					"sectorIdx", sectorIdx,
					"totalSectors", len(sectors),
					"proofDuration", proofDuration,
					"totalSectorDuration", totalSectorDuration,
					"rgDuration", rgDuration,
					"lockDuration", lockDuration,
					"challengeDuration", challengeDuration,
					"timeSinceDispatch", time.Since(dispatchTime),
					"timeSinceStart", time.Since(checkStartTime),
					"update", update,
					"err", err)
				addBad(sector.ID, fmt.Sprintf("generating vanilla proof: %s", err))
				return
			}

			if totalSectorDuration > 30*time.Second {
				log.Warnw("CheckProvable slow sector check",
					"sector", sector.ID.Number,
					"sectorIdx", sectorIdx,
					"totalSectorDuration", totalSectorDuration,
					"proofDuration", proofDuration,
					"rgDuration", rgDuration,
					"lockDuration", lockDuration,
					"challengeDuration", challengeDuration,
					"update", update)
			}
		}(sector, sectorIdx, time.Now())
	}

	wg.Wait()

	checkDuration := time.Since(checkStartTime)
	log.Infow("CheckProvable complete",
		"sectors", len(sectors),
		"bad", len(bad),
		"duration", checkDuration)

	return bad, nil
}
