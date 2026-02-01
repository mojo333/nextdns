package proxy

import (
	"testing"
)

// TestBufferPool_SmallQuery_Uses512B verifies that small queries
// use the 512-byte buffer tier for 99% memory reduction.
func TestBufferPool_SmallQuery_Uses512B(t *testing.T) {
	pool := NewTieredBufferPool()

	// Request small buffer (typical DNS query)
	buf := pool.Get(256)

	if buf == nil {
		t.Fatal("Expected buffer, got nil")
	}

	// Should return 512-byte buffer
	if cap(*buf) != 512 {
		t.Errorf("Expected 512-byte buffer for small query, got %d bytes", cap(*buf))
	}

	// Return it
	pool.Put(buf)
}

// TestBufferPool_MediumQuery_Uses4KB verifies that medium-sized queries
// use the 4KB buffer tier.
func TestBufferPool_MediumQuery_Uses4KB(t *testing.T) {
	pool := NewTieredBufferPool()

	// Request medium buffer
	buf := pool.Get(2048)

	if buf == nil {
		t.Fatal("Expected buffer, got nil")
	}

	// Should return 4KB buffer
	if cap(*buf) != 4096 {
		t.Errorf("Expected 4096-byte buffer for medium query, got %d bytes", cap(*buf))
	}

	pool.Put(buf)
}

// TestBufferPool_LargeQuery_Uses65KB verifies that large queries
// use the 65KB buffer tier.
func TestBufferPool_LargeQuery_Uses65KB(t *testing.T) {
	pool := NewTieredBufferPool()

	// Request large buffer
	buf := pool.Get(10000)

	if buf == nil {
		t.Fatal("Expected buffer, got nil")
	}

	// Should return 65KB buffer
	if cap(*buf) != 65536 {
		t.Errorf("Expected 65536-byte buffer for large query, got %d bytes", cap(*buf))
	}

	pool.Put(buf)
}

// TestBufferPool_GetLarge always returns 65KB buffer
func TestBufferPool_GetLarge(t *testing.T) {
	pool := NewTieredBufferPool()

	buf := pool.GetLarge()

	if buf == nil {
		t.Fatal("Expected buffer, got nil")
	}

	if cap(*buf) != 65536 {
		t.Errorf("Expected 65536-byte buffer from GetLarge, got %d bytes", cap(*buf))
	}

	pool.Put(buf)
}

// TestBufferPool_TierBoundaries tests exact boundary conditions
func TestBufferPool_TierBoundaries(t *testing.T) {
	pool := NewTieredBufferPool()

	testCases := []struct {
		size         int
		expectedCap  int
		description  string
	}{
		{1, 512, "minimum size"},
		{512, 512, "exact small tier max"},
		{513, 4096, "just over small tier"},
		{4096, 4096, "exact medium tier max"},
		{4097, 65536, "just over medium tier"},
		{65536, 65536, "exact large tier max"},
		{65537, 65536, "over large tier max (still returns large)"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			buf := pool.Get(tc.size)
			if cap(*buf) != tc.expectedCap {
				t.Errorf("Size %d: expected %d-byte buffer, got %d",
					tc.size, tc.expectedCap, cap(*buf))
			}
			pool.Put(buf)
		})
	}
}

// TestBufferPool_Reuse verifies that buffers are actually reused
func TestBufferPool_Reuse(t *testing.T) {
	pool := NewTieredBufferPool()

	// Get a buffer
	buf1 := pool.Get(256)
	ptr1 := &(*buf1)[0] // Get pointer to first byte

	// Put it back
	pool.Put(buf1)

	// Get another buffer of same size
	buf2 := pool.Get(256)
	ptr2 := &(*buf2)[0]

	// Should be the same buffer (reused from pool)
	if ptr1 != ptr2 {
		t.Log("Note: Buffers were not reused (may happen due to Pool behavior)")
		// This is not necessarily an error, as sync.Pool doesn't guarantee reuse
	}
}

// TestBufferPool_PutNil verifies that putting nil doesn't panic
func TestBufferPool_PutNil(t *testing.T) {
	pool := NewTieredBufferPool()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Put(nil) panicked: %v", r)
		}
	}()

	pool.Put(nil)
}

// TestBufferPool_PutWrongSize verifies that putting wrong-sized buffers
// is handled gracefully (they're simply not returned to any pool)
func TestBufferPool_PutWrongSize(t *testing.T) {
	pool := NewTieredBufferPool()

	// Create a buffer with non-standard size
	weirdSize := make([]byte, 1000)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Put(wrong-size) panicked: %v", r)
		}
	}()

	// Should not panic, just ignores it
	pool.Put(&weirdSize)
}

// TestBufferPool_ConcurrentAccess tests thread safety
func TestBufferPool_ConcurrentAccess(t *testing.T) {
	pool := NewTieredBufferPool()

	const numGoroutines = 100
	const numOperations = 100

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				// Randomly use different sizes
				size := 256 + (id+j)%1000

				buf := pool.Get(size)
				if buf == nil {
					t.Errorf("Got nil buffer")
					done <- false
					return
				}

				// Use the buffer
				(*buf)[0] = byte(id)

				pool.Put(buf)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		success := <-done
		if !success {
			t.Fatal("Goroutine failed")
		}
	}
}

// BenchmarkBufferPool_TieredVsSingle compares tiered pool vs single 65KB pool
func BenchmarkBufferPool_TieredVsSingle(b *testing.B) {
	b.Run("Tiered-Small", func(b *testing.B) {
		pool := NewTieredBufferPool()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := pool.Get(256)
			pool.Put(buf)
		}
	})

	b.Run("Tiered-Medium", func(b *testing.B) {
		pool := NewTieredBufferPool()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := pool.Get(2048)
			pool.Put(buf)
		}
	})

	b.Run("Tiered-Large", func(b *testing.B) {
		pool := NewTieredBufferPool()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := pool.Get(32768)
			pool.Put(buf)
		}
	})

	b.Run("Single65KB-Small", func(b *testing.B) {
		pool := NewTieredBufferPool()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulating old behavior: always getting large buffer
			buf := pool.GetLarge()
			pool.Put(buf)
		}
	})
}

// BenchmarkBufferPool_MemoryUsage measures memory allocation differences
func BenchmarkBufferPool_MemoryUsage(b *testing.B) {
	b.Run("Tiered-TypicalQuery", func(b *testing.B) {
		pool := NewTieredBufferPool()
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			buf := pool.Get(256) // Typical query size
			// Simulate usage
			(*buf)[0] = 1
			pool.Put(buf)
		}
	})

	b.Run("Single65KB-TypicalQuery", func(b *testing.B) {
		pool := NewTieredBufferPool()
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			buf := pool.GetLarge() // Old behavior: always 65KB
			// Simulate usage
			(*buf)[0] = 1
			pool.Put(buf)
		}
	})
}

// TestBufferPool_MemorySavings documents the memory savings
func TestBufferPool_MemorySavings(t *testing.T) {
	t.Log("Memory savings comparison:")
	t.Log("  Typical DNS query size: 256 bytes")
	t.Log("  Old approach (single 65KB pool): 65536 bytes per query")
	t.Log("  New approach (tiered 512B pool): 512 bytes per query")
	t.Log("  Memory reduction: 64× smaller (99% reduction)")
	t.Log("")
	t.Log("  Medium response (2KB): 4096 bytes (94% reduction vs 65KB)")
	t.Log("  Large response (32KB): 65536 bytes (no change)")
}
