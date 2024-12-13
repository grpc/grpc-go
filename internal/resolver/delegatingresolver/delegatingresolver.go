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

// Package delegatingresolver implements a resolver capable of resolving both
// target URIs and proxy addresses.
package delegatingresolver

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/proxyattributes"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

var logger = grpclog.Component("delegating-resolver")

func init() {
	// internal.HTTPSProxyFromEnvironment will be overwritten in the tests
	internal.HTTPSProxyFromEnvironment = http.ProxyFromEnvironment
}

// delegatingResolver manages both target URI and proxy address resolution by
// delegating these tasks to separate child resolvers. Essentially, it acts as
// a intermediatory between the gRPC ClientConn and the child resolvers.
//
// It implements the [resolver.Resolver] interface.
type delegatingResolver struct {
	target         resolver.Target     // parsed target URI to be resolved
	cc             resolver.ClientConn // gRPC ClientConn
	targetResolver resolver.Resolver   // resolver for the target URI, based on its scheme
	proxyResolver  resolver.Resolver   // resolver for the proxy URI; nil if no proxy is configured

	mu              sync.Mutex          // protects access to the resolver state and addresses during updates
	targetAddrs     []resolver.Address  // resolved or unresolved target addresses, depending on proxy configuration
	targetEndpoints []resolver.Endpoint // resolved target endpoint
	proxyAddrs      []resolver.Address  // resolved proxy addresses; empty if no proxy is configured
	curState        resolver.State      // current resolver state

	proxyURL            *url.URL // proxy URL, derived from proxy environment and target
	targetResolverReady bool     // indicates if an update from the target resolver has been received
	proxyResolverReady  bool     // indicates if an update from the proxy resolver has been received
}

// parsedURLForProxy determines the proxy URL for the given address based on
// the environment. It can return the following:
//   - nil URL, nil error: No proxy is configured or the address is excluded
//     using the `NO_PROXY` environment variable or if req.URL.Host is
//     "localhost" (with or without // a port number)
//   - nil URL, non-nil error: An error occurred while retrieving the proxy URL.
//   - non-nil URL, nil error: A proxy is configured, and the proxy URL was
//     retrieved successfully without any errors.
func parsedURLForProxy(address string) (*url.URL, error) {
	pf := (internal.HTTPSProxyFromEnvironment).(func(req *http.Request) (*url.URL, error))
	req := &http.Request{URL: &url.URL{
		Scheme: "https",
		Host:   address,
	}}

	return pf(req)
}

// New creates a new delegating resolver that can create up to two child
// resolvers:
//   - one to resolve the proxy address specified using the supported
//     environment variables. This uses the registered resolver for the "dns"
//     scheme.
//   - one to resolve the target URI using the resolver specified by the scheme
//     in the target URI or specified by the user using the WithResolvers dial
//     option. As a special case, if the target URI's scheme is "dns" and a
//     proxy is specified using the supported environment variables, the target
//     URI's path portion is used as the resolved address unless target
//     resolution is enabled using the dial option.
func New(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions, targetResolverBuilder resolver.Builder, targetResolutionEnabled bool) (resolver.Resolver, error) {
	r := &delegatingResolver{
		target: target,
		cc:     cc,
	}

	var err error
	r.proxyURL, err = parsedURLForProxy(target.Endpoint())
	if err != nil {
		return nil, fmt.Errorf("delegating_resolver: failed to determine proxy URL for target %s: %v", target, err)
	}

	// proxy is not configured or proxy address excluded using `NO_PROXY` env
	// var, so only target resolver is used.
	if r.proxyURL == nil {
		return targetResolverBuilder.Build(target, cc, opts)
	}

	if logger.V(2) {
		logger.Info("Proxy URL detected : %s", r.proxyURL)
	}

	// When the scheme is 'dns' and target resolution on client is not enabled,
	// resolution should be handled by the proxy, not the client. Therefore, we
	// bypass the target resolver and store the unresolved target address.
	if target.URL.Scheme == "dns" && !targetResolutionEnabled {
		r.targetAddrs = []resolver.Address{{Addr: target.Endpoint()}}
		r.targetResolverReady = true
	} else {
		wcc := &wrappingClientConn{
			stateListener: r.updateTargetResolverState,
			parent:        r,
		}
		if r.targetResolver, err = targetResolverBuilder.Build(target, wcc, opts); err != nil {
			return nil, fmt.Errorf("delegating_resolver: unable to build the resolver for target %s: %v", target, err)
		}
	}

	if r.proxyResolver, err = r.proxyURIResolver(opts); err != nil {
		return nil, fmt.Errorf("delegating_resolver: failed to build resolver for proxy URL %q: %v", r.proxyURL, err)
	}
	return r, nil
}

// proxyURIResolver creates a resolver for resolving proxy URIs using the
// "dns" scheme. It adjusts the proxyURL to conform to the "dns:///" format and
// builds a resolver with a wrappingClientConn to capture resolved addresses.
func (r *delegatingResolver) proxyURIResolver(opts resolver.BuildOptions) (resolver.Resolver, error) {
	proxyBuilder := resolver.Get("dns")
	if proxyBuilder == nil {
		panic("delegating_resolver: resolver for proxy not found for scheme dns")
	}
	r.proxyURL.Scheme = "dns"
	r.proxyURL.Path = "/" + r.proxyURL.Host
	r.proxyURL.Host = "" // Clear the Host field to conform to the "dns:///" format

	proxyTarget := resolver.Target{URL: *r.proxyURL}
	wcc := &wrappingClientConn{
		stateListener: r.updateProxyResolverState,
		parent:        r}
	return proxyBuilder.Build(proxyTarget, wcc, opts)
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
		r.targetResolver = nil
	}
	if r.proxyResolver != nil {
		r.proxyResolver.Close()
		r.proxyResolver = nil
	}
}

