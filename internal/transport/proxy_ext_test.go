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

package transport_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"golang.org/x/net/http/httpproxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/resolver/delegatingresolver"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/transport"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

const (
	unresolvedTargetURI = "example.com"
	unresolvedProxyURI  = "proxyexample.com"
	defaultTestTimeout  = 10 * time.Second
)

// dnsCache is a map used in the proxy server to mimic DNS name resolution.
// It maps unresolved hostnames to their resolved addresses. If an entry
// is found in the cache, the proxy server uses it; otherwise, it defaults
// to the original hostname for connection.
var dnsCache = make(map[string]string)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func createAndStartBackendServer(t *testing.T) string {
	t.Helper()
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testgrpc.Empty) (*testgrpc.Empty, error) { return &testgrpc.Empty{}, nil },
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(backend.Stop)
	// return backend.Address
	return fmt.Sprintf("localhost:%d", testutils.ParsePort(t, backend.Address))
}

// setupDNS sets up a manual DNS resolver and registers it, returning the
// builder for test customization.
func setupDNS(t *testing.T) *manual.Resolver {
	t.Helper()
	mr := manual.NewBuilderWithScheme("dns")
	origRes := resolver.Get("dns")
	resolver.Register(mr)
	t.Cleanup(func() {
		resolver.Register(origRes)
	})
	return mr
}

// Tests the scenario where grpc.Dial is performed using a proxy with the
// default resolver in the target URI. The test verifies that the connection is
// established to the proxy server, sends the unresolved target URI in the HTTP
// CONNECT request, and is successfully connected to the backend server.
// func (s) TestGRPCDialWithProxy(t *testing.T) {
// 	backendAddr := createAndStartBackendServer(t)
// 	dnsCache := map[string]string{unresolvedTargetURI: backendAddr}
// 	pLis, _ := transport.SetupProxy(t, dnsCache, transport.RequestCheck(unresolvedTargetURI), false)

// 	// Overwrite the proxy environment and restore it after the test.
// 	t.Setenv("HTTPS_PROXY", pLis.Addr().String())

// 	// Use the httpproxy package functions instead of `http.ProxyFromEnvironment`
// 	// because the latter reads proxy-related environment variables only once at
// 	// initialization. This behavior causes issues when running multiple tests
// 	// with different proxy configurations, as changes to environment variables
// 	// during tests would be ignored. By using `httpproxy.FromEnvironment()`, we
// 	// ensure proxy settings are read dynamically in each test execution.
// 	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
// 	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
// 		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
// 	}
// 	defer func() {
// 		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
// 	}()

// 	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
// 	defer cancel()
// 	conn, err := grpc.Dial(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		t.Fatalf("grpc.Dial(%s) failed: %v", unresolvedTargetURI, err)
// 	}
// 	defer conn.Close()

// 	// Send an empty RPC to the backend through the proxy.
// 	client := testgrpc.NewTestServiceClient(conn)
// 	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
// 		t.Errorf("EmptyCall failed: %v", err)
// 	}
// }

// // Tests the scenario where `grpc.Dial` is performed with a proxy and the "dns"
// // scheme for the target. The test verifies that the proxy URI is correctly
// // resolved and that the target URI resolution on the client preserves the
// // original behavior of `grpc.Dial`. It also ensures that a connection is
// // established to the proxy server, with the resolved target URI sent in the
// // HTTP CONNECT request, successfully connecting to the backend server.
// func TestGRPCDialWithDNSAndProxy(t *testing.T) {
// 	backendAddr := createAndStartBackendServer(t)
// 	pLis, _ := transport.SetupProxy(t, dnsCache, transport.RequestCheck(backendAddr), false)

// 	// Overwrite the proxy environment and restore it after the test.
// 	t.Setenv("HTTPS_PROXY", unresolvedProxyURI)

// 	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
// 	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
// 		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
// 	}
// 	defer func() {
// 		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
// 	}()

// 	// Configure manual resolvers for both proxy and target backends
// 	targetResolver := setupDNS(t)
// 	targetResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})

// 	// Temporarily modify ProxyScheme for this test.
// 	origScheme := delegatingresolver.ProxyScheme
// 	delegatingresolver.ProxyScheme = "test"
// 	t.Cleanup(func() { delegatingresolver.ProxyScheme = origScheme })

// 	proxyResolver := manual.NewBuilderWithScheme("test")
// 	resolver.Register(proxyResolver)
// 	proxyResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: pLis.Addr().String()}}})

// 	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
// 	defer cancel()
// 	conn, err := grpc.Dial(targetResolver.Scheme()+":///"+unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		t.Fatalf("grpc.Dial(%s) failed: %v", targetResolver.Scheme()+":///"+unresolvedTargetURI, err)
// 	}
// 	defer conn.Close()

