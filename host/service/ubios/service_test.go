package ubios

import (
	"strings"
	"testing"
)

// The router sends its DNS to this daemon, so a crashed daemon must come back.
// systemd never restarts a service unless Restart= is set.
func TestUnitRestartsAFailedDaemon(t *testing.T) {
	for _, line := range []string{"Restart=on-failure", "RestartSec=5"} {
		if !strings.Contains(tmpl, "\n"+line+"\n") {
			t.Errorf("unit template lacks %q", line)
		}
	}
}
