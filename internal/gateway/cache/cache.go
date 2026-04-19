// Package cache provides an in-memory LRU cache for gateway response caching.
package cache

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// entry holds a cached response and its position in the LRU list.
type entry struct {
	key      string
	value    []byte
	cachedAt time.Time
	listElem *list.Element
}

// Cache is a thread-safe LRU cache with TTL expiration.
type Cache struct {
	mu       sync.RWMutex
	entries  map[string]*entry
	lru      *list.List
	maxItems int
	ttl      time.Duration
	now      func() time.Time
}

// NewCache creates a new LRU cache.
// maxEntries controls the maximum number of cached items.
// ttlSec controls how long each entry remains valid.
// If maxEntries <= 0 a default of 1000 is used.
// If ttlSec <= 0 a default of 300 seconds is used.
func NewCache(maxEntries int, ttlSec int) *Cache {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if ttlSec <= 0 {
		ttlSec = 300
	}
	return &Cache{
		entries:  make(map[string]*entry),
		lru:      list.New(),
		maxItems: maxEntries,
		ttl:      time.Duration(ttlSec) * time.Second,
		now:      time.Now,
	}
}

// Get retrieves a cached value by key.
// Returns the value and true if found and not expired, nil and false otherwise.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	// Check TTL expiration.
	if c.now().Sub(e.cachedAt) > c.ttl {
		c.removeEntry(e)
		return nil, false
	}

	// Promote to front of LRU.
	c.lru.MoveToFront(e.listElem)
	return e.value, true
}

// Put stores a value in the cache.
// If adding the entry would exceed the item limit, the least recently used
// entry is evicted.
func (c *Cache) Put(key string, value []byte) {
	if len(value) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If the key already exists, remove the old entry first.
	if existing, ok := c.entries[key]; ok {
		c.removeEntry(existing)
	}

	// Evict until we have room for the new value.
	for c.lru.Len() >= c.maxItems {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		if e, ok := oldest.Value.(*entry); ok {
			c.removeEntry(e)
		}
	}

	e := &entry{
		key:      key,
		value:    value,
		cachedAt: c.now(),
	}
	e.listElem = c.lru.PushFront(e)
	c.entries[key] = e
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


// MakeKey produces a cache key from the request body and model name.
// The key is the hex-encoded SHA-256 of (body + model).
func (c *Cache) MakeKey(body []byte, model string) string {
	h := sha256.New()
	h.Write(body)
	h.Write([]byte(model))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// removeEntry removes an entry from both the map and the LRU list.
// Must be called with c.mu held.
func (c *Cache) removeEntry(e *entry) {
	delete(c.entries, e.key)
	c.lru.Remove(e.listElem)
}
