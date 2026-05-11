package cache

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestNewCacheAppliesDefaults(t *testing.T) {
	c := NewCache(0, 0)
	if c.maxItems != 1000 {
		t.Fatalf("expected default 1000 items, got %d", c.maxItems)
	}
	if c.ttl != 300*time.Second {
		t.Fatalf("expected default 300s TTL, got %v", c.ttl)
	}
}

func TestNewCacheUsesProvidedValues(t *testing.T) {
	c := NewCache(10, 60)
	if c.maxItems != 10 {
		t.Fatalf("expected 10 items, got %d", c.maxItems)
	}
	if c.ttl != 60*time.Second {
		t.Fatalf("expected 60s TTL, got %v", c.ttl)
	}
}

func TestPutAndGet(t *testing.T) {
	c := NewCache(1, 60)
	key := c.MakeKey([]byte(`{"model":"gpt-4"}`), "gpt-4", "")
	value := []byte(`{"id":"chatcmpl-123","choices":[]}`)

	c.Put(key, value)

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got) != string(value) {
		t.Fatalf("expected %q, got %q", value, got)
	}
}

func TestGetMiss(t *testing.T) {
	c := NewCache(1, 60)
	_, ok := c.Get("nonexistent")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestGetExpired(t *testing.T) {
	c := NewCache(1, 1) // 1 second TTL
	key := c.MakeKey([]byte("body"), "model", "")
	c.Put(key, []byte("value"))

	// Advance time past TTL.
	originalNow := c.now
	c.now = func() time.Time { return originalNow().Add(2 * time.Second) }
	defer func() { c.now = originalNow }()

	_, ok := c.Get(key)
	if ok {
		t.Fatal("expected expired entry to be a miss")
	}
	// Get() eagerly removes expired entries.
	if c.Len() != 0 {
		t.Fatalf("expected 0 entries after expiry eviction, got %d", c.Len())
	}
}

func TestPutOverwrite(t *testing.T) {
	c := NewCache(1, 60)
	key := "overwrite-key"

	c.Put(key, []byte("first"))
	c.Put(key, []byte("second"))

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got) != "second" {
		t.Fatalf("expected %q, got %q", "second", got)
	}
	if c.Len() != 1 {
		t.Fatalf("expected 1 entry after overwrite, got %d", c.Len())
	}
}

func TestDelete(t *testing.T) {
	c := NewCache(1, 60)
	key := "delete-key"
	c.Put(key, []byte("value"))

	c.Delete(key)

	_, ok := c.Get(key)
	if ok {
		t.Fatal("expected cache miss after delete")
	}
	if c.Len() != 0 {
		t.Fatalf("expected 0 entries after delete, got %d", c.Len())
	}
}

func TestDeleteNonexistent(t *testing.T) {
	c := NewCache(1, 60)
	c.Delete("no-such-key")
	if c.Len() != 0 {
		t.Fatalf("expected 0 entries, got %d", c.Len())
	}
}

func TestLRUEviction(t *testing.T) {
	// Cache with max 4 items
	c := NewCache(4, 60)

	// Fill cache: should hold exactly 4 entries.
	for i := 0; i < 4; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Put(key, []byte("value"))
	}
	if c.Len() != 4 {
		t.Fatalf("expected 4 entries, got %d", c.Len())
	}

	// Adding a 5th should evict the oldest (key-0).
	c.Put("key-4", []byte("value"))
	if c.Len() != 4 {
		t.Fatalf("expected 4 entries after eviction, got %d", c.Len())
	}

	_, ok := c.Get("key-0")
	if ok {
		t.Fatal("expected key-0 to be evicted")
	}
	_, ok = c.Get("key-4")
	if !ok {
		t.Fatal("expected key-4 to be present")
	}
}

