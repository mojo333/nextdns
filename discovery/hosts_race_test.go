package discovery

import (
	"sync"
	"testing"
)

// Every query reaches the hosts source, so a refresh runs while other lookups
// are in progress. Run with -race.
func TestHosts_ConcurrentLookups(t *testing.T) {
	r := &Hosts{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			r.LookupHost("localhost.")
			r.LookupAddr("127.0.0.1")
			r.Visit(func(string, []string) {})
		})
	}
	wg.Wait()
}
