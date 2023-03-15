package grpc

import "sync"

// SharedBufferPool is a pool of buffers that can be shared.
type SharedBufferPool interface {
	Get(length int) []byte
	Put(*[]byte)
}

// NewSimpleSharedBufferPool creates a new SimpleSharedBufferPool.
func NewSimpleSharedBufferPool() SharedBufferPool {
	return &simpleSharedBufferPool{
		Pool: sync.Pool{
			New: func() interface{} {
				bs := make([]byte, 0)
				return &bs
			},
		},
	}
}

// simpleSharedBufferPool is a simple implementation of SharedBufferPool.
type simpleSharedBufferPool struct {
	sync.Pool
}

func (p *simpleSharedBufferPool) Get(size int) []byte {
	bs := p.Pool.Get().(*[]byte)
	if cap(*bs) < size {
		*bs = make([]byte, size)
		return *bs
	}

	return (*bs)[:size]
}

func (p *simpleSharedBufferPool) Put(bs *[]byte) {
	p.Pool.Put(bs)
}
