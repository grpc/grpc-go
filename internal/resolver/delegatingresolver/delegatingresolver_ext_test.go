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

package delegatingresolver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/proxyattributes"
	"google.golang.org/grpc/internal/resolver/delegatingresolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	_ "google.golang.org/grpc/resolver/dns" // To register dns resolver.
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	targetTestAddr          = "test.com"
	resolvedTargetTestAddr  = "1.2.3.4:8080"
	resolvedTargetTestAddr1 = "1.2.3.5:8080"
	envProxyAddr            = "proxytest.com"
	resolvedProxyTestAddr   = "2.3.4.5:7687"
	resolvedProxyTestAddr1  = "2.3.4.6:7687"
)

// overrideHTTPSProxyFromEnvironment function overwrites HTTPSProxyFromEnvironment and
// returns a function to restore the default values.
func overrideHTTPSProxyFromEnvironment(hpfe func(req *http.Request) (*url.URL, error)) func() {
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	return func() {
		internal.HTTPSProxyFromEnvironmentForTesting = nil
	}
}

// createTestResolverClientConn initializes a test ResolverClientConn and
// returns it along with channels for resolver state updates and errors.
func createTestResolverClientConn(t *testing.T) (*testutils.ResolverClientConn, chan resolver.State, chan error) {
	stateCh := make(chan resolver.State, 1)
	errCh := make(chan error, 1)

	tcc := &testutils.ResolverClientConn{
		Logger:       t,
		UpdateStateF: func(s resolver.State) error { stateCh <- s; return nil },
		ReportErrorF: func(err error) { errCh <- err },
	}
	return tcc, stateCh, errCh
}

// Tests that the delegating resolver behaves correctly when no proxy
// environment variables are set. In this case, no proxy resolver is created,
// only a target resolver is used, and the addresses returned by the delegating
// resolver should exactly match those returned by the target resolver.
func (s) TestDelegatingResolverNoProxy(t *testing.T) {
	mr := manual.NewBuilderWithScheme("test") // Set up a manual resolver to control the address resolution.
	target := mr.Scheme() + ":///" + targetTestAddr

	tcc, stateCh, _ := createTestResolverClientConn(t)
	// Create a delegating resolver with no proxy configuration
	_, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mr, false)
	if err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	// Update the manual resolver with a test address.
	mr.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedTargetTestAddr}},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	// Verify that the delegating resolver outputs the same address.
	wantedState := resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedTargetTestAddr}},
		ServiceConfig: &serviceconfig.ParseResult{},
	}

	if state := <-stateCh; !cmp.Equal(wantedState, state) {
		t.Fatalf("Unexpected state from delegating resolver: %v, want %v", state, wantedState)
	}
}

// setupDNS registers a new manual resolver for the DNS scheme, effectively
// overwriting the previously registered DNS resolver. This allows the test to
// mock the DNS resolution for the proxy resolver. It also registers the original
// DNS resolver after the test is done.
func setupDNS(t *testing.T) *manual.Resolver {
	mr := manual.NewBuilderWithScheme("dns")

	dnsResolverBuilder := resolver.Get("dns")
	resolver.Register(mr)

	t.Cleanup(func() { resolver.Register(dnsResolverBuilder) })
	return mr
}

// Tests that the delegating resolver correctly updates state with resolver
// proxy address with resolved target URI as attribute of the proxy address when
// the target URI scheme is DNS and a proxy is configured and target resolution
// is enabled.
func (s) TestDelegatingResolverwithDNSAndProxyWithTargetResolution(t *testing.T) {
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == targetTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()
	mrTarget := setupDNS(t) // Manual resolver to control the target resolution.
	mrProxy := setupDNS(t)  // Set up a manual DNS resolver to control the proxy address resolution.
	target := "dns:///" + targetTestAddr

	tcc, stateCh, _ := createTestResolverClientConn(t)
	_, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, true)
	if err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	mrTarget.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: resolvedTargetTestAddr},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	mrProxy.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedProxyTestAddr}},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	// Verify that the delegating resolver outputs the same address.
	wantedAddr := resolver.Address{Addr: resolvedProxyTestAddr}
	wantedAddr = proxyattributes.Populate(wantedAddr, nil, resolvedTargetTestAddr)
	wantedState := resolver.State{Addresses: []resolver.Address{wantedAddr}}

	if state := <-stateCh; len(state.Addresses) != 1 || !cmp.Equal(wantedState, state) {
		t.Fatalf("Unexpected state from delegating resolver: %v\n, want %v\n", state, wantedState)
	}
}

