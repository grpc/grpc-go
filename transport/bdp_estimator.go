package transport

import (
	"sync"
)

const (
	alpha = 0.5
	beta  = 2
)

type bdpEstimator struct {
	p                *ping
	mu               sync.Mutex
	bdp              uint32
	sample           uint32 // Number of bytes received.
	isSent           bool
	send             func(*ping)    // Callback to send ping.
	updateConnWindow func(n uint32) // Callback to update window size.
}

func (b *bdpEstimator) add(n uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.isSent {
		b.isSent = true
		b.sample = 0
		b.send(b.p)
	}
	b.sample += n
}

func (b *bdpEstimator) calculate(d [8]byte) {
	if b.p.data != d {
		return
	}
	b.mu.Lock()
	b.isSent = false
	newbdp := float64(b.bdp) + alpha*float64(b.sample-b.bdp)
	b.bdp = uint32(newbdp)
	b.mu.Unlock()
	b.updateConnWindow(b.bdp)
}
