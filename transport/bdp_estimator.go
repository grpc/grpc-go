package transport

import (
	"sync"
)

type bdpEstimator struct {
	p                *ping
	maxWindow        uint32
	mu               sync.Mutex
	estimate         uint32
	accumulator      uint32 // Number of bytes received.
	isSent           bool
	send             func(*ping)    // Callback to send ping.
	updateConnWindow func(n uint32) // Callback to update window size.
}

func (b *bdpEstimator) add(n uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.isSent {
		b.isSent = true
		b.accumulator = 0
		b.send(b.p)
	}
	b.accumulator += n
}

func (b *bdpEstimator) calculate(d [8]byte) {
	if b.p.data != d {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.isSent = false
	estimate := 2 * b.accumulator
	if estimate > b.estimate {
		if estimate > b.maxWindow {
			estimate = b.maxWindow
			//log.Println("Max window limit reached, can't increase window more than ", b.maxWindow)
		}
		delta := estimate - b.estimate
		b.estimate = estimate
		//log.Println("Updating the intial window with new estimate: ", b.estimate)
		b.updateConnWindow(delta)
	}
}
