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
	unresolvedProxyURI  = "proxyexample.com"
)

// createAndStartBackendServer creates and starts a test backend server,
// registering a cleanup to stop it.
func createAndStartBackendServer(t *testing.T) string {
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testgrpc.Empty) (*testgrpc.Empty, error) { return &testgrpc.Empty{}, nil },
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(backend.Stop)
	return backend.Address
}

// setupDNS sets up a manual DNS resolver and registers it, returning the
// builder for test customization.
func setupDNS(t *testing.T) *manual.Resolver {
	mr := manual.NewBuilderWithScheme("dns")
	origRes := resolver.Get("dns")
	resolver.Register(mr)
	t.Cleanup(func() {
		resolver.Register(origRes)
	})
	return mr
}

// setupProxy initializes and starts a proxy server, registers a cleanup to
// stop it, and returns the proxy's listener and helper channels.
func setupProxy(t *testing.T, backendAddr string, resolutionOnClient bool, reqCheck func(*http.Request) error) (net.Listener, chan error, chan struct{}, chan struct{}) {
	pLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	proxyStartedCh := make(chan struct{})
	t.Cleanup(func() {
		close(errCh)
	})

	proxyServer := testutils.NewProxyServer(pLis, reqCheck, errCh, doneCh, backendAddr, resolutionOnClient, func() { close(proxyStartedCh) })
	t.Cleanup(proxyServer.Stop)

	return pLis, errCh, doneCh, proxyStartedCh
}

// requestCheck returns a function that checks the HTTP CONNECT request for the
// correct CONNECT method and address.
func requestCheck(connectAddr string) func(*http.Request) error {
	return func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != connectAddr {
			return fmt.Errorf("unexpected URL.Host in CONNECT req %q, want %q", req.URL.Host, connectAddr)
		}
		return nil
	}
}

// Tests the scenario where grpc.Dial is performed using a proxy with the
// default resolver in the target URI. The test verifies that the connection is
// established to the proxy server, sends the unresolved target URI in the HTTP
// CONNECT request, and is successfully connected to the backend server.
func (s) TestGRPCDialWithProxy(t *testing.T) {
	backendAddr := createAndStartBackendServer(t)
	pLis, errCh, doneCh, _ := setupProxy(t, backendAddr, false, requestCheck(unresolvedTargetURI))

	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresolvedTargetURI {
			return &url.URL{
				Scheme: "https",
				Host:   pLis.Addr().String(),
			}, nil
		}
		return nil, nil
	}
	orighpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = orighpfe
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.Dial(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial(%s) failed: %v", unresolvedTargetURI, err)
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

// Tests the scenario where `grpc.Dial` is performed with a proxy and the "dns"
// scheme for the target. The test verifies that the proxy URI is correctly
// resolved and that the target URI resolution on the client preserves the
// original behavior of `grpc.Dial`. It also ensures that a connection is
// established to the proxy server, with the resolved target URI sent in the
// HTTP CONNECT request, successfully connecting to the backend server.
func (s) TestGRPCDialWithDNSAndProxy(t *testing.T) {
	backendAddr := createAndStartBackendServer(t)
	pLis, errCh, doneCh, _ := setupProxy(t, backendAddr, false, requestCheck(backendAddr))

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
	orighpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = orighpfe
	}()

	// Configure manual resolvers for both proxy and target backends
	mrTarget := setupDNS(t)
	mrTarget.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})
	// Temporarily modify ProxyScheme for this test.
	origScheme := delegatingresolver.ProxyScheme
	delegatingresolver.ProxyScheme = "whatever"
	t.Cleanup(func() { delegatingresolver.ProxyScheme = origScheme })
	mrProxy := manual.NewBuilderWithScheme("whatever")
	resolver.Register(mrProxy)
	mrProxy.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: pLis.Addr().String()}}})

	// Dial to the proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.Dial(mrTarget.Scheme()+":///"+unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial(%s) failed: %v", mrTarget.Scheme()+":///"+unresolvedTargetURI, err)
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

// Tests the scenario where grpc.NewClient is used with the default DNS
// resolver for the target URI and a proxy is configured. The test verifies
// that the client. resolves proxy URI, connects to the proxy server, sends the
// unresolved target URI in the HTTP CONNECT request, and successfully
// establishes a connection to the backend server.
func (s) TestGRPCNewClientWithProxy(t *testing.T) {
	// Set up a channel to receive signals from OnClientResolution.
	resCh := make(chan bool, 1)
	// Overwrite OnClientResolution to send a signal to the channel.
	origOnClientResolution := delegatingresolver.OnClientResolution
	delegatingresolver.OnClientResolution = func(int) {
		resCh <- true
	}
	t.Cleanup(func() { delegatingresolver.OnClientResolution = origOnClientResolution })

	backendAddr := createAndStartBackendServer(t)
	pLis, errCh, doneCh, _ := setupProxy(t, backendAddr, false, requestCheck(unresolvedTargetURI))

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
	orighpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = orighpfe
	}()

	// Set up and update a manual resolver for proxy resolution.
	mrProxy := setupDNS(t)
	mrProxy.InitialState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: pLis.Addr().String()},
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
	case <-resCh:
		t.Fatal("target resolution occurred on client unexpectedly")
	default:
		t.Log("target resolution did not occur on the client")
	}
}

