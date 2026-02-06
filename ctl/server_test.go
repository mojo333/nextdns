package ctl

import (
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestServer_ConcurrentClientManagement tests that concurrent client
// add/remove operations don't cause race conditions or memory leaks.
func TestServer_ConcurrentClientManagement(t *testing.T) {
	// Track connection/disconnection counts
	var connected, disconnected int32

	s := &Server{
		Addr: testAddr(t),
	}

	// Set callbacks BEFORE starting to avoid race conditions
	s.OnConnect = func(c net.Conn) {
		atomic.AddInt32(&connected, 1)
	}

	s.OnDisconnect = func(c net.Conn) {
		atomic.AddInt32(&disconnected, 1)
	}

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	// Spawn many concurrent clients
	const numClients = 50
	var wg sync.WaitGroup
	wg.Add(numClients)

	for i := 0; i < numClients; i++ {
		go func(id int) {
			defer wg.Done()

			c, err := Dial(s.Addr)
			if err != nil {
				t.Errorf("Client %d: dial failed: %v", id, err)
				return
			}
			defer c.Close()

			// Send a few events
			for j := 0; j < 3; j++ {
				_, err := c.Send(Event{Name: "test", Data: id})
				if err != nil {
					return // Connection closed
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Give time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify all clients connected and disconnected
	if atomic.LoadInt32(&connected) != numClients {
		t.Errorf("Expected %d connections, got %d", numClients, connected)
	}
	if atomic.LoadInt32(&disconnected) != numClients {
		t.Errorf("Expected %d disconnections, got %d", numClients, disconnected)
	}

	// Verify all clients were removed
	s.mu.Lock()
	clientCount := len(s.clients)
	s.mu.Unlock()

	if clientCount != 0 {
		t.Errorf("Expected 0 clients remaining, got %d (memory leak)", clientCount)
	}
}

// TestServer_RemoveClient_NoMemoryLeak verifies that removeClient
// correctly removes the client from the slice without memory leaks.
func TestServer_RemoveClient_NoMemoryLeak(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	// Create multiple clients
	const numClients = 10
	clients := make([]*Client, numClients)

	for i := 0; i < numClients; i++ {
		c, err := Dial(s.Addr)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		clients[i] = c
	}

	// Wait for all to connect
	time.Sleep(100 * time.Millisecond)

	s.mu.Lock()
	initialCount := len(s.clients)
	s.mu.Unlock()

	if initialCount != numClients {
		t.Fatalf("Expected %d clients, got %d", numClients, initialCount)
	}

	// Close clients one by one and verify removal
	for i, c := range clients {
		c.Close()
		time.Sleep(50 * time.Millisecond)

		s.mu.Lock()
		currentCount := len(s.clients)
		s.mu.Unlock()

		expectedCount := numClients - i - 1
		if currentCount != expectedCount {
			t.Errorf("After closing client %d: expected %d clients, got %d",
				i, expectedCount, currentCount)
		}
	}

	// Final verification
	s.mu.Lock()
	finalCount := len(s.clients)
	s.mu.Unlock()

	if finalCount != 0 {
		t.Errorf("Expected 0 clients after all closed, got %d", finalCount)
	}
}

// TestServer_ClientTracking_RaceDetector runs concurrent operations
// that would trigger race detector if mutex is not properly used.
// Run with: go test -race
func TestServer_ClientTracking_RaceDetector(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	// Concurrently connect, disconnect, and broadcast
	const duration = 500 * time.Millisecond
	var wg sync.WaitGroup
	wg.Add(3)

	// Goroutine 1: Constantly connect/disconnect
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			c, err := Dial(s.Addr)
			if err != nil {
				continue
			}
			c.Close()
		}
	}()

	// Goroutine 2: Constantly broadcast
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			s.Broadcast(Event{Name: "test", Data: "hello"})
			time.Sleep(time.Millisecond)
		}
	}()

	// Goroutine 3: Constantly read client list
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(duration)
		for time.Now().Before(deadline) {
			s.mu.Lock()
			_ = len(s.clients)
			s.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestServer_CommandHandling tests that commands are handled correctly
func TestServer_CommandHandling(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	var receivedData interface{}
	s.Command("echo", func(data interface{}) interface{} {
		receivedData = data
		return map[string]string{"reply": "pong"}
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	c, err := Dial(s.Addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer c.Close()

	// Send command
	response, err := c.Send(Event{Name: "echo", Data: "test-data"})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Verify command was executed
	if receivedData != "test-data" {
		t.Errorf("Expected command to receive 'test-data', got %v", receivedData)
	}

	// Verify response
	respMap, ok := response.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map response, got %T", response)
	}

	if respMap["reply"] != "pong" {
		t.Errorf("Expected reply 'pong', got %v", respMap["reply"])
	}
}

// TestServer_Broadcast tests broadcasting to multiple clients
func TestServer_Broadcast(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	// Create multiple raw connections (not Dial, to avoid readLoop competing
	// with the test's own reader goroutine on the same connection)
	const numClients = 5
	conns := make([]net.Conn, numClients)
	receivers := make([]chan Event, numClients)

	for i := 0; i < numClients; i++ {
		c, err := dial(s.Addr)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		defer c.Close()
		conns[i] = c
		receivers[i] = make(chan Event, 10)

		// Start goroutine to receive broadcasts
		go func(idx int, conn net.Conn) {
			dec := json.NewDecoder(conn)
			for {
				var e Event
				if err := dec.Decode(&e); err != nil {
					return
				}
				if !e.Reply {
					receivers[idx] <- e
				}
			}
		}(i, c)
	}

	// Wait for connections
	time.Sleep(100 * time.Millisecond)

	// Broadcast event
	broadcastEvent := Event{Name: "broadcast-test", Data: "hello-all"}
	if err := s.Broadcast(broadcastEvent); err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	// Verify all clients received the broadcast
	timeout := time.After(time.Second)
	for i := 0; i < numClients; i++ {
		select {
		case e := <-receivers[i]:
			if e.Name != "broadcast-test" {
				t.Errorf("Client %d: expected 'broadcast-test', got '%s'", i, e.Name)
			}
			if e.Data != "hello-all" {
				t.Errorf("Client %d: expected 'hello-all', got %v", i, e.Data)
			}
		case <-timeout:
			t.Errorf("Client %d: did not receive broadcast within timeout", i)
		}
	}
}

// testAddr generates a unique test address for the server
func testAddr(t *testing.T) string {
	return "nextdns-test-" + t.Name()
}
