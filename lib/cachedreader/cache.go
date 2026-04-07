package cachedreader

import (
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/jellydator/ttlcache/v2"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/commcidv2"
)

var cacheLog = logging.Logger("cacheReaderCache")

type pieceCidKeyCache struct {
	cache *ttlcache.Cache
}

func newPieceCidKeyCache(ttl time.Duration, limit int, skipTTLExtension bool) *pieceCidKeyCache {
	c := ttlcache.NewCache()
	_ = c.SetTTL(ttl)
	c.SetCacheSizeLimit(limit)
	c.SkipTTLExtensionOnHit(skipTTLExtension)
	return &pieceCidKeyCache{cache: c}
}

func (m *pieceCidKeyCache) Get(pieceCid cid.Cid) (interface{}, bool) {
	keys := getCacheKeys(pieceCid)
	for i := range keys {
		v, err := m.cache.Get(keys[i])
		if err == nil {
			return v, true
		}
		cacheLog.Debugw("failed to get piece CID from cache", "piececid", pieceCid, "keys", keys[i], "err", err)
	}
	return nil, false
}

func (m *pieceCidKeyCache) Set(pieceCid cid.Cid, data interface{}) {
	keys := getCacheKeys(pieceCid)
	for i := range keys {
		err := m.cache.Set(keys[i], data)
		if err != nil {
			cacheLog.Debugw("failed to set cache value", "piececid", pieceCid, "keys", keys[i], "err", err)
		}
	}
}

func (m *pieceCidKeyCache) SetExpirationReasonCallback(callback ttlcache.ExpireReasonCallback) {
	m.cache.SetExpirationReasonCallback(callback)
}

func (m *pieceCidKeyCache) Count() int {
	return m.cache.Count()
}

func (m *pieceCidKeyCache) Remove(pieceCid cid.Cid) {
	keys := getCacheKeys(pieceCid)
	for i := range keys {
		err := m.cache.Remove(keys[i])
		if err != nil {
			cacheLog.Debugw("failed to remove piece CID from cache", "piececid", pieceCid, "keys", keys[i], "err", err)
		}
	}
}

func getCacheKeys(pieceCid cid.Cid) []string {
	if commcidv2.IsPieceCidV2(pieceCid) {
		pcid1, _, err := commcid.PieceCidV1FromV2(pieceCid)
		if err != nil {
			cacheLog.Debugw("failed to get piece commitment from piece CID v2", "piececid", pieceCid, "err", err)
		}
		return []string{pieceCid.String(), pcid1.String()}
	}
	if commcidv2.IsCidV1PieceCid(pieceCid) {
		return []string{pieceCid.String()}
	}
	cacheLog.Debugw("unknown piece CID type", "piececid", pieceCid)
	return []string{}
}
