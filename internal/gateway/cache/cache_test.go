package cache

import (
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