func TestLRUPromotion(t *testing.T) {
	// Cache with max 3 items — Get() promotes to front, so eviction is LRU.
	c := NewCache(3, 60)

	c.Put("old", []byte("value1"))
	c.Put("mid", []byte("value2"))
	c.Put("new", []byte("value3"))

	// Accessing "old" promotes it to the front of LRU.
	_, ok := c.Get("old")
	if !ok {
		t.Fatal("expected old to be present")
	}

	// Adding two new entries: the first evicts "mid" (now least recently used),
	// the second evicts "new" (now least recently used).
	// "old" survives because it was promoted by the Get above.
	c.Put("extra1", []byte("v1"))
	c.Put("extra2", []byte("v2"))

	_, ok = c.Get("old")
	if !ok {
		t.Fatal("expected old to survive (promoted by recent access)")
	}
	_, ok = c.Get("mid")
	if ok {
		t.Fatal("expected mid to be evicted (least recently used)")
	}
	_, ok = c.Get("new")
	if ok {
		t.Fatal("expected new to be evicted (least recently used)")
	}
	_, ok = c.Get("extra1")
	if !ok {
		t.Fatal("expected extra1 to survive")
	}
	_, ok = c.Get("extra2")
	if !ok {
		t.Fatal("expected extra2 to survive")
	}
}

func TestCurrentBytes(t *testing.T) {
	c := NewCache(10, 60)

	// Initially empty
	if c.CurrentBytes() != 0 {
		t.Fatalf("expected 0 bytes, got %d", c.CurrentBytes())
	}

	// Add entries
	c.Put("a", []byte("hello"))
	c.Put("b", []byte("world"))

	expected := int64(5 + 5) // "hello" + "world"
	if c.CurrentBytes() != expected {
		t.Fatalf("expected %d bytes, got %d", expected, c.CurrentBytes())
	}

	// Delete one
	c.Delete("a")
	if c.CurrentBytes() != 5 {
		t.Fatalf("expected 5 bytes after delete, got %d", c.CurrentBytes())
	}
}

func TestMaxItems(t *testing.T) {
	c := NewCache(100, 60)
	if c.MaxItems() != 100 {
		t.Fatalf("expected MaxItems 100, got %d", c.MaxItems())
	}
}

func TestTTL(t *testing.T) {
	c := NewCache(10, 300)
	if c.TTL() != 300*time.Second {
		t.Fatalf("expected TTL 300s, got %v", c.TTL())
	}
}

func TestMakeKeyDeterministic(t *testing.T) {
	c := NewCache(1, 60)
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	model := "gpt-4"

	key1 := c.MakeKey(body, model, "")
	key2 := c.MakeKey(body, model, "")
	if key1 != key2 {
		t.Fatal("expected deterministic keys for identical inputs")
	}
}

func TestMakeKeyDiffersOnModel(t *testing.T) {
	c := NewCache(1, 60)
	body := []byte("same-body")

	key1 := c.MakeKey(body, "model-a", "")
	key2 := c.MakeKey(body, "model-b", "")
	if key1 == key2 {
		t.Fatal("expected different keys for different models")
	}
}

func TestMakeKeyDiffersOnBody(t *testing.T) {
	c := NewCache(1, 60)

	key1 := c.MakeKey([]byte("body-a"), "same-model", "")
	key2 := c.MakeKey([]byte("body-b"), "same-model", "")
	if key1 == key2 {
		t.Fatal("expected different keys for different bodies")
	}
}

func TestPutEmptyValueIgnored(t *testing.T) {
	c := NewCache(1, 60)
	c.Put("key", []byte{})
	if c.Len() != 0 {
		t.Fatalf("expected 0 entries for empty value, got %d", c.Len())
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewCache(1, 60)
	done := make(chan struct{})

	// Writer goroutine.
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key-%d", i%20)
			c.Put(key, []byte(fmt.Sprintf("value-%d", i)))
		}
	}()

	// Reader goroutine.
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key-%d", i%20)
			c.Get(key)
		}
	}()

	// Deleter goroutine.
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key-%d", i%20)
			c.Delete(key)
		}
	}()

	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestCloseStopsSweep(t *testing.T) {
	c := NewCache(10, 1)
	c.Close()
	// Second close should be a no-op (idempotent).
	c.Close()
}

