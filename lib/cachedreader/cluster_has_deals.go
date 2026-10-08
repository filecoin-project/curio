package cachedreader

import (
	"context"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	// DEALS_TRUE_TTL is how long "this cluster has deals" is trusted. Deals
	// rarely disappear, and a stale true only means the legacy lookups run.
	DEALS_TRUE_TTL = 5 * time.Minute

	// DEALS_FALSE_TTL is how long "no deals" is trusted, so a cluster that gets
	// its first deal starts serving it quickly.
	DEALS_FALSE_TTL = 5 * time.Second

	DEALS_QUERY_TIMEOUT = 10 * time.Second
)

type clusterHasDealsState struct {
	has     bool
	checked time.Time
}

// clusterHasDeals tells whether market_piece_deal has any row. That table is the
// only precondition of the sector, market piece-park and PDP v1 retrieval
// paths, so with no rows a piece that is not in hash space cannot be found
// by them and their SQL lookups can be skipped.
type clusterHasDeals struct {
	exists func(ctx context.Context) (bool, error)
	now    func() time.Time

	state atomic.Pointer[clusterHasDealsState]
	group singleflight.Group
}

func newClusterHasDeals(db *harmonydb.DB) *clusterHasDeals {
	g := &clusterHasDeals{now: time.Now}
	if db != nil {
		g.exists = func(ctx context.Context) (bool, error) {
			var has bool
			err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market_piece_deal)`).Scan(&has)
			return has, err
		}
	}
	return g
}

// HasDeals reports whether any deal exists. Without a database, or when the
// check fails, it answers true so callers take the full lookup.
func (g *clusterHasDeals) HasDeals(ctx context.Context) bool {
	if g == nil || g.exists == nil {
		return true
	}
	if st := g.state.Load(); st != nil {
		ttl := DEALS_FALSE_TTL
		if st.has {
			ttl = DEALS_TRUE_TTL
		}
		if g.now().Sub(st.checked) < ttl {
			return st.has
		}
	}

	v, err, _ := g.group.Do("deals", func() (any, error) {
		qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DEALS_QUERY_TIMEOUT)
		defer cancel()
		has, err := g.exists(qctx)
		if err != nil {
			return nil, err
		}
		g.state.Store(&clusterHasDealsState{has: has, checked: g.now()})
		return has, nil
	})
	if err != nil {
		log.Warnw("checking for market deals, taking the full lookup", "error", err)
		return true
	}
	return v.(bool)
}
