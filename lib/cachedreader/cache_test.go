package cachedreader

import (
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/jellydator/ttlcache/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPieceCidKeyCache(t *testing.T) {
	v1, err := cid.Decode("baga6ea4seaqomqafu276g53zko4k23xzh4h4uecjwicbmvhsuqi7o4bhthhm4aq")
	require.NoError(t, err)
	v2, err := cid.Decode("bafkzcibcaap6mqafu276g53zko4k23xzh4h4uecjwicbmvhsuqi7o4bhthhm4aq")
	require.NoError(t, err)

	tv1, err := cid.Parse("baga6ea4seaqes3nobte6ezpp4wqan2age2s5yxcatzotcvobhgcmv5wi2xh5mbi")
	require.NoError(t, err)
	tv2, err := cid.Parse("bafkzcibcaaces3nobte6ezpp4wqan2age2s5yxcatzotcvobhgcmv5wi2xh5mbi")
	require.NoError(t, err)

	notPieceCID, err := cid.Parse("bafybeihkqv2ukwgpgzkwsuz7whmvneztvxglkljbs3zosewgku2cfluvba")
	require.NoError(t, err)

	cache := newPieceCidKeyCache(2*time.Second, 100, true)

	// Test case: Get from empty cache
	t.Run("empty cache", func(t *testing.T) {
		value, found := cache.Get(v1)
		assert.Nil(t, value)
		assert.False(t, found)
	})

	// Test case: Get after setting a value
	t.Run("get existing value", func(t *testing.T) {
		expectedData := "testdata"
		cache.Set(v1, expectedData)

		value, found := cache.Get(v1)
		assert.Equal(t, expectedData, value)
		assert.True(t, found)
	})

	// Test: Cache miss with different Piece Cid
	t.Run("cache miss with different Piece Cid", func(t *testing.T) {
		cache.Set(v2, "testdata")
		value, found := cache.Get(tv1)
		assert.Nil(t, value)
		assert.False(t, found)
		value, found = cache.Get(tv2)
		assert.Nil(t, value)
		assert.False(t, found)
	})

	// Test case: Cache hit with v2 when v1 is added
	t.Run("cache git with piece Cid v2 when v1 is added as key", func(t *testing.T) {
		expectedData := "testdata"
		cache.Set(v1, expectedData)

		value, found := cache.Get(v2)
		assert.Equal(t, expectedData, value)
		assert.True(t, found)
	})

	// Test case: Cache hit with v1 when v2 is added
	t.Run("cache hit with v1 when v2 is added", func(t *testing.T) {
		cache.Remove(v1)
		cache.Set(v2, "testdata")

		value, found := cache.Get(v1)
		assert.Equal(t, "testdata", value)
		assert.True(t, found)
	})

	// Test case: Cache miss due to TTL
	t.Run("cache miss with v2 when v1 is added", func(t *testing.T) {
		cache.Set(v2, "testdata")
		time.Sleep(3 * time.Second)

		value, found := cache.Get(v2)
		assert.Nil(t, value)
		assert.False(t, found)
		value, found = cache.Get(v1)
		assert.Nil(t, value)
		assert.False(t, found)
	})

	// Test case: Count should be 1 after cache hit if v1
	t.Run("count after cache hit", func(t *testing.T) {
		cache.Set(v1, "testdata")
		count := cache.Count()
		assert.Equal(t, 1, count)
	})

	// Test case: Count should be 2 after cache hit if v2
	t.Run("count after cache hit", func(t *testing.T) {
		cache.Set(v2, "testdata")
		count := cache.Count()
		assert.Equal(t, 2, count)
	})

	t.Run("remove non-existent key", func(t *testing.T) {
		cache.Remove(v2)
		cache.Remove(v1) // Should not cause any panic or errors
	})

	// Test case: Set expiration callback
	t.Run("expiration callback", func(t *testing.T) {
		callbackCalled := false
		cache.SetExpirationReasonCallback(func(key string, reason ttlcache.EvictionReason, value interface{}) {
			callbackCalled = true
		})

		// Add item and wait for expiration
		cache.Set(tv1, "value")
		time.Sleep(3 * time.Second)

		assert.True(t, callbackCalled, "Callback should have been triggered")
	})

	// Test case: Overwrite an existing key
	t.Run("overwrite value", func(t *testing.T) {
		initialData := "initial"
		updatedData := "updated"

		cache.Set(v1, initialData)
		cache.Set(v1, updatedData)

		value, found := cache.Get(v1)
		assert.Equal(t, updatedData, value)
		assert.True(t, found)
	})

	t.Run("should not accept non-piece CID", func(t *testing.T) {
		cache.Set(notPieceCID, "testdata")
		data, found := cache.Get(notPieceCID)
		assert.Nil(t, data)
		assert.False(t, found)
	})
}
