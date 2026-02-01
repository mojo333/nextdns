package discovery

import (
	"net"
	"sync"
	"testing"
	"time"
)

// TestDiscoveryDNS_IDMismatch_MaxRetries tests that DNS ID mismatch
// respects the maxRetries limit to prevent DoS via infinite loops.
// This is a regression test for the DoS fix in commit f465500.
func TestDiscoveryDNS_IDMismatch_MaxRetries(t *testing.T) {
	// Create a mock DNS server that always returns mismatched IDs
	mismatchServer := &mockDNSServer{
		respondWithMismatchedID: true,
	}

	addr, cleanup := startMockDNSServer(t, mismatchServer)
	defer cleanup()

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
	// With 5 retries and network timeouts, should complete in < 10 seconds
	if duration > 10*time.Second {
		t.Errorf("Query took too long (%v), possible infinite loop", duration)
	}

	// Verify it actually retried (should have made multiple attempts)
	if mismatchServer.getRequestCount() < 2 {
		t.Errorf("Expected multiple retries, got %d requests", mismatchServer.getRequestCount())
	}

	// Verify it didn't retry infinitely (should stop at maxRetries=5)
	if mismatchServer.getRequestCount() > 10 {
		t.Errorf("Too many retries (%d), should stop at maxRetries",
			mismatchServer.getRequestCount())
	}
}

// TestDiscoveryDNS_DoSProtection verifies protection against DoS attacks
// via malicious DNS responses with mismatched IDs.
func TestDiscoveryDNS_DoSProtection(t *testing.T) {
	mismatchServer := &mockDNSServer{
		respondWithMismatchedID: true,
	}

	addr, cleanup := startMockDNSServer(t, mismatchServer)
	defer cleanup()

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

	// Total requests should be bounded (each query makes at most ~5 retries)
	totalRequests := mismatchServer.getRequestCount()
	expectedMax := numConcurrent * 10 // Some tolerance for timing

	if totalRequests > expectedMax {
		t.Errorf("Too many total requests (%d), expected max ~%d. Possible retry limit bypass",
			totalRequests, expectedMax)
	}
}

// TestDiscoveryDNS_ValidResponse tests normal operation with valid responses
func TestDiscoveryDNS_ValidResponse(t *testing.T) {
	validServer := &mockDNSServer{
		respondWithMismatchedID: false,
		respondWithNames:        []string{"test.local"},
	}

	addr, cleanup := startMockDNSServer(t, validServer)
	defer cleanup()

	names, err := queryPTR(addr, net.ParseIP("192.168.1.1"), false)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(names) != 1 || names[0] != "test.local" {
		t.Errorf("Expected ['test.local'], got %v", names)
	}

	// Should only make one request for valid response
	if validServer.getRequestCount() != 1 {
		t.Errorf("Expected 1 request, got %d", validServer.getRequestCount())
	}
}

// TestDiscoveryDNS_EventualSuccess tests that retries work when
// server returns mismatched IDs initially but then succeeds.
func TestDiscoveryDNS_EventualSuccess(t *testing.T) {
	server := &mockDNSServer{
		respondWithMismatchedID: true,
		succeedAfterAttempts:    3, // Fail twice, succeed on 3rd attempt
		respondWithNames:        []string{"eventual.local"},
	}

	addr, cleanup := startMockDNSServer(t, server)
	defer cleanup()

	names, err := queryPTR(addr, net.ParseIP("192.168.1.1"), false)

	if err != nil {
		t.Fatalf("Expected eventual success, got error: %v", err)
	}

	if len(names) != 1 || names[0] != "eventual.local" {
		t.Errorf("Expected ['eventual.local'], got %v", names)
	}

	// Should have made 3 attempts
	attempts := server.getRequestCount()
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

// Mock DNS server for testing
type mockDNSServer struct {
	mu                      sync.Mutex
	requestCount            int
	respondWithMismatchedID bool
	succeedAfterAttempts    int
	respondWithNames        []string
}

func (m *mockDNSServer) getRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requestCount
}

func (m *mockDNSServer) incrementRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestCount++
	return m.requestCount
}

func (m *mockDNSServer) shouldMismatchID() bool {
	count := m.incrementRequestCount()
	if m.succeedAfterAttempts > 0 && count >= m.succeedAfterAttempts {
		return false
	}
	return m.respondWithMismatchedID
}

func startMockDNSServer(t *testing.T, server *mockDNSServer) (string, func()) {
	// Create UDP listener
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}

	addr := conn.LocalAddr().String()

	// Start server goroutine
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 512)
		for {
			select {
			case <-done:
				return
			default:
			}

			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				continue
			}

			// Parse request to get ID
			if n < 12 {
				continue
			}

			requestID := uint16(buf[0])<<8 | uint16(buf[1])
			responseID := requestID

			// Optionally mismatch the ID
			if server.shouldMismatchID() {
				responseID = requestID + 1 // Wrong ID
			}

			// Build minimal DNS response
			response := make([]byte, 12)
			response[0] = byte(responseID >> 8)
			response[1] = byte(responseID)
			response[2] = 0x81 // Response, Authoritative
			response[3] = 0x80 // No error

			// Send response
			conn.WriteToUDP(response, clientAddr)
		}
	}()

	cleanup := func() {
		close(done)
		conn.Close()
	}

	return addr, cleanup
}
