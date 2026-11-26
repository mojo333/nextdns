package resolver

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// longLivedResponse is the cached test answer with a one hour record TTL.
func longLivedResponse(response []byte) []byte {
	long := bytes.Clone(response)
	binary.BigEndian.PutUint32(long[len(long)-10:], 3600)
	return long
}

// assertZeroTTL fails unless every answer in msg tells the client to ask again.
func assertZeroTTL(t *testing.T, msg []byte) {
	t.Helper()
	var m dnsmessage.Message
	if err := m.Unpack(msg); err != nil {
		t.Fatal(err)
	}
	if len(m.Answers) == 0 {
		t.Fatal("no answers")
	}
	for _, a := range m.Answers {
		if a.Header.TTL != 0 {
			t.Errorf("answer TTL = %d, want 0 so the client asks again", a.Header.TTL)
		}
	}
}

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
		maxAge        uint32 // cache-max-age in seconds
		wantAnswer    bool
	}{
		{"expired", recent, time.Time{}, 0, true},
		{"past cache-max-age but within its TTL", time.Now().Add(-5 * time.Minute), time.Time{}, 60, true},
		// Serving it could undo a block added since it was cached.
		{"older than the last configuration change", recent, recent.Add(time.Minute), 0, false},
		// The address may long since have moved to somebody else.
		{"cached more than a day ago", time.Now().Add(-25 * time.Hour), time.Time{}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := response
			if tt.maxAge > 0 {
				msg = longLivedResponse(response)
			}
			r := DOH{URL: url, CacheMaxAge: tt.maxAge, Cache: staticCache{value: &cacheValue{time: tt.cachedAt, msg: msg}}}
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
			if tt.wantAnswer {
				assertZeroTTL(t, buf[:n])
			}
		})
	}
}

func TestDNS53FallbackToExpiredCacheEntry(t *testing.T) {
	q, response := cachedTestQuery(t)
	tests := []struct {
		name       string
		age        time.Duration
		maxAge     uint32 // cache-max-age in seconds
		wantAnswer bool
	}{
		{"expired", time.Hour, 0, true},
		{"past cache-max-age but within its TTL", 5 * time.Minute, 60, true},
		{"cached more than a day ago", 25 * time.Hour, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := response
			if tt.maxAge > 0 {
				msg = longLivedResponse(response)
			}
			r := DNS53{CacheMaxAge: tt.maxAge, Cache: staticCache{value: &cacheValue{time: time.Now().Add(-tt.age), msg: msg}}}
			buf := make([]byte, 512)

			// Port "invalid" cannot be dialled, so the upstream query fails.
			n, info, err := r.resolve(context.Background(), q, buf, "192.0.2.1:invalid", newRequestLimiter(1))

			if err == nil {
				t.Fatal("resolve() error = nil, want the upstream failure")
			}
			if got := n > 0 && info.FromCache; got != tt.wantAnswer {
				t.Fatalf("fallback answer = %v (n=%d, FromCache=%v), want %v", got, n, info.FromCache, tt.wantAnswer)
			}
			if tt.wantAnswer {
				assertZeroTTL(t, buf[:n])
			}
		})
	}
}
