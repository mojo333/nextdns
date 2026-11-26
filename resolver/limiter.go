package resolver

import "github.com/nextdns/nextdns/resolver/endpoint"

// DefaultMaxInflightUpstreamRequests is the per-resolver network query limit
// used when DNS.MaxInflightRequests is zero.
const DefaultMaxInflightUpstreamRequests = 32

// ErrUpstreamBusy indicates that a resolver has reached its upstream network
// concurrency limit. It is intentionally distinct from an endpoint failure:
// callers should fail the query quickly without changing endpoint health state.
var ErrUpstreamBusy = endpoint.ErrUpstreamBusy

type requestLimiter struct {
	tokens chan struct{}
}

func newRequestLimiter(limit uint) *requestLimiter {
	if limit == 0 {
		limit = DefaultMaxInflightUpstreamRequests
	}
	return &requestLimiter{tokens: make(chan struct{}, limit)}
}

func (l *requestLimiter) tryAcquire() error {
	select {
	case l.tokens <- struct{}{}:
		return nil
	default:
		return ErrUpstreamBusy
	}
}

func (l *requestLimiter) release() {
	select {
	case <-l.tokens:
	default:
		panic("release of unacquired upstream request token")
	}
}
