package discovery

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func openFiles(t *testing.T) int {
	t.Helper()
	d, err := os.Open("/dev/fd")
	if err != nil {
		t.Skipf("cannot list open files: %v", err)
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		t.Skipf("cannot list open files: %v", err)
	}
	return len(names)
}

func TestDHCP_ClosesTheLeaseFile(t *testing.T) {
	// The garbage collector would eventually close a leaked file and hide it.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	file := filepath.Join(t.TempDir(), "dnsmasq.leases")
	r := &DHCP{}
	before := openFiles(t)

	for i := 1; i <= 20; i++ {
		// A different size each time forces the file to be read again.
		lease := "1 00:00:5e:00:53:01 192.0.2.1 host" + strings.Repeat("x", i) + " *\n"
		if err := os.WriteFile(file, []byte(lease), 0644); err != nil {
			t.Fatal(err)
		}
		if err := r.readLeaseLocked(file, "dnsmasq"); err != nil {
			t.Fatal(err)
		}
	}

	if after := openFiles(t); after > before+2 {
		t.Fatalf("open files grew from %d to %d over 20 reads", before, after)
	}
}
