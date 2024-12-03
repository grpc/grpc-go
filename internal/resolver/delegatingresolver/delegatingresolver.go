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

// Package delegatingresolver defines a resolver that can handle both target URI
// and proxy address resolution, unless:
//   - A custom dialer is set using WithContextDialer dialoption.
//   - Proxy usage is explicitly disabled using WithNoProxy dialoption.
//   - Client-side resolution is explicitly enforced using WithTargetResolutionEnabled.
package delegatingresolver

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/attributes"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

var (
	// HTTPSProxyFromEnvironment will be used and overwritten in the tests.
	HTTPSProxyFromEnvironment = http.ProxyFromEnvironment
	// ProxyScheme will be ovwewritten in tests
	ProxyScheme = "dns"
	logger      = grpclog.Component("delegating-resolver")
)

// delegatingResolver implements the `resolver.Resolver` interface. It uses child
// resolvers for the target and proxy resolution. It acts as an intermediatery
// between the child resolvers and the gRPC ClientConn.
type delegatingResolver struct {
	target         resolver.Target     // parsed target URI to be resolved
	cc             resolver.ClientConn // gRPC ClientConn
	targetResolver resolver.Resolver   // resolver for the target URI, based on its scheme
	proxyResolver  resolver.Resolver   // resolver for the proxy URI; nil if no proxy is configured

	mu          sync.Mutex         // protects access to the resolver state and addresses during updates
	targetAddrs []resolver.Address // resolved or unresolved target addresses, depending on proxy configuration
	proxyAddrs  []resolver.Address // resolved proxy addresses; empty if no proxy is configured
	proxyURL    *url.URL           // proxy URL, derived from proxy environment and target

	targetResolverReady bool // indicates if an update from the target resolver has been received
	proxyResolverReady  bool // indicates if an update from the proxy resolver has been received
}

func parsedURLForProxy(address string) (*url.URL, error) {
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: address}}
	url, err := HTTPSProxyFromEnvironment(req)
	if err != nil {
		return nil, err
	}
	return url, nil
}

// OnClientResolution is a no-op function in non-test code. In tests, it can
// be overwritten to send a signal to a channel, indicating that client-side
// name resolution was triggered.  This enables tests to verify that resolution
// is bypassed when a proxy is in use.
var OnClientResolution = func(int) { /* no-op */ }

