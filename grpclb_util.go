/*
 *
 * Copyright 2016 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package grpc

import (
	"fmt"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
)

// The parent ClientConn should re-resolve when grpclb loses connection to the
// remote balancer. When the ClientConn inside grpclb gets a TransientFailure,
// it calls lbManualResolver.ResolveNow(), which calls parent ClientConn's
// ResolveNow, and eventually results in re-resolve happening in parent
// ClientConn's resolver (DNS for example).
//
//                          parent
//                          ClientConn
//  +-----------------------------------------------------------------+
//  |             parent          +---------------------------------+ |
//  | DNS         ClientConn      |  grpclb                         | |
//  | resolver    balancerWrapper |                                 | |
//  | +              +            |    grpclb          grpclb       | |
//  | |              |            |    ManualResolver  ClientConn   | |
//  | |              |            |     +              +            | |
//  | |              |            |     |              | Transient  | |
//  | |              |            |     |              | Failure    | |
//  | |              |            |     |  <---------  |            | |
//  | |              | <--------------- |  ResolveNow  |            | |
//  | |  <---------  | ResolveNow |     |              |            | |
//  | |  ResolveNow  |            |     |              |            | |
//  | |              |            |     |              |            | |
//  | +              +            |     +              +            | |
//  |                             +---------------------------------+ |
//  +-----------------------------------------------------------------+

// lbManualResolver is used by the ClientConn inside grpclb. It's a manual
// resolver with a special ResolveNow() function.
//
// When ResolveNow() is called, it calls ResolveNow() on the parent ClientConn,
// so when grpclb client lose contact with remote balancers, the parent
// ClientConn's resolver will re-resolve.
type lbManualResolver struct {
	scheme string
	ccr    resolver.ClientConn

	ccb balancer.ClientConn
}

func (r *lbManualResolver) Build(_ resolver.Target, cc resolver.ClientConn, _ resolver.BuildOption) (resolver.Resolver, error) {
	r.ccr = cc
	return r, nil
}

func (r *lbManualResolver) Scheme() string {
	return r.scheme
}

// ResolveNow calls resolveNow on the parent ClientConn.
func (r *lbManualResolver) ResolveNow(o resolver.ResolveNowOption) {
	r.ccb.ResolveNow(o)
}

// Close is a noop for Resolver.
func (*lbManualResolver) Close() {}

// NewAddress calls cc.NewAddress.
func (r *lbManualResolver) NewAddress(addrs []resolver.Address) {
	r.ccr.NewAddress(addrs)
}

// NewServiceConfig calls cc.NewServiceConfig.
func (r *lbManualResolver) NewServiceConfig(sc string) {
	r.ccr.NewServiceConfig(sc)
}

const subConnCacheTime = time.Second * 10

// lbCacheClientConn is a wrapper balancer.ClientConn with a SubConn cache.
// SubConns will be kept in cache for subConnCacheTime before being removed.
//
// Its new and remove methods are updated to do cache first.
type lbCacheClientConn struct {
	balancer.ClientConn

	// subConnCache only keeps subConns that are being deleted.
	subConnCache map[resolver.Address]*subConnCacheEntry

	subConnToAddr map[balancer.SubConn]resolver.Address
}

type subConnCacheEntry struct {
	sc     balancer.SubConn
	cancel func()
}

func newLBCacheClientConn(cc balancer.ClientConn) *lbCacheClientConn {
	return &lbCacheClientConn{
		ClientConn:    cc,
		subConnCache:  make(map[resolver.Address]*subConnCacheEntry),
		subConnToAddr: make(map[balancer.SubConn]resolver.Address),
	}
}

// The caller of this function (refreshSubConns) is call synchronously, so
// thers's no need to hold any lock.
func (ccc *lbCacheClientConn) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	if len(addrs) != 1 {
		return nil, fmt.Errorf("grpclb calling NewSubConn with addrs of length %v", len(addrs))
	}

	if entry, ok := ccc.subConnCache[addrs[0]]; ok {
		// If entry is in subConnCache, the SubConn was being deleted.
		// cancel function will never be nil.
		entry.cancel()
		return entry.sc, nil
	}

	scNew, err := ccc.ClientConn.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}

	ccc.subConnToAddr[scNew] = addrs[0]
	return scNew, nil
}

func (ccc *lbCacheClientConn) RemoveSubConn(sc balancer.SubConn) {
	addr, ok := ccc.subConnToAddr[sc]
	if !ok {
		return
	}

	if entry, ok := ccc.subConnCache[addr]; ok {
		if entry.sc != sc {
			// This could happen if NewSubConn was called multiple times for the
			// same address, and those SubConns are all removed. We remove sc
			// immediately here.
			delete(ccc.subConnToAddr, sc)
			ccc.ClientConn.RemoveSubConn(sc)
		}
		return
	}

	timer := time.AfterFunc(subConnCacheTime, func() {
		delete(ccc.subConnToAddr, sc)
		ccc.ClientConn.RemoveSubConn(sc)
	})

	ccc.subConnCache[addr] = &subConnCacheEntry{
		sc: sc,
		cancel: func() {
			if !timer.Stop() {
				<-timer.C
			}
		},
	}
	return
}

func (ccc *lbCacheClientConn) close() {
	// Only cancel all existing timers. There's no need to remove SubConns.
	for _, entry := range ccc.subConnCache {
		entry.cancel()
	}
}
