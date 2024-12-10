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
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/proxyattributes"
	"google.golang.org/grpc/internal/resolver/delegatingresolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"

	_ "google.golang.org/grpc/resolver/dns" // To register dns resolver.
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	targetTestAddr          = "test.com"
	resolvedTargetTestAddr1 = "1.2.3.4:8080"
	resolvedTargetTestAddr2 = "1.2.3.5:8080"
	envProxyAddr            = "proxytest.com"
	resolvedProxyTestAddr1  = "2.3.4.5:7687"
	resolvedProxyTestAddr2  = "2.3.4.6:7687"
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

// createTestResolverClientConn initializes a [testutils.ResolverClientConn] and
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

// Tests the scenario where no proxy environment variables are set or proxying
// is disabled by the `NO_PROXY` environment variable. The test verifies that
// the delegating resolver creates only a target resolver and that the
// addresses returned by the delegating resolver exactly match those returned
// by the target resolver.
func (s) TestDelegatingResolverNoProxyEnvVarsSet(t *testing.T) {
	hpfe := func(req *http.Request) (*url.URL, error) { return nil, nil }
	originalhpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = originalhpfe
	}()

	mr := manual.NewBuilderWithScheme("test") // Set up a manual resolver to control the address resolution.
	target := mr.Scheme() + ":///" + targetTestAddr

	// Create a delegating resolver with no proxy configuration
	tcc, stateCh, _ := createTestResolverClientConn(t)
	if _, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mr, false); err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	// Update the manual resolver with a test address.
	mr.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedTargetTestAddr1}},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	// Verify that the delegating resolver outputs the same addresses, as returned
	// by the target resolver.
	wantState := resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedTargetTestAddr1}},
		ServiceConfig: &serviceconfig.ParseResult{},
	}

	var gotState resolver.State
	select {
	case gotState = <-stateCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for a state update from the delegating resolver")
	}

	if !cmp.Equal(gotState, wantState) {
		t.Fatalf("Unexpected state from delegating resolver: %s, want %s", pretty.ToJSON(gotState), pretty.ToJSON(wantState))
	}
}

// setupDNS registers a new manual resolver for the DNS scheme, effectively
// overwriting the previously registered DNS resolver. This allows the test to
// mock the DNS resolution for the proxy resolver. It also registers the
// original DNS resolver after the test is done.
func setupDNS(t *testing.T) *manual.Resolver {
	mr := manual.NewBuilderWithScheme("dns")

	dnsResolverBuilder := resolver.Get("dns")
	resolver.Register(mr)

	t.Cleanup(func() { resolver.Register(dnsResolverBuilder) })
	return mr
}

// proxyAddressWithTargetAttribute creates a resolver.Address for the proxy,
// adding the target address as an attribute.
func proxyAddressWithTargetAttribute(proxyAddr string, targetAddr string) resolver.Address {
	addr := resolver.Address{Addr: proxyAddr}
	addr = proxyattributes.Populate(addr, proxyattributes.Options{
		ConnectAddr: targetAddr,
	})
	return addr
}

// Tests the scenario where proxy is configured and the target URI contains the
// "dns" scheme and target resolution is enabled. The test verifies that the
// addresses returned by the delegating resolver combines the addresses
// returned by the proxy resolver and the target resolver.
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
	originalhpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = originalhpfe
	}()

	mrTarget := setupDNS(t) // Manual resolver to control the target resolution.
	target := mrTarget.Scheme() + ":///" + targetTestAddr
	mrProxy := setupDNS(t) // Set up a manual DNS resolver to control the proxy address resolution.

	tcc, stateCh, _ := createTestResolverClientConn(t)
	if _, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, true); err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	mrProxy.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: resolvedProxyTestAddr1},
			{Addr: resolvedProxyTestAddr2},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	select {
	case <-stateCh:
		t.Fatalf("Delegating resolver invoked UpdateState before both the proxy and target resolvers had updated their states.")
	case <-time.After(defaultTestShortTimeout):
	}

	mrTarget.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: resolvedTargetTestAddr1},
			{Addr: resolvedTargetTestAddr2},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	// Verify that the delegating resolver outputs the expected address.
	wantState := resolver.State{
		Addresses: []resolver.Address{
			proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, resolvedTargetTestAddr1),
			proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, resolvedTargetTestAddr2),
			proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, resolvedTargetTestAddr1),
			proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, resolvedTargetTestAddr2),
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	}
	var gotState resolver.State
	select {
	case gotState = <-stateCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for a state update from the delegating resolver")
	}

	if !cmp.Equal(gotState, wantState) {
		t.Fatalf("Unexpected state from delegating resolver: %s, want %s", pretty.ToJSON(gotState), pretty.ToJSON(wantState))
	}
}

