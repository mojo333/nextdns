package proxy

import "testing"

func TestBufferPool_GetReturnsMaxMessageSize(t *testing.T) {
	// Arrange
	p := NewBufferPool()

	// Act
	bp := p.Get()

	// Assert: replies of any size up to the DNS maximum fit, so UDP and TCP
	// share cache entries without truncation.
	if len(*bp) != bufferSize {
		t.Fatalf("len = %d, want %d", len(*bp), bufferSize)
	}
}

func TestBufferPool_PutNilIsIgnored(t *testing.T) {
	NewBufferPool().Put(nil)
}

func TestBufferPool_ReuseDoesNotAllocate(t *testing.T) {
	// Arrange
	p := NewBufferPool()
	p.Put(p.Get())

	// Act
	allocs := testing.AllocsPerRun(100, func() {
		bp := p.Get()
		p.Put(bp)
	})

	// Assert: returning the pointer we were given keeps reuse free; the old
	// pool returned &buf of a copied slice, which allocated on every query.
	if allocs != 0 {
		t.Fatalf("allocs per Get/Put = %v, want 0", allocs)
	}
}
