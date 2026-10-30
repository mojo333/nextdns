package proxy

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

var exampleQuery = []byte{
	0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm', 0x00,
	0x00, 0x01, 0x00, 0x01,
}

// fullInflight returns an in-flight channel with every slot already taken,
// as when slow upstream queries hold them all.
func fullInflight() chan struct{} {
	c := make(chan struct{}, 1)
	c <- struct{}{}
	return c
}

func TestServeUDP_AnswersServfailWhenNoSlotIsFree(t *testing.T) {
	// Arrange
	l, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var inflight sync.WaitGroup
	go func() { _ = (Proxy{}).serveUDP(ctx, l, fullInflight(), &inflight) }()

	client, err := net.Dial("udp4", l.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Act
	if _, err := client.Write(exampleQuery); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	reply := make([]byte, 512)
	n, err := client.Read(reply)

	// Assert: the query is refused at once rather than left waiting for a slot.
	if err != nil {
		t.Fatalf("no reply while every in-flight slot was taken: %v", err)
	}
	if n < 4 || binary.BigEndian.Uint16(reply) != 0x1234 || reply[3]&0x0f != 2 {
		t.Fatalf("reply = %x, want SERVFAIL for ID 1234", reply[:n])
	}
}

func TestServeTCPConn_ReturnsPromptlyWhenContextCancelled(t *testing.T) {
	// The shutdown watcher sets an immediate read deadline, but the read loop
	// sets its own idle deadline each iteration and could overwrite it, leaving
	// shutdown to wait for the full idle timeout.
	for i := 0; i < 20; i++ {
		server, client := net.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		errC := make(chan error, 1)
		go func() {
			errC <- (Proxy{}).serveTCPConn(ctx, server, make(chan struct{}, 1), NewBufferPool())
		}()
		select {
		case <-errC:
		case <-time.After(time.Second):
			client.Close()
			t.Fatalf("run %d: serveTCPConn still reading 1s after its context was cancelled", i)
		}
		client.Close()
	}
}

// startTCPConn serves one TCP connection and, at the end of the test, closes
// the client and waits for the handler to return. A handler left running
// would race with tests that change tcpClientReadTimeout.
func startTCPConn(t *testing.T, p Proxy, inflightRequests chan struct{}) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = p.serveTCPConn(context.Background(), server, inflightRequests, NewBufferPool())
	}()
	t.Cleanup(func() {
		client.Close()
		<-done
	})
	return client
}

func TestServeTCPConn_AnswersServfailWhenNoSlotIsFree(t *testing.T) {
	// Arrange
	client := startTCPConn(t, Proxy{}, fullInflight())

	// Act
	frame := binary.BigEndian.AppendUint16(nil, uint16(len(exampleQuery)))
	go func() { _, _ = client.Write(append(frame, exampleQuery...)) }()
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	var length uint16
	err := binary.Read(client, binary.BigEndian, &length)

	// Assert: the query is answered at once, as on UDP, rather than blocking.
	if err != nil {
		t.Fatalf("no reply while every in-flight slot was taken: %v", err)
	}
	reply := make([]byte, length)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(reply) != 0x1234 || reply[3]&0x0f != 2 {
		t.Fatalf("reply = %x, want SERVFAIL for ID 1234", reply)
	}
}

func TestServeTCPConn_IdleConnectionHoldsNoSlot(t *testing.T) {
	// Arrange: a client that connects and sends nothing.
	inflightRequests := make(chan struct{}, 1)
	startTCPConn(t, Proxy{}, inflightRequests)

	// Act
	time.Sleep(100 * time.Millisecond)

	// Assert: otherwise a few idle connections could hold every slot for the
	// whole idle timeout and turn all UDP queries into SERVFAIL.
	if n := len(inflightRequests); n != 0 {
		t.Fatalf("idle connection holds %d in-flight slot(s), want 0", n)
	}
}

func TestServeTCPConn_ClientThatNeverReadsReleasesItsSlot(t *testing.T) {
	// Arrange: replies are written with a deadline, so a client that sends a
	// query and never reads cannot keep the reply goroutine, and its in-flight
	// slot, forever. Enough such queries would otherwise hold every slot.
	prev := tcpWriteTimeout
	tcpWriteTimeout = 50 * time.Millisecond
	t.Cleanup(func() { tcpWriteTimeout = prev })
	inflightRequests := make(chan struct{}, 1)
	p := Proxy{Upstream: &mockSlowResolver{}}
	client := startTCPConn(t, p, inflightRequests)

	// Act
	frame := binary.BigEndian.AppendUint16(nil, uint16(len(exampleQuery)))
	if _, err := client.Write(append(frame, exampleQuery...)); err != nil {
		t.Fatal(err)
	}

	// Assert
	deadline := time.Now().Add(2 * time.Second)
	for len(inflightRequests) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("in-flight slot still held 2s after the client stopped reading")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeTCPConn_ClosesConnectionWhenClientStopsReading(t *testing.T) {
	// Arrange: a client that keeps sending queries but never reads. Each new
	// reply used to push the connection's shared write deadline back, so no
	// write ever timed out and every blocked reply kept its in-flight slot.
	prev := tcpWriteTimeout
	tcpWriteTimeout = 50 * time.Millisecond
	t.Cleanup(func() { tcpWriteTimeout = prev })
	client := startTCPConn(t, Proxy{Upstream: &mockSlowResolver{}}, make(chan struct{}, 1000))
	frame := append(binary.BigEndian.AppendUint16(nil, uint16(len(exampleQuery))), exampleQuery...)

	// Act and Assert: the server gives up on the connection, so sends fail.
	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = client.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		if _, err := client.Write(frame); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("connection still open 2s after the client stopped reading replies")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeTCPConn_ReleasesSlotsWhileRepliesWaitToBeWritten(t *testing.T) {
	// Arrange: a client that pipelines queries and does not read yet. Slots
	// bound upstream work, so a reply waiting for a slow reader must not hold
	// one, or a trickle-reading client could keep many slots for minutes.
	inflightRequests := make(chan struct{}, 100)
	client := startTCPConn(t, Proxy{Upstream: &mockSlowResolver{}}, inflightRequests)
	frame := append(binary.BigEndian.AppendUint16(nil, uint16(len(exampleQuery))), exampleQuery...)

	// Act
	for i := 0; i < 10; i++ {
		if _, err := client.Write(frame); err != nil {
			t.Fatal(err)
		}
	}

	// Assert
	deadline := time.Now().Add(2 * time.Second)
	for len(inflightRequests) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d in-flight slots held by replies waiting to be written", len(inflightRequests))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeTCPConn_CapsPipelinedQueriesPerConnection(t *testing.T) {
	// Arrange: a client that pipelines far more queries than the cap and never
	// reads. Without a cap each would park a goroutine and two 64KB buffers.
	var resolved atomic.Int32
	p := Proxy{Upstream: &mockSlowResolver{
		resolveFunc: func(ctx context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
			resolved.Add(1)
			return 12, resolver.ResolveInfo{}, nil
		},
	}}
	client := startTCPConn(t, p, make(chan struct{}, 1000))
	frame := append(binary.BigEndian.AppendUint16(nil, uint16(len(exampleQuery))), exampleQuery...)

	// Act: writes block once the server stops reading this connection.
	go func() {
		for i := 0; i < 10*maxTCPPipelinedQueries; i++ {
			if _, err := client.Write(frame); err != nil {
				return
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)

	// Assert
	if got := resolved.Load(); got > maxTCPPipelinedQueries {
		t.Fatalf("%d queries accepted from a client that never reads, want at most %d", got, maxTCPPipelinedQueries)
	}
}
