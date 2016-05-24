/*
 *
 * Copyright 2016, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpc

import (
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/naming"
	"google.golang.org/grpc/transport"
)

// Address represents a server the client connects to.
type Address struct {
	// Addr is the server address on which a connection will be established.
	Addr string
	// Metadata is the information associated with Addr, which may be used
	// to make load balancing decision. This is from the metadata attached
	// in the address updates from name resolver.
	Metadata interface{}
}

// Balancer chooses network addresses for RPCs.
// This is the EXPERIMENTAL API and may be changed or extended in the future.
type Balancer interface {
	// Start does the initialization work to bootstrap a Balancer. For example,
	// this function may start the service discovery and watch the name resolution
	// updates.
	Start(target string) error
	// Up informs the balancer that gRPC has a connection to the server at
	// addr. It returns down which is called once the connection to addr gets
	// lost or closed. Once down is called, addr may no longer be returned
	// by Get.
	Up(addr Address) (down func(error))
	// Get gets the address of a server for the rpc corresponding to ctx.
	// It may block if there is no server available. It respects the
	// timeout or cancellation of ctx when blocking. It returns put which
	// is called once the rpc has completed or failed. put can collect and
	// report rpc stats to remote load balancer.
	Get(ctx context.Context) (addr Address, put func(), err error)
	// Notify gRPC internals the list of Address which should be connected. gRPC
	// internals will compare it with the exisiting connected addresses. If the
	// address Balancer notified is not in the list of the connected addresses,
	// gRPC starts to connect the address. If an address in the connected
	// addresses is not in the notification list, the corresponding connect will be
	// shutdown gracefully. Otherwise, there are no operations. Note that this
	// function must return the full list of the Addrresses which should be connected.
	// It is NOT delta.
	Notify() <-chan []Address
	// Close shuts down the balancer.
	Close() error
}

// RoundRobin returns a Balancer that selects addresses round-robin. It starts to watch
// the name resolution updates.
func RoundRobin(r naming.Resolver) Balancer {
	return &roundRobin{r: r}
}

type roundRobin struct {
	r         naming.Resolver
	open      []Address // all the known addresses the client can potentially connect
	mu        sync.Mutex
	addrCh    chan []Address // the channel to notify gRPC internals the list of addresses the client should connect to.
	connected []Address      // all the connected addresses
	next      int            // index of the next address to return for Get()
	waitCh    chan struct{}  // the channel to block when there is no connected address available
	done      bool           // The Balancer is closed.
}

func (rr *roundRobin) watchAddrUpdates(w naming.Watcher) error {
	updates, err := w.Next()
	if err != nil {
		return err
	}
	for _, update := range updates {
		addr := Address{
			Addr:     update.Addr,
			Metadata: update.Metadata,
		}
		switch update.Op {
		case naming.Add:
			var exisit bool
			for _, v := range rr.open {
				if addr == v {
					exisit = true
					grpclog.Println("grpc: The name resolver wanted to add an existing address: ", addr)
					break
				}
			}
			if exisit {
				continue
			}
			rr.open = append(rr.open, addr)
		case naming.Delete:
			for i, v := range rr.open {
				if v == addr {
					copy(rr.open[i:], rr.open[i+1:])
					rr.open = rr.open[:len(rr.open)-1]
					break
				}
			}
		default:
			grpclog.Println("Unknown update.Op ", update.Op)
		}
	}
	// Make a copy of rr.open and write it onto rr.addrCh so that gRPC internals gets notified.
	open := make([]Address, len(rr.open))
	rr.mu.Lock()
	defer rr.mu.Unlock()
	copy(open, rr.open)
	if rr.done {
		return ErrClientConnClosing
	}
	rr.addrCh <- open
	return nil
}

func (rr *roundRobin) Start(target string) error {
	if rr.r == nil {
		return nil
	}
	w, err := rr.r.Resolve(target)
	if err != nil {
		return err
	}
	rr.addrCh = make(chan []Address)
	go func() {
		for {
			if err := rr.watchAddrUpdates(w); err != nil {
				return
			}
		}
	}()
	return nil
}

// Up appends addr to the end of rr.addrs and sends notification if there
// are pending Get() calls.
func (rr *roundRobin) Up(addr Address) func(error) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	for _, a := range rr.connected {
		if a == addr {
			return nil
		}
	}
	rr.connected = append(rr.connected, addr)
	if len(rr.connected) == 1 {
		// addr is only one available. Notify the Get() callers who are blocking.
		if rr.waitCh != nil {
			close(rr.waitCh)
			rr.waitCh = nil
		}
	}
	return func(err error) {
		rr.down(addr, err)
	}
}

// down removes addr from rr.addrs and moves the remaining addrs forward.
func (rr *roundRobin) down(addr Address, err error) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	for i, a := range rr.connected {
		if a == addr {
			copy(rr.connected[i:], rr.connected[i+1:])
			rr.connected = rr.connected[:len(rr.connected)-1]
			return
		}
	}
}

// Get returns the next addr in the rotation. It blocks if there is no address available.
func (rr *roundRobin) Get(ctx context.Context) (addr Address, put func(), err error) {
	var ch chan struct{}
	rr.mu.Lock()
	if rr.done {
		rr.mu.Unlock()
		err = ErrClientConnClosing
		return
	}
	if rr.next >= len(rr.connected) {
		rr.next = 0
	}
	if len(rr.connected) > 0 {
		addr = rr.connected[rr.next]
		rr.next++
		rr.mu.Unlock()
		return
	}
	// There is no address available. Wait on rr.waitCh.
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
			if rr.done {
				rr.mu.Unlock()
				err = ErrClientConnClosing
				return
			}
			if len(rr.connected) == 0 {
				// The newly added addr got removed by Down() again.
				rr.mu.Unlock()
				continue
			}
			if rr.next >= len(rr.connected) {
				rr.next = 0
			}
			addr = rr.connected[rr.next]
			rr.next++
			rr.mu.Unlock()
			return
		}
	}
}

func (rr *roundRobin) Notify() <-chan []Address {
	return rr.addrCh
}

func (rr *roundRobin) Close() error {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.done = true
	if rr.waitCh != nil {
		close(rr.waitCh)
		rr.waitCh = nil
	}
	if rr.addrCh != nil {
		close(rr.addrCh)
	}
	return nil
}
