package proxy

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

// mockResolver implements a slow resolver for testing race conditions
type mockSlowResolver struct {
	delay       time.Duration
	resolveFunc func(ctx context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error)
}

func (m *mockSlowResolver) Resolve(ctx context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, q, buf)
	}
	// Return a simple NXDOMAIN response
	return 12, resolver.ResolveInfo{}, nil
}

// TestTCPConn_WaitsForInflightQueries verifies that serveTCPConn waits
// for all in-flight query goroutines to complete before closing the connection.
// This prevents "use of closed network connection" errors.
func TestTCPConn_WaitsForInflightQueries(t *testing.T) {
	// Create a listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	var writeAttempts int32
	var writeErrors int32

	// Create a slow resolver that delays responses
	slowResolver := &mockSlowResolver{
		delay: 200 * time.Millisecond,
		resolveFunc: func(ctx context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
			atomic.AddInt32(&writeAttempts, 1)
			return 12, resolver.ResolveInfo{}, nil
		},
	}

	p := Proxy{
		Upstream: slowResolver,
		Timeout:  5 * time.Second,
		ErrorLog: func(err error) {
			// Count write errors
			if err != nil && err.Error() == "use of closed network connection" {
				atomic.AddInt32(&writeErrors, 1)
			}
		},
	}

	// Start server goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		inflightRequests := make(chan struct{}, 10)
		bpool := NewTieredBufferPool()
		_ = p.serveTCPConn(conn, inflightRequests, bpool)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Connect as client
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Send a DNS query (minimal valid query for A record)
	query := []byte{
		0x00, 0x1e, // ID
		0x01, 0x00, // Flags (standard query)
		0x00, 0x01, // Questions: 1
		0x00, 0x00, // Answer RRs: 0
		0x00, 0x00, // Authority RRs: 0
		0x00, 0x00, // Additional RRs: 0
		// Query: example.com A
		0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65,
		0x03, 0x63, 0x6f, 0x6d, 0x00,
		0x00, 0x01, // Type A
		0x00, 0x01, // Class IN
	}

	// Write query length and query
	if err := binary.Write(conn, binary.BigEndian, uint16(len(query))); err != nil {
		t.Fatalf("Failed to write query length: %v", err)
	}
	if _, err := conn.Write(query); err != nil {
		t.Fatalf("Failed to write query: %v", err)
	}

	// Immediately close the connection (simulating client disconnect)
	// This should trigger the read loop to exit
	time.Sleep(10 * time.Millisecond)
	conn.Close()

	// Wait for server to finish
	wg.Wait()

	// Verify that the resolver attempted to process the query
	if atomic.LoadInt32(&writeAttempts) == 0 {
		t.Error("Expected resolver to be called, but it wasn't")
	}

	// The key test: we should NOT see "use of closed network connection" errors
	// because serveTCPConn waits for the WaitGroup before closing
	if atomic.LoadInt32(&writeErrors) > 0 {
		t.Errorf("Got %d 'use of closed network connection' errors - the fix didn't work!",
			atomic.LoadInt32(&writeErrors))
	}
}

// TestTCPConn_MultipleInflightQueries tests multiple concurrent queries
// to ensure the WaitGroup properly tracks all of them.
func TestTCPConn_MultipleInflightQueries(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	var resolveCount int32

	slowResolver := &mockSlowResolver{
		delay: 100 * time.Millisecond,
		resolveFunc: func(ctx context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
			atomic.AddInt32(&resolveCount, 1)
			return 12, resolver.ResolveInfo{}, nil
		},
	}

	p := Proxy{
		Upstream:            slowResolver,
		Timeout:             5 * time.Second,
		MaxInflightRequests: 10,
	}

	// Start server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		inflightRequests := make(chan struct{}, 10)
		bpool := NewTieredBufferPool()
		_ = p.serveTCPConn(conn, inflightRequests, bpool)
	}()

	time.Sleep(50 * time.Millisecond)

	// Connect and send multiple queries
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	query := []byte{
		0x00, 0x1e, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65,
		0x03, 0x63, 0x6f, 0x6d, 0x00,
		0x00, 0x01, 0x00, 0x01,
	}

	// Send 3 queries rapidly
	for i := 0; i < 3; i++ {
		if err := binary.Write(conn, binary.BigEndian, uint16(len(query))); err != nil {
			t.Fatalf("Failed to write query length: %v", err)
		}
		if _, err := conn.Write(query); err != nil {
			t.Fatalf("Failed to write query: %v", err)
		}
	}

	// Close immediately after sending
	time.Sleep(10 * time.Millisecond)
	conn.Close()

	// Wait for server to finish
	wg.Wait()

	// All 3 queries should have been processed
	count := atomic.LoadInt32(&resolveCount)
	if count != 3 {
		t.Errorf("Expected 3 queries to be resolved, got %d", count)
	}
}

// TestTCPConn_GracefulShutdown verifies that when the read loop exits,
// it waits for all query goroutines to finish processing.
func TestTCPConn_GracefulShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	var processingStarted int32
	var processingCompleted int32

	slowResolver := &mockSlowResolver{
		resolveFunc: func(ctx context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
			atomic.AddInt32(&processingStarted, 1)
			time.Sleep(300 * time.Millisecond) // Simulate slow query
			atomic.AddInt32(&processingCompleted, 1)
			return 12, resolver.ResolveInfo{}, nil
		},
	}

	p := Proxy{
		Upstream: slowResolver,
		Timeout:  5 * time.Second,
	}

	serverDone := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			close(serverDone)
			return
		}
		inflightRequests := make(chan struct{}, 10)
		bpool := NewTieredBufferPool()
		_ = p.serveTCPConn(conn, inflightRequests, bpool)
		close(serverDone)
	}()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	query := []byte{
		0x00, 0x1e, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65,
		0x03, 0x63, 0x6f, 0x6d, 0x00,
		0x00, 0x01, 0x00, 0x01,
	}

	// Send query
	if err := binary.Write(conn, binary.BigEndian, uint16(len(query))); err != nil {
		t.Fatalf("Failed to write query length: %v", err)
	}
	if _, err := conn.Write(query); err != nil {
		t.Fatalf("Failed to write query: %v", err)
	}

	// Wait for processing to start
	time.Sleep(50 * time.Millisecond)

	// Close connection while query is being processed
	conn.Close()

	// Wait for server to finish with timeout
	select {
	case <-serverDone:
		// Server finished
	case <-time.After(2 * time.Second):
		t.Fatal("Server didn't finish within timeout - possible deadlock")
	}

	// Verify query processing completed
	if atomic.LoadInt32(&processingStarted) != 1 {
		t.Errorf("Expected 1 query to start processing, got %d", atomic.LoadInt32(&processingStarted))
	}
	if atomic.LoadInt32(&processingCompleted) != 1 {
		t.Errorf("Expected 1 query to complete processing, got %d", atomic.LoadInt32(&processingCompleted))
	}
}
