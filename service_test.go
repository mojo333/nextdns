package main

import (
	"strings"
	"testing"
	"time"
)

// TestService_UnknownCommand_NoPanic verifies that unknown commands
// return an error instead of panicking.
func TestService_UnknownCommand_NoPanic(t *testing.T) {
	unknownCommands := []string{
		"invalid",
		"foo",
		"bar",
		"hack",
	}

	for _, cmd := range unknownCommands {
		t.Run(cmd, func(t *testing.T) {
			// Ensure it doesn't panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Command '%s' caused panic: %v", cmd, r)
				}
			}()

			// Run with timeout to prevent hangs
			done := make(chan error, 1)
			go func() {
				done <- svc([]string{cmd})
			}()

			select {
			case err := <-done:
				// Should return an error, not panic
				if err == nil {
					t.Errorf("Expected error for unknown command '%s', got nil", cmd)
				}
				// Error should mention it's unknown
				if !strings.Contains(err.Error(), "unknown command") {
					t.Errorf("Expected 'unknown command' error, got: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Errorf("Command '%s' timed out", cmd)
			}
		})
	}
}

// TestService_PanicRecovery tests that service calls don't cause panics
// This is a regression test for the bug fix in commit 8f10626
func TestService_PanicRecovery(t *testing.T) {
	// List of potentially problematic inputs
	testCases := []struct {
		name string
		args []string
		expectError bool
	}{
		{"unknown", []string{"unknown-command"}, true},
		{"special chars", []string{"!@#$%"}, true},
		{"very long", []string{strings.Repeat("a", 1000)}, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Args %v caused panic: %v", tc.args, r)
				}
			}()

			// Run with timeout
			done := make(chan error, 1)
			go func() {
				done <- svc(tc.args)
			}()

			select {
			case err := <-done:
				if tc.expectError && err == nil {
					t.Errorf("Expected error, got nil")
				}
			case <-time.After(2 * time.Second):
				t.Errorf("Test timed out")
			}
		})
	}
}

// TestService_ErrorHandling tests that commands handle errors gracefully
func TestService_ErrorHandling(t *testing.T) {
	t.Run("unknown command returns error", func(t *testing.T) {
		err := svc([]string{"nonexistent"})
		if err == nil {
			t.Error("Expected error for unknown command")
		}
		if !strings.Contains(err.Error(), "unknown command") {
			t.Errorf("Expected 'unknown command' error, got: %v", err)
		}
	})
}
