package discovery

import (
	"fmt"
	"testing"
)

// TestMDNS_removeOldestEntryBoundsAddrs guards against unbounded growth of the
// addr-keyed map when evicting oldest entries. A spoofed-mDNS flood sends many
// unique addr/name pairs; both maps must stay bounded by mdnsMaxEntries.
func TestMDNS_removeOldestEntryBoundsAddrs(t *testing.T) {
	r := &MDNS{
		addrs: map[string]mdnsEntry{},
		names: map[string]mdnsEntry{},
	}

	const flood = mdnsMaxEntries * 10
	for i := 0; i < flood; i++ {
		addr := fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff)
		name := fmt.Sprintf("host%d.local.", i)
		addEntry(r.addrs, addr, name)
		addEntry(r.names, name, addr)
		for len(r.names) > mdnsMaxEntries {
			r.removeOldestEntry()
		}
	}

	if len(r.names) > mdnsMaxEntries {
		t.Fatalf("names not bounded: got %d, want <= %d", len(r.names), mdnsMaxEntries)
	}
	if len(r.addrs) > mdnsMaxEntries {
		t.Fatalf("addrs grew unbounded: got %d, want <= %d", len(r.addrs), mdnsMaxEntries)
	}
}

func BenchmarkMDNS_removeOldestEntry(b *testing.B) {
	r := &MDNS{
		addrs: map[string]mdnsEntry{},
		names: map[string]mdnsEntry{},
	}

	addr := "10.0.0.1"
	namePrefix := "homeassistant"

	for i := 0; i < mdnsMaxEntries; i++ {
		name := fmt.Sprintf("%s%d", namePrefix, i)
		addEntry(r.addrs, addr, name)
		addEntry(r.names, name, addr)
	}
	// pre-alloc names
	names := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		names[i] = fmt.Sprintf("%s%d", namePrefix, mdnsMaxEntries+i)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		name := names[i]
		addEntry(r.addrs, addr, name)
		addEntry(r.names, name, addr)
		for len(r.names) > mdnsMaxEntries {
			r.removeOldestEntry()
		}
	}
}