// Tests that the delegating resolver correctly updates state with resolver
// proxy address with unresolved target URI as attribute of the proxy address
// when the target URI scheme is DNS and a proxy is configured and default target
// resolution(that is not enabled.)
func (s) TestDelegatingResolverwithDNSAndProxyWithNoTargetResolution(t *testing.T) {
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == targetTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()
	mrTarget := manual.NewBuilderWithScheme("test") // Manual resolver to control the target resolution.
	mrProxy := setupDNS(t)                          // Set up a manual DNS resolver to control the proxy address resolution.
	target := "dns:///" + targetTestAddr

	tcc, stateCh, _ := createTestResolverClientConn(t)
	_, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, false)
	if err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	mrProxy.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedProxyTestAddr}},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	// Verify that the delegating resolver outputs the same address.
	wantedAddr := resolver.Address{Addr: resolvedProxyTestAddr}
	wantedAddr = proxyattributes.Populate(wantedAddr, nil, targetTestAddr)
	wantedState := resolver.State{Addresses: []resolver.Address{wantedAddr}}

	if state := <-stateCh; len(state.Addresses) != 1 || !cmp.Equal(wantedState, state) {
		t.Fatalf("Unexpected state from delegating resolver: %v\n, want %v\n", state, wantedState)
	}
}

// Tests that the delegating resolver
// correctly updates state with resolved proxy address and custom resolved target
// address as the proxyattributes when the target URI scheme is not DNS and a proxy is
// configured.
func (s) TestDelegatingResolverwithCustomResolverAndProxy(t *testing.T) {
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == targetTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()

	mrTarget := manual.NewBuilderWithScheme("test") // Manual resolver to control the target resolution.
	mrProxy := setupDNS(t)                          // Set up a manual DNS resolver to control the proxy address resolution.
	target := mrTarget.Scheme() + ":///" + targetTestAddr

	tcc, stateCh, _ := createTestResolverClientConn(t)
	_, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, false)
	if err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	mrProxy.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: resolvedProxyTestAddr},
			{Addr: resolvedProxyTestAddr1},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})
	mrTarget.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: resolvedTargetTestAddr},
			{Addr: resolvedTargetTestAddr1},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	wantedAddr := resolver.Address{Addr: resolvedProxyTestAddr}
	wantedAddr = proxyattributes.Populate(wantedAddr, nil, resolvedTargetTestAddr)

	wantedAddr1 := resolver.Address{Addr: resolvedProxyTestAddr}
	wantedAddr1 = proxyattributes.Populate(wantedAddr1, nil, resolvedTargetTestAddr1)

	wantedAddr2 := resolver.Address{Addr: resolvedProxyTestAddr1}
	wantedAddr2 = proxyattributes.Populate(wantedAddr2, nil, resolvedTargetTestAddr)

	wantedAddr3 := resolver.Address{Addr: resolvedProxyTestAddr1}
	wantedAddr3 = proxyattributes.Populate(wantedAddr3, nil, resolvedTargetTestAddr1)

	wantedState := resolver.State{Addresses: []resolver.Address{
		wantedAddr,
		wantedAddr1,
		wantedAddr2,
		wantedAddr3,
	},
		ServiceConfig: &serviceconfig.ParseResult{},
	}

	if state := <-stateCh; len(state.Addresses) != 4 || !cmp.Equal(wantedState, state) {
		t.Fatalf("Unexpected state from delegating resolver: %v\n, want %v\n", state, wantedState)
	}
}
