package resolver

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/nextdns/nextdns/internal/testutil"
	"github.com/nextdns/nextdns/resolver/query"
	"golang.org/x/net/dns/dnsmessage"
)

func TestDNS53_Resolve_Success(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	server, err := testutil.NewMockDNSServer(testutil.SimpleDNSHandler(ip))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	r := DNS53{}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	n, info, err := r.resolve(ctx, q, buf, server.Addr)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if n == 0 {
		t.Fatal("expected non-zero response")
	}

	if info.Transport != "UDP" {
		t.Errorf("expected transport UDP, got %s", info.Transport)
	}

	if info.FromCache {
		t.Error("expected not from cache")
	}
}

func TestDNS53_Resolve_WithCache(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	counter := testutil.NewCountingHandler(testutil.SimpleDNSHandler(ip))
	server, err := testutil.NewMockDNSServer(counter.Handle)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	cache := newTestCache()
	r := DNS53{Cache: cache}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)
	ctx := context.Background()

	// First query - should hit server
	n1, info1, err := r.resolve(ctx, q, buf, server.Addr)
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	if info1.FromCache {
		t.Error("first query should not be from cache")
	}

	// Second query - should hit cache
	n2, info2, err := r.resolve(ctx, q, buf, server.Addr)
	if err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if !info2.FromCache {
		t.Error("second query should be from cache")
	}

	if n1 != n2 {
		t.Errorf("response size mismatch: n1=%d, n2=%d", n1, n2)
	}

	// Verify only one query reached the server
	if counter.Count() != 1 {
		t.Errorf("expected 1 server query, got %d", counter.Count())
	}
}

func TestDNS53_Resolve_CacheExpired(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	counter := testutil.NewCountingHandler(testutil.SimpleDNSHandler(ip))
	server, err := testutil.NewMockDNSServer(counter.Handle)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	cache := newTestCache()
	r := DNS53{
		Cache:       cache,
		CacheMaxAge: 1, // 1 second max age
	}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)
	ctx := context.Background()

	// First query
	_, info1, err := r.resolve(ctx, q, buf, server.Addr)
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	if info1.FromCache {
		t.Error("first query should not be from cache")
	}

	// Wait for cache to expire
	time.Sleep(2 * time.Second)

	// Second query - cache expired, should hit server again
	_, info2, err := r.resolve(ctx, q, buf, server.Addr)
	if err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if info2.FromCache {
		t.Error("second query should not be from cache (expired)")
	}

	// Verify two queries reached the server
	if counter.Count() != 2 {
		t.Errorf("expected 2 server queries, got %d", counter.Count())
	}
}

func TestDNS53_Resolve_MaxTTL(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	server, err := testutil.NewMockDNSServer(testutil.SimpleDNSHandler(ip))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	r := DNS53{
		MaxTTL: 60, // 60 seconds max TTL
	}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)
	ctx := context.Background()

	n, _, err := r.resolve(ctx, q, buf, server.Addr)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	// Parse response and verify TTL was capped
	var p dnsmessage.Parser
	_, err = p.Start(buf[:n])
	if err != nil {
		t.Fatalf("parse response failed: %v", err)
	}

	// Skip questions
	if err := p.SkipAllQuestions(); err != nil {
		t.Fatalf("skip questions failed: %v", err)
	}

	// Check answer TTL
	answer, err := p.Answer()
	if err != nil {
		t.Fatalf("parse answer failed: %v", err)
	}

	if answer.Header.TTL > r.MaxTTL {
		t.Errorf("expected TTL <= %d, got %d", r.MaxTTL, answer.Header.TTL)
	}
}

