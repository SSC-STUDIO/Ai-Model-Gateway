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

// Cache is a thread-safe, size-bounded LRU cache with TTL expiration.
type Cache struct {
	mu           sync.RWMutex
	entries      map[string]*entry
	lru          *list.List
	maxBytes     int64
	ttl          time.Duration
	currentBytes int64
	now          func() time.Time
}

// NewCache creates a new LRU cache.
// maxSizeMB controls the maximum total size of cached values in megabytes.
// ttlSec controls how long each entry remains valid.
// If maxSizeMB <= 0 a default of 64 MB is used.
// If ttlSec <= 0 a default of 300 seconds is used.
func NewCache(maxSizeMB int, ttlSec int) *Cache {
	if maxSizeMB <= 0 {
		maxSizeMB = 64
	}
	if ttlSec <= 0 {
		ttlSec = 300
	}
	return &Cache{
		entries:  make(map[string]*entry),
		lru:      list.New(),
		maxBytes: int64(maxSizeMB) * 1024 * 1024,
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
// If adding the entry would exceed the byte budget, the least recently used
// entries are evicted until there is room.
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
	entrySize := int64(len(value))
	for c.currentBytes+entrySize > c.maxBytes && c.lru.Len() > 0 {
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
	c.currentBytes += entrySize
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

// SizeBytes returns the total size of cached values in bytes.
func (c *Cache) SizeBytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentBytes
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
	c.currentBytes -= int64(len(e.value))
	if c.currentBytes < 0 {
		c.currentBytes = 0
	}
}
