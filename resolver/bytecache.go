package resolver

import (
	"github.com/dgraph-io/ristretto/v2"
)

// ByteCache is a ristretto-backed cache implementing the Cacher interface.
// It uses byte cost for eviction (len of cached DNS message) and TinyLFU
// admission policy.
type ByteCache struct {
	cache *ristretto.Cache[uint64, *cacheValue]
}

// NewByteCache creates a new ristretto-backed cache with the given max cost
// in bytes. If metrics is true, ristretto will collect hit/miss/eviction stats.
func NewByteCache(maxCost uint64, metrics bool) (*ByteCache, error) {
	c, err := ristretto.NewCache(&ristretto.Config[uint64, *cacheValue]{
		NumCounters: int64(maxCost / 100), // ~1 counter per 100 bytes of cost
		MaxCost:     int64(maxCost),
		BufferItems: 64,
		Metrics:     metrics,
	})
	if err != nil {
		return nil, err
	}
	return &ByteCache{cache: c}, nil
}

func (c *ByteCache) Get(key uint64) (*cacheValue, bool) {
	return c.cache.Get(key)
}

func (c *ByteCache) Set(key uint64, value *cacheValue) {
	cost := int64(len(value.msg))
	if cost == 0 {
		cost = 1
	}
	c.cache.Set(key, value, cost)
}

// Metrics returns the ristretto metrics, or nil if metrics are not enabled.
func (c *ByteCache) Metrics() *ristretto.Metrics {
	return c.cache.Metrics
}

// Close closes the underlying ristretto cache.
func (c *ByteCache) Close() {
	c.cache.Close()
}
