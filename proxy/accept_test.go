package proxy

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// scriptedListener returns the given Accept errors in turn.
type scriptedListener struct {
	net.Listener
	errs []error
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	err := l.errs[0]
	l.errs = l.errs[1:]
	return nil, err
}

func acceptError(errno syscall.Errno) error {
	return &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept", errno)}
}

func TestServeTCP_KeepsListeningThroughResourceShortages(t *testing.T) {
	prev := acceptRetryDelay
	acceptRetryDelay = time.Millisecond
	t.Cleanup(func() { acceptRetryDelay = prev })
	l := &scriptedListener{errs: []error{
		acceptError(syscall.EMFILE),
		acceptError(syscall.ENFILE),
		acceptError(syscall.ENOBUFS),
		acceptError(syscall.ENOMEM),
		net.ErrClosed,
	}}

	err := (Proxy{}).serveTCP(context.Background(), l, make(chan struct{}, 1), &sync.WaitGroup{})

	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("serveTCP = %v, want it to keep accepting until the listener closed", err)
	}
}

func TestServeTCP_ReturnsOtherAcceptErrors(t *testing.T) {
	l := &scriptedListener{errs: []error{acceptError(syscall.EINVAL), net.ErrClosed}}

	err := (Proxy{}).serveTCP(context.Background(), l, make(chan struct{}, 1), &sync.WaitGroup{})

	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("serveTCP = %v, want EINVAL", err)
	}
}
