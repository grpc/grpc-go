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

package test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/resolver/delegatingresolver"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

const (
	unresolvedTargetURI = "example.com"
	unresolvedProxyURI  = "proxyExample.com"
)

// overrideHTTPSProxyFromEnvironment function overwrites HTTPSProxyFromEnvironment and
// returns a function to restore the default values.
func overrideHTTPSProxyFromEnvironment(hpfe func(req *http.Request) (*url.URL, error)) func() {
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	return func() {
		internal.HTTPSProxyFromEnvironmentForTesting = nil
	}
}

// createAndStartBackendServer creates and starts a test backend server,
// registering a cleanup to stop it.
func createAndStartBackendServer(t *testing.T) string {
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testgrpc.Empty) (*testgrpc.Empty, error) { return &testgrpc.Empty{}, nil },
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(backend.Stop)
	return backend.Address
}

// setupDNS sets up a manual DNS resolver and registers it, returning the
// builder for test customization.
func setupDNS(t *testing.T) *manual.Resolver {
	manualResolver := manual.NewBuilderWithScheme("dns")
	originalResolver := resolver.Get("dns")
	resolver.Register(manualResolver)
	t.Cleanup(func() {
		resolver.Register(originalResolver)
	})
	return manualResolver
}

// setupProxy initializes and starts a proxy server, registers a cleanup to
// stop it, and returns the proxy's listener and helper channels.
func setupProxy(t *testing.T, backendAddr string, resolutionOnClient bool, reqCheck func(*http.Request) error) (net.Listener, chan error, chan struct{}, chan struct{}) {
	proxyListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	proxyStartedCh := make(chan struct{})
	t.Cleanup(func() {
		close(errCh)
	})

	proxyServer := testutils.NewProxyServer(proxyListener, reqCheck, errCh, doneCh, backendAddr, resolutionOnClient, func() { close(proxyStartedCh) })
	t.Cleanup(proxyServer.Stop)

	return proxyListener, errCh, doneCh, proxyStartedCh
}

// TestGrpcDialWithProxy tests grpc.Dial using a proxy and default
// resolver in the target URI and verifies that it connects to the proxy server
// and sends unresolved target URI in the HTTP CONNECT request and then
// connects to the backend server.
func (s) TestGrpcDialWithProxy(t *testing.T) {
	backendAddr := createAndStartBackendServer(t)
	proxyListener, errCh, doneCh, _ := setupProxy(t, backendAddr, false, func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != unresolvedTargetURI {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, unresolvedTargetURI)
		}
		return nil
	})

	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresolvedTargetURI {
			return &url.URL{
				Scheme: "https",
				Host:   proxyListener.Addr().String(),
			}, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	conn, err := grpc.Dial(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial failed: %v", err)
	}
	defer conn.Close()

	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall failed: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	case <-doneCh:
		t.Logf("proxy server succeeded")
	}
}

// TestGrpcDialWithProxyAndResolution tests grpc.Dial with a proxy and ensures DNS
// resolution of the proxy URI is performed.
func (s) TestGrpcDialWithProxyAndResolution(t *testing.T) {
	// Create and start a backend server.
	backendAddr := createAndStartBackendServer(t)

	// Set up and start a proxy listener.
	proxyLis, errCh, doneCh, _ := setupProxy(t, backendAddr, false, func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != unresolvedTargetURI {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, unresolvedTargetURI)
		}
		return nil
	})

	// Overwrite the default proxy function and restore it after the test.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresolvedTargetURI {
			return &url.URL{
				Scheme: "https",
				Host:   unresolvedProxyURI,
			}, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()

	// Set up a manual resolver for proxy resolution.
	mrProxy := setupDNS(t)

	// Update the proxy resolver state with the proxy's address.
	mrProxy.InitialState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: proxyLis.Addr().String()},
		},
	})
	// Dial to the proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.Dial(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial failed: %v", err)
	}
	defer conn.Close()

	// Send an RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall failed: %v", err)
	}

	// Verify that the proxy server encountered no errors.
	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	case <-doneCh:
		t.Logf("proxy server succeeded")
	}
}

