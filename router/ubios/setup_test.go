package ubios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextdns/nextdns/router/internal"
)

// fakeDnsmasq writes a stand-in that accepts only the arguments in script.
func fakeDnsmasq(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dnsmasq")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// UniFi OS builds dnsmasq with the version "UNKNOWN", so support is detected by
// asking dnsmasq to check the options rather than by reading its version.
func TestCanStripClientOptions(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"accepts the options", fakeDnsmasq(t, `[ "$*" = "--test --conf-file=/dev/null --strip-mac --strip-subnet" ]`), true},
		{"rejects an unknown option", fakeDnsmasq(t, `echo "bad command line options" >&2; exit 1`), false},
		{"is not installed", filepath.Join(t.TempDir(), "missing"), false},
	}
	for _, tt := range tests {
		if got := canStripClientOptions(tt.path); got != tt.want {
			t.Errorf("dnsmasq %s: canStripClientOptions = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDnsmasqConfig_StripsClientOptionsOnlyWhenSupported(t *testing.T) {
	// An option dnsmasq does not know stops it starting, which takes the whole
	// network's DNS with it.
	for _, supported := range []bool{true, false} {
		path := filepath.Join(t.TempDir(), "nextdns.conf")
		r := &Router{ListenPort: "5342", ClientReporting: true, StripClientOptions: supported}
		if err := internal.WriteTemplate(path, tmpl, r, 0644); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		conf := "\n" + string(b)
		for _, option := range []string{"strip-mac", "strip-subnet"} {
			if got := strings.Contains(conf, "\n"+option+"\n"); got != supported {
				t.Errorf("supported=%v: %q present = %v\n%s", supported, option, got, conf)
			}
		}
		for _, line := range []string{"add-mac", "add-subnet=32,128", "max-cache-ttl=0"} {
			if !strings.Contains(conf, "\n"+line+"\n") {
				t.Errorf("supported=%v: %q missing\n%s", supported, line, conf)
			}
		}
	}
}
