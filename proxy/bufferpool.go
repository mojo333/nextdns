package proxy

import "sync"

// TieredBufferPool manages multiple buffer pools of different sizes
// to minimize memory usage for typical DNS queries while supporting
// large responses.
type TieredBufferPool struct {
	small  sync.Pool // 512 bytes  - typical DNS query/response
	medium sync.Pool // 4096 bytes - larger responses
	large  sync.Pool // 65536 bytes - maximum DNS message size
}

// NewTieredBufferPool creates a new tiered buffer pool.
func NewTieredBufferPool() *TieredBufferPool {
	return &TieredBufferPool{
		small: sync.Pool{
			New: func() interface{} {
				b := make([]byte, 512)
				return &b
			},
		},
		medium: sync.Pool{
			New: func() interface{} {
				b := make([]byte, 4096)
				return &b
			},
		},
		large: sync.Pool{
			New: func() interface{} {
				b := make([]byte, 65536)
				return &b
			},
		},
	}
}

// Get returns a buffer of at least the requested size.
// For typical DNS queries (<512 bytes), this saves 99% memory vs 65KB pool.
func (p *TieredBufferPool) Get(size int) *[]byte {
	switch {
	case size <= 512:
		return p.small.Get().(*[]byte)
	case size <= 4096:
		return p.medium.Get().(*[]byte)
	default:
		return p.large.Get().(*[]byte)
	}
}

// Put returns a buffer to the appropriate pool based on its capacity.
func (p *TieredBufferPool) Put(buf *[]byte) {
	if buf == nil {
		return
	}
	switch cap(*buf) {
	case 512:
		p.small.Put(buf)
	case 4096:
		p.medium.Put(buf)
	case 65536:
		p.large.Put(buf)
	}
}

// GetLarge always returns a large (65KB) buffer.
// Use this when you need maximum DNS message size.
func (p *TieredBufferPool) GetLarge() *[]byte {
	return p.large.Get().(*[]byte)
}
