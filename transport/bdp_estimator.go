package transport

import (
	"sync"
	"time"
)

const (
	limit = (1 << 20) * 4
)

type bdpEstimator struct {
	p                 *ping
	mu                sync.Mutex
	bdp               uint32
	sample            uint32    // Current bdp sample..
	sentAt            time.Time // Time when the ping was sent.
	bwMax             float64
	isSent            bool
	send              func(*ping)    // Callback to send ping.
	updateFlowControl func(n uint32) // Callback to update window size.
}

func (b *bdpEstimator) add(n uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bdp == limit {
		return
	}
	if !b.isSent {
		b.isSent = true
		b.sample = 0
		b.send(b.p)
		b.sentAt = time.Now()
	}
	b.sample += n
}

func (b *bdpEstimator) calculate(d [8]byte) {
	// Check if the ping acked for was the bdp ping.
	if b.p.data != d {
		return
	}
	b.mu.Lock()
	b.isSent = false
	rtt := time.Since(b.sentAt).Seconds()
	bwCurrent := float64(b.sample) / rtt
	if bwCurrent > b.bwMax {
		b.bwMax = bwCurrent
	}
	if float64(b.sample) > 0.66*float64(b.bdp) && bwCurrent == b.bwMax {
		b.bdp *= 2
		if b.bdp > limit {
			b.bdp = limit
		}
	}
	b.mu.Unlock()
	b.updateFlowControl(b.bdp)
}