// TestGrpcNewClientWithProxy tests grpc.NewClient with default i.e DNS resolver
// for targetURI and a proxy and verifies that it connects to proxy server and
// sends unresolved target URI in the HTTP CONNECT req and connects to backend.
func (s) TestGrpcNewClientWithProxy(t *testing.T) {
	// Set up a channel to receive signals from OnClientResolution.
	resolutionCh := make(chan bool, 1)

	// Overwrite OnClientResolution to send a signal to the channel.
	origOnClientResolution := delegatingresolver.OnClientResolution
	delegatingresolver.OnClientResolution = func(int) {
		resolutionCh <- true
	}
	t.Cleanup(func() { delegatingresolver.OnClientResolution = origOnClientResolution })

	// Create and start a backend server.
	backendAddr := createAndStartBackendServer(t)

	// Set up and start the proxy server.
	proxyLis, errCh, doneCh, _ := setupProxy(t, backendAddr, false, func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != unresolvedTargetURI {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, unresolvedTargetURI)
		}
		return nil
	})

	// Overwrite the proxy resolution function and restore it afterward.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresolvedTargetURI {
			return &url.URL{
				Scheme: "https",
				Host:   unresolvedProxyURI,
			}, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()

	// Set up a manual resolver for proxy resolution.
	mrProxy := setupDNS(t)

	// Update the proxy resolver state with the proxy's address.
	mrProxy.InitialState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: proxyLis.Addr().String()},
		},
	})

	// Dial to the proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	conn, err := grpc.NewClient(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer conn.Close()

	// Send an RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall failed: %v", err)
	}

	// Verify if the proxy server encountered any errors.
	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	case <-doneCh:
		t.Logf("proxy server succeeded")
	}

	// Verify if OnClientResolution was triggered.
	select {
	case <-resolutionCh:
		t.Error("Client-side resolution was unexpectedly called")
	default:
		// Success: OnClientResolution was not called.
	}
}

// TestGrpcNewClientWithProxyAndCustomResolver tests grpc.NewClient with a
// resolver other than "dns" mentioned in targetURI with a proxyand verifies
// that it connects to proxy server and passes resolved target URI in the
// HTTP CONNECT req and connects to the backend server.
func (s) TestGrpcNewClientWithProxyAndCustomResolver(t *testing.T) {
	// Set up a channel to receive signals from OnClientResolution.
	resolutionCh := make(chan bool, 1)

	// Overwrite OnClientResolution to send a signal to the channel.
	origOnClientResolution := delegatingresolver.OnClientResolution
	delegatingresolver.OnClientResolution = func(int) {
		resolutionCh <- true
	}
	t.Cleanup(func() { delegatingresolver.OnClientResolution = origOnClientResolution })

	// Create and start a backend server.
	backendAddr := createAndStartBackendServer(t)

	// Set up and start the proxy server.
	proxyLis, errCh, doneCh, _ := setupProxy(t, backendAddr, true, func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != backendAddr {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, backendAddr)
		}
		return nil
	})

	// Overwrite the proxy resolution function and restore it afterward.
	hpfe := func(req *http.Request) (*url.URL, error) {
		fmt.Printf("emchandwani : the request URL host is  %v\n", req.URL.Host)
		if req.URL.Host == unresolvedTargetURI {
			return &url.URL{
				Scheme: "https",
				Host:   unresolvedProxyURI,
			}, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()

	// Dial options for the gRPC client.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	// Create a custom resolver.
	mrTarget := manual.NewBuilderWithScheme("whatever")
	resolver.Register(mrTarget)
	mrTarget.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})

	// Set up a manual resolver for proxy resolution.
	mrProxy := setupDNS(t)
	// Update the proxy resolver state with the proxy's address.
	mrProxy.InitialState(resolver.State{
		Addresses: []resolver.Address{{Addr: proxyLis.Addr().String()}}})

	// Dial to the proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	conn, err := grpc.NewClient(mrTarget.Scheme()+":///"+unresolvedTargetURI, dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Create a test service client and make an RPC call.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall() failed: %v", err)
	}

	// Check if the proxy encountered any errors.
	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	case <-doneCh:
		t.Logf("proxy server succeeded")
	}

	// Check if client-side resolution signal was sent to the channel.
	select {
	case <-resolutionCh:
		// Success: OnClientResolution was called.
	default:
		t.Fatalf("Client side resolution should be called but wasn't")
	}
}

