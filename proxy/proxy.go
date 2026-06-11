package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/nextdns/nextdns/hosts"
	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

// QueryInfo provides information about a DNS query handled by Proxy.
type QueryInfo struct {
	SourceIP          net.IP
	RemotePort        int
	LocalPort         int
	Protocol          string
	Profile           string
	PeerIP            net.IP
	Type              string
	Name              string
	QuerySize         int
	ResponseSize      int
	Duration          time.Duration
	FromCache         bool
	UpstreamTransport string
	Error             error
}

type HostResolver interface {
	LookupAddr(addr string) []string
	LookupHost(addr string) []string
}

// Proxy is a DNS53 to DNS over anything proxy.
type Proxy struct {
	// Addrs specifies the TCP/UDP address to listen to, :53 if empty.
	Addrs []string

	// LocalResolver is called before the upstream to resolve local hostnames or
	// IPs.
	LocalResolver HostResolver

	// Upstream specifies the resolver used for incoming queries.
	Upstream resolver.Resolver

	// DiscoveryResolver is called after the upstream if no result was found.
	DiscoveryResolver HostResolver

	// BogusPriv specifies that reverse lookup on private subnets are answerd
	// with NXDOMAIN.
	BogusPriv bool

	// Timeout defines the maximum allowed time allowed for a request before
	// being cancelled.
	Timeout time.Duration

	// Maximum number of inflight requests. Further requests will
	// not be answered.
	MaxInflightRequests uint

	// QueryLog specifies an optional log function called for each received query.
	QueryLog func(QueryInfo)

	// InfoLog specifies an option log function called when some actions are
	// performed.
	InfoLog func(string)

	// ErrorLog specifies an optional log function for errors. If not set,
	// errors are not reported.
	ErrorLog func(error)
}

const defaultMaxInflightRequests = 256

// defaultRequestTimeout bounds request handling when Timeout is left unset
// (or set to 0) so a hung upstream can never pin an inflight slot forever.
const defaultRequestTimeout = 5 * time.Second

// shutdownGraceTimeout bounds how long ListenAndServe waits for in-flight
// request handlers after its context is cancelled.
const shutdownGraceTimeout = 5 * time.Second

func (p Proxy) requestTimeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return defaultRequestTimeout
}

// ListenAndServe listens on UDP and TCP and serve DNS queries. If ctx is
// canceled, listeners are closed and ListenAndServe returns context.Canceled
// error.
func (p Proxy) ListenAndServe(ctx context.Context) error {
	var addrs []string

	for _, addr := range p.Addrs {
		if addr == "" {
			addr = ":53"
		}

		// Try to lookup the given addr in the /etc/hosts file (for localhost for
		// instance).
		found := false
		if host, port, err := net.SplitHostPort(addr); err == nil {
			if ips := hosts.LookupHost(host); len(ips) > 0 {
				for _, ip := range ips {
					found = true
					addrs = append(addrs, net.JoinHostPort(ip, port))
				}
			}
		}
		if !found {
			addrs = append(addrs, addr)
		}
	}

	lc := &net.ListenConfig{}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	expReturns := (len(addrs) * 2) + 1
	errs := make(chan error, expReturns)
	var closeAll []func() error
	var closeAllMu sync.Mutex
	closing := false
	// registerCloser records a listener to close on shutdown, or closes it
	// right away if shutdown already started while the listener was binding.
	registerCloser := func(close func() error) {
		closeAllMu.Lock()
		defer closeAllMu.Unlock()
		if closing {
			_ = close()
			return
		}
		closeAll = append(closeAll, close)
	}
	var inflight sync.WaitGroup
	inflightRequests := make(chan struct{}, p.maxInflightRequests())

	for _, addr := range addrs {
		go func(addr string) {
			var err error
			p.logInfof("Listening on UDP/%s", addr)
			udp, err := lc.ListenPacket(ctx, "udp", addr)
			if err == nil {
				registerCloser(udp.Close)
				err = p.serveUDP(ctx, udp, inflightRequests, &inflight)
			}
			cancel()
			if err != nil {
				err = fmt.Errorf("udp: %w", err)
			}
			errs <- err
		}(addr)

		go func(addr string) {
			var err error
			p.logInfof("Listening on TCP/%s", addr)
			tcp, err := lc.Listen(ctx, "tcp", addr)
			if err == nil {
				registerCloser(tcp.Close)
				err = p.serveTCP(ctx, tcp, inflightRequests, &inflight)
			}
			cancel()
			if err != nil {
				err = fmt.Errorf("tcp: %w", err)
			}
			errs <- err
		}(addr)
	}

	<-ctx.Done()
	errs <- ctx.Err()
	closeAllMu.Lock()
	closing = true
	for _, close := range closeAll {
		_ = close()
	}
	closeAllMu.Unlock()
	// Wait for the two sockets (+ ctx err) to be terminated and return the
	// initial error.
	var err error
	for i := 0; i < expReturns; i++ {
		if e := <-errs; (err == nil || errors.Is(err, context.Canceled)) && e != nil {
			err = e
		}
	}
	// Wait for in-flight request handlers (bounded by their request timeout)
	// so callers can safely tear down the upstream resolver once we return.
	handlersDone := make(chan struct{})
	go func() {
		inflight.Wait()
		close(handlersDone)
	}()
	select {
	case <-handlersDone:
	case <-time.After(shutdownGraceTimeout):
		p.logErr(errors.New("shutdown: in-flight requests did not complete within grace period"))
	}
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	return nil
}

func (p Proxy) Resolve(ctx context.Context, q query.Query, buf []byte) (n int, i resolver.ResolveInfo, err error) {
	if p.LocalResolver != nil {
		if _n, _i, _err := hostsResolve(p.LocalResolver, q, buf); _err == nil {
			return _n, _i, nil
		}
	}

	priv := q.Type == query.TypePTR && isPrivateReverse(q.Name)

	if !p.BogusPriv || !priv {
		n, i, err = p.Upstream.Resolve(ctx, q, buf)
	}

	if q.RecursionDesired && p.DiscoveryResolver != nil && (n <= 0 || isNXDomain(buf[:n])) {
		if _n, _i, _err := hostsResolve(p.DiscoveryResolver, q, buf); _err == nil {
			return _n, _i, nil
		}
	}

	if p.BogusPriv && priv {
		n = replyRCode(dnsmessage.RCodeNameError, q, buf)
		return n, i, nil
	}

	return n, i, err
}

func (p Proxy) maxInflightRequests() int {
	if p.MaxInflightRequests == 0 {
		return defaultMaxInflightRequests
	}
	return int(p.MaxInflightRequests)
}

func (p Proxy) logQuery(q QueryInfo) {
	if p.QueryLog != nil {
		p.QueryLog(q)
	}
}

func (p Proxy) logInfof(format string, a ...interface{}) {
	if p.InfoLog != nil {
		p.InfoLog(fmt.Sprintf(format, a...))
	}
}

func (p Proxy) logErr(err error) {
	if err != nil && p.ErrorLog != nil {
		p.ErrorLog(err)
	}
}
