package proxy

import "sync"

// bufferSize fits the largest DNS message. UDP and TCP share the cache, so UDP
// reads use the full size too and truncate later, rather than caching a
// truncated reply that a TCP retry would then receive.
const bufferSize = 65536

// BufferPool reuses message buffers between queries.
type BufferPool struct {
	pool sync.Pool
}

func NewBufferPool() *BufferPool {
	return &BufferPool{pool: sync.Pool{New: func() any {
		b := make([]byte, bufferSize)
		return &b
	}}}
}

// Get returns a buffer of bufferSize bytes. Pass the same pointer to Put.
func (p *BufferPool) Get() *[]byte {
	return p.pool.Get().(*[]byte)
}

func (p *BufferPool) Put(bp *[]byte) {
	if bp == nil {
		return
	}
	p.pool.Put(bp)
}
