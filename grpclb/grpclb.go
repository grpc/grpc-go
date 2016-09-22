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

// Package grpclb implements the load balancing protocol defined at
// https://github.com/grpc/grpc/blob/master/doc/load-balancing.md.
// The implementation is currently EXPERIMENTAL.
package grpclb

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	lbpb "google.golang.org/grpc/grpclb/grpc_lb_v1"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/naming"
)

// Balancer creates a grpclb load balancer.
func Balancer(r naming.Resolver) grpc.Balancer {
	return &balancer{
		r: r,
	}
}

type remoteBalancerInfo struct {
	addr grpc.Address
	name string
}

// addrInfo consists of the information of a backend server.
type addrInfo struct {
	addr        grpc.Address
	connected   bool
	dropRequest bool
}

type balancer struct {
	r      naming.Resolver
	mu     sync.Mutex
	seq    int // a sequence number to make sure addrCh does not get stale addresses.
	w      naming.Watcher
	addrCh chan []grpc.Address
	rbs    []remoteBalancerInfo
	addrs  []addrInfo
	next   int
	waitCh chan struct{}
	done   bool
}

func (b *balancer) watchAddrUpdates(w naming.Watcher, ch chan remoteBalancerInfo) error {
	updates, err := w.Next()
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return grpc.ErrClientConnClosing
	}
	var bAddr remoteBalancerInfo
	if len(b.rbs) > 0 {
		bAddr = b.rbs[0]
	}
	for _, update := range updates {
		addr := grpc.Address{
			Addr:     update.Addr,
			Metadata: update.Metadata,
		}
		switch update.Op {
		case naming.Add:
			var exist bool
			for _, v := range b.rbs {
				// TODO: Is the same addr with different server name a different balancer?
				if addr == v.addr {
					exist = true
					break
				}
			}
			if exist {
				continue
			}
			b.rbs = append(b.rbs, remoteBalancerInfo{addr: addr})
		case naming.Delete:
			for i, v := range b.rbs {
				if addr == v.addr {
					copy(b.rbs[i:], b.rbs[i+1:])
					b.rbs = b.rbs[:len(b.rbs)-1]
					break
				}
			}
		default:
			grpclog.Println("Unknown update.Op ", update.Op)
		}
	}
	// TODO: Fall back to the basic round-robin load balancing if the resulting address is
	// not a load balancer.
	if len(b.rbs) > 0 {
		// For simplicity, always use the first one now. May revisit this decision later.
		if b.rbs[0] != bAddr {
			select {
			case <-ch:
			default:
			}
			ch <- b.rbs[0]
		}
	}
	return nil
}

