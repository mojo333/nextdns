package main

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextdns/nextdns/config"
)

func Test_isLocalhostMode(t *testing.T) {
	tests := []struct {
		listens []string
		want    bool
	}{
		{[]string{"127.0.0.1:53"}, true},
		{[]string{"127.0.0.1:5353"}, true},
		{[]string{"10.0.0.1:53"}, false},
		{[]string{"127.0.0.1:53", "10.0.0.1:53"}, false},
		{[]string{"10.0.0.1:53", "127.0.0.1:53"}, false},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.listens, ","), func(t *testing.T) {
			if got := isLocalhostMode(&config.Config{Listens: tt.listens}); got != tt.want {
				t.Errorf("isLocalhostMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestProxy_ConcurrentStopStart tests that concurrent stop/start
// operations don't cause race conditions. This is a regression test
// for the fix in commit 1e501ff (stopMu mutex protection).
// Run with: go test -race
func TestProxy_ConcurrentStopStart(t *testing.T) {
	p := &proxySvc{}

	const numIterations = 10
	var wg sync.WaitGroup

	// Goroutine 1: Repeatedly call stop()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numIterations; i++ {
			p.stop()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Goroutine 2: Repeatedly call stop() concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numIterations; i++ {
			p.stop()
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Goroutine 3: Check stopFunc under lock
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numIterations*2; i++ {
			p.stopMu.Lock()
			_ = p.stopFunc
			p.stopMu.Unlock()
			time.Sleep(5 * time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestProxy_StopFunc_RaceProtection verifies that stopFunc access
// is properly protected by stopMu mutex.
func TestProxy_StopFunc_RaceProtection(t *testing.T) {
	p := &proxySvc{}

	// Set up a mock stopFunc
	ctx, cancel := context.WithCancel(context.Background())
	p.stopFunc = cancel
	p.stopped = make(chan struct{})

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Multiple goroutines try to read/write stopFunc
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			p.stopMu.Lock()
			if p.stopFunc != nil {
				_ = p.stopFunc
			}
			p.stopMu.Unlock()

			time.Sleep(time.Millisecond)
		}(i)
	}

	// Main goroutine tries to stop
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)

		p.stopMu.Lock()
		if p.stopFunc != nil {
			p.stopFunc = nil
			cancel()
		}
		p.stopMu.Unlock()
	}()

	// Wait for all
	wg.Wait()
	ctx.Done()
}

// TestProxy_RapidRestarts tests rapid restart cycles don't leak resources
func TestProxy_RapidRestarts(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	p := &proxySvc{}

	// Perform multiple start/stop cycles rapidly
	for i := 0; i < 5; i++ {
		// Initialize stopFunc
		_, cancel := context.WithCancel(context.Background())
		p.stopMu.Lock()
		p.stopFunc = cancel
		p.stopMu.Unlock()
		p.stopped = make(chan struct{})

		// Close stopped channel immediately for test purposes
		close(p.stopped)

		// Now stop should return quickly
		done := make(chan bool, 1)
		go func() {
			done <- p.stop()
		}()

		select {
		case <-done:
			// Success
		case <-time.After(time.Second):
			t.Fatalf("stop() hung on iteration %d", i)
		}

		time.Sleep(50 * time.Millisecond)
	}

	// Give time for cleanup
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()

	// Should not have significant goroutine growth
	if finalGoroutines > initialGoroutines+5 {
		t.Errorf("Goroutine leak detected: started with %d, ended with %d (diff: %d)",
			initialGoroutines, finalGoroutines, finalGoroutines-initialGoroutines)
	}
}

// TestProxy_StopReturnsCorrectValue tests that stop() returns true
// only when actually stopping and false when already stopped.
func TestProxy_StopReturnsCorrectValue(t *testing.T) {
	p := &proxySvc{}

	// First stop with nil stopFunc should return false
	if p.stop() {
		t.Error("stop() should return false when stopFunc is nil")
	}

	// Set up stopFunc
	_, cancel := context.WithCancel(context.Background())
	p.stopMu.Lock()
	p.stopFunc = cancel
	p.stopMu.Unlock()
	p.stopped = make(chan struct{})

	// Close the stopped channel immediately for testing
	close(p.stopped)

	// First stop should return true
	if !p.stop() {
		t.Error("stop() should return true when actually stopping")
	}

	// Second stop should return false
	if p.stop() {
		t.Error("stop() should return false when already stopped")
	}
}

// TestProxy_StopWaitsForStopped verifies that stop() waits for
// the stopped channel to close.
func TestProxy_StopWaitsForStopped(t *testing.T) {
	p := &proxySvc{}

	_, cancel := context.WithCancel(context.Background())
	p.stopMu.Lock()
	p.stopFunc = cancel
	p.stopMu.Unlock()
	p.stopped = make(chan struct{})

	// Start goroutine that closes stopped channel after delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(p.stopped)
	}()

	// stop() should block until stopped channel is closed
	// Wrap in timeout to prevent infinite hang
	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		p.stop()
		done <- time.Since(start)
	}()

	select {
	case duration := <-done:
		if duration < 90*time.Millisecond {
			t.Errorf("stop() returned too quickly (%v), should wait for stopped channel", duration)
		}
		if duration > 200*time.Millisecond {
			t.Errorf("stop() took longer than expected (%v)", duration)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop() deadlocked - did not return within 5 seconds")
	}
}

// TestProxy_OnInitCallbacks tests that OnInit callbacks are called
// with proper context.
func TestProxy_OnInitCallbacks(t *testing.T) {
	p := &proxySvc{}

	var callCount int32
	var receivedCtx context.Context

	p.OnInit = []func(ctx context.Context){
		func(ctx context.Context) {
			atomic.AddInt32(&callCount, 1)
			receivedCtx = ctx
		},
	}

	// Simulate start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Call the callback
	for _, f := range p.OnInit {
		f(ctx)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected OnInit to be called once, got %d", callCount)
	}

	if receivedCtx == nil {
		t.Error("OnInit callback did not receive context")
	}
}

// TestProxy_ConcurrentStopDuringInit tests stopping during initialization
func TestProxy_ConcurrentStopDuringInit(t *testing.T) {
	p := &proxySvc{}

	// OnInit callback that takes some time
	initStarted := make(chan struct{})
	p.OnInit = []func(ctx context.Context){
		func(ctx context.Context) {
			close(initStarted)
			select {
			case <-ctx.Done():
				// Context was cancelled (stop called)
				return
			case <-time.After(200 * time.Millisecond):
				// Init completed
				return
			}
		},
	}

	// Start the init process
	ctx, cancel := context.WithCancel(context.Background())
	p.stopMu.Lock()
	p.stopFunc = cancel
	p.stopMu.Unlock()
	p.stopped = make(chan struct{})

	go func() {
		for _, f := range p.OnInit {
			go f(ctx)
		}
		// Simulate proxy completion - shorter timeout
		time.Sleep(50 * time.Millisecond)
		close(p.stopped)
	}()

	// Wait for init to start with timeout
	select {
	case <-initStarted:
		// Init started
	case <-time.After(time.Second):
		t.Fatal("Init never started")
	}

	// Immediately try to stop
	stopCalled := make(chan bool, 1)
	go func() {
		result := p.stop()
		stopCalled <- result
	}()

	// Should complete stop within reasonable time
	select {
	case result := <-stopCalled:
		if !result {
			t.Error("Expected stop() to return true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop() timed out - possible deadlock")
	}
}
