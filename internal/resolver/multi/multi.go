/*
 *
 * Copyright 2020 gRPC authors.
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

// Package multi allows you to Dial to multiple hosts/IPs as a single ClientConn.
//
// Usage: multi:///127.0.0.1:1234,dns://example.org:1234
// Note the triple slash at the beginning.
package multi

import (
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc/internal/grpcutil"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

func init() {
	resolver.Register(builder{})
}

type builder struct {
}

func (builder) Scheme() string {
	return "multi"
}

func (builder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	pccg := &partialClientConnGroup{
		cc: cc,
	}
	var mr multiResolver
	for _, t := range strings.Split(target.Endpoint, ",") {
		parsedTarget := grpcutil.ParseTarget(t)
		resolverBuilder := resolver.Get(parsedTarget.Scheme)
		if resolverBuilder == nil {
			parsedTarget = resolver.Target{
				Scheme:   resolver.GetDefaultScheme(),
				Endpoint: t,
			}
			resolverBuilder = resolver.Get(parsedTarget.Scheme)
			if resolverBuilder == nil {
				return nil, fmt.Errorf("could not get resolver for default scheme: %q", parsedTarget.Scheme)
			}
		}
		pcc := &partialClientConn{parent: pccg}
		pccg.parts = append(pccg.parts, pcc)
		resolver, err := resolverBuilder.Build(parsedTarget, pcc, opts)
		if err != nil {
			mr.Close()
			return nil, err
		}
		mr.children = append(mr.children, resolver)
	}
	return mr, nil
}

type partialClientConnGroup struct {
	cc    resolver.ClientConn
	parts []*partialClientConn
}

func (pccg *partialClientConnGroup) updateState() {
	s := resolver.State{}
	pccg.parts[0].mtx.Lock()
	s.ServiceConfig = pccg.parts[0].state.ServiceConfig
	s.Attributes = pccg.parts[0].state.Attributes
	pccg.parts[0].mtx.Unlock()
	for _, p := range pccg.parts {
		p.mtx.Lock()
		s.Addresses = append(s.Addresses, p.state.Addresses...)
		p.mtx.Unlock()
	}
	pccg.cc.UpdateState(s)
}

type partialClientConn struct {
	parent *partialClientConnGroup

	mtx   sync.Mutex
	state resolver.State
}

// UpdateState updates the state of the ClientConn appropriately.
func (cc *partialClientConn) UpdateState(s resolver.State) {
	cc.mtx.Lock()
	cc.state = s
	cc.mtx.Unlock()
	cc.parent.updateState()
}

// ReportError notifies the ClientConn that the Resolver encountered an
// error.  The ClientConn will notify the load balancer and begin calling
// ResolveNow on the Resolver with exponential backoff.
func (cc *partialClientConn) ReportError(err error) {
	cc.parent.cc.ReportError(err)
}

// NewAddress is called by resolver to notify ClientConn a new list
// of resolved addresses.
// The address list should be the complete list of resolved addresses.
//
// Deprecated: Use UpdateState instead.
func (cc *partialClientConn) NewAddress(addresses []resolver.Address) {
	cc.mtx.Lock()
	cc.state.Addresses = addresses
	cc.mtx.Unlock()
	cc.parent.updateState()
}

// NewServiceConfig is called by resolver to notify ClientConn a new
// service config. The service config should be provided as a json string.
//
// Deprecated: Use UpdateState instead.
func (cc *partialClientConn) NewServiceConfig(serviceConfig string) {
	cc.mtx.Lock()
	cc.state.ServiceConfig = cc.ParseServiceConfig(serviceConfig)
	cc.mtx.Unlock()
	cc.parent.updateState()
}

// ParseServiceConfig parses the provided service config and returns an
// object that provides the parsed config.
func (cc *partialClientConn) ParseServiceConfig(serviceConfigJSON string) *serviceconfig.ParseResult {
	return cc.parent.cc.ParseServiceConfig(serviceConfigJSON)
}

type multiResolver struct {
	children []resolver.Resolver
}

// ResolveNow will be called by gRPC to try to resolve the target name
// again. It's just a hint, resolver can ignore this if it's not necessary.
//
// It could be called multiple times concurrently.
func (m multiResolver) ResolveNow(opts resolver.ResolveNowOptions) {
	for _, r := range m.children {
		r.ResolveNow(opts)
	}
}

// Close closes the resolver.
func (m multiResolver) Close() {
	for _, r := range m.children {
		r.Close()
	}
}
