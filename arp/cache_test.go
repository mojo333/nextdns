package arp

import (
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestARPCache_Stop_CancelsGoroutines verifies that Stop() properly
// cancels all spawned goroutines to prevent goroutine leaks.
func TestARPCache_Stop_CancelsGoroutines(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	// Create a new cache
	c := newCache()

	// Trigger goroutine spawn by calling get multiple times
	// This should spawn goroutines for table updates
	for i := 0; i < 10; i++ {
		_ = c.get()
		time.Sleep(50 * time.Millisecond) // Allow time for spawning
	}

	// Stop the cache
	c.Stop()

	// Give goroutines time to exit
	time.Sleep(200 * time.Millisecond)

	// Force GC to clean up any pending goroutines
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()

	// Allow some tolerance for background goroutines
	if finalGoroutines > initialGoroutines+2 {
		t.Errorf("Goroutine leak detected: started with %d, ended with %d (diff: %d)",
			initialGoroutines, finalGoroutines, finalGoroutines-initialGoroutines)
	}
}

// TestARPCache_ContextCancellation verifies that context cancellation
// properly stops goroutines from spawning new update tasks.
func TestARPCache_ContextCancellation(t *testing.T) {
	c := newCache()

	// Call get() a few times to trigger updates
	for i := 0; i < 5; i++ {
		_ = c.get()
		time.Sleep(50 * time.Millisecond)
	}

	goroutinesBeforeStop := runtime.NumGoroutine()

	// Stop the cache (cancels context)
	c.Stop()

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// Try to trigger more updates after stop (shouldn't spawn new goroutines)
	for i := 0; i < 5; i++ {
		_ = c.get()
		time.Sleep(50 * time.Millisecond)
	}

	goroutinesAfterStop := runtime.NumGoroutine()

	// After stop, context should prevent new goroutines
	// There might be slight variance, but shouldn't grow significantly
	if goroutinesAfterStop > goroutinesBeforeStop+2 {
		t.Errorf("New goroutines spawned after stop: before=%d, after=%d",
			goroutinesBeforeStop, goroutinesAfterStop)
	}
}

// TestARPCache_NoGoroutineLeaks tests that multiple Start/Stop cycles
// don't cause goroutine leaks.
func TestARPCache_NoGoroutineLeaks(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	// Run multiple create/use/stop cycles
	for cycle := 0; cycle < 5; cycle++ {
		c := newCache()

		// Use the cache
		for i := 0; i < 3; i++ {
			_ = c.get()
			time.Sleep(50 * time.Millisecond)
		}

		// Stop it
		c.Stop()

		// Wait for cleanup
		time.Sleep(100 * time.Millisecond)
	}

	// Force GC
	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()

	// After 5 cycles, we shouldn't have significant goroutine growth
	if finalGoroutines > initialGoroutines+5 {
		t.Errorf("Goroutine leak across cycles: started with %d, ended with %d (diff: %d)",
			initialGoroutines, finalGoroutines, finalGoroutines-initialGoroutines)
	}
}

// TestARPCache_ConcurrentAccess tests that concurrent get() calls
// are safe and don't cause data races.
// Run with: go test -race
func TestARPCache_ConcurrentAccess(t *testing.T) {
	c := newCache()
	defer c.Stop()

	const numGoroutines = 20
	const numIterations = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				table := c.get()
				// Use the table
				if table != nil {
					_ = table.SearchMAC(net.ParseIP("192.168.1.1"))
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

// TestARPCache_GlobalStop tests the global Stop() function
func TestARPCache_GlobalStop(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	// Use global cache
	for i := 0; i < 5; i++ {
		_ = SearchMAC(net.ParseIP("192.168.1.1"))
		time.Sleep(50 * time.Millisecond)
	}

	// Stop global cache
	Stop()

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()

	// Re-initialize global cache for other tests
	global = newCache()

	if finalGoroutines > initialGoroutines+2 {
		t.Errorf("Global Stop leaked goroutines: started with %d, ended with %d",
			initialGoroutines, finalGoroutines)
	}
}

// TestARPCache_UpdateThrottling verifies that updates are properly
// throttled (30 second interval) and don't spam goroutines.
func TestARPCache_UpdateThrottling(t *testing.T) {
	c := newCache()
	defer c.Stop()

	// Force an immediate update
	_ = c.get()
	time.Sleep(100 * time.Millisecond)

	goroutinesAfterFirst := runtime.NumGoroutine()

	// Call get() multiple times rapidly - should not spawn many goroutines
	// due to 30-second throttle
	for i := 0; i < 10; i++ {
		_ = c.get()
		time.Sleep(10 * time.Millisecond)
	}

	goroutinesAfterRapid := runtime.NumGoroutine()

	// Should not have spawned 10 goroutines
	diff := goroutinesAfterRapid - goroutinesAfterFirst
	if diff > 3 {
		t.Errorf("Update throttling failed: spawned %d extra goroutines for 10 rapid calls", diff)
	}
}

// TestARPCache_StopMultipleTimes verifies that calling Stop() multiple
// times doesn't cause panics or issues.
func TestARPCache_StopMultipleTimes(t *testing.T) {
	c := newCache()

	// Use the cache
	_ = c.get()
	time.Sleep(50 * time.Millisecond)

	// Stop multiple times - should not panic
	c.Stop()
	c.Stop()
	c.Stop()

	// Should still be safe to call get() (though won't update)
	_ = c.get()
}

// TestARPCache_NilContext tests behavior when context is nil
func TestARPCache_NilContext(t *testing.T) {
	c := &cache{
		ctx:    nil,
		cancel: nil,
	}

	// Should not panic even with nil context
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Panicked with nil context: %v", r)
		}
	}()

	_ = c.get()
	c.Stop()
}
