package discovery

import (
	"sync"
	"time"
)

// macTable is a TTL-cached MAC-to-names table shared by the platform
// specific discovery sources (Firewalla, Ubios, Merlin). It is safe for
// concurrent use: refresh re-runs update under the write lock while lookups
// only ever hold the read lock.
type macTable struct {
	mu      sync.RWMutex
	macs    map[string][]string
	expires time.Time
}

// refresh re-runs update and replaces the table content if the cached data
// expired. The expiry is advanced even when update fails so callers retry at
// most once per TTL.
func (t *macTable) refresh(ttl time.Duration, update func() (map[string][]string, error)) error {
	t.mu.RLock()
	expired := !time.Now().Before(t.expires)
	t.mu.RUnlock()
	if !expired {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Now().Before(t.expires) {
		// Another goroutine refreshed while we waited for the write lock.
		return nil
	}
	t.expires = time.Now().Add(ttl)
	macs, err := update()
	if err != nil {
		return err
	}
	t.macs = macs
	return nil
}

func (t *macTable) lookupMAC(mac string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.macs[mac]
}

func (t *macTable) visit(f func(name string, macs []string)) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	m := map[string][]string{}
	for mac, names := range t.macs {
		for _, name := range names {
			m[name] = append(m[name], mac)
		}
	}
	for name, macs := range m {
		f(name, macs)
	}
}
