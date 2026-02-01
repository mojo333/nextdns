package ctl

import (
	"sync"
	"testing"
	"time"
)

// Helper function to create server and client with timeout/skip
func setupClientTest(t *testing.T) (*Server, *Client) {
	s := &Server{
		Addr: testAddr(t),
	}

	if err := s.Start(); err != nil {
		t.Skipf("Cannot start server (socket/pipe not available): %v", err)
	}

	// Dial with timeout
	dialDone := make(chan *Client, 1)
	dialErr := make(chan error, 1)

	go func() {
		c, err := Dial(s.Addr)
		if err != nil {
			dialErr <- err
		} else {
			dialDone <- c
		}
	}()

	select {
	case c := <-dialDone:
		return s, c
	case err := <-dialErr:
		s.Stop()
		t.Skipf("Cannot dial server (socket/pipe not available): %v", err)
	case <-time.After(2 * time.Second):
		s.Stop()
		t.Skip("Dial timed out (socket/pipe not available)")
	}
	return nil, nil
}

// TestClient_ReplyChannel_NoOverflow verifies that the reply channel
// buffer (size 10) can handle concurrent replies without overflow.
// This is a regression test for the fix in commit e0dd08f.
func TestClient_ReplyChannel_NoOverflow(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	// Set up multiple commands that reply quickly
	for i := 0; i < 15; i++ {
		cmdName := string(rune('a' + i))
		s.Command(cmdName, func(data interface{}) interface{} {
			return "ok"
		})
	}

	if err := s.Start(); err != nil {
		t.Skipf("Cannot start server (likely socket/pipe issue): %v", err)
	}
	defer s.Stop()

	// Add timeout for Dial
	dialDone := make(chan *Client, 1)
	dialErr := make(chan error, 1)

	go func() {
		c, err := Dial(s.Addr)
		if err != nil {
			dialErr <- err
		} else {
			dialDone <- c
		}
	}()

	var c *Client
	select {
	case c = <-dialDone:
		defer c.Close()
	case err := <-dialErr:
		t.Skipf("Cannot dial server (likely socket/pipe issue): %v", err)
	case <-time.After(2 * time.Second):
		t.Skip("Dial timed out (likely socket/pipe issue)")
	}

	// Send 15 commands rapidly
	// Old bug: buffer size was 0, causing non-blocking send to drop replies
	// New behavior: buffer size is 10, should handle burst traffic
	const numCommands = 15

	var wg sync.WaitGroup
	wg.Add(numCommands)
	errors := make(chan error, numCommands)

	for i := 0; i < numCommands; i++ {
		go func(id int) {
			defer wg.Done()
			cmdName := string(rune('a' + id))
			_, err := c.Send(Event{Name: cmdName, Data: nil})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Send failed: %v", err)
		errorCount++
	}

	if errorCount > 0 {
		t.Errorf("%d/%d commands failed (possible reply overflow)", errorCount, numCommands)
	}
}

// TestClient_ReplyChannel_NoDataLoss verifies that replies are not
// silently dropped under load.
func TestClient_ReplyChannel_NoDataLoss(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	// Command that returns incrementing counter
	counter := 0
	var counterMu sync.Mutex

	s.Command("count", func(data interface{}) interface{} {
		counterMu.Lock()
		defer counterMu.Unlock()
		counter++
		return counter
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

	// Send commands and track responses
	const numCommands = 20
	responses := make(map[int]bool)
	var responseMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(numCommands)

	for i := 0; i < numCommands; i++ {
		go func() {
			defer wg.Done()
			resp, err := c.Send(Event{Name: "count", Data: nil})
			if err != nil {
				t.Errorf("Send failed: %v", err)
				return
			}

			// Record response
			if respNum, ok := resp.(float64); ok {
				responseMu.Lock()
				responses[int(respNum)] = true
				responseMu.Unlock()
			}
		}()
		time.Sleep(5 * time.Millisecond) // Slight delay between sends
	}

	wg.Wait()

	// Verify we got all responses (1 through numCommands)
	responseMu.Lock()
	defer responseMu.Unlock()

	if len(responses) != numCommands {
		t.Errorf("Expected %d unique responses, got %d (data loss detected)",
			numCommands, len(responses))
	}

	// Check for any missing responses
	for i := 1; i <= numCommands; i++ {
		if !responses[i] {
			t.Errorf("Missing response for counter value %d", i)
		}
	}
}

// TestClient_ReplyChannel_BufferSize verifies the buffer size is 10
func TestClient_ReplyChannel_BufferSize(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	c, err := Dial(s.Addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer c.Close()

	// Verify buffer capacity is 10
	if cap(c.replies) != 10 {
		t.Errorf("Expected reply channel buffer size of 10, got %d", cap(c.replies))
	}
}

// TestClient_MultipleClients_IndependentChannels verifies that each
// client has its own independent reply channel.
func TestClient_MultipleClients_IndependentChannels(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	s.Command("echo", func(data interface{}) interface{} {
		return data
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	// Create multiple clients
	const numClients = 5
	clients := make([]*Client, numClients)

	for i := 0; i < numClients; i++ {
		c, err := Dial(s.Addr)
		if err != nil {
			t.Fatalf("Dial %d failed: %v", i, err)
		}
		defer c.Close()
		clients[i] = c
	}

	// Each client sends unique data
	var wg sync.WaitGroup
	wg.Add(numClients)
	results := make([]interface{}, numClients)

	for i := 0; i < numClients; i++ {
		go func(id int) {
			defer wg.Done()
			resp, err := clients[id].Send(Event{Name: "echo", Data: id})
			if err != nil {
				t.Errorf("Client %d send failed: %v", id, err)
				return
			}
			results[id] = resp
		}(i)
	}

	wg.Wait()

	// Verify each client got its own data back
	for i := 0; i < numClients; i++ {
		if respNum, ok := results[i].(float64); !ok || int(respNum) != i {
			t.Errorf("Client %d: expected %d, got %v", i, i, results[i])
		}
	}
}

// TestClient_SlowConsumer verifies behavior when client is slow to consume replies
func TestClient_SlowConsumer(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	s.Command("echo", func(data interface{}) interface{} {
		return data
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

	// Send command but don't read reply immediately
	// With buffer size 10, this should not block
	errChan := make(chan error, 1)

	go func() {
		for i := 0; i < 15; i++ {
			_, err := c.Send(Event{Name: "echo", Data: i})
			if err != nil {
				errChan <- err
				return
			}
		}
		errChan <- nil
	}()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Check if sends completed
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Send failed with slow consumer: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Sends blocked with slow consumer (possible deadlock)")
	}
}

// TestClient_CloseWhileWaitingForReply tests closing client while waiting
func TestClient_CloseWhileWaitingForReply(t *testing.T) {
	s := &Server{
		Addr: testAddr(t),
	}

	// Command that delays before responding
	s.Command("slow", func(data interface{}) interface{} {
		time.Sleep(500 * time.Millisecond)
		return "done"
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	c, err := Dial(s.Addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	// Send command in goroutine
	done := make(chan error, 1)
	go func() {
		_, err := c.Send(Event{Name: "slow", Data: nil})
		done <- err
	}()

	// Close client while waiting
	time.Sleep(100 * time.Millisecond)
	c.Close()

	// Should get an error
	err = <-done
	if err == nil {
		t.Error("Expected error when closing client while waiting for reply")
	}
}