func (b *balancer) processServerList(l *lbpb.ServerList, seq int) {
	servers := l.GetServers()
	var (
		sl    []addrInfo
		addrs []grpc.Address
	)
	for _, s := range servers {
		// TODO: Support ExpirationInterval
		addr := grpc.Address{
			Addr: fmt.Sprintf("%s:%d", s.IpAddress, s.Port),
			// TODO: include LoadBalanceToken in the Metadata
		}
		sl = append(sl, addrInfo{
			addr: addr,
			// TODO: Support dropRequest feature.
		})
		addrs = append(addrs, addr)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done || seq < b.seq {
		return
	}
	if len(sl) > 0 {
		// reset b.next to 0 when replacing the server list.
		b.next = 0
		b.addrs = sl
		b.addrCh <- addrs
	}
	return
}

func (b *balancer) callRemoteBalancer(lbc lbpb.LoadBalancerClient) (retry bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := lbc.BalanceLoad(ctx, grpc.FailFast(false))
	if err != nil {
		grpclog.Printf("Failed to perform RPC to the remote balancer %v", err)
		return
	}
	b.mu.Lock()
	if b.done {
		b.mu.Unlock()
		return
	}
	b.seq++
	seq := b.seq
	b.mu.Unlock()
	initReq := &lbpb.LoadBalanceRequest{
		LoadBalanceRequestType: &lbpb.LoadBalanceRequest_InitialRequest{
			InitialRequest: new(lbpb.InitialLoadBalanceRequest),
		},
	}
	if err := stream.Send(initReq); err != nil {
		// TODO: backoff on retry?
		return true
	}
	reply, err := stream.Recv()
	if err != nil {
		// TODO: backoff on retry?
		return true
	}
	initResp := reply.GetInitialResponse()
	if initResp == nil {
		grpclog.Println("Failed to receive the initial response from the remote balancer.")
		return
	}
	// TODO: Support delegation.
	if initResp.LoadBalancerDelegate != "" {
		// delegation
		grpclog.Println("TODO: Delegation is not supported yet.")
		return
	}
	// Retrieve the server list.
	for {
		reply, err := stream.Recv()
		if err != nil {
			break
		}
		if serverList := reply.GetServerList(); serverList != nil {
			b.processServerList(serverList, seq)
		}
	}
	return true
}

func (b *balancer) Start(target string, config grpc.BalancerConfig) error {
	// TODO: Fall back to the basic direct connection if there is no name resolver.
	if b.r == nil {
		return errors.New("there is no name resolver installed")
	}
	b.mu.Lock()
	if b.done {
		b.mu.Unlock()
		return grpc.ErrClientConnClosing
	}
	b.addrCh = make(chan []grpc.Address)
	w, err := b.r.Resolve(target)
	if err != nil {
		b.mu.Unlock()
		return err
	}
	b.w = w
	b.mu.Unlock()
	balancerAddrCh := make(chan remoteBalancerInfo, 1)
	// Spawn a goroutine to monitor the name resolution of remote load balancer.
	go func() {
		for {
			if err := b.watchAddrUpdates(w, balancerAddrCh); err != nil {
				grpclog.Printf("grpc: the naming watcher stops working due to %v.\n", err)
				close(balancerAddrCh)
				return
			}
		}
	}()
	// Spawn a goroutine to talk to the remote load balancer.
	go func() {
		var cc *grpc.ClientConn
		for {
			rb, ok := <-balancerAddrCh
			if cc != nil {
				cc.Close()
			}
			if !ok {
				// b is closing.
				return
			}

			// Talk to the remote load balancer to get the server list.
			//
			// TODO: override the server name in creds using Metadata in addr.
			var err error
			creds := config.DialCreds
			if creds == nil {
				cc, err = grpc.Dial(rb.addr.Addr, grpc.WithInsecure())
			} else {
				cc, err = grpc.Dial(rb.addr.Addr, grpc.WithTransportCredentials(creds))
			}
			if err != nil {
				grpclog.Printf("Failed to setup a connection to the remote balancer %v: %v", rb.addr, err)
				return
			}
			go func(cc *grpc.ClientConn) {
				lbc := lbpb.NewLoadBalancerClient(cc)
				for {
					if retry := b.callRemoteBalancer(lbc); !retry {
						cc.Close()
						return
					}
				}
			}(cc)
		}
	}()
	return nil
}

func (b *balancer) down(addr grpc.Address, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, a := range b.addrs {
		if addr == a.addr {
			a.connected = false
			break
		}
	}
}

func (b *balancer) Up(addr grpc.Address) func(error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return nil
	}
	var cnt int
	for _, a := range b.addrs {
		if a.addr == addr {
			if a.connected {
				return nil
			}
			a.connected = true
		}
		if a.connected {
			cnt++
		}
	}
	// addr is the only one which is connected. Notify the Get() callers who are blocking.
	if cnt == 1 && b.waitCh != nil {
		close(b.waitCh)
		b.waitCh = nil
	}
	return func(err error) {
		b.down(addr, err)
	}
}

func (b *balancer) Get(ctx context.Context, opts grpc.BalancerGetOptions) (addr grpc.Address, put func(), err error) {
	var ch chan struct{}
	b.mu.Lock()
	if b.done {
		b.mu.Unlock()
		err = grpc.ErrClientConnClosing
		return
	}

	if len(b.addrs) > 0 {
		if b.next >= len(b.addrs) {
			b.next = 0
		}
		next := b.next
		for {
			a := b.addrs[next]
			next = (next + 1) % len(b.addrs)
			if a.connected {
				addr = a.addr
				b.next = next
				b.mu.Unlock()
				return
			}
			if next == b.next {
				// Has iterated all the possible address but none is connected.
				break
			}
		}
	}
	if !opts.BlockingWait {
		if len(b.addrs) == 0 {
			b.mu.Unlock()
			err = fmt.Errorf("there is no address available")
			return
		}
		// Returns the next addr on b.addrs for a failfast RPC.
		addr = b.addrs[b.next].addr
		b.next++
		b.mu.Unlock()
		return
	}
	// Wait on b.waitCh for non-failfast RPCs.
	if b.waitCh == nil {
		ch = make(chan struct{})
		b.waitCh = ch
	} else {
		ch = b.waitCh
	}
	b.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case <-ch:
			b.mu.Lock()
			if b.done {
				b.mu.Unlock()
				err = grpc.ErrClientConnClosing
				return
			}

			if len(b.addrs) > 0 {
				if b.next >= len(b.addrs) {
					b.next = 0
				}
				next := b.next
				for {
					a := b.addrs[next]
					next = (next + 1) % len(b.addrs)
					if a.connected {
						addr = a.addr
						b.next = next
						b.mu.Unlock()
						return
					}
					if next == b.next {
						// Has iterated all the possible address but none is connected.
						break
					}
				}
			}
			// The newly added addr got removed by Down() again.
			if b.waitCh == nil {
				ch = make(chan struct{})
				b.waitCh = ch
			} else {
				ch = b.waitCh
			}
			b.mu.Unlock()
		}
	}
}

func (b *balancer) Notify() <-chan []grpc.Address {
	return b.addrCh
}

func (b *balancer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.done = true
	if b.waitCh != nil {
		close(b.waitCh)
	}
	if b.addrCh != nil {
		close(b.addrCh)
	}
	if b.w != nil {
		b.w.Close()
	}
	return nil
}
