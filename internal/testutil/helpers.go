package testutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// MockDNSServer provides a mock DNS server for testing (UDP and TCP).
type MockDNSServer struct {
	Addr       string
	UDPAddr    *net.UDPAddr
	TCPAddr    *net.TCPAddr
	Handler    func([]byte) []byte
	udpConn    *net.UDPConn
	tcpLn      net.Listener
	mu         sync.Mutex
	queries    [][]byte
	closeOnce  sync.Once
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewMockDNSServer creates a new mock DNS server.
func NewMockDNSServer(handler func([]byte) []byte) (*MockDNSServer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &MockDNSServer{
		Handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start UDP listener
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		cancel()
		return nil, err
	}
	s.udpConn = udpConn
	s.UDPAddr = udpConn.LocalAddr().(*net.UDPAddr)

	// Start TCP listener
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		udpConn.Close()
		cancel()
		return nil, err
	}
	s.tcpLn = tcpLn
	s.TCPAddr = tcpLn.Addr().(*net.TCPAddr)
	s.Addr = s.UDPAddr.String()

	go s.serveUDP()
	go s.serveTCP()

	return s, nil
}

func (s *MockDNSServer) serveUDP() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		s.udpConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, addr, err := s.udpConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		query := make([]byte, n)
		copy(query, buf[:n])
		s.recordQuery(query)

		if s.Handler != nil {
			resp := s.Handler(query)
			if resp != nil {
				s.udpConn.WriteToUDP(resp, addr)
			}
		}
	}
}

func (s *MockDNSServer) serveTCP() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn, err := s.tcpLn.Accept()
		if err != nil {
			return
		}
		go s.handleTCP(conn)
	}
}

func (s *MockDNSServer) handleTCP(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 4096)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		// Read 2-byte length prefix
		n, err := conn.Read(buf[:2])
		if err != nil {
			return
		}
		if n != 2 {
			return
		}

		msgLen := int(buf[0])<<8 | int(buf[1])
		if msgLen > len(buf)-2 {
			return
		}

		n, err = conn.Read(buf[2 : 2+msgLen])
		if err != nil {
			return
		}

		query := make([]byte, n)
		copy(query, buf[2:2+n])
		s.recordQuery(query)

		if s.Handler != nil {
			resp := s.Handler(query)
			if resp != nil {
				// Write with length prefix
				lenBuf := []byte{byte(len(resp) >> 8), byte(len(resp))}
				conn.Write(lenBuf)
				conn.Write(resp)
			}
		}
	}
}

func (s *MockDNSServer) recordQuery(q []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
}

// Queries returns all received queries.
func (s *MockDNSServer) Queries() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([][]byte, len(s.queries))
	copy(result, s.queries)
	return result
}

// Close stops the mock DNS server.
func (s *MockDNSServer) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		if s.udpConn != nil {
			s.udpConn.Close()
		}
		if s.tcpLn != nil {
			s.tcpLn.Close()
		}
	})
	return nil
}

// MockDoHServer provides a mock DNS-over-HTTPS server.
type MockDoHServer struct {
	*httptest.Server
	Handler func([]byte) []byte
	mu      sync.Mutex
	queries [][]byte
}

// NewMockDoHServer creates a new mock DoH server.
func NewMockDoHServer(handler func([]byte) []byte) *MockDoHServer {
	s := &MockDoHServer{
		Handler: handler,
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handleRequest))
	return s
}

func (s *MockDoHServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	buf := make([]byte, 4096)
	n, err := r.Body.Read(buf)
	if err != nil && n == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	query := make([]byte, n)
	copy(query, buf[:n])
	s.recordQuery(query)

	if s.Handler != nil {
		resp := s.Handler(query)
		if resp != nil {
			w.Header().Set("Content-Type", "application/dns-message")
			w.Write(resp)
			return
		}
	}

	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (s *MockDoHServer) recordQuery(q []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
}

// Queries returns all received queries.
func (s *MockDoHServer) Queries() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([][]byte, len(s.queries))
	copy(result, s.queries)
	return result
}

// DNSMessageBuilder helps build DNS messages for testing.
type DNSMessageBuilder struct {
	msg dnsmessage.Message
}

// NewDNSMessageBuilder creates a new DNS message builder.
func NewDNSMessageBuilder() *DNSMessageBuilder {
	return &DNSMessageBuilder{
		msg: dnsmessage.Message{
			Header: dnsmessage.Header{
				Response:           false,
				Authoritative:      false,
				RecursionDesired:   true,
				RecursionAvailable: false,
			},
		},
	}
}

// SetID sets the message ID.
func (b *DNSMessageBuilder) SetID(id uint16) *DNSMessageBuilder {
	b.msg.Header.ID = id
	return b
}

// SetResponse marks the message as a response.
func (b *DNSMessageBuilder) SetResponse() *DNSMessageBuilder {
	b.msg.Header.Response = true
	b.msg.Header.RecursionAvailable = true
	return b
}

// SetRCode sets the response code.
func (b *DNSMessageBuilder) SetRCode(rcode dnsmessage.RCode) *DNSMessageBuilder {
	b.msg.Header.RCode = rcode
	return b
}

