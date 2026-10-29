package cachedreader

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClusterHasDeals(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	var queries atomic.Int32
	has := false
	var failWith error

	g := &clusterHasDeals{
		now: func() time.Time { return now },
		exists: func(context.Context) (bool, error) {
			queries.Add(1)
			return has, failWith
		},
	}

	t.Run("no deals is cached for the short ttl", func(t *testing.T) {
		require.False(t, g.HasDeals(ctx))
		require.False(t, g.HasDeals(ctx))
		require.Equal(t, int32(1), queries.Load())

		has = true
		now = now.Add(DEALS_FALSE_TTL - time.Second)
		require.False(t, g.HasDeals(ctx))
		require.Equal(t, int32(1), queries.Load())

		now = now.Add(2 * time.Second)
		require.True(t, g.HasDeals(ctx))
		require.Equal(t, int32(2), queries.Load())
	})

	t.Run("deals are cached for the long ttl", func(t *testing.T) {
		has = false
		now = now.Add(time.Minute)
		require.True(t, g.HasDeals(ctx))
		require.Equal(t, int32(2), queries.Load())

		now = now.Add(DEALS_TRUE_TTL)
		require.False(t, g.HasDeals(ctx))
		require.Equal(t, int32(3), queries.Load())
	})

	t.Run("a failed check takes the full lookup", func(t *testing.T) {
		now = now.Add(time.Hour)
		failWith = errors.New("db down")
		require.True(t, g.HasDeals(ctx))
	})
}

func TestClusterHasDealsWithoutDB(t *testing.T) {
	require.True(t, newClusterHasDeals(nil).HasDeals(context.Background()))

	var g *clusterHasDeals
	require.True(t, g.HasDeals(context.Background()))
}