func TestStats(t *testing.T) {
	c := NewCache(2, 60)

	// Empty cache get = miss.
	c.Get("nope")
	c.Get("nope")

	c.Put("k1", []byte("v1"))
	c.Get("k1") // hit
	c.Get("k1") // hit

	stats := c.Stats()
	if stats.Hits != 2 {
		t.Fatalf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Fatalf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.Evictions != 0 {
		t.Fatalf("expected 0 evictions, got %d", stats.Evictions)
	}
}

func TestStatsTracksEvictions(t *testing.T) {
	c := NewCache(2, 60)
	c.Put("a", []byte("1"))
	c.Put("b", []byte("2"))
	c.Put("c", []byte("3")) // evicts "a"

	stats := c.Stats()
	if stats.Evictions != 1 {
		t.Fatalf("expected 1 eviction, got %d", stats.Evictions)
	}
}

func TestMaxBytes(t *testing.T) {
	c := NewCacheWithConfig(Config{
		MaxEntries: 100,
		TTLSec:     60,
		MaxBytes:   20,
	})

	if c.MaxBytes() != 20 {
		t.Fatalf("expected MaxBytes 20, got %d", c.MaxBytes())
	}
}

func TestByteLimitEviction(t *testing.T) {
	c := NewCacheWithConfig(Config{
		MaxEntries: 100,
		TTLSec:     60,
		MaxBytes:   15,
	})

	c.Put("a", []byte("12345")) // 5 bytes
	c.Put("b", []byte("12345")) // 5 bytes, total=10
	c.Put("c", []byte("12345")) // 5 bytes, total=15

	if c.CurrentBytes() != 15 {
		t.Fatalf("expected 15 bytes, got %d", c.CurrentBytes())
	}

	// Adding a 10-byte entry should evict oldest until there's room.
	c.Put("d", []byte("1234567890")) // 10 bytes

	if c.CurrentBytes() > 15 {
		t.Fatalf("expected bytes <= 15, got %d", c.CurrentBytes())
	}
	// "d" must be present.
	if _, ok := c.Get("d"); !ok {
		t.Fatal("expected 'd' to be present after eviction")
	}
}

func TestSweepExpired(t *testing.T) {
	// Use a large sweep interval so the background goroutine doesn't interfere.
	c := NewCacheWithConfig(Config{
		MaxEntries:    100,
		TTLSec:        1,
		SweepInterval: 60 * time.Second,
	})

	c.Put("a", []byte("1"))
	c.Put("b", []byte("2"))

	if c.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", c.Len())
	}

	// Advance time past TTL.
	originalNow := c.now
	c.now = func() time.Time { return originalNow().Add(3 * time.Second) }
	defer func() { c.now = originalNow }()

	// Call sweepExpired directly.
	removed := c.sweepExpired()
	if removed != 2 {
		t.Fatalf("expected 2 expired entries removed, got %d", removed)
	}
	if c.Len() != 0 {
		t.Fatalf("expected 0 entries after sweep, got %d", c.Len())
	}
}

func TestSweepExpiredIgnoresFreshEntries(t *testing.T) {
	c := NewCacheWithConfig(Config{
		MaxEntries:    100,
		TTLSec:        60,
		SweepInterval: 60 * time.Second,
	})

	c.Put("fresh", []byte("data"))

	originalNow := c.now
	c.now = func() time.Time { return originalNow().Add(10 * time.Second) }
	defer func() { c.now = originalNow }()

	removed := c.sweepExpired()
	if removed != 0 {
		t.Fatalf("expected 0 expired, got %d", removed)
	}
	if c.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", c.Len())
	}
}

func TestEnsureSizeEvictsBeyondByteLimit(t *testing.T) {
	c := NewCacheWithConfig(Config{
		MaxEntries: 100,
		TTLSec:     60,
		MaxBytes:   10,
	})

	// Insert directly bypassing Put's eviction (use internal state).
	// Actually, Put already handles this, so we test ensureSize indirectly.
	c.Put("a", []byte("hello")) // 5 bytes
	c.Put("b", []byte("world")) // 5 bytes, total=10

	if c.CurrentBytes() != 10 {
		t.Fatalf("expected 10 bytes, got %d", c.CurrentBytes())
	}

	// Put an entry larger than the limit — should evict everything and keep only the new one.
	c.Put("big", []byte("0123456789extra")) // 15 bytes > 10 maxBytes

	if c.CurrentBytes() != 15 {
		t.Fatalf("expected 15 bytes (only big entry), got %d", c.CurrentBytes())
	}
	if _, ok := c.Get("big"); !ok {
		t.Fatal("expected 'big' to be present")
	}
}

