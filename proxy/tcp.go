package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
	"golang.org/x/net/dns/dnsmessage"
)

const maxTCPSize = 65535

type tcpBuf [maxTCPSize]byte

var tcpClientReadTimeout = 10 * time.Second

// tcpWriteTimeout bounds each reply write, so a client that stops reading
// cannot hold a reply goroutine forever.
var tcpWriteTimeout = 10 * time.Second

// maxTCPPipelinedQueries caps the unanswered queries one TCP connection may
// have. Past it the connection is not read until replies drain, so a client
// that reads slowly or not at all slows only itself and cannot park unbounded
// goroutines and buffers.
const maxTCPPipelinedQueries = 32

// maxTCPConnections caps concurrent TCP connections. Each holds a read buffer
// and up to maxTCPPipelinedQueries waiting replies, so this bounds the memory
// TCP clients can pin. Connections beyond it are closed at once.
var maxTCPConnections = 256

// acceptRetryDelay is the pause after an accept fails for lack of resources.
var acceptRetryDelay = 100 * time.Millisecond

// isResourceShortage reports whether an accept error is likely to clear once
// other connections close or memory is freed.
func isResourceShortage(err error) bool {
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.ENOBUFS) || errors.Is(err, syscall.ENOMEM)
}

func (p Proxy) serveTCP(ctx context.Context, l net.Listener, inflightRequests chan struct{}, inflight *sync.WaitGroup) error {
	bpool := &sync.Pool{
		New: func() any {
			return new(tcpBuf)
		},
	}
	conns := make(chan struct{}, maxTCPConnections)

	for {
		c, err := l.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if isResourceShortage(err) {
				p.logErr(fmt.Errorf("TCP accept: %w", err))
				select {
				case <-time.After(acceptRetryDelay):
					continue
				case <-ctx.Done():
				}
			}
			return err
		}
		select {
		case conns <- struct{}{}:
		default:
			_ = c.Close()
			continue
		}
		inflight.Add(1)
		go func() {
			defer inflight.Done()
			defer func() { <-conns }()
			if err := p.serveTCPConn(ctx, c, inflightRequests, bpool); err != nil {
				if p.ErrorLog != nil {
					p.ErrorLog(err)
				}
			}
		}()
	}
}

