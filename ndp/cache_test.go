package ndp

import (
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNDPCache_ConcurrentAccess(t *testing.T) {
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
					_ = table.SearchMAC(net.ParseIP("fe80::1"))
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
}

// TestNDPCache_UpdateThrottling verifies that refreshes are throttled to
// one per 30 seconds rather than one per lookup.
func TestNDPCache_UpdateThrottling(t *testing.T) {
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

func TestNDPCache_IPv6Addresses(t *testing.T) {
	c := &cache{}
	for _, s := range []string{"fe80::1", "2001:db8::1", "::1", "ff02::1"} {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("failed to parse IP: %s", s)
		}
		if table := c.get(); table != nil {
			_ = table.SearchMAC(ip)
		}
	}
}
