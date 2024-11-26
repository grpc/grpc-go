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

package delegatingresolver

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
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

// overwriteAndRestore overwrite function HTTPSProxyFromEnvironment and
// returns a function to restore the default values.
func overwrite(hpfe func(req *http.Request) (*url.URL, error)) func() {
	backHPFE := HTTPSProxyFromEnvironment
	HTTPSProxyFromEnvironment = hpfe
	return func() {
		HTTPSProxyFromEnvironment = backHPFE
	}
}

func (s) TestUpdateProxyUrlEnv(t *testing.T) {
	// Overwrite the function in the test and restore them in defer.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == targetTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	defer overwrite(hpfe)()

	// envTestAddr should be handled by ProxyFromEnvironment.
	got, err := updateProxyURL(targetTestAddr)
	if err != nil {
		t.Error(err)
	}
	if got.Host != envProxyAddr {
		t.Errorf("want %v, got %v", envProxyAddr, got)
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

// overwriteAndRestoreProxyEnv overwrites the proxy environment variable and
// returns a function to restore the default values.
func overwriteAndRestoreProxyEnv(proxyURI string) func() {
	origHTTPSProxy := envconfig.HTTPSProxy
	envconfig.HTTPSProxy = proxyURI
	return func() { envconfig.HTTPSProxy = origHTTPSProxy }
}

// TestDelegatingResolverNoProxy verifies the behavior of the delegating resolver
// when no proxy is configured.
func (s) TestDelegatingResolverNoProxy(t *testing.T) {
	// Enable HTTP Proxy env var.
	defer overwriteAndRestoreProxyEnv("")()
	mr := manual.NewBuilderWithScheme("test") // Set up a manual resolver to control the address resolution.
	target := "test:///" + targetTestAddr

	tcc, stateCh, _ := createTestResolverClientConn(t)
	// Create a delegating resolver with no proxy configuration
	dr, err := New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mr, false)
	if err != nil || dr == nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}

	// Update the manual resolver with a test address.
	mr.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedTargetTestAddr}},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	// Verify that the delegating resolver outputs the same address.
	expectedState := resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedTargetTestAddr}},
		ServiceConfig: &serviceconfig.ParseResult{},
	}
	state := <-stateCh
	if len(state.Addresses) != 1 || !cmp.Equal(expectedState, state) {
		t.Errorf("Unexpected state from delegating resolver: %v, want %v", state, expectedState)
	}
}

// setupDNS unregisters the DNS resolver and registers a manual resolver for the
// same scheme. This allows the test to mock the DNS resolution for the proxy resolver.
func setupDNS(t *testing.T) *manual.Resolver {

	mr := manual.NewBuilderWithScheme("dns")

	dnsResolverBuilder := resolver.Get("dns")
	resolver.Register(mr)

	t.Cleanup(func() { resolver.Register(dnsResolverBuilder) })
	return mr
}

// TestDelegatingResolverwithDNSAndProxy verifies that the delegating resolver
// correctly updates state when the target URI scheme is DNS and a proxy is
// configured and target resolution is enabled.
func (s) TestDelegatingResolverwithDNSAndProxyWithTargetResolution(t *testing.T) {
	// Enable HTTP Proxy env var.
	defer overwriteAndRestoreProxyEnv(envProxyAddr)()
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == targetTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	defer overwrite(hpfe)()
	mrTarget := setupDNS(t) // Manual resolver to control the target resolution.
	mrProxy := setupDNS(t)  // Set up a manual DNS resolver to control the proxy address resolution.
	target := "dns:///" + targetTestAddr

	tcc, stateCh, _ := createTestResolverClientConn(t)
	dr, err := New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, true)
	if err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}
	if dr == nil {
		t.Fatalf("Failed to create delegating resolver")
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
	expectedAddr := resolver.Address{Addr: resolvedProxyTestAddr}
	expectedAddr = SetConnectAddr(expectedAddr, resolvedTargetTestAddr)
	expectedState := resolver.State{Addresses: []resolver.Address{expectedAddr}}

	state := <-stateCh
	if len(state.Addresses) != 1 || !cmp.Equal(expectedState, state) {
		t.Fatalf("Unexpected state from delegating resolver: %v\n, want %v\n", state, expectedState)
	}
}

