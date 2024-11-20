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

// Package delegatingresolver implements the default resolver for gRPC.
// It handles both target URI and proxy address resolution, unless:
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
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

var (
	// The following variable will be overwritten in the tests.
	httpProxyFromEnvironment = http.ProxyFromEnvironment

	logger = grpclog.Component("delegatingresolver")
)

// delegatingResolver manages the resolution of both a target URI and an
// optional proxy URI. It acts as an intermediary between the underlying
// resolvers and the gRPC ClientConn. This type implements the
// resolver.ClientConn interface to intercept state updates from both the target
// and proxy resolvers, combining the results before forwarding them to the
// client connection.
type delegatingResolver struct {
	target         resolver.Target     // parsed target URI to be resolved
	cc             resolver.ClientConn // gRPC ClientConn
	targetResolver resolver.Resolver   // resolver for the target URI, based on its scheme
	proxyResolver  resolver.Resolver   // resolver for the proxy URI; nil if no proxy is configured
	targetAddrs    []resolver.Address  // resolved or unresolved target addresses, depending on proxy configuration
	proxyAddrs     []resolver.Address  // resolved proxy addresses; empty if no proxy is configured
	proxyURL       *url.URL            // proxy URL, derived from proxy environment and target
	mu             sync.Mutex          // protects access to the resolver state during updates
}

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

// New creates a new delegating resolver that will call the target and proxy
// child resolvers based on the proxy enviorinment configurations.
func New(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions, targetResolverBuilder resolver.Builder, targetResolutionEnabled bool) (resolver.Resolver, error) {
	r := &delegatingResolver{
		target: target,
		cc:     cc,
	}

	var err error
	r.proxyURL, err = mapAddress(target.Endpoint())
	if err != nil {
		return nil, fmt.Errorf("failed to determine proxy URL for target : %v", err)
	}

	if r.proxyURL == nil {
		logger.Info("No proxy URL detected")
		return targetResolverBuilder.Build(target, cc, opts)
	}

	// When the scheme is 'dns', resolution should be handled by the proxy, not
	// the client. Therefore, we bypass the target resolver and store the
	// unresolved target address.
	if target.URL.Scheme == "dns" && !targetResolutionEnabled {
		r.targetAddrs = []resolver.Address{{Addr: target.Endpoint()}}
	} else {
		// targetResolverBuilder must not be nil. If it is nil, the channel should
		// error out and not call this function.
		r.targetResolver, err = targetResolverBuilder.Build(target, &wrappingClientConn{r, "target"}, opts)
		if err != nil {
			return nil, fmt.Errorf("delegating_resolver: unable to build the target resolver : %v", err)
		}
	}
	r.proxyResolver, err = proxyURIResolver(r.proxyURL, opts, r)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func proxyURIResolver(proxyURL *url.URL, opts resolver.BuildOptions, presolver *delegatingResolver) (resolver.Resolver, error) {
	scheme := "dns"
	proxyBuilder := resolver.Get(scheme)
	if proxyBuilder == nil {
		return nil, fmt.Errorf("delegating_resolver: resolver for proxy not found")
	}
	host := "dns:///" + proxyURL.Host
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}

	proxyTarget := resolver.Target{URL: *u}
	return proxyBuilder.Build(proxyTarget, &wrappingClientConn{presolver, "proxy"}, opts)
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

type keyType string

const proxyConnectAddrKey = keyType("grpc.resolver.delegatingresolver.proxyConnectAddr")
const userKey = keyType("grpc.resolver.delegatingresolver.user")

// SetConnectAddr returns a copy of the provided resolver.Address with attributes
// containing address to be sent in connect request to proxy.  It's data should
// not be mutated after calling SetConnectAddr.
func SetConnectAddr(resAddr resolver.Address, addr string) resolver.Address {
	resAddr.Attributes = resAddr.Attributes.WithValue(proxyConnectAddrKey, addr)
	return resAddr
}

// GetConnectAddr returns the proxy connect address in resolver.Address, or nil
// if not present. The returned data should not be mutated.
func GetConnectAddr(addr resolver.Address) string {
	s, _ := addr.Attributes.Value(proxyConnectAddrKey).(string)
	return s
}

// SetUser returns a copy of the provided address with attributes containing
// user info. It's data should not be mutated after calling SetUser.
func SetUser(addr resolver.Address, user *url.Userinfo) resolver.Address {
	addr.Attributes = addr.Attributes.WithValue(userKey, user)
	return addr
}

// GetUser returns the user info in the resolver.Address, or nil if not present.
// The returned data should not be mutated.
func GetUser(addr resolver.Address) *url.Userinfo {
	user, _ := addr.Attributes.Value(userKey).(*url.Userinfo)
	return user
}

func (r *delegatingResolver) updateState(state resolver.State) resolver.State {
	var addresses []resolver.Address
	for _, proxyAddr := range r.proxyAddrs {
		for _, targetAddr := range r.targetAddrs {
			newAddr := resolver.Address{
				Addr: proxyAddr.Addr,
			}
			newAddr = SetConnectAddr(newAddr, targetAddr.Addr)
			if r.proxyURL.User != nil {
				newAddr = SetUser(newAddr, r.proxyURL.User)
			}
			addresses = append(addresses, newAddr)
		}
	}
	// Set the addresses in the current state.
	state.Addresses = addresses
	return state
}

type wrappingClientConn struct {
	parent       *delegatingResolver
	resolverType string
}

// UpdateState intercepts state updates from the target and proxy resolvers.
func (wcc *wrappingClientConn) UpdateState(state resolver.State) error {
	wcc.parent.mu.Lock()
	defer wcc.parent.mu.Unlock()
	var curState resolver.State
	if wcc.resolverType == "target" {
		wcc.parent.targetAddrs = state.Addresses
		curState = state
	}
	if wcc.resolverType == "proxy" {
		wcc.parent.proxyAddrs = state.Addresses
	}

	if len(wcc.parent.targetAddrs) == 0 || len(wcc.parent.proxyAddrs) == 0 {
		return nil
	}
	curState = wcc.parent.updateState(curState)
	return wcc.parent.cc.UpdateState(curState)
}

// ReportError intercepts errors from the child resolvers and pass to ClientConn.
func (wcc *wrappingClientConn) ReportError(err error) {
	wcc.parent.cc.ReportError(err)
}

// NewAddress intercepts the new resolved address from the chid resolvers and
// pass to CLientConn
func (wcc *wrappingClientConn) NewAddress(addrs []resolver.Address) {
	wcc.UpdateState(resolver.State{Addresses: addrs})
}

// ParseServiceConfig parses the provided service config and returns an
// object that provides the parsed config.
func (wcc *wrappingClientConn) ParseServiceConfig(serviceConfigJSON string) *serviceconfig.ParseResult {
	return wcc.parent.cc.ParseServiceConfig(serviceConfigJSON)
}