// New creates a new delegating resolver that is used to call the target and
// proxy child resolver. If proxy is configured and target endpoint points
// correctly points to proxy, both proxy and target resolvers are used else
// only target resolver is used.
//
// For target resolver, if scheme is dns and target resolution is not enabled,
// it stores unresolved target address, bypassing target resolution at the
// client and resolution happens at the proxy server otherwise it resolves
// and store the resolved address.
//
// It returns error if proxy is configured but proxy target doesn't parse to
// correct url or if target resolution at client fails.
func New(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions, targetResolverBuilder resolver.Builder, targetResolutionEnabled bool) (resolver.Resolver, error) {
	r := &delegatingResolver{target: target, cc: cc}

	var err error
	r.proxyURL, err = parsedURLForProxy(target.Endpoint())
	// if proxy is configured but proxy target is wrong, so return early with error.
	if err != nil {
		return nil, fmt.Errorf("failed to determine proxy URL for %v  target endpoint: %v", target.Endpoint(), err)
	}

	// proxy is not configured or proxy address excluded using `NO_PROXY` env var,
	// so only target resolver is used.
	if r.proxyURL == nil {
		OnClientResolution(1)
		return targetResolverBuilder.Build(target, cc, opts)
	}

	if logger.V(2) {
		logger.Info("Proxy URL detected : %+v", r.proxyURL)
	}
	// When the scheme is 'dns' and target resolution on client is not enabled,
	// resolution should be handled by the proxy, not the client. Therefore, we
	// bypass the target resolver and store the unresolved target address.
	if target.URL.Scheme == "dns" && !targetResolutionEnabled {
		r.targetAddrs = []resolver.Address{{Addr: target.Endpoint()}}
		r.targetResolverReady = true
	} else {
		OnClientResolution(1)
		if r.targetResolver, err = targetResolverBuilder.Build(target, &wrappingClientConn{parent: r, resolverType: targetResolverType}, opts); err != nil {
			return nil, fmt.Errorf("delegating_resolver: unable to build the resolver for target %v : %v", target, err)
		}
	}

	if r.proxyResolver, err = r.proxyURIResolver(opts); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *delegatingResolver) proxyURIResolver(opts resolver.BuildOptions) (resolver.Resolver, error) {
	proxyBuilder := resolver.Get(ProxyScheme)
	if proxyBuilder == nil {
		panic(fmt.Sprintln("delegating_resolver: resolver for proxy not found for scheme dns"))
	}
	r.proxyURL.Scheme = "dns"
	r.proxyURL.Path = "/" + r.proxyURL.Host
	r.proxyURL.Host = "" // Clear the Host field to conform to the "dns:///" format
	proxyTarget := resolver.Target{URL: *r.proxyURL}
	return proxyBuilder.Build(proxyTarget, &wrappingClientConn{parent: r, resolverType: proxyResolverType}, opts)
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

func (r *delegatingResolver) updateState() []resolver.Address {
	var addresses []resolver.Address
	for _, proxyAddr := range r.proxyAddrs {
		for _, targetAddr := range r.targetAddrs {
			newAddr := resolver.Address{Addr: proxyAddr.Addr}
			newAddr = attributes.SetUserAndConnectAddr(newAddr, r.proxyURL.User, targetAddr.Addr)
			addresses = append(addresses, newAddr)
		}
	}
	// return the combined addresses.
	return addresses
}

// resolverType is an enum representing the type of resolver (target or proxy).
type resolverType int

const (
	targetResolverType resolverType = iota
	proxyResolverType
)

type wrappingClientConn struct {
	parent       *delegatingResolver
	resolverType resolverType // represents the type of resolver (target or proxy)
}

// UpdateState intercepts state updates from the target and proxy resolvers.
func (wcc *wrappingClientConn) UpdateState(state resolver.State) error {
	wcc.parent.mu.Lock()
	defer wcc.parent.mu.Unlock()
	var curState resolver.State
	if wcc.resolverType == targetResolverType {
		wcc.parent.targetAddrs = state.Addresses
		logger.Infof("%v addresses received from target resolver", len(wcc.parent.targetAddrs))
		wcc.parent.targetResolverReady = true
		curState = state
	}
	if wcc.resolverType == proxyResolverType {
		wcc.parent.proxyAddrs = state.Addresses
		logger.Infof("%v addresses received from proxy resolver", len(wcc.parent.proxyAddrs))
		wcc.parent.proxyResolverReady = true
	}

	// Proceed only if updates from both resolvers have been received.
	if !wcc.parent.targetResolverReady || !wcc.parent.proxyResolverReady {
		return nil
	}
	curState.Addresses = wcc.parent.updateState()
	return wcc.parent.cc.UpdateState(curState)
}

// ReportError intercepts errors from the child resolvers and pass to ClientConn.
func (wcc *wrappingClientConn) ReportError(err error) {
	wcc.parent.cc.ReportError(err)
}

// NewAddress intercepts the new resolved address from the child resolvers and
// pass to ClientConn.
func (wcc *wrappingClientConn) NewAddress(addrs []resolver.Address) {
	wcc.UpdateState(resolver.State{Addresses: addrs})
}

// ParseServiceConfig parses the provided service config and returns an
// object that provides the parsed config.
func (wcc *wrappingClientConn) ParseServiceConfig(serviceConfigJSON string) *serviceconfig.ParseResult {
	return wcc.parent.cc.ParseServiceConfig(serviceConfigJSON)
}
