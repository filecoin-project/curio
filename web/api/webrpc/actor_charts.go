package webrpc

import (
	"context"
	"sort"

	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/lib/curiochain"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
)

type SectorBucket struct {
	BucketEpoch       abi.ChainEpoch // e.g., (Expiration / 10000) * 10000
	Count             int64          // how many sectors
	QAP               abi.DealWeight // Total Deal weight - sum of DealWeights in this bucket
	Days              int64
	VestedLockedFunds abi.TokenAmount // Total locked funds (Vesting) - Vested in 10000 epochs
}

type SectorBuckets struct {
	All []SectorBucket
	CC  []SectorBucket
}

func (a *WebRPC) ActorCharts(ctx context.Context, maddr address.Address) (*SectorBuckets, error) {
	out := SectorBuckets{
		All: []SectorBucket{},
		CC:  []SectorBucket{},
	}

	stor := store.ActorStore(ctx,
		blockstore.NewReadCachedBlockstore(blockstore.NewAPIBlockstore(a.deps.Chain), curiochain.ChainBlockCache))

	now, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting head: %w", err)
	}

	mact, err := a.deps.Chain.StateGetActor(ctx, maddr, now.Key())
	if err != nil {
		return nil, xerrors.Errorf("getting actor: %w", err)
	}

	mas, err := miner.Load(stor, mact)
	if err != nil {
		return nil, err
	}

	sectors, err := mas.LoadSectors(nil)
	if err != nil {
		return nil, xerrors.Errorf("loading sectors: %w", err)
	}

	locked, err := mas.LockedFunds()
	if err != nil {
		return nil, xerrors.Errorf("loading locked funds: %w", err)
	}

	// 1) Group by 10,000-epoch buckets
	bucketsMap := make(map[abi.ChainEpoch]*SectorBucket)
	bucketsMapCC := make(map[abi.ChainEpoch]*SectorBucket)

	for _, sector := range sectors {
		if sector == nil {
			continue
		}

		// Determine bucket: e.g., 120,000, 130,000, etc.
		bucket := (sector.Expiration / 10000) * 10000

		// Initialize if not found
		sb, found := bucketsMap[bucket]
		if !found {
			vested, err := mas.VestedFunds(bucket)
			if err != nil {
				return nil, err
			}
			sb = &SectorBucket{
				BucketEpoch:       bucket,
				Count:             0,
				QAP:               abi.NewStoragePower(0),
				VestedLockedFunds: big.Sub(locked.VestingFunds, vested),
			}
			bucketsMap[bucket] = sb
		}

		// Accumulate data
		sb.Count++
		sb.QAP = big.Add(sb.QAP, sector.VerifiedDealWeight)

		if sector.DealWeight.Equals(abi.NewStoragePower(0)) {
			sbc, ok := bucketsMapCC[bucket]
			if !ok {
				sbc = &SectorBucket{
					BucketEpoch:       bucket,
					Count:             0,
					QAP:               abi.NewStoragePower(0),                   // Dummy value for CC TODO: Figure out the correct value
					VestedLockedFunds: big.Sub(locked.VestingFunds, big.Zero()), // Dummy value for CC TODO: Figure out the correct value
				}
				bucketsMapCC[bucket] = sbc
			}

			// Accumulate data
			sbc.Count++
		}
	}

	// 2) Convert map to a slice and sort by BucketEpoch
	buckets := make([]SectorBucket, 0, len(bucketsMap))
	for _, sb := range bucketsMap {
		buckets = append(buckets, *sb)
	}
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].BucketEpoch < buckets[j].BucketEpoch
	})
	bucketsCC := make([]SectorBucket, 0, len(bucketsMapCC))
	for _, sb := range bucketsMapCC {
		bucketsCC = append(bucketsCC, *sb)
	}
	sort.Slice(bucketsCC, func(i, j int) bool {
		return bucketsCC[i].BucketEpoch < bucketsCC[j].BucketEpoch
	})

	out.All, err = a.prepExpirationBucket(buckets, now)
	if err != nil {
		return nil, err
	}
	out.CC, err = a.prepExpirationBucket(bucketsCC, now)
	if err != nil {
		return nil, err
	}

	//If first point in CC is larger than first point in All, shift the first CC point to the first All point
	if len(out.CC) > 0 && len(out.All) > 0 && out.CC[0].BucketEpoch > out.All[0].BucketEpoch {
		out.CC[0].BucketEpoch = out.All[0].BucketEpoch
	}

	return &out, nil
}

func (a *WebRPC) prepExpirationBucket(out []SectorBucket, now *types.TipSet) ([]SectorBucket, error) {
	totalCount := lo.Reduce(out, func(acc int64, b SectorBucket, _ int) int64 {
		return acc + b.Count
	}, int64(0))
	totalPower := lo.Reduce(out, func(agg big.Int, b SectorBucket, _ int) big.Int { return big.Add(agg, b.QAP) }, big.Zero())

	if len(out) == 0 {
		return out, nil
	}

	if out[0].BucketEpoch > now.Height() {
		nowBucket := SectorBucket{
			BucketEpoch:       now.Height(),
			Count:             0,
			QAP:               out[0].QAP,
			VestedLockedFunds: out[0].VestedLockedFunds,
		}
		out = append([]SectorBucket{nowBucket}, out...)
	}

	for i := range out {
		newTotal := totalCount - out[i].Count
		out[i].Count = newTotal
		totalCount = newTotal

		newTotalPower := big.Sub(totalPower, out[i].QAP)
		out[i].QAP = newTotalPower
		totalPower = newTotalPower

		epochsToExpiry := out[i].BucketEpoch - now.Height()
		secsToExpiry := int64(epochsToExpiry) * int64(build.BlockDelaySecs)
		daysToExpiry := secsToExpiry / (60 * 60 * 24)

		out[i].Days = daysToExpiry
	}

	return out, nil
}