func (p Proxy) serveTCPConn(ctx context.Context, c net.Conn, inflightRequests chan struct{}, bpool *sync.Pool) error {
	localAddr := c.LocalAddr()
	remoteAddr := c.RemoteAddr()
	localIP := addrIP(localAddr)
	sourceIP := addrIP(remoteAddr)
	localPort := addrPort(localAddr)
	remotePort := addrPort(remoteAddr)
	w := &tcpWriter{c: c}
	pending := make(chan struct{}, maxTCPPipelinedQueries)
	var wg sync.WaitGroup
	defer func() {
		// Wait for all query processing goroutines to complete before closing.
		wg.Wait()
		c.Close()
	}()

	// Unblock the read loop as soon as the serve context is cancelled so
	// shutdown does not have to wait for the client read timeout.
	watcherDone := make(chan struct{})
	defer close(watcherDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = c.SetReadDeadline(time.Now())
		case <-watcherDone:
		}
	}()

	for {
		// Wait for room under this connection's pipelining cap. Only this
		// client waits, and a failed write or shutdown still frees the loop.
		select {
		case pending <- struct{}{}:
		case <-ctx.Done():
			return nil
		}
		if tcpClientReadTimeout > 0 {
			_ = c.SetReadDeadline(time.Now().Add(tcpClientReadTimeout))
		}
		// The shutdown watcher's immediate deadline may have been overwritten
		// just above, so check for cancellation here rather than wait out the
		// idle timeout.
		if ctx.Err() != nil {
			return nil
		}
		bp := bpool.Get().(*tcpBuf)
		buf := bp[:]
		qsize, err := readTCP(c, buf)
		_ = c.SetReadDeadline(time.Time{})
		if err != nil {
			bpool.Put(bp)
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("TCP read: %v", err)
		}
		if qsize <= 14 {
			bpool.Put(bp)
			return fmt.Errorf("query too small: %d", qsize)
		}
		start := time.Now()
		// Take the slot only once a whole query has arrived, so an idle
		// connection never holds one.
		if !tryAcquireRequest(inflightRequests) {
			p.rejectTCP(w, bp, qsize, localIP, sourceIP, localPort, remotePort, start)
			bpool.Put(bp)
			<-pending
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-pending }()
			// The in-flight slot bounds upstream work, so it is released as soon
			// as resolving ends rather than held while the reply waits for a
			// slow reader.
			slotHeld := true
			releaseSlot := func() {
				if slotHeld {
					slotHeld = false
					<-inflightRequests
				}
			}
			var err error
			var rsize int
			var ri resolver.ResolveInfo
			buf := bp[:]
			q, err := query.New(buf[:qsize], sourceIP, localIP)
			if err != nil {
				p.logErr(err)
			}
			rbp := bpool.Get().(*tcpBuf)
			rbuf := rbp[:]
			buffersHeld := true
			releaseBuffers := func() {
				if buffersHeld {
					buffersHeld = false
					bpool.Put(bp)
					bpool.Put(rbp)
				}
			}
			defer func() {
				if r := recover(); r != nil {
					stackBuf := make([]byte, 64<<10)
					stackBuf = stackBuf[:runtime.Stack(stackBuf, false)]
					err = fmt.Errorf("panic: %v: %s", r, string(stackBuf))
				}
				releaseBuffers()
				releaseSlot()
				p.logQuery(QueryInfo{
					SourceIP:          sourceIP,
					RemotePort:        remotePort,
					LocalPort:         localPort,
					PeerIP:            q.PeerIP,
					Protocol:          "TCP",
					Type:              q.Type.String(),
					Name:              q.Name,
					QuerySize:         qsize,
					ResponseSize:      rsize,
					Duration:          time.Since(start),
					Profile:           ri.Profile,
					FromCache:         ri.FromCache,
					UpstreamTransport: ri.Transport,
					Error:             err,
				})
			}()
			// reply sends the answer in rbuf. A reply waiting for a slow reader
			// holds only its frame, not the two 64KB buffers or a slot.
			reply := func() error {
				frame := newTCPFrame(rbuf[:rsize])
				releaseBuffers()
				releaseSlot()
				return w.writeFrame(frame)
			}

			if err != nil {
				// Malformed query: reply with FORMERR and skip upstream resolution.
				rsize = replyRCode(dnsmessage.RCodeFormatError, q, rbuf)
				if werr := reply(); werr != nil {
					// Keep the parse error but include the write error.
					err = fmt.Errorf("%v (write: %w)", err, werr)
				}
				return
			}
			// Derive from the serve context so shutdown cancels in-flight
			// requests; the timeout bounds them even when Timeout is 0.
			ctx, cancel := context.WithTimeout(ctx, p.requestTimeout())
			defer cancel()
			if rsize, ri, err = p.Resolve(ctx, q, rbuf); err != nil || rsize <= 0 || rsize > maxTCPSize {
				rsize = replyRCode(dnsmessage.RCodeServerFailure, q, rbuf)
			}
			werr := reply()
			if err == nil {
				// Do not overwrite resolve error when on cache fallback.
				err = werr
			}
		}()
	}
}

func readTCP(r io.Reader, buf []byte) (int, error) {
	var length uint16
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return -1, err
	}
	if length > maxTCPSize {
		return -1, errors.New("message too large")
	}
	return io.ReadFull(r, buf[:length])
}

// tcpWriter writes the replies for one TCP connection, which come from a
// separate goroutine per pipelined query.
type tcpWriter struct {
	mu sync.Mutex
	c  net.Conn
}

// write sends one reply with its length prefix.
func (w *tcpWriter) write(buf []byte) error {
	return w.writeFrame(newTCPFrame(buf))
}

// newTCPFrame copies buf behind its 2-byte length prefix.
func newTCPFrame(buf []byte) []byte {
	frame := make([]byte, 2+len(buf))
	binary.BigEndian.PutUint16(frame, uint16(len(buf)))
	copy(frame[2:], buf)
	return frame
}

// writeFrame serialises writes so the write deadline, which belongs to the
// whole connection, cannot be pushed back by the next reply while a client that
// never reads keeps sending queries. Any failed write closes the connection, so
// every queued reply fails at once.
func (w *tcpWriter) writeFrame(frame []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.c.SetWriteDeadline(time.Now().Add(tcpWriteTimeout))
	if _, err := w.c.Write(frame); err != nil {
		_ = w.c.Close()
		return err
	}
	return nil
}

// rejectTCP answers a query that found no free in-flight slot and keeps the
// connection open for the client's next query.
func (p Proxy) rejectTCP(w *tcpWriter, bp *tcpBuf, qsize int, localIP, sourceIP net.IP, localPort, remotePort int, start time.Time) {
	buf := bp[:]
	q, rsize, err := rejectionReply(buf, qsize, sourceIP, localIP)
	if werr := w.write(buf[:rsize]); werr != nil {
		err = fmt.Errorf("%v (write: %w)", err, werr)
	}
	p.logRejection("TCP", q, sourceIP, localPort, remotePort, qsize, rsize, start, err)
}
