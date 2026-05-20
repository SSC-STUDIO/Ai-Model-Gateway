// Package cache provides an in-memory LRU cache for gateway response caching.
package cache

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// entry holds a cached response and its position in the LRU list.
type entry struct {
	key      string
	value    []byte
	bytes    int64
	cachedAt time.Time
	listElem *list.Element
}

// Stats contains cache performance counters.
type Stats struct {
	Hits      int64
	Misses    int64
	Evictions int64
}

// Config configures cache behaviour.
type Config struct {
	MaxEntries    int
	TTLSec        int
	MaxBytes      int64
	SweepInterval time.Duration
	SweepOnGet    bool
}

// Cache is a thread-safe LRU cache with TTL expiration and byte-level limits.
type Cache struct {
	mu       sync.RWMutex
	entries  map[string]*entry
	lru      *list.List
	maxItems int
	ttl      time.Duration
	now      func() time.Time

	// Byte-level accounting
	maxBytes     int64
	currentBytes atomic.Int64

	// Counters
	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64

	// Background sweep
	sweepInterval time.Duration
	stopCh        chan struct{}
	sweepOnGet    bool
}

// NewCache creates a new LRU cache.
// maxEntries controls the maximum number of cached items.
// ttlSec controls how long each entry remains valid.
// If maxEntries <= 0 a default of 1000 is used.
// If ttlSec <= 0 a default of 300 seconds is used.
func NewCache(maxEntries int, ttlSec int) *Cache {
	return NewCacheWithConfig(Config{
		MaxEntries: maxEntries,
		TTLSec:     ttlSec,
	})
}

// NewCacheWithConfig creates a new LRU cache from a Config.
func NewCacheWithConfig(cfg Config) *Cache {
	maxEntries := cfg.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	ttlSec := cfg.TTLSec
	if ttlSec <= 0 {
		ttlSec = 300
	}
	sweepInterval := cfg.SweepInterval
	if sweepInterval <= 0 {
		sweepInterval = 30 * time.Second
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 0 // 0 means unlimited
	}

	c := &Cache{
		entries:       make(map[string]*entry),
		lru:           list.New(),
		maxItems:      maxEntries,
		ttl:           time.Duration(ttlSec) * time.Second,
		now:           time.Now,
		maxBytes:      maxBytes,
		sweepInterval: sweepInterval,
		stopCh:        make(chan struct{}),
		sweepOnGet:    cfg.SweepOnGet,
	}

	// Start background sweep goroutine.
	go c.sweepLoop()

	return c
}

// Close stops the background sweep goroutine.
func (c *Cache) Close() {
	select {
	case <-c.stopCh:
		return // already closed
	default:
		close(c.stopCh)
	}
}

// Get retrieves a cached value by key.
// Returns the value and true if found and not expired, nil and false otherwise.
// Promotes the entry to the front of the LRU list on a hit.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	e, ok := c.entries[key]
	if !ok {
		c.misses.Add(1)
		c.mu.Unlock()
		return nil, false
	}

	// Check TTL expiration.
	if c.now().Sub(e.cachedAt) > c.ttl {
		c.misses.Add(1)
		c.removeEntry(e)
		c.evictions.Add(1)
		c.mu.Unlock()
		return nil, false
	}

	// Promote to front of LRU (true LRU behaviour).
	c.lru.MoveToFront(e.listElem)
	c.hits.Add(1)
	val := e.value
	c.mu.Unlock()
	return val, true
}

// Put stores a value in the cache.
// If adding the entry would exceed the item limit or byte limit,
// the least recently used entry is evicted.
func (c *Cache) Put(key string, value []byte) {
	if len(value) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	newBytes := int64(len(value))

	// If the key already exists, remove the old entry first.
	if existing, ok := c.entries[key]; ok {
		c.removeEntry(existing)
	}

	// Evict by count until we have room for the new entry.
	for c.lru.Len() >= c.maxItems {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		if e, ok := oldest.Value.(*entry); ok {
			c.removeEntry(e)
			c.evictions.Add(1)
		}
	}

	// Evict by bytes if adding this entry would exceed the byte limit.
	for c.maxBytes > 0 && c.currentBytes.Load()+newBytes > c.maxBytes {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		e, ok := oldest.Value.(*entry)
		if !ok {
			break
		}
		c.removeEntry(e)
		c.evictions.Add(1)
	}

	e := &entry{
		key:      key,
		value:    value,
		bytes:    newBytes,
		cachedAt: c.now(),
	}
	e.listElem = c.lru.PushFront(e)
	c.entries[key] = e
	c.currentBytes.Add(newBytes)
}

// Delete removes a single entry from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[key]; ok {
		c.removeEntry(e)
	}
}

// Len returns the number of entries currently in the cache.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// CurrentBytes returns the total size of cached values in bytes.
func (c *Cache) CurrentBytes() int64 {
	return c.currentBytes.Load()
}

// MaxItems returns the maximum number of items allowed in the cache.
func (c *Cache) MaxItems() int {
	return c.maxItems
}

// MaxBytes returns the byte-level limit (0 means unlimited).
func (c *Cache) MaxBytes() int64 {
	return c.maxBytes
}

// TTL returns the cache entry TTL duration.
func (c *Cache) TTL() time.Duration {
	return c.ttl
}

// Stats returns a snapshot of the cache counters.
func (c *Cache) Stats() Stats {
	return Stats{
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		Evictions: c.evictions.Load(),
	}
}

// MakeKey produces a cache key from the request body, model name, and an
// optional namespace (e.g. tenant ID) for isolation.
func (c *Cache) MakeKey(body []byte, model string, namespace string) string {
	h := sha256.New()
	h.Write(body)
	h.Write([]byte(model))
	if namespace != "" {
		h.Write([]byte(namespace))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// MakeKeyLegacy produces a cache key without namespace, for backward compatibility.
func (c *Cache) MakeKeyLegacy(body []byte, model string) string {
	return c.MakeKey(body, model, "")
}

// removeEntry removes an entry from both the map and the LRU list.
// Must be called with c.mu held.
func (c *Cache) removeEntry(e *entry) {
	delete(c.entries, e.key)
	c.lru.Remove(e.listElem)
	c.currentBytes.Add(-e.bytes)
}

// sweepLoop runs a background sweep that removes expired entries periodically.
func (c *Cache) sweepLoop() {
	ticker := time.NewTicker(c.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.sweepExpired()
		}
	}
}

// sweepExpired removes all expired entries from the cache.
func (c *Cache) sweepExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	expired := make([]*entry, 0)
	for _, e := range c.entries {
		if now.Sub(e.cachedAt) > c.ttl {
			expired = append(expired, e)
		}
	}

	for _, e := range expired {
		c.removeEntry(e)
	}
	c.evictions.Add(int64(len(expired)))

	return len(expired)
}

// ensureSize evicts the least recently used entry until the cache fits within
// its byte limit. Must be called with c.mu held.
func (c *Cache) ensureSize() int {
	if c.maxBytes <= 0 {
		return 0
	}

	var evicted int
	for c.currentBytes.Load() > c.maxBytes {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		e, ok := oldest.Value.(*entry)
		if !ok {
			break
		}
		c.removeEntry(e)
		evicted++
	}
	c.evictions.Add(int64(evicted))
	return evicted
}

// makeKeyRaw produces a deterministic key for testing without a Cache instance.
func makeKeyRaw(body []byte, model, namespace string) string {
	h := sha256.New()
	h.Write(body)
	h.Write([]byte(model))
	if namespace != "" {
		h.Write([]byte(namespace))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
