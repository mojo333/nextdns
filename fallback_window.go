package main

import (
	"sync/atomic"
	"time"
)

// fallbackWindow tracks how long plain-DNS fallback stays allowed after
// startup or a network change. It is reset by the network-change goroutine and
// read on the query path, so the reset time is stored atomically.
//
// The reset time is kept as an offset from a fixed base rather than as Unix
// nanoseconds, because converting to Unix time drops the monotonic clock
// reading. Routers often boot with the wrong wall clock and have NTP step it
// later, and a backwards step would otherwise hold the window open, allowing
// unencrypted fallback for as long as the step.
type fallbackWindow struct {
	base   time.Time
	offset atomic.Int64 // time since base at the last reset, in nanoseconds
	length time.Duration
}

func newFallbackWindow(length time.Duration) *fallbackWindow {
	return &fallbackWindow{base: time.Now(), length: length}
}

// reset reopens the window from now.
func (w *fallbackWindow) reset() {
	w.offset.Store(int64(time.Since(w.base)))
}

// open reports whether fallback is still allowed. A negative elapsed time
// cannot happen on a monotonic clock, so it is treated as closed.
func (w *fallbackWindow) open() bool {
	elapsed := time.Since(w.base) - time.Duration(w.offset.Load())
	return elapsed >= 0 && elapsed < w.length
}
