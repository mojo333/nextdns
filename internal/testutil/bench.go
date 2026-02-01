package testutil

import (
	"sync"
	"testing"
	"time"
)

// BufferPool is a generic buffer pool for benchmarking.
type BufferPool struct {
	pool sync.Pool
	size int
}

// NewBufferPool creates a new buffer pool with fixed size buffers.
func NewBufferPool(size int) *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		},
		size: size,
	}
}

// Get retrieves a buffer from the pool.
func (p *BufferPool) Get() []byte {
	return p.pool.Get().([]byte)
}

// Put returns a buffer to the pool.
func (p *BufferPool) Put(buf []byte) {
	p.pool.Put(buf)
}

// TieredBufferPool manages multiple buffer pools of different sizes.
type TieredBufferPool struct {
	small  *BufferPool // 512 bytes
	medium *BufferPool // 4KB
	large  *BufferPool // 65KB
}

// NewTieredBufferPool creates a new tiered buffer pool.
func NewTieredBufferPool() *TieredBufferPool {
	return &TieredBufferPool{
		small:  NewBufferPool(512),
		medium: NewBufferPool(4096),
		large:  NewBufferPool(65536),
	}
}

// Get returns a buffer of appropriate size.
func (p *TieredBufferPool) Get(size int) []byte {
	switch {
	case size <= 512:
		return p.small.Get()
	case size <= 4096:
		return p.medium.Get()
	default:
		return p.large.Get()
	}
}

// Put returns a buffer to the appropriate pool.
func (p *TieredBufferPool) Put(buf []byte) {
	switch cap(buf) {
	case 512:
		p.small.Put(buf)
	case 4096:
		p.medium.Put(buf)
	case 65536:
		p.large.Put(buf)
	}
}

// BenchmarkBufferPool benchmarks buffer pool operations.
func BenchmarkBufferPool(b *testing.B, pool *BufferPool) {
	b.Run("Get", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := pool.Get()
			pool.Put(buf)
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := pool.Get()
				pool.Put(buf)
			}
		})
	})
}

// BenchmarkTieredPool benchmarks tiered buffer pool.
func BenchmarkTieredPool(b *testing.B, pool *TieredBufferPool) {
	sizes := []int{256, 1024, 8192}

	for _, size := range sizes {
		b.Run(string(rune(size)), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				buf := pool.Get(size)
				pool.Put(buf)
			}
		})
	}
}

// AllocationTracker tracks memory allocations during a test.
type AllocationTracker struct {
	before testing.MemStats
	after  testing.MemStats
}

// NewAllocationTracker creates a new allocation tracker.
func NewAllocationTracker() *AllocationTracker {
	at := &AllocationTracker{}
	testing.ReadMemStats(&at.before)
	return at
}

// Stop stops tracking and returns allocation stats.
func (at *AllocationTracker) Stop() (allocs uint64, bytes uint64) {
	testing.ReadMemStats(&at.after)
	return at.after.Mallocs - at.before.Mallocs,
		at.after.TotalAlloc - at.before.TotalAlloc
}

// ThroughputMeasurer measures throughput (ops/sec).
type ThroughputMeasurer struct {
	start time.Time
	count uint64
}

// NewThroughputMeasurer creates a new throughput measurer.
func NewThroughputMeasurer() *ThroughputMeasurer {
	return &ThroughputMeasurer{
		start: time.Now(),
	}
}

// Inc increments the operation counter.
func (tm *ThroughputMeasurer) Inc() {
	tm.count++
}

// Add adds multiple operations to the counter.
func (tm *ThroughputMeasurer) Add(n uint64) {
	tm.count += n
}

// Rate returns operations per second.
func (tm *ThroughputMeasurer) Rate() float64 {
	elapsed := time.Since(tm.start).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(tm.count) / elapsed
}

// Total returns total operations.
func (tm *ThroughputMeasurer) Total() uint64 {
	return tm.count
}

// Elapsed returns elapsed time.
func (tm *ThroughputMeasurer) Elapsed() time.Duration {
	return time.Since(tm.start)
}

// BenchmarkHelper provides common benchmarking utilities.
type BenchmarkHelper struct {
	b *testing.B
}

// NewBenchmarkHelper creates a new benchmark helper.
func NewBenchmarkHelper(b *testing.B) *BenchmarkHelper {
	return &BenchmarkHelper{b: b}
}

// ResetTimer resets the benchmark timer.
func (bh *BenchmarkHelper) ResetTimer() {
	bh.b.ResetTimer()
}

// ReportAllocs enables allocation reporting.
func (bh *BenchmarkHelper) ReportAllocs() {
	bh.b.ReportAllocs()
}

// SetBytes sets the number of bytes processed per operation.
func (bh *BenchmarkHelper) SetBytes(n int64) {
	bh.b.SetBytes(n)
}

// RunParallel runs the benchmark in parallel.
func (bh *BenchmarkHelper) RunParallel(body func()) {
	bh.b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			body()
		}
	})
}

// LatencyMeasurer measures operation latencies.
type LatencyMeasurer struct {
	mu        sync.Mutex
	latencies []time.Duration
}

// NewLatencyMeasurer creates a new latency measurer.
func NewLatencyMeasurer() *LatencyMeasurer {
	return &LatencyMeasurer{
		latencies: make([]time.Duration, 0, 1000),
	}
}

// Measure measures the latency of an operation.
func (lm *LatencyMeasurer) Measure(fn func()) {
	start := time.Now()
	fn()
	latency := time.Since(start)
	lm.mu.Lock()
	lm.latencies = append(lm.latencies, latency)
	lm.mu.Unlock()
}

// Average returns the average latency.
func (lm *LatencyMeasurer) Average() time.Duration {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if len(lm.latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, l := range lm.latencies {
		total += l
	}
	return total / time.Duration(len(lm.latencies))
}

// Min returns the minimum latency.
func (lm *LatencyMeasurer) Min() time.Duration {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if len(lm.latencies) == 0 {
		return 0
	}
	min := lm.latencies[0]
	for _, l := range lm.latencies[1:] {
		if l < min {
			min = l
		}
	}
	return min
}

// Max returns the maximum latency.
func (lm *LatencyMeasurer) Max() time.Duration {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if len(lm.latencies) == 0 {
		return 0
	}
	max := lm.latencies[0]
	for _, l := range lm.latencies[1:] {
		if l > max {
			max = l
		}
	}
	return max
}

// Count returns the number of measurements.
func (lm *LatencyMeasurer) Count() int {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return len(lm.latencies)
}

// Reset clears all measurements.
func (lm *LatencyMeasurer) Reset() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.latencies = lm.latencies[:0]
}

// CompareResults compares two benchmark results.
type CompareResults struct {
	Before BenchResult
	After  BenchResult
}

// BenchResult holds benchmark results.
type BenchResult struct {
	Name         string
	NsPerOp      float64
	AllocsPerOp  uint64
	BytesPerOp   uint64
	MBPerSec     float64
	Iterations   int
}

// Improvement returns the percentage improvement.
func (cr *CompareResults) Improvement() float64 {
	if cr.Before.NsPerOp == 0 {
		return 0
	}
	return ((cr.Before.NsPerOp - cr.After.NsPerOp) / cr.Before.NsPerOp) * 100
}

// AllocReduction returns the allocation reduction percentage.
func (cr *CompareResults) AllocReduction() float64 {
	if cr.Before.AllocsPerOp == 0 {
		return 0
	}
	return ((float64(cr.Before.AllocsPerOp) - float64(cr.After.AllocsPerOp)) /
		float64(cr.Before.AllocsPerOp)) * 100
}
