package grpc

import (
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc/transport"
)

type Address struct {
	// Addr is the peer address on which a connection will be established.
	Addr string
	// Metadata is the information associated with Addr, which may be used
	// to make load balancing decision. This is from the metadata attached
	// in the address updates from name resolver.
	Metadata interface{}
}

// Balancer chooses network addresses for RPCs.
type Balancer interface {
	// Up informs the balancer that gRPC has a connection to the server at
	// addr. It returns down which will be called once the connection gets
	// lost. Once down is called, addr may no longer be returned by Get.
	Up(addr Address) (down func(error))
	// Get gets the address of a server for the rpc corresponding to ctx.
	// It may block if there is no server available. It respects the
	// timeout or cancellation of ctx when blocking. It returns put which
	// is called once the rpc has completed or failed. put can collect and
	// report rpc stats to remote load balancer.
	Get(ctx context.Context) (addr Address, put func(), err error)
	// Close shuts down the balancer.
	Close() error
}

func RoundRobin() Balancer {
	return &roundRobin{}
}

type roundRobin struct {
	mu      sync.Mutex
	addrs   []Address
	next    int
	waitCh  chan struct{}
	pending int
}

func (rr *roundRobin) Up(addr Address) func() {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	for _, a := range rr.addrs {
		if a == addr {
			return nil
		}
	}
	rr.addrs = append(rr.addrs, addr)
	if len(rr.addrs) == 1 {
		if rr.waitCh != nil {
			close(rr.waitCh)
			rr.waitCh = nil
		}
	}
	return func() {
		rr.down(addr)
	}
}

func (rr *roundRobin) down(addr Address) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	for i, a := range rr.addrs {
		if a == addr {
			copy(rr.addrs[i:], rr.addrs[i+1:])
			rr.addrs = rr.addrs[:len(rr.addrs)-1]
			return
		}
	}
}

func (rr *roundRobin) Get(ctx context.Context) (addr Address, put func(), err error) {
	var ch chan struct{}
	rr.mu.Lock()
	if rr.next >= len(rr.addrs) {
		rr.next = 0
	}
	if len(rr.addrs) > 0 {
		addr = rr.addrs[rr.next]
		rr.next++
		rr.pending++
		rr.mu.Unlock()
		put = func() {
			rr.put(ctx, addr)
		}
		return
	}
	if rr.waitCh == nil {
		ch = make(chan struct{})
		rr.waitCh = ch
	} else {
		ch = rr.waitCh
	}
	rr.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			err = transport.ContextErr(ctx.Err())
			return
		case <-ch:
			rr.mu.Lock()
			if len(rr.addrs) == 0 {
				// The newly added addr got removed by Down() again.
				rr.mu.Unlock()
				continue
			}
			if rr.next >= len(rr.addrs) {
				rr.next = 0
			}
			addr = rr.addrs[rr.next]
			rr.next++
			rr.pending++
			rr.mu.Unlock()
			put = func() {
				rr.put(ctx, addr)
			}
			return
		}
	}
}

func (rr *roundRobin) put(ctx context.Context, addr Address) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.pending--
}

func (rr *roundRobin) Close() error {
	return nil
}
