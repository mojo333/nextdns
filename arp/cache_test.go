package arp

import (
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestARPCache_ConcurrentAccess(t *testing.T) {
	c := &cache{}

	const numGoroutines = 20
	const numIterations = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				if table := c.get(); table != nil {
					_ = table.SearchMAC(net.ParseIP("192.168.1.1"))
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
}

// TestARPCache_UpdateThrottling verifies that refreshes are throttled to
// one per 30 seconds rather than one per lookup.
func TestARPCache_UpdateThrottling(t *testing.T) {
	c := &cache{}
	_ = c.get()
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		_ = c.get()
		time.Sleep(10 * time.Millisecond)
	}

	if diff := runtime.NumGoroutine() - before; diff > 3 {
		t.Errorf("update throttling failed: %d extra goroutines for 10 rapid lookups", diff)
	}
}