// TestDelegatingResolverwithDNSAndProxy verifies that the delegating resolver
// correctly updates state when the target URI scheme is DNS and a proxy is
// configured.
func (s) TestDelegatingResolverwithDNSAndProxyWithNoTargetResolution(t *testing.T) {
	// Enable HTTP Proxy env var.
	defer overwriteAndRestoreProxyEnv(envProxyAddr)()
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == targetTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	defer overwrite(hpfe)()
	mrTarget := manual.NewBuilderWithScheme("test") // Manual resolver to control the target resolution.
	mrProxy := setupDNS(t)                          // Set up a manual DNS resolver to control the proxy address resolution.
	target := "dns:///" + targetTestAddr

	tcc, stateCh, _ := createTestResolverClientConn(t)
	dr, err := New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, false)
	if err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}
	if dr == nil {
		t.Fatalf("Failed to create delegating resolver")
	}

	mrProxy.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: resolvedProxyTestAddr}},
		ServiceConfig: &serviceconfig.ParseResult{},
	})

	// Verify that the delegating resolver outputs the same address.
	expectedAddr := resolver.Address{Addr: resolvedProxyTestAddr}
	expectedAddr = SetConnectAddr(expectedAddr, targetTestAddr)
	expectedState := resolver.State{Addresses: []resolver.Address{expectedAddr}}

	state := <-stateCh
	if len(state.Addresses) != 1 || !cmp.Equal(expectedState, state) {
		t.Fatalf("Unexpected state from delegating resolver: %v\n, want %v\n", state, expectedState)
	}
}

// TestDelegatingResolverwithDNSAndProxy verifies that the delegating resolver
// correctly updates state when the target URI scheme is not DNS and a proxy is
// configured.
func (s) TestDelegatingResolverwithCustomResolverAndProxy(t *testing.T) {
	// Enable HTTP Proxy env var.
	defer overwriteAndRestoreProxyEnv(envProxyAddr)()
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == targetTestAddr {
			return &url.URL{
				Scheme: "https",
				Host:   envProxyAddr,
			}, nil
		}
		return nil, nil
	}
	defer overwrite(hpfe)()

	mrTarget := manual.NewBuilderWithScheme("test") // Manual resolver to control the target resolution.
	mrProxy := setupDNS(t)                          // Set up a manual DNS resolver to control the proxy address resolution.
	target := "test:///" + targetTestAddr

	tcc, stateCh, _ := createTestResolverClientConn(t)
	dr, err := New(resolver.Target{URL: *testutils.MustParseURL(target)}, tcc, resolver.BuildOptions{}, mrTarget, false)
	if err != nil {
		t.Fatalf("Failed to create delegating resolver: %v", err)
	}
	if dr == nil {
		t.Fatalf("Failed to create delegating resolver")
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

	expectedAddr := resolver.Address{Addr: resolvedProxyTestAddr}
	expectedAddr = SetConnectAddr(expectedAddr, resolvedTargetTestAddr)

	expectedAddr1 := resolver.Address{Addr: resolvedProxyTestAddr}
	expectedAddr1 = SetConnectAddr(expectedAddr1, resolvedTargetTestAddr1)

	expectedAddr2 := resolver.Address{Addr: resolvedProxyTestAddr1}
	expectedAddr2 = SetConnectAddr(expectedAddr2, resolvedTargetTestAddr)

	expectedAddr3 := resolver.Address{Addr: resolvedProxyTestAddr1}
	expectedAddr3 = SetConnectAddr(expectedAddr3, resolvedTargetTestAddr1)

	expectedState := resolver.State{Addresses: []resolver.Address{
		expectedAddr,
		expectedAddr1,
		expectedAddr2,
		expectedAddr3,
	},
		ServiceConfig: &serviceconfig.ParseResult{},
	}

	state := <-stateCh
	if len(state.Addresses) != 4 || !cmp.Equal(expectedState, state) {
		t.Fatalf("Unexpected state from delegating resolver: %v\n, want %v\n", state, expectedState)
	}
}
