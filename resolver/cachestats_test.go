package resolver

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestDNS_CacheStatsConcurrentWithResolve reads the counters while they are
// incremented the way Resolve does, so `go test -race` catches a plain read.
func TestDNS_CacheStatsConcurrentWithResolve(t *testing.T) {
	// Arrange
	r := &DNS{}
	const increments = 1000
	var wg sync.WaitGroup
	wg.Add(2)

	// Act
	go func() {
		defer wg.Done()
		for i := 0; i < increments; i++ {
			atomic.AddUint32(&r.cacheStats.Hit, 1)
			atomic.AddUint32(&r.cacheStats.Miss, 1)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < increments; i++ {
			_ = r.CacheStats()
		}
	}()
	wg.Wait()

	// Assert
	if got := r.CacheStats(); got.Hit != increments || got.Miss != increments {
		t.Fatalf("CacheStats() = %+v, want %d hits and misses", got, increments)
	}
}
