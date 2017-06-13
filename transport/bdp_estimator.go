package transport

import (
	"fmt"
	"sync"
	"time"
)

const (
	limit = (1 << 20) * 4
)

var (
	bdpPing = &ping{data: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
)

type bdpEstimator struct {
	mu                sync.Mutex
	bdp               uint32
	sample            uint32    // Current bdp sample..
	sentAt            time.Time // Time when the ping was sent.
	bwMax             float64
	isSent            bool
	updateFlowControl func(n uint32) // Callback to update window size.
	side              string
}

// timesnap registers the time the ping was sent out so that
// network rtt can be calculated when it's ack is recieved.
// It is called (by controller) when the bdpPing is
// being written on the wire.
func (b *bdpEstimator) timesnap(d [8]byte) {
	if bdpPing.data != d {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sentAt = time.Now()
}

// add adds bytes to the current sample for calculating bdp.
// It returns true only if a ping is sent. This can be used
// by the caller (handleData) to make decision about batching
// a window update with it.
func (b *bdpEstimator) add(n uint32) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bdp == limit {
		return false
	}
	if !b.isSent {
		b.isSent = true
		b.sample = n
		b.sentAt = time.Time{}
		return true
	}
	b.sample += n
	return false
}

// calculate is called when an ack for a bdp ping is received.
// Here we calculate the current bdp and bandwidth sample and
// decide if the flow control windows should go up.
func (b *bdpEstimator) calculate(d [8]byte) {
	// Check if the ping acked for was the bdp ping.
	if bdpPing.data != d {
		return
	}
	b.mu.Lock()
	rtt := time.Since(b.sentAt).Seconds()
	b.isSent = false
	bwCurrent := float64(b.sample) / rtt
	if bwCurrent > b.bwMax {
		// debug beg
		fmt.Printf("Max bw noted on %s-side: %v. Sample was: %v and  RTT was %v secs\n", b.side, bwCurrent, b.sample, rtt)
		// debug end
		b.bwMax = bwCurrent
	}
	if float64(b.sample) > float64(0.66)*float64(b.bdp) && bwCurrent == b.bwMax {
		// debug beg
		//fmt.Printf("The sample causing bdp to go up on %s-side: %v\n", b.side, b.sample)
		// debug end
		b.bdp = uint32(2) * b.sample
		if b.bdp > limit {
			b.bdp = limit
		}
		bdp := b.bdp
		b.mu.Unlock()
		// debug beg
		fmt.Println(b.side, " updating bdp to:", bdp)
		// debug end
		b.updateFlowControl(bdp)
		return
	}
	b.mu.Unlock()
}