// Tests the scenario where grpc.NewClient is used with a custom target URI
// scheme and a proxy is configured. The test verifies that the client
// successfully connects to the proxy server, resolves the proxy URI correctly,
// includes the resolved target URI in the HTTP CONNECT request, and
// establishes a connection to the backend server.
func (s) TestGRPCNewClientWithProxyAndCustomResolver(t *testing.T) {
	// Set up a channel to receive signals from OnClientResolution.
	resCh := make(chan bool, 1)
	// Overwrite OnClientResolution to send a signal to the channel.
	origOnClientResolution := delegatingresolver.OnClientResolution
	delegatingresolver.OnClientResolution = func(int) {
		resCh <- true
	}
	t.Cleanup(func() { delegatingresolver.OnClientResolution = origOnClientResolution })

	backendAddr := createAndStartBackendServer(t)
	// Set up and start the proxy server.
	pLis, errCh, doneCh, _ := setupProxy(t, backendAddr, true, requestCheck(backendAddr))

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
	orighpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = orighpfe
	}()

	// Create and update a custom resolver for target URI.
	mrTarget := manual.NewBuilderWithScheme("whatever")
	resolver.Register(mrTarget)
	mrTarget.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})
	// Set up and update a manual resolver for proxy resolution.
	mrProxy := setupDNS(t)
	mrProxy.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: pLis.Addr().String()}}})

	// Dial to the proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(mrTarget.Scheme()+":///"+unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", mrTarget.Scheme()+":///"+unresolvedTargetURI, err)
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
	case <-resCh:
		t.Log("target resolution occurred on client")
	default:
		t.Fatal("target resolution did not occur on the client unexpectedly")
	}
}

// Tests the scenario where grpc.NewClient is used with the default "dns"
// resolver and the dial option grpc.WithTargetResolutionEnabled() is set,
// enabling target resolution on the client. The test verifies that target
// resolution happens on the client by sending resolved target URi in HTTP
// CONNECT request, the proxy URI is resolved correctly, and the connection is
// successfully established with the backend server through the proxy.
func (s) TestGRPCNewClientWithProxyAndTargetResolutionEnabled(t *testing.T) {
	// Set up a channel to receive signals from OnClientResolution.
	resCh := make(chan bool, 1)
	// Overwrite OnClientResolution to send a signal to the channel.
	origOnClientResolution := delegatingresolver.OnClientResolution
	delegatingresolver.OnClientResolution = func(int) {
		resCh <- true
	}
	t.Cleanup(func() { delegatingresolver.OnClientResolution = origOnClientResolution })

	backendAddr := createAndStartBackendServer(t)
	pLis, errCh, doneCh, _ := setupProxy(t, backendAddr, true, requestCheck(backendAddr))

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
	orighpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = orighpfe
	}()

	// Configure manual resolvers for both proxy and target backends
	mrTarget := setupDNS(t)
	mrTarget.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})
	// Temporarily modify ProxyScheme for this test.
	origScheme := delegatingresolver.ProxyScheme
	delegatingresolver.ProxyScheme = "whatever"
	t.Cleanup(func() { delegatingresolver.ProxyScheme = origScheme })
	mrProxy := manual.NewBuilderWithScheme("whatever")
	resolver.Register(mrProxy)
	mrProxy.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: pLis.Addr().String()}}})

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
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
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
	case <-resCh:
		t.Log("target resolution occurred on client")
	default:
		t.Fatal("target resolution did not occur on client unexpectedly")
	}
}

