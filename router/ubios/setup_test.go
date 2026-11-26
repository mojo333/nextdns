package ubios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextdns/nextdns/router/internal"
)

func TestDnsmasqCanStripClientOptions(t *testing.T) {
	tests := []struct {
		output string
		want   bool
	}{
		{"Dnsmasq version 2.90  Copyright (c) 2000-2024 Simon Kelley\n", true},
		{"Dnsmasq version 2.87  Copyright (c) 2000-2022 Simon Kelley\n", true},
		{"Dnsmasq version 3.0  Copyright\n", true},
		{"Dnsmasq version 2.86  Copyright (c) 2000-2021 Simon Kelley\n", false},
		{"Dnsmasq version 2.9  Copyright\n", false},
		{"Dnsmasq version UNKNOWN  Copyright\n", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := canStripClientOptions(tt.output); got != tt.want {
			t.Errorf("canStripClientOptions(%q) = %v, want %v", tt.output, got, tt.want)
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
