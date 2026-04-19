package cache

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewCacheAppliesDefaults(t *testing.T) {
	c := NewCache(0, 0)
	if c.maxBytes != 64*1024*1024 {
		t.Fatalf("expected default 64 MB, got %d", c.maxBytes)
	}
	if c.ttl != 300*time.Second {
		t.Fatalf("expected default 300s TTL, got %v", c.ttl)
	}
}

func TestNewCacheUsesProvidedValues(t *testing.T) {
	c := NewCache(10, 60)
	if c.maxBytes != 10*1024*1024 {
		t.Fatalf("expected 10 MB, got %d", c.maxBytes)
	}
	if c.ttl != 60*time.Second {
		t.Fatalf("expected 60s TTL, got %v", c.ttl)
	}
}

func TestPutAndGet(t *testing.T) {
	c := NewCache(1, 60)
	key := c.MakeKey([]byte(`{"model":"gpt-4"}`), "gpt-4")
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
	key := c.MakeKey([]byte("body"), "model")
	c.Put(key, []byte("value"))

	// Advance time past TTL.
	originalNow := c.now
	c.now = func() time.Time { return originalNow().Add(2 * time.Second) }
	defer func() { c.now = originalNow }()

	_, ok := c.Get(key)
	if ok {
		t.Fatal("expected expired entry to be a miss")
	}
	if c.Len() != 0 {
		t.Fatalf("expected 0 entries after expiry, got %d", c.Len())
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
	// 1 MB cache, each entry is 256 KB.
	c := NewCache(1, 60)
	entrySize := 256 * 1024

	// Fill cache: should hold exactly 4 entries.
	for i := 0; i < 4; i++ {
		key := fmt.Sprintf("key-%d", i)
		c.Put(key, make([]byte, entrySize))
	}
	if c.Len() != 4 {
		t.Fatalf("expected 4 entries, got %d", c.Len())
	}

	// Adding a 5th should evict the oldest (key-0).
	c.Put("key-4", make([]byte, entrySize))
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

func TestLRUPromotionOnGet(t *testing.T) {
	// 1 MB cache, each entry is 256 KB (max 4 entries).
	c := NewCache(1, 60)
	entrySize := 256 * 1024

	c.Put("old", make([]byte, entrySize))
	c.Put("mid", make([]byte, entrySize))
	c.Put("new", make([]byte, entrySize))

	// Accessing "old" promotes it to the front, making "mid" the LRU.
	_, ok := c.Get("old")
	if !ok {
		t.Fatal("expected old to be present")
	}

	// Adding two new entries: the first fits (total = 4), the second must
	// evict the LRU entry, which is "mid" after "old" was promoted.
	c.Put("extra1", make([]byte, entrySize))
	c.Put("extra2", make([]byte, entrySize))

	_, ok = c.Get("mid")
	if ok {
		t.Fatal("expected mid to be evicted")
	}
	_, ok = c.Get("old")
	if !ok {
		t.Fatal("expected old to survive after promotion")
	}
}

func TestSizeTracking(t *testing.T) {
	c := NewCache(1, 60)
	val := []byte(strings.Repeat("x", 1024))
	c.Put("a", val)
	c.Put("b", val)

	expected := int64(2 * 1024)
	if c.SizeBytes() != expected {
		t.Fatalf("expected %d bytes, got %d", expected, c.SizeBytes())
	}

	c.Delete("a")
	if c.SizeBytes() != 1024 {
		t.Fatalf("expected 1024 bytes after delete, got %d", c.SizeBytes())
	}
}

func TestMakeKeyDeterministic(t *testing.T) {
	c := NewCache(1, 60)
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	model := "gpt-4"

	key1 := c.MakeKey(body, model)
	key2 := c.MakeKey(body, model)
	if key1 != key2 {
		t.Fatal("expected deterministic keys for identical inputs")
	}
}

func TestMakeKeyDiffersOnModel(t *testing.T) {
	c := NewCache(1, 60)
	body := []byte("same-body")

	key1 := c.MakeKey(body, "model-a")
	key2 := c.MakeKey(body, "model-b")
	if key1 == key2 {
		t.Fatal("expected different keys for different models")
	}
}

func TestMakeKeyDiffersOnBody(t *testing.T) {
	c := NewCache(1, 60)

	key1 := c.MakeKey([]byte("body-a"), "same-model")
	key2 := c.MakeKey([]byte("body-b"), "same-model")
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