// 	// Send an empty RPC to the backend through the proxy.
// 	client := testgrpc.NewTestServiceClient(conn)
// 	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
// 		t.Errorf("EmptyCall failed: %v", err)
// 	}
// }

// // Tests the scenario where grpc.NewClient is used with the default DNS
// // resolver for the target URI and a proxy is configured. The test verifies
// // that the client resolves proxy URI, connects to the proxy server, sends the
// // unresolved target URI in the HTTP CONNECT request, and successfully
// // establishes a connection to the backend server.
// func (s) TestGRPCNewClientWithProxy(t *testing.T) {
// 	backendAddr := createAndStartBackendServer(t)
// 	dnsCache := map[string]string{unresolvedTargetURI: backendAddr}
// 	pLis, _ := transport.SetupProxy(t, dnsCache, transport.RequestCheck(unresolvedTargetURI), false)

// 	// Overwrite the proxy environment and restore it after the test.
// 	t.Setenv("HTTPS_PROXY", unresolvedProxyURI)

// 	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
// 	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
// 		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
// 	}
// 	defer func() {
// 		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
// 	}()

// 	// Set up and update a manual resolver for proxy resolution.

// 	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
// 	defer cancel()
// 	conn, err := grpc.NewClient(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		t.Fatalf("grpc.NewClient failed: %v", err)
// 	}
// 	defer conn.Close()

// 	// Send an empty RPC to the backend through the proxy.
// 	client := testgrpc.NewTestServiceClient(conn)
// 	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
// 		t.Errorf("EmptyCall failed: %v", err)
// 	}
// }

// // Tests the scenario where grpc.NewClient is used with a custom target URI
// // scheme and a proxy is configured. The test verifies that the client
// // successfully connects to the proxy server, resolves the proxy URI correctly,
// // includes the resolved target URI in the HTTP CONNECT request, and
// // establishes a connection to the backend server.
// func (s) TestGRPCNewClientWithProxyAndCustomResolver(t *testing.T) {
// 	backendAddr := createAndStartBackendServer(t)
// 	pLis, _ := transport.SetupProxy(t, dnsCache, transport.RequestCheck(backendAddr), false)

// 	// Overwrite the proxy environment and restore it after the test.
// 	t.Setenv("HTTPS_PROXY", unresolvedProxyURI)

// 	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
// 	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
// 		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
// 	}
// 	defer func() {
// 		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
// 	}()

// 	// Dial to the proxy server.
// 	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
// 	defer cancel()
// 	conn, err := grpc.NewClient(+":///"+unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		t.Fatalf("grpc.NewClient(%s) failed: %v", targetResolver.Scheme()+":///"+unresolvedTargetURI, err)
// 	}
// 	t.Cleanup(func() { conn.Close() })

// 	// Send an empty RPC to the backend through the proxy.
// 	client := testgrpc.NewTestServiceClient(conn)
// 	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
// 		t.Errorf("EmptyCall() failed: %v", err)
// 	}
// }

// // Tests the scenario where grpc.NewClient is used with a custom target URI
// // scheme and a proxy is configured and both the resolvers return endpoints.
// // The test verifies that the client successfully connects to the proxy server,
// // resolves the proxy URI correctly, includes the resolved target URI in the
// // HTTP CONNECT request, and establishes a connection to the backend server.
// func (s) TestGRPCNewClientWithProxyAndCustomResolverWithEndpoints(t *testing.T) {
// 	backendAddr := createAndStartBackendServer(t)
// 	pLis, _ := transport.SetupProxy(t, dnsCache, transport.RequestCheck(backendAddr), false)

// 	// Overwrite the proxy environment and restore it after the test.
// 	t.Setenv("HTTPS_PROXY", unresolvedProxyURI)

// 	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
// 	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
// 		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
// 	}
// 	defer func() {
// 		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
// 	}()

// 	// Create and update a custom resolver for target URI.
// 	targetResolver := manual.NewBuilderWithScheme("test")
// 	resolver.Register(targetResolver)
// 	targetResolver.InitialState(resolver.State{Endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: backendAddr}}}}})

// 	// Set up and update a manual resolver for proxy resolution.
// 	proxyResolver := setupDNS(t)
// 	proxyResolver.InitialState(resolver.State{Endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: pLis.Addr().String()}}}}})

// 	// Dial to the proxy server.
// 	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
// 	defer cancel()
// 	conn, err := grpc.NewClient(targetResolver.Scheme()+":///"+unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
// 	if err != nil {
// 		t.Fatalf("grpc.NewClient(%s) failed: %v", targetResolver.Scheme()+":///"+unresolvedTargetURI, err)
// 	}
// 	t.Cleanup(func() { conn.Close() })

