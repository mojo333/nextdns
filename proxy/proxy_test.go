package proxy

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

func TestProxy_maxInflightRequests(t *testing.T) {
	tests := []struct {
		name string
		p    Proxy
		want int
	}{
		{
			name: "default when unset",
			p:    Proxy{},
			want: defaultMaxInflightRequests,
		},
		{
			name: "configured limit",
			p:    Proxy{MaxInflightRequests: 123},
			want: 123,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.maxInflightRequests(); got != tt.want {
				t.Fatalf("maxInflightRequests() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestProxy_requestTimeout(t *testing.T) {
	tests := []struct {
		name string
		p    Proxy
		want time.Duration
	}{
		{
			name: "default when unset",
			p:    Proxy{},
			want: defaultRequestTimeout,
		},
		{
			name: "default when negative",
			p:    Proxy{Timeout: -time.Second},
			want: defaultRequestTimeout,
		},
		{
			name: "configured timeout",
			p:    Proxy{Timeout: 3 * time.Second},
			want: 3 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.requestTimeout(); got != tt.want {
				t.Fatalf("requestTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

// testDNSQueryExampleCom is a minimal valid A query for example.com.
var testDNSQueryExampleCom = []byte{
	0x00, 0x1e, // ID
	0x01, 0x00, // Flags (standard query)
	0x00, 0x01, // Questions: 1
	0x00, 0x00, // Answer RRs: 0
	0x00, 0x00, // Authority RRs: 0
	0x00, 0x00, // Additional RRs: 0
	0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, // "example"
	0x03, 0x63, 0x6f, 0x6d, 0x00, // "com"
	0x00, 0x01, // Type A
	0x00, 0x01, // Class IN
}

// TestListenAndServe_WaitsForInflightOnShutdown verifies two shutdown
// guarantees: cancelling the serve context cancels the per-request contexts
// (so handlers blocked on a hung upstream unblock immediately), and
// ListenAndServe does not return while request handlers are still running,
// so callers can safely tear down the upstream resolver afterwards.
func TestListenAndServe_WaitsForInflightOnShutdown(t *testing.T) {
	// Arrange: reserve a local UDP/TCP port for the proxy to bind.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := pc.LocalAddr().String()
	pc.Close()

	var resolveStartedOnce sync.Once
	resolveStarted := make(chan struct{})
	var resolverDone int32
	blockingResolver := &mockSlowResolver{
		resolveFunc: func(ctx context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
			resolveStartedOnce.Do(func() { close(resolveStarted) })
			<-ctx.Done()
			atomic.StoreInt32(&resolverDone, 1)
			return 12, resolver.ResolveInfo{}, nil
		},
	}

	listening := make(chan struct{}, 2)
	p := Proxy{
		Addrs:    []string{addr},
		Upstream: blockingResolver,
		Timeout:  5 * time.Second,
		InfoLog:  func(string) { listening <- struct{}{} },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- p.ListenAndServe(ctx) }()
	for i := 0; i < 2; i++ {
		select {
		case <-listening:
		case <-time.After(2 * time.Second):
			t.Fatal("proxy did not start listening")
		}
	}

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write(testDNSQueryExampleCom); err != nil {
		t.Fatalf("send query: %v", err)
	}
	select {
	case <-resolveStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("query never reached the resolver")
	}

	// Act
	cancel()

	// Assert
	select {
	case <-served:
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServe did not return after cancel")
	}
	if atomic.LoadInt32(&resolverDone) != 1 {
		t.Error("ListenAndServe returned while a request handler was still running")
	}
}
