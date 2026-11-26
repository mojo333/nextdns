package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

// failedUpstream fails every query after optionally leaving a reply in buf, as
// the resolver does when it has an expired cache entry to fall back on.
type failedUpstream struct {
	fromCache bool
	empty     bool
}

func (u failedUpstream) Resolve(_ context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
	info := resolver.ResolveInfo{FromCache: u.fromCache}
	err := errors.New("upstream down")
	if u.empty {
		return 0, info, err
	}
	n := copy(buf, q.Payload)
	buf[2] |= 0x80 // response
	return n, info, err
}

func TestServe_AnswersFromCacheFallbackWhenUpstreamFails(t *testing.T) {
	tests := []struct {
		name     string
		upstream failedUpstream
		want     dnsmessage.RCode
	}{
		{"expired cache entry", failedUpstream{fromCache: true}, dnsmessage.RCodeSuccess},
		{"failure without a cache entry", failedUpstream{}, dnsmessage.RCodeServerFailure},
		{"cache entry that was lost", failedUpstream{fromCache: true, empty: true}, dnsmessage.RCodeServerFailure},
	}
	for _, tt := range tests {
		for _, network := range []string{"udp4", "tcp4"} {
			t.Run(tt.name+" over "+network, func(t *testing.T) {
				reply := exchange(t, Proxy{Upstream: tt.upstream}, network)

				var p dnsmessage.Parser
				h, err := p.Start(reply)
				if err != nil {
					t.Fatalf("parse reply %x: %v", reply, err)
				}
				if h.RCode != tt.want {
					t.Fatalf("rcode = %v, want %v", h.RCode, tt.want)
				}
			})
		}
	}
}

// exchange serves exampleQuery once with p over network and returns the reply.
func exchange(t *testing.T, p Proxy, network string) []byte {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var inflight sync.WaitGroup
	served := make(chan struct{})
	var addr string
	var closeListener func() error
	if network == "udp4" {
		l, err := net.ListenPacket(network, "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr, closeListener = l.LocalAddr().String(), l.Close
		go func() { defer close(served); _ = p.serveUDP(ctx, l, make(chan struct{}, 1), &inflight) }()
	} else {
		l, err := net.Listen(network, "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr, closeListener = l.Addr().String(), l.Close
		go func() { defer close(served); _ = p.serveTCP(ctx, l, make(chan struct{}, 1), &inflight) }()
	}
	t.Cleanup(func() {
		cancel()
		_ = closeListener()
		<-served
		inflight.Wait()
	})

	c, err := net.Dial(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	msg := exampleQuery
	if network == "tcp4" {
		msg = append(binary.BigEndian.AppendUint16(nil, uint16(len(msg))), msg...)
	}
	if _, err := c.Write(msg); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 512)
	if network == "udp4" {
		n, err := c.Read(reply)
		if err != nil {
			t.Fatal(err)
		}
		return reply[:n]
	}
	var length uint16
	if err := binary.Read(c, binary.BigEndian, &length); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, reply[:length]); err != nil {
		t.Fatal(err)
	}
	return reply[:length]
}
