package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nextdns/nextdns/host"
)

// stubStartup shortens the startup window and records process exits instead of
// ending the test binary.
func stubStartup(t *testing.T) <-chan int {
	t.Helper()
	prevGrace, prevExit := startupGrace, exitProcess
	startupGrace = 20 * time.Millisecond
	exited := make(chan int, 1)
	exitProcess = func(code int) { exited <- code }
	t.Cleanup(func() { startupGrace, exitProcess = prevGrace, prevExit })
	return exited
}

func TestStartServing_ExitsWhenListeningEndsAfterStartup(t *testing.T) {
	exited := stubStartup(t)
	p := &proxySvc{log: host.NewConsoleLogger("test")}
	fail := make(chan struct{})

	if err := p.startServing(func(context.Context) error {
		<-fail
		return errors.New("accept: too many open files")
	}); err != nil {
		t.Fatalf("startServing: %v", err)
	}
	close(fail)

	select {
	case code := <-exited:
		if code == 0 {
			t.Fatal("exit status 0, so the supervisor would not restart the daemon")
		}
	case <-time.After(time.Second):
		t.Fatal("daemon kept running after it stopped listening")
	}
}

func TestStartServing_ReturnsAnErrorDuringStartup(t *testing.T) {
	exited := stubStartup(t)
	startupGrace = time.Second
	p := &proxySvc{log: host.NewConsoleLogger("test")}
	want := errors.New("bind: address already in use")

	if err := p.startServing(func(context.Context) error { return want }); !errors.Is(err, want) {
		t.Fatalf("startServing = %v, want %v", err, want)
	}
	if len(exited) != 0 {
		t.Fatal("exited instead of returning the startup error")
	}
}

func TestStartServing_StopDoesNotExit(t *testing.T) {
	exited := stubStartup(t)
	p := &proxySvc{log: host.NewConsoleLogger("test")}

	// The proxy reports a closed socket, not context.Canceled, when stopped.
	if err := p.startServing(func(ctx context.Context) error {
		<-ctx.Done()
		return errors.New("udp: use of closed network connection")
	}); err != nil {
		t.Fatalf("startServing: %v", err)
	}
	if !p.stop() {
		t.Fatal("stop() = false")
	}

	if len(exited) != 0 {
		t.Fatal("a requested stop exited the process before the router could be restored")
	}
}