// Tests the scenario where grpc.NewClient is used with grpc.WithNoProxy() set,
// explicitly disabling proxy usage. The test verifies that the client does not
// dial the proxy but directly connects to the backend server. It also checks
// that the proxy resolution function is not called and that the proxy server
// never receives a connection request.
func (s) TestGRPCNewClientWithNoProxy(t *testing.T) {
	backendAddr := createAndStartBackendServer(t)
	_, _, _, proxyStartedCh := setupProxy(t, backendAddr, true, func(req *http.Request) error {
		return fmt.Errorf("proxy server should not have received a Connect request: %v", req)
	})

	proxyCalled := false
	hpfe := func(_ *http.Request) (*url.URL, error) {
		proxyCalled = true
		return &url.URL{
			Scheme: "https",
			Host:   unresolvedProxyURI,
		}, nil

	}
	orighpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = orighpfe
	}()

	mrTarget := setupDNS(t)
	mrTarget.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})

	// Dial options with proxy explicitly disabled.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(), // Disable proxy.
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(unresolvedTargetURI, dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
	}
	defer conn.Close()

	// Create a test service client and make an RPC call.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall() failed: %v", err)
	}

	if proxyCalled {
		t.Error("http.ProxyFromEnvironment function was unexpectedly called")
	}
	// Verify that the proxy was not dialed.
	select {
	case <-proxyStartedCh:
		t.Fatal("unexpected dial to proxy server")
	default:
		t.Log("proxy server was not dialed")
	}
}

// Tests the scenario where grpc.NewClient is used with grpc.WithContextDialer()
// set. The test verifies that the client bypasses proxy dialing and uses the
// custom dialer instead. It ensures that the proxy server is never dialed, the
// proxy resolution function is not triggered, and the custom dialer is invoked
// as expected.
func (s) TestGRPCNewClientWithContextDialer(t *testing.T) {
	backendAddr := createAndStartBackendServer(t)
	_, _, _, proxyStartedCh := setupProxy(t, backendAddr, true, func(req *http.Request) error {
		return fmt.Errorf("proxy server should not have received a Connect request: %v", req)
	})

	proxyCalled := false
	hpfe := func(_ *http.Request) (*url.URL, error) {
		proxyCalled = true
		return &url.URL{
			Scheme: "https",
			Host:   unresolvedProxyURI,
		}, nil

	}
	orighpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = orighpfe
	}()

	// Create a custom dialer that directly dials the backend.  We'll use this
	// to bypass any proxy logic.
	dialerCalled := make(chan bool, 1)
	customDialer := func(_ context.Context, backendAddr string) (net.Conn, error) {
		dialerCalled <- true
		return net.Dial("tcp", backendAddr)
	}

	mrTarget := setupDNS(t)
	mrTarget.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(customDialer),
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(unresolvedTargetURI, dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
	}
	defer conn.Close()

	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall() failed: %v", err)
	}

	if proxyCalled {
		t.Error("http.ProxyFromEnvironment function was unexpectedly called")
	}

	select {
	case <-dialerCalled:
		t.Log("custom dialer was invoked")
	default:
		t.Error("custom dialer was not invoked")
	}

	select {
	case <-proxyStartedCh:
		t.Fatal("unexpected dial to proxy server")
	default:
		t.Log("proxy server was not dialled")
	}
}

// Tests the scenario where grpc.NewClient is used with the default DNS resolver
// for targetURI and a proxy. The test verifies that the client connects to the
// proxy server, sends the unresolved target URI in the HTTP CONNECT request,
// and successfully connects to the backend. Additionally, it checks that the
// correct user information is included in the Proxy-Authorization header of
// the CONNECT request. The test also ensures that target resolution does not
// happen on the client.
func (s) TestBasicAuthInGrpcNewClientWithProxy(t *testing.T) {
	// Set up a channel to receive signals from OnClientResolution.
	resCh := make(chan bool, 1)
	// Overwrite OnClientResolution to send a signal to the channel.
	origOnClientResolution := delegatingresolver.OnClientResolution
	delegatingresolver.OnClientResolution = func(int) {
		resCh <- true
	}
	t.Cleanup(func() { delegatingresolver.OnClientResolution = origOnClientResolution })

	backendAddr := createAndStartBackendServer(t)
	const (
		user     = "notAUser"
		password = "notAPassword"
	)
	pLis, errCh, doneCh, _ := setupProxy(t, backendAddr, false, func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != unresolvedTargetURI {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, unresolvedTargetURI)
		}
		wantProxyAuthStr := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
		if got := req.Header.Get("Proxy-Authorization"); got != wantProxyAuthStr {
			gotDecoded, err := base64.StdEncoding.DecodeString(got)
			if err != nil {
				return fmt.Errorf("failed to decode Proxy-Authorization header: %v", err)
			}
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
	orighpfe := internal.HTTPSProxyFromEnvironmentForTesting
	internal.HTTPSProxyFromEnvironmentForTesting = hpfe
	defer func() {
		internal.HTTPSProxyFromEnvironmentForTesting = orighpfe
	}()

	// Set up and update a manual resolver for proxy resolution.
	mrProxy := setupDNS(t)
	mrProxy.InitialState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: pLis.Addr().String()},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
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
	case <-resCh:
		t.Fatal("target resolution occurred on client unexpectedly")
	default:
		t.Log("target resolution did not occur on the client")
	}
}