// TestGrpcNewClientWithProxyAndTargetResolutionEnabled tests grpc.NewClient with
// the default "dns" resolver and dial option enabling target resolution on the
// client and verifies that the resoltion happens on client.
func (s) TestGrpcNewClientWithProxyAndTargetResolutionEnabled(t *testing.T) {
	// Set up a channel to receive signals from OnClientResolution.
	resolutionCh := make(chan bool, 1)

	// Temporarily modify ProxyScheme for this test.
	origProxyScheme := delegatingresolver.ProxyScheme
	delegatingresolver.ProxyScheme = "whatever"
	t.Cleanup(func() { delegatingresolver.ProxyScheme = origProxyScheme })

	// Overwrite OnClientResolution to send a signal to the channel.
	origOnClientResolution := delegatingresolver.OnClientResolution
	delegatingresolver.OnClientResolution = func(int) {
		resolutionCh <- true
	}
	t.Cleanup(func() { delegatingresolver.OnClientResolution = origOnClientResolution })

	// Create and start a backend server.
	backendAddr := createAndStartBackendServer(t)

	// Set up the proxy server.
	proxyLis, errCh, doneCh, _ := setupProxy(t, backendAddr, true, func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != backendAddr {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, backendAddr)
		}
		return nil
	})

	// Overwrite the proxy resolution function and restore it afterward.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresolvedTargetURI {
			return &url.URL{
				Scheme: "https",
				Host:   unresolvedProxyURI,
			}, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()

	// Configure manual resolvers for both proxy and target backends
	targetResolver := setupDNS(t)
	targetResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})
	proxyResolver := manual.NewBuilderWithScheme("whatever")
	resolver.Register(proxyResolver)
	proxyResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: proxyLis.Addr().String()}}})

	// Dial options with target resolution enabled.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTargetResolutionEnabled(),
	}

	// Dial to the proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	conn, err := grpc.NewClient(unresolvedTargetURI, dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Create a test service client and make an RPC call.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall() failed: %v", err)
	}

	// Verify if the proxy encountered any errors.
	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	case <-doneCh:
		t.Logf("proxy server succeeded")
	}

	// Check if client-side resolution signal was sent to the channel.
	select {
	case <-resolutionCh:
		// Success: OnClientResolution was called.
	default:
		t.Fatalf("Client-side resolution should be called but wasn't")
	}
}

// TestGrpcNewClientWithNoProxy tests grpc.NewClient with grpc.WithNoProxy() set
// and verifies that it does not dail to proxy, but directly to backend.
func (s) TestGrpcNewClientWithNoProxy(t *testing.T) {
	// Create and start a backend server.
	backendAddr := createAndStartBackendServer(t)

	// Set up and start a proxy server.
	_, _, _, proxyStartedCh := setupProxy(t, backendAddr, true, func(req *http.Request) error {
		return fmt.Errorf("Proxy server received Connect req %v, but should not", req)
	})

	// Dial options with proxy explicitly disabled.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(), // Disable proxy.
	}

	targetResolver := setupDNS(t)
	targetResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})
	// Establish a connection.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	conn, err := grpc.NewClient(unresolvedTargetURI, dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer conn.Close()

	// Create a test service client and make an RPC call.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall() failed: %v", err)
	}

	// Verify that the proxy was not dialed.
	select {
	case <-proxyStartedCh:
		t.Error("Proxy server was dialed, but it should have been bypassed")
	default:
	}

}

