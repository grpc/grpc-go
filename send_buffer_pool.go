package grpc

import "sync"

type SendBufferPool interface {
	Get() []byte
	Put([]byte)
}

type sendBufferPool struct {
	pool sync.Pool
}

const defaultSendBufferPoolBufSize = 1 << 10 /* 1kb */

func NewSendBufferPool() SendBufferPool {
	return &sendBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, defaultSendBufferPoolBufSize)
			},
		},
	}
}

func (p *sendBufferPool) Get() []byte {
	return p.pool.Get().([]byte)
}

func (p *sendBufferPool) Put(buf []byte) {
	// TODO(PapaCharlie): uncomment after 1.21 migration
	// clear(b[:cap(b)])
	buf = buf[:0]
	p.pool.Put(buf)
}

type nopSendBufferPool struct{}

func (n nopSendBufferPool) Get() []byte {
	return nil
}

func (n nopSendBufferPool) Put([]byte) {
}