// combinedAddressesLocked creates a list of combined addresses by
// pairing each proxy address with every target address. For each pair, it
// generates a new [resolver.Address] using the proxy address, and adding the
// target address as the attribute along with user info.
func (r *delegatingResolver) combinedAddressesLocked() ([]resolver.Address, []resolver.Endpoint) {
	var addresses []resolver.Address
	for _, proxyAddr := range r.proxyAddrs {
		for _, targetAddr := range r.targetAddrs {
			newAddr := resolver.Address{Addr: proxyAddr.Addr}
			newAddr = proxyattributes.Populate(newAddr, proxyattributes.Options{
				User:        r.proxyURL.User,
				ConnectAddr: targetAddr.Addr,
			})
			addresses = append(addresses, newAddr)
		}
	}

	// Create a list of combined addresses by pairing each proxy endpoint
	// with every target endpoint. For each pair, it constructs a new
	// [resolver.Endpoint] using the first address from the proxy endpoint,
	// as the DNS resolver is expected to return only one address per proxy
	// endpoint. The target address and user information from the proxy URL
	// are added as attributes to this address. The resulting list of addresses
	// is then grouped into endpoints, covering all combinations of proxy and
	// target endpoints.
	var endpoints []resolver.Endpoint
	for _, endpt := range r.targetEndpoints {
		var addrs []resolver.Address
		for _, proxyAddr := range r.proxyAddrs {
			for _, targetAddr := range endpt.Addresses {
				newAddr := resolver.Address{Addr: proxyAddr.Addr}
				newAddr = proxyattributes.Populate(newAddr, proxyattributes.Options{
					User:        r.proxyURL.User,
					ConnectAddr: targetAddr.Addr,
				})
				addrs = append(addrs, newAddr)
			}
		}
		endpoints = append(endpoints, resolver.Endpoint{Addresses: addrs})
	}
	return addresses, endpoints
}

// updateProxyResolverState updates the proxy resolver state by storing proxy
// addresses and endpoints, marking the resolver as ready, and triggering a
// state update if both proxy and target resolvers are ready. It is a
// StateListener function of wrappingClientConn passed to the proxy resolver.
func (r *delegatingResolver) updateProxyResolverState(state resolver.State) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if logger.V(2) {
		logger.Infof("Addresses received from proxy resolver: %s", state.Addresses)
	}
	r.proxyAddrs = state.Addresses
	if r.proxyAddrs == nil && state.Endpoints != nil {
		for _, endpoint := range state.Endpoints {
			r.proxyAddrs = append(r.proxyAddrs, endpoint.Addresses...)
		}
	}
	r.proxyResolverReady = true
	r.updateStateIfReadyLocked()
	return nil
}

// updateTargetResolverState updates the target resolver state by storing target
// addresses, endpoints, and service config, marking the resolver as ready, and
// triggering a state update if both resolvers are ready. It is a StateListener
// function of wrappingClientConn passed to the target resolver.
func (r *delegatingResolver) updateTargetResolverState(state resolver.State) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if logger.V(2) {
		logger.Infof("Addresses received from target resolver: %v", state.Addresses)
	}
	r.targetAddrs = state.Addresses
	r.targetEndpoints = state.Endpoints
	r.targetResolverReady = true
	// Update curState to include other state information, such as the
	// service config, provided by the target resolver. This ensures
	// curState contains all necessary information when passed to
	// UpdateState. The state update is only sent after both the target and
	// proxy resolvers have sent their updates, and curState has been
	// updated with the combined addresses.
	r.curState = state
	r.updateStateIfReadyLocked()
	return nil
}

// updateStateIfReadyLocked checks if both proxy and target resolvers are ready,
// combines addresses, and updates the ClientConn state.
func (r *delegatingResolver) updateStateIfReadyLocked() {
	if !r.targetResolverReady || !r.proxyResolverReady {
		return
	}
	r.curState.Addresses, r.curState.Endpoints = r.combinedAddressesLocked()
	r.cc.UpdateState(r.curState)
}

// wrappingClientConn serves as an intermediary between the parent ClientConn
// and the child resolvers created here. It implements the resolver.ClientConn
// interface and is passed in that capacity to the child resolvers.
//
// Its primary function is to aggregate addresses returned by the child
// resolvers before passing them to the parent ClientConn. Any errors returned
// by the child resolvers are propagated verbatim to the parent ClientConn.
type wrappingClientConn struct {
	// Callback to deliver resolver state updates
	stateListener func(state resolver.State) error
	parent        *delegatingResolver
}

// UpdateState receives resolver state updates and forwards them to the
// appropriate listener function (either for the proxy or target resolver).
func (wcc *wrappingClientConn) UpdateState(state resolver.State) error {
	if wcc.stateListener == nil {
		return fmt.Errorf("delegating_resolver: stateListener not set")
	}
	return wcc.stateListener(state)
}

// ReportError intercepts errors from the child resolvers and passes them to ClientConn.
func (wcc *wrappingClientConn) ReportError(err error) {
	wcc.parent.cc.ReportError(err)
}

// NewAddress intercepts the new resolved address from the child resolvers and
// passes them to ClientConn.
func (wcc *wrappingClientConn) NewAddress(addrs []resolver.Address) {
	wcc.UpdateState(resolver.State{Addresses: addrs})
}

// ParseServiceConfig parses the provided service config and returns an
// object that provides the parsed config.
func (wcc *wrappingClientConn) ParseServiceConfig(serviceConfigJSON string) *serviceconfig.ParseResult {
	return wcc.parent.cc.ParseServiceConfig(serviceConfigJSON)
}
