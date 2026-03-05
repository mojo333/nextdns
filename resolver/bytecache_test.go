package resolver

import (
	"testing"
	"time"
)

// Compile-time check that ByteCache implements Cacher.
var _ Cacher = (*ByteCache)(nil)

func TestNewByteCache(t *testing.T) {
	c, err := NewByteCache(1024, false)
	if err != nil {
		t.Fatalf("NewByteCache: %v", err)
	}
	defer c.Close()
}

func TestByteCache_GetSet(t *testing.T) {
	c, err := NewByteCache(1<<20, false) // 1MB
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	key := uint64(42)
	val := &cacheValue{
		time: time.Now(),
		msg:  []byte{0x00, 0x01, 0x02, 0x03},
	}

	// Get on empty cache
	if _, ok := c.Get(key); ok {
		t.Error("expected miss on empty cache")
	}

	// Set then Get
	c.Set(key, val)
	c.cache.Wait() // ensure ristretto buffers are flushed

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected hit after Set")
	}
	if got != val {
		t.Error("returned value does not match stored value")
	}
}

func TestByteCache_GetSet_MultipleKeys(t *testing.T) {
	c, err := NewByteCache(1<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	v1 := &cacheValue{time: time.Now(), msg: []byte("dns-resp-1")}
	v2 := &cacheValue{time: time.Now(), msg: []byte("dns-resp-2")}

	c.Set(1, v1)
	c.Set(2, v2)
	c.cache.Wait()

	got1, ok := c.Get(1)
	if !ok || got1 != v1 {
		t.Error("key 1: expected v1")
	}

	got2, ok := c.Get(2)
	if !ok || got2 != v2 {
		t.Error("key 2: expected v2")
	}

	// Non-existent key
	if _, ok := c.Get(999); ok {
		t.Error("expected miss for non-existent key")
	}
}

func TestByteCache_Overwrite(t *testing.T) {
	c, err := NewByteCache(1<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	key := uint64(7)
	v1 := &cacheValue{time: time.Now(), msg: []byte("first")}
	v2 := &cacheValue{time: time.Now(), msg: []byte("second")}

	c.Set(key, v1)
	c.cache.Wait()
	c.Set(key, v2)
	c.cache.Wait()

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected hit after overwrite")
	}
	if got != v2 {
		t.Error("expected latest value after overwrite")
	}
}

func TestByteCache_Metrics_Disabled(t *testing.T) {
	c, err := NewByteCache(1024, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if m := c.Metrics(); m != nil {
		t.Error("expected nil metrics when disabled")
	}
}

func TestByteCache_Metrics_Enabled(t *testing.T) {
	c, err := NewByteCache(1<<20, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	m := c.Metrics()
	if m == nil {
		t.Fatal("expected non-nil metrics when enabled")
	}

	// Miss
	c.Get(123)

	// Hit
	v := &cacheValue{time: time.Now(), msg: []byte("data")}
	c.Set(123, v)
	c.cache.Wait()
	c.Get(123)

	if m.Misses() == 0 {
		t.Error("expected at least 1 miss")
	}
}

func TestByteCache_ZeroLengthMsg(t *testing.T) {
	c, err := NewByteCache(1<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// A cacheValue with empty msg should still be storable (cost = 1).
	v := &cacheValue{time: time.Now(), msg: []byte{}}
	c.Set(1, v)
	c.cache.Wait()

	got, ok := c.Get(1)
	if !ok {
		t.Fatal("expected hit for zero-length msg value")
	}
	if got != v {
		t.Error("returned value does not match")
	}
}
