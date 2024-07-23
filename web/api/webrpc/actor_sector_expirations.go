package webrpc

import (
	"context"

	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/lotus/chain/types"
)

type SectorExpirationBucket struct {
	Expiration int64 `db:"expiration_bucket"`
	Count      int64 `db:"count"`

	// db ignored
	Days int64 `db:"-"`
}

type SectorExpirations struct {
	All []SectorExpirationBucket
	CC  []SectorExpirationBucket
}

func (a *WebRPC) ActorSectorExpirations(ctx context.Context, maddr address.Address) (*SectorExpirations, error) {
	spID, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("id from %s: %w", maddr, err)
	}

	out := SectorExpirations{
		All: []SectorExpirationBucket{},
		CC:  []SectorExpirationBucket{},
	}

	now, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting head: %w", err)
	}

	nowBucketNum := int64(now.Height()) / 10000

	err = a.deps.DB.Select(ctx, &out.All, `SELECT
			(expiration_epoch / 10000) * 10000 AS expiration_bucket, 
			COUNT(*) as count
		FROM 
			sectors_meta 
		WHERE 
			sp_id = $1
			AND expiration_epoch IS NOT NULL
			AND expiration_epoch >= $2
		GROUP BY
			(expiration_epoch / 10000) * 10000 
		ORDER BY 
		expiration_bucket`, int64(spID), nowBucketNum)
	if err != nil {
		return nil, err
	}
	err = a.deps.DB.Select(ctx, &out.CC, `SELECT
			(expiration_epoch / 10000) * 10000 AS expiration_bucket, 
			COUNT(*) as count
		FROM 
			sectors_meta 
		WHERE 
			sp_id = $1
		  	AND is_cc = true
			AND expiration_epoch IS NOT NULL
			AND expiration_epoch >= $2
		GROUP BY
			(expiration_epoch / 10000) * 10000 
		ORDER BY 
		expiration_bucket`, int64(spID), nowBucketNum)
	if err != nil {
		return nil, err
	}

	out.All, err = a.prepExpirationBucket(out.All, now)
	if err != nil {
		return nil, err
	}
	out.CC, err = a.prepExpirationBucket(out.CC, now)
	if err != nil {
		return nil, err
	}

	// If first point in CC is larger than first point in All, shift the first CC point to the first All point
	if len(out.CC) > 0 && len(out.All) > 0 && out.CC[0].Expiration > out.All[0].Expiration {
		out.CC[0].Expiration = out.All[0].Expiration
	}

	return &out, nil
}

func (a *WebRPC) prepExpirationBucket(out []SectorExpirationBucket, now *types.TipSet) ([]SectorExpirationBucket, error) {
	totalCount := lo.Reduce(out, func(acc int64, b SectorExpirationBucket, _ int) int64 {
		return acc + b.Count
	}, int64(0))

	if len(out) == 0 {
		return out, nil
	}

	if out[0].Expiration > int64(now.Height()) {
		nowBucket := SectorExpirationBucket{
			Expiration: int64(now.Height()),
			Count:      0,
		}
		out = append([]SectorExpirationBucket{nowBucket}, out...)
	}

	for i := range out {
		newTotal := totalCount - out[i].Count
		out[i].Count = newTotal
		totalCount = newTotal

		epochsToExpiry := abi.ChainEpoch(out[i].Expiration) - now.Height()
		secsToExpiry := epochsToExpiry * builtin.EpochDurationSeconds
		daysToExpiry := secsToExpiry / (60 * 60 * 24)

		out[i].Days = int64(daysToExpiry)
	}

	return out, nil
}
