package transport

import (
	"log"
	"sync"
)

type bdpEstimator struct {
	p           *ping
	maxWindow   uint32
	mu          sync.Mutex
	estimate    uint32 // estimate
	accumulator uint32 // Number of bytes received.
	isSent      bool
}

func (b *bdpEstimator) start() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.isSent {
		return false
	}
	b.isSent = true
	b.accumulator = 0
	return true
}

func (b *bdpEstimator) add(n uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.isSent {
		return
	}
	b.accumulator += n
}

func (b *bdpEstimator) stop(d [8]byte) {
	if b.p.data != d {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.isSent = false
	b.estimate = 2 * b.accumulator
	log.Println("The new estimate is ", b.estimate)
}