func TestDNS53_Resolve_Timeout(t *testing.T) {
	// Create server that never responds
	server, err := testutil.NewMockDNSServer(func(query []byte) []byte {
		return nil // No response
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	r := DNS53{}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, err = r.resolve(ctx, q, buf, server.Addr)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestDNS53_Resolve_IDMismatch(t *testing.T) {
	// Custom UDP server that sends 2 wrong-ID responses then 1 correct,
	// simulating stale responses from previous queries on the same socket.
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().String()

	go func() {
		buf := make([]byte, 4096)
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil || n < 12 {
			return
		}
		var p dnsmessage.Parser
		h, err := p.Start(buf[:n])
		if err != nil {
			return
		}
		q, err := p.Question()
		if err != nil {
			return
		}
		// Send 2 responses with wrong ID (simulating stale responses)
		for i := 0; i < 2; i++ {
			resp, _ := testutil.NewTestResponse(h.ID+uint16(i+1), q.Name.String(),
				net.ParseIP("1.2.3.4"), 300)
			conn.WriteToUDP(resp, clientAddr)
		}
		// Send correct response
		resp, _ := testutil.NewTestResponse(h.ID, q.Name.String(),
			net.ParseIP("1.2.3.4"), 300)
		conn.WriteToUDP(resp, clientAddr)
	}()

	r := DNS53{}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	n, _, err := r.resolve(ctx, q, buf, addr)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if n == 0 {
		t.Error("expected successful response after ID mismatches")
	}
}

func TestDNS53_Resolve_IDMismatchMaxRetries(t *testing.T) {
	// Custom UDP server that sends 6 wrong-ID responses to a single query.
	// maxRetries in DNS53.resolve is 5, so this should trigger the limit.
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().String()

	go func() {
		buf := make([]byte, 4096)
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil || n < 12 {
			return
		}
		var p dnsmessage.Parser
		h, err := p.Start(buf[:n])
		if err != nil {
			return
		}
		q, err := p.Question()
		if err != nil {
			return
		}
		// Send 6 responses with wrong IDs
		for i := 0; i < 6; i++ {
			resp, _ := testutil.NewTestResponse(h.ID+uint16(i+1), q.Name.String(),
				net.ParseIP("1.2.3.4"), 300)
			conn.WriteToUDP(resp, clientAddr)
		}
	}()

	r := DNS53{}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err = r.resolve(ctx, q, buf, addr)
	if err == nil {
		t.Error("expected max retries error")
	}

	if err.Error() != "max retries exceeded: DNS ID mismatch" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDNS53_Resolve_ShortResponse(t *testing.T) {
	// Custom UDP server that sends 6 too-short responses to a single query.
	// maxRetries in DNS53.resolve is 5, so this should trigger the limit.
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().String()

	go func() {
		buf := make([]byte, 4096)
		_, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		// Send 6 short responses
		for i := 0; i < 6; i++ {
			conn.WriteToUDP([]byte{0}, clientAddr)
		}
	}()

	r := DNS53{}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, _, err = r.resolve(ctx, q, buf, addr)
	if err == nil {
		t.Error("expected error for short responses")
	}

	if err.Error() != "max retries exceeded waiting for valid response" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDNS53_Resolve_PTRNotCached(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	counter := testutil.NewCountingHandler(testutil.SimpleDNSHandler(ip))
	server, err := testutil.NewMockDNSServer(counter.Handle)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	cache := newTestCache()
	r := DNS53{Cache: cache}
	q := makeTestQuery(t, "4.3.2.1.in-addr.arpa.", dnsmessage.TypePTR)
	buf := make([]byte, 512)
	ctx := context.Background()

	// First query
	_, info1, err := r.resolve(ctx, q, buf, server.Addr)
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	if info1.FromCache {
		t.Error("first PTR query should not be from cache")
	}

	// Second query - PTR should NOT be cached per RFC1035
	_, info2, err := r.resolve(ctx, q, buf, server.Addr)
	if err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if info2.FromCache {
		t.Error("PTR query should not be cached")
	}

	// Verify both queries reached the server
	if counter.Count() != 2 {
		t.Errorf("expected 2 server queries for PTR, got %d", counter.Count())
	}
}

func TestDNS53_Resolve_ConcurrentQueries(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	server, err := testutil.NewMockDNSServer(testutil.SimpleDNSHandler(ip))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	r := DNS53{}
	ctx := context.Background()

	// Run 100 concurrent queries
	errors := make(chan error, 100)
	for i := 0; i < 100; i++ {
		go func(id int) {
			q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
			buf := make([]byte, 512)
			_, _, err := r.resolve(ctx, q, buf, server.Addr)
			errors <- err
		}(i)
	}

	// Collect results
	for i := 0; i < 100; i++ {
		if err := <-errors; err != nil {
			t.Errorf("concurrent query %d failed: %v", i, err)
		}
	}
}

func TestDNS53_Resolve_DialError(t *testing.T) {
	r := DNS53{}
	q := makeTestQuery(t, "example.com.", dnsmessage.TypeA)
	buf := make([]byte, 512)
	ctx := context.Background()

	// Invalid address
	_, _, err := r.resolve(ctx, q, buf, "invalid:99999")
	if err == nil {
		t.Error("expected dial error for invalid address")
	}
}

// Helper functions

func makeTestQuery(t *testing.T, name string, qtype dnsmessage.Type) query.Query {
	msg, err := testutil.NewTestQuery(12345, name, qtype)
	if err != nil {
		t.Fatalf("failed to create query: %v", err)
	}

	return query.Query{
		ID:      12345,
		Name:    name,
		Type:    query.Type(qtype),
		Class:   query.ClassINET,
		Payload: msg,
	}
}

func newTestCache() *testCache {
	return &testCache{
		data: make(map[cacheKey]interface{}),
	}
}

type testCache struct {
	data map[cacheKey]interface{}
	mu   sync.Mutex
}

func (c *testCache) Get(key interface{}) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[key.(cacheKey)]
	return v, ok
}

func (c *testCache) Add(key, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key.(cacheKey)] = value
}
