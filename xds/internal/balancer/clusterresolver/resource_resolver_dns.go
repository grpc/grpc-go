/*
 *
 * Copyright 2021 gRPC authors.
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

package clusterresolver

import (
	"fmt"

	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

var (
	newDNS = func(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
		dnsB := resolver.Get("dns")
		return dnsB.Build(target, cc, opts)
	}
)

// dnsResolver watches updates for the given DNS hostname.
//
// It implements resolver.ClientConn interface to work with the DNS resolver.
type dnsResolver struct {
	target           string
	topLevelResolver *resourceResolver
	r                resolver.Resolver

	addrs          []string
	updateReceived bool
}

func newDNSResolver(target string, topLevelResolver *resourceResolver) *dnsResolver {
	ret := &dnsResolver{
		target:           target,
		topLevelResolver: topLevelResolver,
	}
	r, err := newDNS(resolver.Target{Scheme: "dns", Endpoint: target}, ret, resolver.BuildOptions{})
	if err != nil {
		select {
		case <-topLevelResolver.updateChannel:
		default:
		}
		topLevelResolver.updateChannel <- &resourceUpdate{err: err}
	}
	ret.r = r
	return ret
}

func (dr *dnsResolver) lastUpdate() (interface{}, bool) {
	if !dr.updateReceived {
		return nil, false
	}
	return dr.addrs, true
}

func (dr *dnsResolver) resolveNow() {
	dr.r.ResolveNow(resolver.ResolveNowOptions{})
}

func (dr *dnsResolver) stop() {
	dr.r.Close()
}

// dnsResolver needs to implement resolver.ClientConn interface to receive
// updates from the real DNS resolver.

func (dr *dnsResolver) UpdateState(state resolver.State) error {
	dr.topLevelResolver.mu.Lock()
	defer dr.topLevelResolver.mu.Unlock()
	addrs := make([]string, len(state.Addresses))
	for i, a := range state.Addresses {
		addrs[i] = a.Addr
	}
	dr.addrs = addrs
	dr.updateReceived = true
	dr.topLevelResolver.generate()
	return nil
}

func (dr *dnsResolver) ReportError(err error) {
	select {
	case <-dr.topLevelResolver.updateChannel:
	default:
	}
	dr.topLevelResolver.updateChannel <- &resourceUpdate{err: err}
}

func (dr *dnsResolver) NewAddress(addresses []resolver.Address) {
	dr.UpdateState(resolver.State{Addresses: addresses})
}

func (dr *dnsResolver) NewServiceConfig(string) {
	// This method is deprecated, and service config isn't supported.
}

func (dr *dnsResolver) ParseServiceConfig(string) *serviceconfig.ParseResult {
	return &serviceconfig.ParseResult{Err: fmt.Errorf("service config not supported")}
}
