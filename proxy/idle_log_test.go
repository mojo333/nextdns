package proxy

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// A client closing out an idle connection is normal. A broken query is not.
func TestServeTCP_LogsBrokenQueriesButNotIdleTimeouts(t *testing.T) {
	prev := tcpClientReadTimeout
	tcpClientReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { tcpClientReadTimeout = prev })
	tests := []struct {
		name   string
		send   []byte
		logged int
	}{
		{"idle connection", nil, 0},
		{"query too small", []byte{0x00, 0x02, 0x12, 0x34}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var logged []error
			p := Proxy{ErrorLog: func(err error) {
				mu.Lock()
				defer mu.Unlock()
				logged = append(logged, err)
			}}
			var inflight sync.WaitGroup
			served := make(chan struct{})
			go func() {
				defer close(served)
				_ = p.serveTCP(context.Background(), l, make(chan struct{}, 1), &inflight)
			}()
			c, err := net.Dial("tcp4", l.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if _, err := c.Write(tt.send); err != nil {
				t.Fatal(err)
			}
			// The server closes the connection once it gives up on it.
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, _ = c.Read(make([]byte, 1))
			l.Close()
			<-served
			inflight.Wait()

			mu.Lock()
			defer mu.Unlock()
			if len(logged) != tt.logged {
				t.Fatalf("logged %v, want %d error(s)", logged, tt.logged)
			}
		})
	}
}
