package main

import (
	"testing"
	"time"
)

func TestFallbackWindow_OpenAfterResetClosedAfterWindow(t *testing.T) {
	// Arrange: a window whose last reset was 11 minutes ago.
	w := newFallbackWindow(10 * time.Minute)
	w.base = w.base.Add(-11 * time.Minute)

	// Act and Assert
	if w.open() {
		t.Fatal("open() = true 11 minutes after reset, want false")
	}
	w.reset()
	if !w.open() {
		t.Fatal("open() = false right after reset, want true")
	}
}

func TestFallbackWindow_NegativeElapsedIsClosed(t *testing.T) {
	// Arrange: a reset recorded in the future relative to the base, which is
	// what a wall-clock step backwards produced before this type existed.
	w := newFallbackWindow(10 * time.Minute)
	w.offset.Store(int64(time.Hour))

	// Act and Assert
	if w.open() {
		t.Fatal("open() = true with negative elapsed time, want false")
	}
}

func TestFallbackWindow_UsesMonotonicClock(t *testing.T) {
	// Arrange
	w := newFallbackWindow(10 * time.Minute)

	// Assert: the base keeps its monotonic reading, so wall-clock steps from
	// NTP after boot cannot stretch or shrink the window.
	if w.base.Round(0) == w.base {
		t.Fatal("base has no monotonic clock reading")
	}
}
