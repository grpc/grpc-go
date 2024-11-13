/*
 *
 * Copyright 2024 gRPC authors.
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

// Package prxyresolver implements the default resolver that creates child
// resolvers to resolver targetURI as well as proxy adress.

package delegatingresolver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

type delegatingResolver struct {
	target         resolver.Target
	cc             resolver.ClientConn
	isProxy        bool
	targetResolver resolver.Resolver
	proxyResolver  resolver.Resolver
	targetAddrs    []resolver.Address
	proxyAddrs     []resolver.Address
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex // Protects the state below

}

type innerClientConn struct {
	*delegatingResolver // might not be needed
	resolverType        string
}

var (
	// The following variable will be overwritten in the tests.
	httpProxyFromEnvironment = http.ProxyFromEnvironment
)

func mapAddress(address string) (*url.URL, error) {
	req := &http.Request{
		URL: &url.URL{
			Scheme: "https",
			Host:   address,
		},
	}
	url, err := httpProxyFromEnvironment(req)
	if err != nil {
		return nil, err
	}
	return url, nil
}

func New(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions, targetResolverBuilder resolver.Builder) (resolver.Resolver, error) {
	r := &delegatingResolver{
		target: target,
		cc:     cc,
	}
	proxyURL, err := mapAddress(target.Endpoint())
	if err != nil {
		return nil, err
	}
	r.isProxy = true
	if proxyURL == nil {
		r.isProxy = false
	}
	if r.isProxy {
		if target.URL.Scheme == "dns" {
			r.targetAddrs = []resolver.Address{{Addr: r.target.Endpoint()}}
		} else {
			r.targetResolver, err = targetURIResolver(target, opts, targetResolverBuilder, r)
		}
		r.proxyResolver, err = proxyURIResolver(proxyURL, opts, r)
	} else {
		// r.targetResolver, err = targetURIResolver(target, opts, targetResolverBuilder, r) //
		return targetResolverBuilder.Build(target, cc, opts)
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func targetURIResolver(target resolver.Target, opts resolver.BuildOptions, targetResolverBuilder resolver.Builder, resolver *delegatingResolver) (resolver.Resolver, error) {
	targetBuilder := targetResolverBuilder
	fmt.Printf("the delegated resolver buildr is %v \n", targetBuilder)
	if targetBuilder == nil {
		return nil, fmt.Errorf("resolver for target scheme %q not found", target.URL.Scheme)
	}
	fmt.Printf("the delegated resolver buildr 1 is %v \n", targetBuilder)
	return targetBuilder.Build(target, &innerClientConn{resolver, "target"}, opts)
}

// func proxyURIResolver(target resolver.Target, opts resolver.BuildOptions, presolver *delegatingResolver) (resolver.Resolver, error) {
// 	fmt.Printf("proxy scheme %v\n", presolver.target.URL.Scheme)
// 	scheme := "dns"
// 	proxyBuilder := resolver.Get(scheme)
// 	if proxyBuilder == nil {
// 		return nil, fmt.Errorf("resolver for proxy not found")
// 	}
// 	fmt.Printf("the target endpoint is %v \n", target.Endpoint())
// 	proxyURL, err := mapAddress(target.Endpoint())
// 	if err != nil {
// 		return nil, err
// 	}
// 	fmt.Printf("the proxy url is %v, %v \n", proxyURL, err)
// 	fmt.Printf("the proxy resolver builder is %v \n", proxyBuilder)
// 	fmt.Printf("the proxy resolver builder scheme is %v \n", proxyBuilder.Scheme())
// 	x, y := proxyBuilder.Build(resolver.Target{URL: *proxyURL}, &innerClientConn{presolver, "proxy"}, opts)
// 	fmt.Printf("the proxy resolver builder build returns is %v, %v \n", x, y)

// 	return proxyBuilder.Build(resolver.Target{URL: *proxyURL}, &innerClientConn{presolver, "proxy"}, opts)
// }

func proxyURIResolver(proxyURL *url.URL, opts resolver.BuildOptions, presolver *delegatingResolver) (resolver.Resolver, error) {
	fmt.Printf("proxy scheme %v\n", presolver.target.URL.Scheme)
	scheme := "dns"
	proxyBuilder := resolver.Get(scheme)
	if proxyBuilder == nil {
		return nil, fmt.Errorf("resolver for proxy not found")
	}
	k := proxyURL.Hostname()
	fmt.Printf("proxy url string : %v\n", k)
	u, err := url.Parse(k)
	if err != nil {
		return nil, err
	}
	proxyTarget := resolver.Target{URL: *u}
	return proxyBuilder.Build(proxyTarget, &innerClientConn{presolver, "proxy"}, opts)
}

func (r *delegatingResolver) ResolveNow(o resolver.ResolveNowOptions) {
	if r.targetResolver != nil {
		r.targetResolver.ResolveNow(o)
	}
	if r.proxyResolver != nil {
		r.proxyResolver.ResolveNow(o)
	}
}

func (r *delegatingResolver) Close() {
	if r.targetResolver != nil {
		r.targetResolver.Close()
	}
	if r.proxyResolver != nil {
		r.proxyResolver.Close()
	}
}

// UpdateState intercepts state updates from the childtarget and proxy resolvers.
func (icc *innerClientConn) UpdateState(state resolver.State) error {
	//write the logic to combine and get the final state
	// icc.parent.updateState(state.Addresses, nil)
	icc.mu.Lock()
	defer icc.mu.Unlock()

	var curState resolver.State
	if icc.resolverType == "target" {
		icc.targetAddrs = state.Addresses
		curState = state
	}
	if icc.resolverType == "proxy" {
		icc.proxyAddrs = state.Addresses
	}

	// if icc.isProxy { // TODO : Ask if
	//check for both slices are filled or not i.e both the resolvers have
	// updated their state.
	if len(icc.targetAddrs) == 0 || len(icc.proxyAddrs) == 0 {
		return nil
	}

	var addresses []resolver.Address
	for _, proxyAddr := range icc.proxyAddrs {
		for _, targetAddr := range icc.targetAddrs {
			addresses = append(addresses, resolver.Address{
				Addr:       proxyAddr.Addr,
				Attributes: attributes.New("proxyConnectAddr", targetAddr.Addr),
			})
		}
	}
	// Set the addresses in the current state.
	curState.Addresses = addresses
	return icc.cc.UpdateState(curState)

	// } else {
	// 	if icc.resolverType == "target" {
	// 		return icc.cc.UpdateState(curState)
	// 	} else {
	// 		return nil // ask what error to send??
	// 	}
}

// ReportError intercepts errors from the child DNS resolver.
func (icc *innerClientConn) ReportError(err error) {
	icc.cc.ReportError(err)
}

func (icc *innerClientConn) NewAddress(addrs []resolver.Address) {
}

// ParseServiceConfig parses the provided service config and returns an
// object that provides the parsed config.
func (icc *innerClientConn) ParseServiceConfig(serviceConfigJSON string) *serviceconfig.ParseResult {
	fmt.Printf("config received by delegating resolver : %v \n", serviceConfigJSON)
	if icc.resolverType == "target" {
		return icc.cc.ParseServiceConfig(serviceConfigJSON)
	}
	return nil
}