// 	// Send an empty RPC to the backend through the proxy.
// 	client := testgrpc.NewTestServiceClient(conn)
// 	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
// 		t.Errorf("EmptyCall() failed: %v", err)
// 	}
// }

// Tests the scenario where grpc.NewClient is used with the default "dns"
// resolver and the dial option grpc.WithTargetResolutionEnabled() is set,
// enabling target resolution on the client. The test verifies that target
// resolution happens on the client by sending resolved target URI in HTTP
// CONNECT request, the proxy URI is resolved correctly, and the connection is
// successfully established with the backend server through the proxy.
func TestGRPCNewClientWithProxyAndTargetResolutionEnabled(t *testing.T) {
	unresolvedTargetURI := createAndStartBackendServer(t)
	unresolvedProxyURI, _ := transport.SetupProxy(t, dnsCache, func(req *http.Request) error {
		host, _, err := net.SplitHostPort(req.URL.Host)
		if err != nil {
			return err
		}
		_, err = netip.ParseAddr(host)
		return err
	}, false)

	// Overwrite the proxy environment and restore it after the test.
	t.Setenv("HTTPS_PROXY", unresolvedProxyURI)

	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
	}
	defer func() {
		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
	}()

	// // Configure manual resolvers for both proxy and target backends
	// targetResolver := setupDNS(t)
	// targetResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})

	// // Temporarily modify ProxyScheme for this test.
	// origScheme := delegatingresolver.ProxyScheme
	// delegatingresolver.ProxyScheme = "test"
	// t.Cleanup(func() { delegatingresolver.ProxyScheme = origScheme })

	// proxyResolver := manual.NewBuilderWithScheme("test")
	// resolver.Register(proxyResolver)
	// proxyResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: pLis.Addr().String()}}})

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTargetResolutionEnabled(), // Target resolution on client enabled.
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(unresolvedTargetURI, dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
	}
	t.Cleanup(func() { conn.Close() })

	// Send an empty RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall() failed: %v", err)
	}
}

// Tests the scenario where grpc.NewClient is used with grpc.WithNoProxy() set,
// explicitly disabling proxy usage. The test verifies that the client does not
// dial the proxy but directly connects to the backend server. It also checks
// that the proxy resolution function is not called and that the proxy server
// never receives a connection request.
func (s) TestGRPCNewClientWithNoProxy(t *testing.T) {
	backendAddr := createAndStartBackendServer(t)
	_, proxyStartedCh := transport.SetupProxy(t, dnsCache, func(req *http.Request) error {
		return fmt.Errorf("proxy server should not have received a Connect request: %v", req)
	}, false)

	// Overwrite the proxy environment and restore it after the test.
	t.Setenv("HTTPS_PROXY", unresolvedProxyURI)

	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
	}
	defer func() {
		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
	}()

	targetResolver := setupDNS(t)
	targetResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(), // Diable proxy.
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

	// Verify that the proxy server was not dialed.
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
	_, proxyStartedCh := transport.SetupProxy(t, dnsCache, func(req *http.Request) error {
		return fmt.Errorf("proxy server should not have received a Connect request: %v", req)
	}, false)

	// Overwrite the proxy environment and restore it after the test.
	t.Setenv("HTTPS_PROXY", unresolvedProxyURI)

	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
	}
	defer func() {
		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
	}()

	// Create a custom dialer that directly dials the backend.ß
	dialerCalled := make(chan struct{})
	customDialer := func(_ context.Context, backendAddr string) (net.Conn, error) {
		close(dialerCalled)
		return net.Dial("tcp", backendAddr)
	}

	targetResolver := setupDNS(t)
	targetResolver.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backendAddr}}})

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(customDialer), // Use a custom dialer.
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
	backendAddr := createAndStartBackendServer(t)
	const (
		user     = "notAUser"
		password = "notAPassword"
	)
	dnsCache := map[string]string{unresolvedTargetURI: backendAddr}
	pLis, _ := transport.SetupProxy(t, dnsCache, func(req *http.Request) error {
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
	}, false)

	// Overwrite the proxy environment and restore it after the test.
	t.Setenv("HTTPS_PROXY", user+":"+password+"@"+unresolvedProxyURI)

	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
	}
	defer func() {
		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
	}()

	// Set up and update a manual resolver for proxy resolution.
	proxyResolver := setupDNS(t)
	proxyResolver.InitialState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: pLis},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
	}
	defer conn.Close()

	// Send an empty RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Errorf("EmptyCall failed: %v", err)
	}
}
