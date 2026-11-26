package resolver

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestDOHFallbackToExpiredCacheEntry(t *testing.T) {
	const url = "https://resolver.test/dns-query"
	q, response := cachedTestQuery(t)
	failing := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("upstream down")
	})
	recent := time.Now().Add(-time.Hour)
	tests := []struct {
		name          string
		cachedAt      time.Time
		configChanged time.Time
		wantAnswer    bool
	}{
		{"expired", recent, time.Time{}, true},
		// Serving it could undo a block added since it was cached.
		{"older than the last configuration change", recent, recent.Add(time.Minute), false},
		// The address may long since have moved to somebody else.
		{"cached more than a day ago", time.Now().Add(-25 * time.Hour), time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := DOH{URL: url, Cache: staticCache{value: &cacheValue{time: tt.cachedAt, msg: response}}}
			if !tt.configChanged.IsZero() {
				r.updateLastMod(url, tt.configChanged.UTC().Format(time.RFC1123))
			}
			buf := make([]byte, 512)

			n, info, err := r.resolve(context.Background(), q, buf, failing, newRequestLimiter(1))

			if err == nil {
				t.Fatal("resolve() error = nil, want the upstream failure")
			}
			if got := n > 0 && info.FromCache; got != tt.wantAnswer {
				t.Fatalf("fallback answer = %v (n=%d, FromCache=%v), want %v", got, n, info.FromCache, tt.wantAnswer)
			}
			if !tt.wantAnswer {
				return
			}
			var m dnsmessage.Message
			if err := m.Unpack(buf[:n]); err != nil {
				t.Fatal(err)
			}
			for _, a := range m.Answers {
				if a.Header.TTL != 0 {
					t.Errorf("answer TTL = %d, want 0 so the client asks again", a.Header.TTL)
				}
			}
		})
	}
}

func TestDNS53FallbackToExpiredCacheEntry(t *testing.T) {
	q, response := cachedTestQuery(t)
	tests := []struct {
		name       string
		age        time.Duration
		wantAnswer bool
	}{
		{"expired", time.Hour, true},
		{"cached more than a day ago", 25 * time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := DNS53{Cache: staticCache{value: &cacheValue{time: time.Now().Add(-tt.age), msg: response}}}

			// Port "invalid" cannot be dialled, so the upstream query fails.
			n, info, err := r.resolve(context.Background(), q, make([]byte, 512), "192.0.2.1:invalid", newRequestLimiter(1))

			if err == nil {
				t.Fatal("resolve() error = nil, want the upstream failure")
			}
			if got := n > 0 && info.FromCache; got != tt.wantAnswer {
				t.Fatalf("fallback answer = %v (n=%d, FromCache=%v), want %v", got, n, info.FromCache, tt.wantAnswer)
			}
		})
	}
}
