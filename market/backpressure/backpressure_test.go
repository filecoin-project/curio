package backpressure

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jellydator/ttlcache/v2"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func newTestBackPressure(t *testing.T) *CachedBackPressure {
	t.Helper()

	cache := ttlcache.NewCache()
	cache.SkipTTLExtensionOnHit(true)
	t.Cleanup(func() {
		_ = cache.Close()
	})

	return &CachedBackPressure{cache: cache}
}

func TestMK20ReleasePressureBypassesCache(t *testing.T) {
	bp := newTestBackPressure(t)
	require.NoError(t, bp.cache.SetWithTTL(mk20BackpressureKey, true, 2*time.Minute))
	require.NoError(t, bp.cache.SetWithTTL(sectorBackpressureKey, true, 10*time.Minute))

	var mk20Calls, sectorCalls int
	bp.checkMK20Pipeline = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
		mk20Calls++
		return false, nil
	}
	bp.checkSector = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
		sectorCalls++
		return false, nil
	}

	pressure, err := bp.MK20ReleasePressure(context.Background(), nil, nil)
	require.NoError(t, err)
	require.False(t, pressure)
	require.Equal(t, 1, mk20Calls)
	require.Equal(t, 1, sectorCalls)

	// The fresh path neither consumes nor replaces the cached intake results.
	cachedMK20, err := bp.MK20Pressure(context.Background(), nil, nil)
	require.NoError(t, err)
	require.True(t, cachedMK20)
	cachedSector, err := bp.SectorPressure(context.Background(), nil, nil)
	require.NoError(t, err)
	require.True(t, cachedSector)
	require.Equal(t, 1, mk20Calls)
	require.Equal(t, 1, sectorCalls)
}

func TestMK20ReleasePressureObservesEachPass(t *testing.T) {
	bp := newTestBackPressure(t)

	mk20Checks := []bool{true, false}
	bp.checkMK20Pipeline = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
		pressure := mk20Checks[0]
		mk20Checks = mk20Checks[1:]
		return pressure, nil
	}
	var sectorCalls int
	bp.checkSector = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
		sectorCalls++
		return false, nil
	}

	pressure, err := bp.MK20ReleasePressure(context.Background(), nil, nil)
	require.NoError(t, err)
	require.True(t, pressure)
	require.Equal(t, 0, sectorCalls, "sector query should be skipped when MK20 pressure already stops release")

	pressure, err = bp.MK20ReleasePressure(context.Background(), nil, nil)
	require.NoError(t, err)
	require.False(t, pressure)
	require.Equal(t, 1, sectorCalls)
}

func TestMK20ReleasePressureIncludesSectorPressure(t *testing.T) {
	bp := newTestBackPressure(t)
	bp.checkMK20Pipeline = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
		return false, nil
	}
	bp.checkSector = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
		return true, nil
	}

	pressure, err := bp.MK20ReleasePressure(context.Background(), nil, nil)
	require.NoError(t, err)
	require.True(t, pressure)
}

func TestMK20ReleasePressureFailsClosedOnCheckErrors(t *testing.T) {
	t.Run("MK20", func(t *testing.T) {
		bp := newTestBackPressure(t)
		expected := errors.New("mk20 query failed")
		bp.checkMK20Pipeline = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
			return false, expected
		}
		bp.checkSector = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
			t.Fatal("sector check must not run after an MK20 check error")
			return false, nil
		}

		pressure, err := bp.MK20ReleasePressure(context.Background(), nil, nil)
		require.False(t, pressure)
		require.ErrorIs(t, err, expected)
	})

	t.Run("sector", func(t *testing.T) {
		bp := newTestBackPressure(t)
		expected := errors.New("sector query failed")
		bp.checkMK20Pipeline = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
			return false, nil
		}
		bp.checkSector = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
			return false, expected
		}

		pressure, err := bp.MK20ReleasePressure(context.Background(), nil, nil)
		require.False(t, pressure)
		require.ErrorIs(t, err, expected)
	})
}

func TestCachedPressureTTLsRemainUnchanged(t *testing.T) {
	bp := newTestBackPressure(t)

	var mk20Calls, sectorCalls int
	bp.checkMK20Pipeline = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
		mk20Calls++
		return false, nil
	}
	bp.checkSector = func(context.Context, *config.CurioIngestConfig, *harmonydb.DB) (bool, error) {
		sectorCalls++
		return false, nil
	}

	for range 2 {
		pressure, err := bp.MK20Pressure(context.Background(), nil, nil)
		require.NoError(t, err)
		require.False(t, pressure)
		pressure, err = bp.SectorPressure(context.Background(), nil, nil)
		require.NoError(t, err)
		require.False(t, pressure)
	}
	require.Equal(t, 1, mk20Calls)
	require.Equal(t, 1, sectorCalls)

	_, mk20TTL, err := bp.cache.GetWithTTL(mk20BackpressureKey)
	require.NoError(t, err)
	require.Greater(t, mk20TTL, 119*time.Second)
	require.LessOrEqual(t, mk20TTL, 2*time.Minute)

	_, sectorTTL, err := bp.cache.GetWithTTL(sectorBackpressureKey)
	require.NoError(t, err)
	require.Greater(t, sectorTTL, 599*time.Second)
	require.LessOrEqual(t, sectorTTL, 10*time.Minute)
}
