package internal

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func requireShell(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
}

// Output that fits the pipe buffer lets the command exit before it has been read.
func TestRunOutputReturnsAllOutput(t *testing.T) {
	requireShell(t)
	const size = 60000
	for i := 0; i < 50; i++ {
		out, err := RunOutput("sh", "-c", `head -c 60000 /dev/zero | tr '\0' a`)
		if err != nil || len(out) != size {
			t.Fatalf("run %d: got %d bytes, want %d, err=%v", i, len(out), size, err)
		}
	}
}

// Upstream issue #156: a child that outlives the command keeps the pipes open.
func TestRunOutputDoesNotWaitForBackgroundChild(t *testing.T) {
	requireShell(t)
	start := time.Now()
	out, err := RunOutput("sh", "-c", "sleep 5 & echo active")
	if d := time.Since(start); out != "active" || err != nil || d > 3*time.Second {
		t.Errorf("out=%q err=%v after %v", out, err, d)
	}
}

func TestRunOutputKeepsOutputAndExitCodeOnFailure(t *testing.T) {
	requireShell(t)
	const script = "echo inactive; echo reason >&2; exit 3"
	for name, s := range map[string]string{
		"exits alone":            script,
		"leaves child with pipe": "sleep 5 & " + script,
	} {
		t.Run(name, func(t *testing.T) {
			out, err := RunOutput("sh", "-c", s)
			if out != "inactive" || ExitCode(err) != 3 || !strings.Contains(err.Error(), "reason") {
				t.Errorf("out=%q err=%v", out, err)
			}
		})
	}
}