// AddQuestion adds a question to the message.
func (b *DNSMessageBuilder) AddQuestion(name string, qtype dnsmessage.Type) *DNSMessageBuilder {
	n, _ := dnsmessage.NewName(name)
	b.msg.Questions = append(b.msg.Questions, dnsmessage.Question{
		Name:  n,
		Type:  qtype,
		Class: dnsmessage.ClassINET,
	})
	return b
}

// AddAnswer adds an A record answer.
func (b *DNSMessageBuilder) AddAnswer(name string, ttl uint32, ip net.IP) *DNSMessageBuilder {
	n, _ := dnsmessage.NewName(name)
	var rBody dnsmessage.ResourceBody
	if ip4 := ip.To4(); ip4 != nil {
		rBody = &dnsmessage.AResource{A: [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]}}
	} else {
		var a [16]byte
		copy(a[:], ip)
		rBody = &dnsmessage.AAAAResource{AAAA: a}
	}
	b.msg.Answers = append(b.msg.Answers, dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{
			Name:  n,
			Type:  dnsmessage.TypeA,
			Class: dnsmessage.ClassINET,
			TTL:   ttl,
		},
		Body: rBody,
	})
	return b
}

// Build packs the message into bytes.
func (b *DNSMessageBuilder) Build() ([]byte, error) {
	return b.msg.Pack()
}

// CheckGoroutineLeak checks for goroutine leaks during test.
func CheckGoroutineLeak(t *testing.T) func() {
	before := runtime.NumGoroutine()
	return func() {
		// Wait a bit for goroutines to exit
		time.Sleep(100 * time.Millisecond)
		after := runtime.NumGoroutine()
		if after > before+5 { // Allow some tolerance
			t.Errorf("Potential goroutine leak: before=%d, after=%d", before, after)
		}
	}
}

// CheckContextCancelled checks if context was properly cancelled.
func CheckContextCancelled(t *testing.T, ctx context.Context, timeout time.Duration) {
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(timeout):
		t.Error("Context was not cancelled within timeout")
	}
}

// WaitForCondition waits for a condition to be true or times out.
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, msg string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for condition: %s", msg)
}

// AssertNoError fails the test if err is not nil.
func AssertNoError(t *testing.T, err error, msg string) {
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

// AssertError fails the test if err is nil.
func AssertError(t *testing.T, err error, msg string) {
	if err == nil {
		t.Fatalf("%s: expected error but got nil", msg)
	}
}

// AssertEqual fails if expected != actual.
func AssertEqual(t *testing.T, expected, actual interface{}, msg string) {
	if expected != actual {
		t.Fatalf("%s: expected %v, got %v", msg, expected, actual)
	}
}

// NewTestQuery creates a test DNS query message.
func NewTestQuery(id uint16, name string, qtype dnsmessage.Type) ([]byte, error) {
	return NewDNSMessageBuilder().
		SetID(id).
		AddQuestion(name, qtype).
		Build()
}

// NewTestResponse creates a test DNS response message.
func NewTestResponse(id uint16, name string, ip net.IP, ttl uint32) ([]byte, error) {
	return NewDNSMessageBuilder().
		SetID(id).
		SetResponse().
		AddQuestion(name, dnsmessage.TypeA).
		AddAnswer(name, ttl, ip).
		Build()
}

// SimpleDNSHandler creates a simple DNS handler that responds with a fixed IP.
func SimpleDNSHandler(ip net.IP) func([]byte) []byte {
	return func(query []byte) []byte {
		var p dnsmessage.Parser
		h, err := p.Start(query)
		if err != nil {
			return nil
		}
		q, err := p.Question()
		if err != nil {
			return nil
		}

		resp, _ := NewTestResponse(h.ID, q.Name.String(), ip, 300)
		return resp
	}
}

// ErrorDNSHandler creates a DNS handler that returns a specific error code.
func ErrorDNSHandler(rcode dnsmessage.RCode) func([]byte) []byte {
	return func(query []byte) []byte {
		var p dnsmessage.Parser
		h, err := p.Start(query)
		if err != nil {
			return nil
		}
		q, err := p.Question()
		if err != nil {
			return nil
		}

		resp, _ := NewDNSMessageBuilder().
			SetID(h.ID).
			SetResponse().
			SetRCode(rcode).
			AddQuestion(q.Name.String(), q.Type).
			Build()
		return resp
	}
}

// CountingHandler wraps a handler and counts calls.
type CountingHandler struct {
	Handler func([]byte) []byte
	mu      sync.Mutex
	count   int
}

// NewCountingHandler creates a new counting handler.
func NewCountingHandler(handler func([]byte) []byte) *CountingHandler {
	return &CountingHandler{Handler: handler}
}

func (h *CountingHandler) Handle(query []byte) []byte {
	h.mu.Lock()
	h.count++
	h.mu.Unlock()
	if h.Handler != nil {
		return h.Handler(query)
	}
	return nil
}

// Count returns the number of calls.
func (h *CountingHandler) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

// Reset resets the counter.
func (h *CountingHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count = 0
}

// TestAddr returns a test address for the given port.
func TestAddr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// GetFreePort finds a free port for testing.
func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