func TestMakeKeyLegacy(t *testing.T) {
	c := NewCache(1, 60)

	legacy := c.MakeKeyLegacy([]byte("body"), "model")
	// Legacy key should match MakeKey with empty namespace.
	full := c.MakeKey([]byte("body"), "model", "")
	if legacy != full {
		t.Fatalf("expected MakeKeyLegacy == MakeKey(empty ns), got %q vs %q", legacy, full)
	}
}

func TestMakeKeyWithNamespace(t *testing.T) {
	c := NewCache(1, 60)

	noNs := c.MakeKey([]byte("body"), "model", "")
	withNs := c.MakeKey([]byte("body"), "model", "tenant-1")

	if noNs == withNs {
		t.Fatal("expected different keys when namespace differs")
	}
}

func TestMakeKeyRaw(t *testing.T) {
	k1 := makeKeyRaw([]byte("body"), "model", "")
	k2 := cMakeKeySHA([]byte("body"), "model", "")
	if k1 != k2 {
		t.Fatalf("expected makeKeyRaw to match reference SHA, got %q vs %q", k1, k2)
	}

	kNs := makeKeyRaw([]byte("body"), "model", "ns")
	if kNs == k1 {
		t.Fatal("expected different key with namespace")
	}
}

func cMakeKeySHA(body []byte, model, namespace string) string {
	h := sha256.New()
	h.Write(body)
	h.Write([]byte(model))
	if namespace != "" {
		h.Write([]byte(namespace))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestByteLimitAccounting(t *testing.T) {
	c := NewCacheWithConfig(Config{
		MaxEntries: 100,
		TTLSec:     60,
		MaxBytes:   100,
	})

	c.Put("a", []byte("12345")) // 5
	c.Put("b", []byte("1234567890")) // 10
	if c.CurrentBytes() != 15 {
		t.Fatalf("expected 15 bytes, got %d", c.CurrentBytes())
	}

	// Overwrite "a" with a larger value.
	c.Put("a", []byte("123456789012345")) // 15, old 5 removed
	if c.CurrentBytes() != 25 { // 15+10
		t.Fatalf("expected 25 bytes after overwrite, got %d", c.CurrentBytes())
	}
}

func TestDeleteDecrementsBytes(t *testing.T) {
	c := NewCache(10, 60)
	c.Put("x", []byte("abcdefghij")) // 10 bytes
	c.Put("y", []byte("kl"))         // 2 bytes

	if c.CurrentBytes() != 12 {
		t.Fatalf("expected 12 bytes, got %d", c.CurrentBytes())
	}

	c.Delete("x")
	if c.CurrentBytes() != 2 {
		t.Fatalf("expected 2 bytes after delete, got %d", c.CurrentBytes())
	}

	c.Delete("y")
	if c.CurrentBytes() != 0 {
		t.Fatalf("expected 0 bytes after delete, got %d", c.CurrentBytes())
	}
}

func TestZeroByteCacheEvictsEverything(t *testing.T) {
	// maxBytes=1 effectively means each entry overwrites the entire cache.
	c := NewCacheWithConfig(Config{
		MaxEntries: 100,
		TTLSec:     60,
		MaxBytes:   1,
	})

	c.Put("a", []byte("x")) // 1 byte, fits
	if c.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", c.Len())
	}

	c.Put("b", []byte("y")) // 1 byte, evicts "a"
	if c.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", c.Len())
	}
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected 'a' to be evicted")
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("expected 'b' to be present")
	}
}

func TestSweepWithGetExpiryRemoval(t *testing.T) {
	c := NewCacheWithConfig(Config{
		MaxEntries:    100,
		TTLSec:        1,
		SweepInterval: 60 * time.Second,
		SweepOnGet:    true,
	})

	c.Put("k", []byte("v"))

	originalNow := c.now
	c.now = func() time.Time { return originalNow().Add(3 * time.Second) }
	defer func() { c.now = originalNow }()

	// Get on expired entry triggers eager removal.
	_, ok := c.Get("k")
	if ok {
		t.Fatal("expected expired entry to be a miss")
	}
	if c.Len() != 0 {
		t.Fatalf("expected 0 after Get removes expired entry, got %d", c.Len())
	}
}
