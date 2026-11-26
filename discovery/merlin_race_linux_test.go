package discovery

import (
	"sync"
	"testing"
)

// Lookups refresh the client list once it expires, so concurrent lookups must
// not do that under a read lock. Run with -race.
func TestMerlin_ConcurrentLookups(t *testing.T) {
	r := &Merlin{}
	r.once.Do(func() { r.supported = true })
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			r.LookupMAC("00:00:5e:00:53:01")
			r.Visit(func(string, []string) {})
		})
	}
	wg.Wait()
}