// Tests the scenario where a proxy is configured, the target URI contains the
// "dns" scheme, and target resolution is disabled(default behavior). The test
// verifies that the addresses returned by the delegating resolver include the
// proxy resolver's addresses, with the unresolved target URI as an attribute
// of the proxy address.
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
	originalhpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = originalhpfe
	}()

	mrTarget := setupDNS(t) // Manual resolver to control the target resolution.
	target := mrTarget.Scheme() + ":///" + targetTestAddr
	mrProxy := setupDNS(t) // Set up a manual DNS resolver to control the proxy address resolution.

	tcc, stateCh, _ := createTestResolverClientConn(t)
	if _, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, false); err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	mrProxy.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: resolvedProxyTestAddr1},
			{Addr: resolvedProxyTestAddr2},
		},
	})

	wantState := resolver.State{Addresses: []resolver.Address{
		proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, targetTestAddr),
		proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, targetTestAddr),
	}}

	var gotState resolver.State
	select {
	case gotState = <-stateCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for a state update from the delegating resolver")
	}

	if !cmp.Equal(gotState, wantState) {
		t.Fatalf("Unexpected state from delegating resolver: %s, want %s", pretty.ToJSON(gotState), pretty.ToJSON(wantState))
	}
}

// Tests the scenario where a proxy is configured, and the target URI scheme is
// not "dns". The test verifies that the addresses returned by the delegating
// resolver include the resolved proxy address and the custom resolved target
// address as attributes of the proxy address.
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
	originalhpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = originalhpfe
	}()

	mrTarget := manual.NewBuilderWithScheme("test") // Manual resolver to control the target resolution.
	target := mrTarget.Scheme() + ":///" + targetTestAddr
	mrProxy := setupDNS(t) // Set up a manual DNS resolver to control the proxy address resolution.

	tcc, stateCh, _ := createTestResolverClientConn(t)
	if _, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, false); err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	mrProxy.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: resolvedProxyTestAddr1},
			{Addr: resolvedProxyTestAddr2},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	select {
	case <-stateCh:
		t.Fatalf("Delegating resolver invoked UpdateState before both the proxy and target resolvers had updated their states.")
	case <-time.After(defaultTestShortTimeout):
	}

	mrTarget.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: resolvedTargetTestAddr1},
			{Addr: resolvedTargetTestAddr2},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	wantState := resolver.State{
		Addresses: []resolver.Address{
			proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, resolvedTargetTestAddr1),
			proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, resolvedTargetTestAddr2),
			proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, resolvedTargetTestAddr1),
			proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, resolvedTargetTestAddr2),
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	}
	var gotState resolver.State
	select {
	case gotState = <-stateCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for a state update from the delegating resolver")
	}

	if !cmp.Equal(gotState, wantState) {
		t.Fatalf("Unexpected state from delegating resolver: %s, want %s", pretty.ToJSON(gotState), pretty.ToJSON(wantState))
	}
}

// Tests the scenario where a proxy is configured, the target URI scheme is not 
// "dns," and both the proxy and target resolvers return endpoints. The test 
// verifies that the delegating resolver combines resolved proxy and target 
// addresses correctly, returning endpoints with the proxy address populated 
// and the target address included as an attribute of the proxy address for 
// each combination of proxy and target endpoints.
func (s) TestDelegatingResolverForEndpointsWithProxy(t *testing.T) {
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == targetTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	originalhpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = originalhpfe
	}()

	mrTarget := manual.NewBuilderWithScheme("test") // Manual resolver to control the target resolution.
	target := mrTarget.Scheme() + ":///" + targetTestAddr
	mrProxy := setupDNS(t) // Set up a manual DNS resolver to control the proxy address resolution.

	tcc, stateCh, _ := createTestResolverClientConn(t)
	if _, err := delegatingresolver.New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, false); err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	mrProxy.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: resolvedProxyTestAddr1}}},
			{Addresses: []resolver.Address{{Addr: resolvedProxyTestAddr2}}},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	select {
	case <-stateCh:
		t.Fatalf("Delegating resolver invoked UpdateState before both the proxy and target resolvers had updated their states.")
	case <-time.After(defaultTestShortTimeout):
	}
	const (
		resolvedTargetTestAddr3 = "1.2.3.6:8080"
		resolvedTargetTestAddr4 = "1.2.3.7:8080"
	)
	mrTarget.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{
				Addresses: []resolver.Address{
					{Addr: resolvedTargetTestAddr1},
					{Addr: resolvedTargetTestAddr2}},
			},
			{
				Addresses: []resolver.Address{
					{Addr: resolvedTargetTestAddr3},
					{Addr: resolvedTargetTestAddr4}},
			},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	wantState := resolver.State{
		Endpoints: []resolver.Endpoint{
			{
				Addresses: []resolver.Address{
					proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, resolvedTargetTestAddr1),
					proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, resolvedTargetTestAddr2),
				},
			},
			{
				Addresses: []resolver.Address{
					proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, resolvedTargetTestAddr3),
					proxyAddressWithTargetAttribute(resolvedProxyTestAddr1, resolvedTargetTestAddr4),
				},
			},
			{
				Addresses: []resolver.Address{
					proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, resolvedTargetTestAddr1),
					proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, resolvedTargetTestAddr2),
				},
			},
			{
				Addresses: []resolver.Address{
					proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, resolvedTargetTestAddr3),
					proxyAddressWithTargetAttribute(resolvedProxyTestAddr2, resolvedTargetTestAddr4),
				},
			},
		},
		ServiceConfig: &serviceconfig.ParseResult{},
	}
	var gotState resolver.State
	select {
	case gotState = <-stateCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for a state update from the delegating resolver")
	}

	if !cmp.Equal(gotState, wantState) {
		t.Fatalf("Unexpected state from delegating resolver: %s, want %s", pretty.ToJSON(gotState), pretty.ToJSON(wantState))
	}
}
