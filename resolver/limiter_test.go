package resolver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/nextdns/nextdns/resolver/query"
)

type staticCache struct {
	value *cacheValue
}

func (c staticCache) Get(uint64) (*cacheValue, bool) {
	return c.value, c.value != nil
}

func (staticCache) Set(uint64, *cacheValue) {}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRequestLimiter(t *testing.T) {
	l := newRequestLimiter(1)
	if err := l.tryAcquire(); err != nil {
		t.Fatalf("first tryAcquire() error = %v", err)
	}
	if err := l.tryAcquire(); !errors.Is(err, ErrUpstreamBusy) {
		t.Fatalf("second tryAcquire() error = %v, want ErrUpstreamBusy", err)
	}
	l.release()
	if err := l.tryAcquire(); err != nil {
		t.Fatalf("tryAcquire() after release error = %v", err)
	}
	l.release()
}

func BenchmarkDNSUpstreamLimiter(b *testing.B) {
	r := DNS{MaxInflightRequests: 32}
	_ = r.upstreamLimiter()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = r.upstreamLimiter()
	}
}

func TestNewUsesFirstEndpointWithoutBlockingBootstrap(t *testing.T) {
	r, err := New("192.0.2.1,192.0.2.2")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	dns := r.(*DNS)
	if got := dns.Manager.InitEndpoint.String(); got != "192.0.2.1:53" {
		t.Fatalf("InitEndpoint = %q, want first configured endpoint", got)
	}
}

func TestDNS53FreshCacheBypassesBusyLimiter(t *testing.T) {
	q, response := cachedTestQuery(t)
	l := newRequestLimiter(1)
	if err := l.tryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer l.release()

	r := DNS53{Cache: staticCache{value: &cacheValue{time: time.Now(), msg: response}}}
	buf := make([]byte, 512)
	n, info, err := r.resolve(context.Background(), q, buf, "192.0.2.1:53", l)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if n == 0 || !info.FromCache {
		t.Fatalf("resolve() = n %d, FromCache %v; want cached response", n, info.FromCache)
	}
}

func TestDOHFreshCacheBypassesBusyLimiter(t *testing.T) {
	q, response := cachedTestQuery(t)
	l := newRequestLimiter(1)
	if err := l.tryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer l.release()

	r := DOH{
		URL:   "https://resolver.test/dns-query",
		Cache: staticCache{value: &cacheValue{time: time.Now(), msg: response, trans: "test"}},
	}
	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("RoundTrip called for a fresh cache hit")
		return nil, nil
	})
	buf := make([]byte, 512)
	n, info, err := r.resolve(context.Background(), q, buf, rt, l)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if n == 0 || !info.FromCache {
		t.Fatalf("resolve() = n %d, FromCache %v; want cached response", n, info.FromCache)
	}
}

func TestDOHBusyLimiterFailsBeforeNetwork(t *testing.T) {
	q, _ := cachedTestQuery(t)
	l := newRequestLimiter(1)
	if err := l.tryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer l.release()

	r := DOH{URL: "https://resolver.test/dns-query"}
	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("RoundTrip called after upstream admission was rejected")
		return nil, nil
	})
	_, _, err := r.resolve(context.Background(), q, make([]byte, 512), rt, l)
	if !errors.Is(err, ErrUpstreamBusy) {
		t.Fatalf("resolve() error = %v, want ErrUpstreamBusy", err)
	}
}

func TestDNSUpstreamLimitIsIsolatedPerResolver(t *testing.T) {
	blockedAddr, blocked, releaseBlocked := startTestDNSServer(t, true)
	healthyAddr, _, _ := startTestDNSServer(t, false)

	blockedResolver, err := New(blockedAddr)
	if err != nil {
		t.Fatalf("New(blocked) error = %v", err)
	}
	blockedDNS := blockedResolver.(*DNS)
	blockedDNS.MaxInflightRequests = 1
	healthyResolver, err := New(healthyAddr)
	if err != nil {
		t.Fatalf("New(healthy) error = %v", err)
	}
	healthyDNS := healthyResolver.(*DNS)
	healthyDNS.MaxInflightRequests = 1

	q, _ := cachedTestQuery(t)
	firstDone := make(chan error, 1)
	go func() {
		_, _, err := blockedDNS.Resolve(context.Background(), q, make([]byte, 512))
		firstDone <- err
	}()
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("first blocked resolver request did not reach the server")
	}

	start := time.Now()
	_, _, err = blockedDNS.Resolve(context.Background(), q, make([]byte, 512))
	if !errors.Is(err, ErrUpstreamBusy) {
		t.Fatalf("second blocked Resolve() error = %v, want ErrUpstreamBusy", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("overloaded resolver took %v to fail", elapsed)
	}

	if n, _, err := healthyDNS.Resolve(context.Background(), q, make([]byte, 512)); err != nil || n == 0 {
		t.Fatalf("healthy Resolve() = %d, %v; want an independent response", n, err)
	}

	close(releaseBlocked)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first blocked Resolve() error after release = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first blocked resolver request did not finish after release")
	}
}

func startTestDNSServer(t *testing.T, blockQueries bool) (string, <-chan struct{}, chan struct{}) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	blocked := make(chan struct{})
	release := make(chan struct{})
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			response := append([]byte(nil), buf[:n]...)
			if blockQueries && n >= 2 && response[0] == 0 && response[1] == 1 {
				select {
				case <-blocked:
				default:
					close(blocked)
				}
				<-release
			}
			if len(response) >= 3 {
				response[2] |= 0x80
			}
			_, _ = conn.WriteToUDP(response, addr)
		}
	}()
	return conn.LocalAddr().String(), blocked, release
}

func cachedTestQuery(t *testing.T) (query.Query, []byte) {
	t.Helper()
	payload := []byte{
		0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x04, 't', 'e', 's', 't', 0x03, 'c', 'o', 'm', 0x00,
		0x00, 0x01, 0x00, 0x01,
	}
	q, err := query.New(payload, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("query.New() error = %v", err)
	}
	response := []byte{
		0x00, 0x01, 0x81, 0x80, 0x00, 0x01, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x00,
		0x04, 't', 'e', 's', 't', 0x03, 'c', 'o', 'm', 0x00,
		0x00, 0x01, 0x00, 0x01,
		0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x3c, 0x00, 0x04, 192, 0, 2, 1,
	}
	return q, response
}
