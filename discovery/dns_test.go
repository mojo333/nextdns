package discovery

import (
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// TestDiscoveryDNS_IDMismatch_MaxRetries tests that DNS ID mismatch
// respects the maxRetries limit to prevent DoS via infinite loops.
// This is a regression test for the DoS fix in commit f465500.
func TestDiscoveryDNS_IDMismatch_MaxRetries(t *testing.T) {
	// Create a mock DNS server that sends multiple mismatched-ID responses
	// per query (simulating stale responses on the same UDP socket).
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().String()

	var mu sync.Mutex
	requestCount := 0

	go func() {
		buf := make([]byte, 512)
		for {
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}

			mu.Lock()
			requestCount++
			mu.Unlock()

			requestID := uint16(buf[0])<<8 | uint16(buf[1])

			// Send 6 responses with wrong IDs (maxRetries is 5)
			for i := 0; i < 6; i++ {
				resp := buildMinimalDNSResponse(requestID+uint16(i+1), buf[:n])
				conn.WriteToUDP(resp, clientAddr)
			}
		}
	}()

	// Attempt a PTR query - should fail after maxRetries
	start := time.Now()
	names, err := queryPTR(addr, net.ParseIP("192.168.1.1"), false)
	duration := time.Since(start)

	// Should return error due to max retries
	if err == nil {
		t.Error("Expected error due to max retries, got nil")
	}

	if err != nil && err.Error() != "max retries exceeded: DNS ID mismatch" {
		t.Errorf("Expected 'max retries exceeded' error, got: %v", err)
	}

	// Should have no results
	if len(names) > 0 {
		t.Errorf("Expected no results, got %d names", len(names))
	}

	// Verify it didn't take too long (should fail fast, not infinite loop)
	if duration > 10*time.Second {
		t.Errorf("Query took too long (%v), possible infinite loop", duration)
	}
}

// TestDiscoveryDNS_DoSProtection verifies protection against DoS attacks
// via malicious DNS responses with mismatched IDs.
func TestDiscoveryDNS_DoSProtection(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().String()

	var mu sync.Mutex
	totalRequests := 0

	go func() {
		buf := make([]byte, 512)
		for {
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}

			mu.Lock()
			totalRequests++
			mu.Unlock()

			requestID := uint16(buf[0])<<8 | uint16(buf[1])

			// Send 6 responses with wrong IDs per query
			for i := 0; i < 6; i++ {
				resp := buildMinimalDNSResponse(requestID+uint16(i+1), buf[:n])
				conn.WriteToUDP(resp, clientAddr)
			}
		}
	}()

	// Attempt multiple queries concurrently
	const numConcurrent = 10
	var wg sync.WaitGroup
	wg.Add(numConcurrent)

	start := time.Now()

	for i := 0; i < numConcurrent; i++ {
		go func(id int) {
			defer wg.Done()
			ip := net.ParseIP("192.168.1.1")
			_, err := queryPTR(addr, ip, false)
			if err == nil {
				t.Errorf("Goroutine %d: expected error, got nil", id)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	// All queries should complete in reasonable time (not stuck in infinite loops)
	if duration > 20*time.Second {
		t.Errorf("Concurrent queries took too long (%v), possible DoS vulnerability", duration)
	}
}

// TestDiscoveryDNS_ValidResponse tests normal operation with valid responses
func TestDiscoveryDNS_ValidResponse(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().String()

	var mu sync.Mutex
	requestCount := 0

	go func() {
		buf := make([]byte, 512)
		for {
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}

			mu.Lock()
			requestCount++
			mu.Unlock()

			requestID := uint16(buf[0])<<8 | uint16(buf[1])
			resp := buildPTRResponse(t, requestID, buf[:n], []string{"test.local."})
			conn.WriteToUDP(resp, clientAddr)
		}
	}()

	names, err := queryPTR(addr, net.ParseIP("192.168.1.1"), false)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(names) != 1 || names[0] != "test.local." {
		t.Errorf("Expected ['test.local.'], got %v", names)
	}

	mu.Lock()
	rc := requestCount
	mu.Unlock()
	if rc != 1 {
		t.Errorf("Expected 1 request, got %d", rc)
	}
}

// TestDiscoveryDNS_EventualSuccess tests that retries work when
// server returns mismatched IDs initially but then succeeds.
func TestDiscoveryDNS_EventualSuccess(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().String()

	go func() {
		buf := make([]byte, 512)
		for {
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}

			requestID := uint16(buf[0])<<8 | uint16(buf[1])

			// Send 2 wrong-ID responses first
			for i := 0; i < 2; i++ {
				resp := buildMinimalDNSResponse(requestID+uint16(i+1), buf[:n])
				conn.WriteToUDP(resp, clientAddr)
			}
			// Then send correct response with PTR records
			resp := buildPTRResponse(t, requestID, buf[:n], []string{"eventual.local."})
			conn.WriteToUDP(resp, clientAddr)
		}
	}()

	names, err := queryPTR(addr, net.ParseIP("192.168.1.1"), false)

	if err != nil {
		t.Fatalf("Expected eventual success, got error: %v", err)
	}

	if len(names) != 1 || names[0] != "eventual.local." {
		t.Errorf("Expected ['eventual.local.'], got %v", names)
	}
}

// buildMinimalDNSResponse builds a minimal DNS response header with the given ID.
func buildMinimalDNSResponse(id uint16, query []byte) []byte {
	response := make([]byte, 12)
	response[0] = byte(id >> 8)
	response[1] = byte(id)
	response[2] = 0x81 // Response, Authoritative
	response[3] = 0x80 // Recursion available, no error
	return response
}

// buildPTRResponse builds a proper DNS response with PTR answer records.
func buildPTRResponse(t *testing.T, id uint16, query []byte, names []string) []byte {
	t.Helper()

	// Parse the question from the query
	var p dnsmessage.Parser
	_, err := p.Start(query)
	if err != nil {
		t.Fatalf("Failed to parse query: %v", err)
	}
	q, err := p.Question()
	if err != nil {
		t.Fatalf("Failed to parse question: %v", err)
	}

	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 id,
			Response:           true,
			Authoritative:      true,
			RecursionAvailable: true,
		},
		Questions: []dnsmessage.Question{q},
	}

	for _, name := range names {
		ptrName, err := dnsmessage.NewName(name)
		if err != nil {
			t.Fatalf("Failed to create PTR name %q: %v", name, err)
		}
		msg.Answers = append(msg.Answers, dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{
				Name:  q.Name,
				Type:  dnsmessage.TypePTR,
				Class: dnsmessage.ClassINET,
				TTL:   300,
			},
			Body: &dnsmessage.PTRResource{PTR: ptrName},
		})
	}

	packed, err := msg.Pack()
	if err != nil {
		t.Fatalf("Failed to pack DNS response: %v", err)
	}
	return packed
}