// TestGrpcNewClientWithContextDialer tests grpc.NewClient with
// grpc.WithContextDialer() set and verifies that it does not dial to proxy but
// rather uses the custom dialer.
func (s) TestGrpcNewClientWithContextDialer(t *testing.T) {
	backendAddr := createAndStartBackendServer(t)

	// Set up and start a proxy server.
	_, _, _, proxyStartedCh := setupProxy(t, backendAddr, true, func(req *http.Request) error {
		return fmt.Errorf("Proxy server received Connect req %v, but should not", req)
	})

	// Create a custom dialer that directly dials the backend.  We'll use this
	// to bypass any proxy logic.
	dialerCalled := make(chan bool, 1)
	customDialer := func(_ context.Context, backendAddr string) (net.Conn, error) {
		dialerCalled <- true
		return net.Dial("tcp", backendAddr)
	}

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(customDialer),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	targetResolver := setupDNS(t)
	targetResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})

	conn, err := grpc.NewClient(unresolvedTargetURI, dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer conn.Close()

	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall() failed: %v", err)
	}
	select {
	case <-dialerCalled:
	default:
		t.Errorf("custom dialer was not called by grpc.NewClient()")
	}
	select {
	case <-proxyStartedCh:
		t.Fatalf("Proxy Server was dialled")
	default:
	}
}

// TestBasicAuthInGrpcNewClientWithProxy tests grpc.NewClient with default i.e
// DNS resolver for targetURI and a proxy and verifies that it connects to proxy
// server and sends unresolved target URI in the HTTP CONNECT req and connects
// to backend. Also verifies that correct user info is sent in the CONNECT.
func (s) TestBasicAuthInGrpcNewClientWithProxy(t *testing.T) {
	// Set up a channel to receive signals from OnClientResolution.
	resolutionCh := make(chan bool, 1)

	// Overwrite OnClientResolution to send a signal to the channel.
	origOnClientResolution := delegatingresolver.OnClientResolution
	delegatingresolver.OnClientResolution = func(int) {
		resolutionCh <- true
	}
	t.Cleanup(func() { delegatingresolver.OnClientResolution = origOnClientResolution })

	// Create and start a backend server.
	backendAddr := createAndStartBackendServer(t)
	const (
		user     = "notAUser"
		password = "notAPassword"
	)
	// Set up and start the proxy server.
	proxyLis, errCh, doneCh, _ := setupProxy(t, backendAddr, false, func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != unresolvedTargetURI {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, unresolvedTargetURI)
		}
		wantProxyAuthStr := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
		if got := req.Header.Get("Proxy-Authorization"); got != wantProxyAuthStr {
			gotDecoded, _ := base64.StdEncoding.DecodeString(got)
			wantDecoded, _ := base64.StdEncoding.DecodeString(wantProxyAuthStr)
			return fmt.Errorf("unexpected auth %q (%q), want %q (%q)", got, gotDecoded, wantProxyAuthStr, wantDecoded)
		}
		return nil
	})

	// Overwrite the proxy resolution function and restore it afterward.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresolvedTargetURI {
			u := url.URL{
				Scheme: "https",
				Host:   unresolvedProxyURI,
			}
			u.User = url.UserPassword(user, password)
			return &u, nil
		}
		return nil, nil
	}
	defer overrideHTTPSProxyFromEnvironment(hpfe)()

	// Set up a manual resolver for proxy resolution.
	mrProxy := setupDNS(t)

	// Update the proxy resolver state with the proxy's address.
	mrProxy.InitialState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: proxyLis.Addr().String()},
		},
	})

	// Dial to the proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	conn, err := grpc.NewClient(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer conn.Close()

	// Send an RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall failed: %v", err)
	}

	// Verify if the proxy server encountered any errors.
	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	case <-doneCh:
		t.Logf("proxy server succeeded")
	}

	// Verify if OnClientResolution was triggered.
	select {
	case <-resolutionCh:
		t.Error("Client-side resolution was unexpectedly called")
	default:
		// Success: OnClientResolution was not called.
	}
}
