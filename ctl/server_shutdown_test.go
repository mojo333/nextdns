package ctl

import (
	"net"
	"testing"
	"time"
)

func TestServer_AcceptLoopEndsWhenListenerCloses(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	done := make(chan struct{})
	go func() {
		(&Server{}).run(l)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("accept loop still running on a closed listener")
	}
}

func TestServer_SlowReaderDoesNotBlockOtherCalls(t *testing.T) {
	// A pipe has no buffer, so the reply write blocks until the peer reads.
	conn, peer := net.Pipe()
	defer peer.Close()
	defer conn.Close()
	s := &Server{}
	s.Command("ping", func(any) any { return "pong" })
	go s.handle(conn, Event{Name: "ping"})
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		s.Command("other", func(any) any { return nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server lock held while writing to a client that is not reading")
	}
}
