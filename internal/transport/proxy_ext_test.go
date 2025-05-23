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
	"google.golang.org/grpc/internal/testutils/proxyserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

const defaultTestTimeout = 10 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func startBackendServer(t *testing.T) *stubserver.StubServer {
	t.Helper()
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(backend.Stop)
	return backend
}

func isIPAddr(addr string) bool {
	_, err := netip.ParseAddr(addr)
	return err == nil
}

func overrideTestHTTPSProxy(t *testing.T, proxyAddr string) {
	t.Helper()
	hpfe := func(req *http.Request) (*url.URL, error) {
		return &url.URL{
			Scheme: "https",
			Host:   proxyAddr,
		}, nil
	}
	originalhpfe := delegatingresolver.HTTPSProxyFromEnvironment
	delegatingresolver.HTTPSProxyFromEnvironment = hpfe
	t.Cleanup(func() { delegatingresolver.HTTPSProxyFromEnvironment = originalhpfe })
}

// Tests the scenario where grpc.Dial is performed using a proxy with the
// default resolver in the target URI. The test verifies that the connection is
// established to the proxy server, sends the unresolved target URI in the HTTP
// CONNECT request and is successfully connected to the backend server.
func (s) TestGRPCDialWithProxy(t *testing.T) {
	backend := startBackendServer(t)
	unresolvedTargetURI := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, backend.Address))
	proxyCalled := false
	reqCheck := func(req *http.Request) {
		proxyCalled = true
		host, _, err := net.SplitHostPort(req.URL.Host)
		if err != nil {
			t.Error(err)
		}
		if got, want := host, "localhost"; got != want {
			t.Errorf(" Unexpected request host: %s, want = %s ", got, want)
		}
	}
	pServer := proxyserver.New(t, reqCheck, false)
	// Use "localhost:<port>" to verify the proxy address is handled
	// correctly by the delegating resolver and connects to the proxy server
	// correctly even when unresolved.
	pAddr := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, pServer.Addr))

	overrideTestHTTPSProxy(t, pAddr)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.Dial(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial(%s) failed: %v", unresolvedTargetURI, err)
	}
	defer conn.Close()

	// Send an empty RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}

	if !proxyCalled {
		t.Fatalf("Proxy not connected")
	}
}

// Tests the scenario where `grpc.Dial` is performed with a proxy and the "dns"
// scheme for the target. The test verifies that the proxy URI is correctly
// resolved and that the target URI resolution on the client preserves the
// original behavior of `grpc.Dial`. It also ensures that a connection is
// established to the proxy server, with the resolved target URI sent in the
// HTTP CONNECT request, successfully connecting to the backend server.
func (s) TestGRPCDialWithDNSAndProxy(t *testing.T) {
	backend := startBackendServer(t)
	unresolvedTargetURI := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, backend.Address))
	proxyCalled := false
	reqCheck := func(req *http.Request) {
		proxyCalled = true

		host, _, err := net.SplitHostPort(req.URL.Host)
		if err != nil {
			t.Error(err)
		}
		if got, want := isIPAddr(host), true; got != want {
			t.Errorf("isIPAddr(%q) = %t, want = %t", host, got, want)
		}
	}
	pServer := proxyserver.New(t, reqCheck, false)

	overrideTestHTTPSProxy(t, pServer.Addr)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.Dial("dns:///"+unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial(%s) failed: %v", "dns:///"+unresolvedTargetURI, err)
	}
	defer conn.Close()

	// Send an empty RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}

	if !proxyCalled {
		t.Fatalf("Proxy not connected")
	}
}

// Tests the scenario where `grpc.NewClient` is used with the default DNS
// resolver for the target URI and a proxy is configured. The test verifies
// that the client resolves proxy URI, connects to the proxy server, sends the
// unresolved target URI in the HTTP CONNECT request, and successfully
// establishes a connection to the backend server.
func (s) TestNewClientWithProxy(t *testing.T) {
	backend := startBackendServer(t)
	unresolvedTargetURI := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, backend.Address))
	proxyCalled := false
	reqCheck := func(req *http.Request) {
		proxyCalled = true
		host, _, err := net.SplitHostPort(req.URL.Host)
		if err != nil {
			t.Error(err)
		}
		if got, want := host, "localhost"; got != want {
			t.Errorf(" Unexpected request host: %s, want = %s ", got, want)
		}
	}
	pServer := proxyserver.New(t, reqCheck, false)
	// Use "localhost:<port>" to verify the proxy address is handled
	// correctly by the delegating resolver and connects to the proxy server
	// correctly even when unresolved.
	pAddr := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, pServer.Addr))

	overrideTestHTTPSProxy(t, pAddr)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
	}
	defer conn.Close()

	// Send an empty RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}
	if !proxyCalled {
		t.Fatalf("Proxy not connected")
	}
}

// Tests the scenario where grpc.NewClient is used with a custom target URI
// scheme and a proxy is configured. The test verifies that the client
// successfully connects to the proxy server, resolves the proxy URI correctly,
// includes the resolved target URI in the HTTP CONNECT request, and
// establishes a connection to the backend server.
func (s) TestNewClientWithProxyAndCustomResolver(t *testing.T) {
	backend := startBackendServer(t)
	unresolvedTargetURI := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, backend.Address))
	proxyCalled := false
	reqCheck := func(req *http.Request) {
		proxyCalled = true
		host, _, err := net.SplitHostPort(req.URL.Host)
		if err != nil {
			t.Error(err)
		}
		if got, want := isIPAddr(host), true; got != want {
			t.Errorf("isIPAddr(%q) = %t, want = %t", host, got, want)
		}
	}
	pServer := proxyserver.New(t, reqCheck, false)

	overrideTestHTTPSProxy(t, pServer.Addr)

	// Create and update a custom resolver for target URI.
	targetResolver := manual.NewBuilderWithScheme("test")
	resolver.Register(targetResolver)
	targetResolver.InitialState(resolver.State{Endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: backend.Address}}}}})

	// Dial to the proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(targetResolver.Scheme()+":///"+unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", targetResolver.Scheme()+":///"+unresolvedTargetURI, err)
	}
	defer conn.Close()

	// Send an empty RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	if !proxyCalled {
		t.Fatalf("Proxy not connected")
	}
}

// Tests the scenario where grpc.NewClient is used with the default "dns"
// resolver and the dial option grpc.WithLocalDNSResolution() is set,
// enabling target resolution on the client. The test verifies that target
// resolution happens on the client by sending resolved target URI in HTTP
// CONNECT request, the proxy URI is resolved correctly, and the connection is
// successfully established with the backend server through the proxy.
func (s) TestNewClientWithProxyAndTargetResolutionEnabled(t *testing.T) {
	backend := startBackendServer(t)
	unresolvedTargetURI := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, backend.Address))
	proxyCalled := false
	reqCheck := func(req *http.Request) {
		proxyCalled = true
		host, _, err := net.SplitHostPort(req.URL.Host)
		if err != nil {
			t.Error(err)
		}
		if got, want := isIPAddr(host), true; got != want {
			t.Errorf("isIPAddr(%q) = %t, want = %t", host, got, want)
		}
	}
	pServer := proxyserver.New(t, reqCheck, false)

	overrideTestHTTPSProxy(t, pServer.Addr)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(unresolvedTargetURI, grpc.WithLocalDNSResolution(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
	}
	defer conn.Close()

	// Send an empty RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}

	if !proxyCalled {
		t.Fatalf("Proxy not connected")
	}
}

// Tests the scenario where grpc.NewClient is used with grpc.WithNoProxy() set,
// explicitly disabling proxy usage. The test verifies that the client does not
// dial the proxy but directly connects to the backend server. It also checks
// that the proxy resolution function is not called and that the proxy server
// never receives a connection request.
func (s) TestNewClientWithNoProxy(t *testing.T) {
	backend := startBackendServer(t)
	unresolvedTargetURI := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, backend.Address))
	reqCheck := func(_ *http.Request) { t.Error("proxy server should not have received a Connect request") }
	pServer := proxyserver.New(t, reqCheck, false)

	overrideTestHTTPSProxy(t, pServer.Addr)

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
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests the scenario where grpc.NewClient is used with grpc.WithContextDialer()
// set. The test verifies that the client bypasses proxy dialing and uses the
// custom dialer instead. It ensures that the proxy server is never dialed, the
// proxy resolution function is not triggered, and the custom dialer is invoked
// as expected.
func (s) TestNewClientWithContextDialer(t *testing.T) {
	backend := startBackendServer(t)
	unresolvedTargetURI := fmt.Sprintf("localhost:%d", testutils.ParsePort(t, backend.Address))
	reqCheck := func(_ *http.Request) { t.Error("proxy server should not have received a Connect request") }
	pServer := proxyserver.New(t, reqCheck, false)

	overrideTestHTTPSProxy(t, pServer.Addr)

	// Create a custom dialer that directly dials the backend.
	customDialer := func(_ context.Context, unresolvedTargetURI string) (net.Conn, error) {
		return net.Dial("tcp", unresolvedTargetURI)
	}

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
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests the scenario where grpc.NewClient is used with the default DNS resolver
// for targetURI and a proxy. The test verifies that the client connects to the
// proxy server, sends the unresolved target URI in the HTTP CONNECT request,
// and successfully connects to the backend. Additionally, it checks that the
// correct user information is included in the Proxy-Authorization header of
// the CONNECT request. The test also ensures that target resolution does not
// happen on the client.
func (s) TestBasicAuthInNewClientWithProxy(t *testing.T) {
	unresolvedTargetURI := "example.test"
	const (
		user     = "notAUser"
		password = "notAPassword"
	)
	proxyCalled := false
	reqCheck := func(req *http.Request) {
		proxyCalled = true
		if got, want := req.URL.Host, "example.test"; got != want {
			t.Errorf(" Unexpected request host: %s, want = %s ", got, want)
		}
		wantProxyAuthStr := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
		if got := req.Header.Get("Proxy-Authorization"); got != wantProxyAuthStr {
			gotDecoded, err := base64.StdEncoding.DecodeString(got)
			if err != nil {
				t.Errorf("failed to decode Proxy-Authorization header: %v", err)
			}
			wantDecoded, _ := base64.StdEncoding.DecodeString(wantProxyAuthStr)
			t.Errorf("unexpected auth %q (%q), want %q (%q)", got, gotDecoded, wantProxyAuthStr, wantDecoded)
		}
	}
	pServer := proxyserver.New(t, reqCheck, false)

	t.Setenv("HTTPS_PROXY", user+":"+password+"@"+pServer.Addr)

	// Use the httpproxy package functions instead of `http.ProxyFromEnvironment`
	// because the latter reads proxy-related environment variables only once at
	// initialization. This behavior causes issues when running test multiple
	// times, as changes to environment variables during tests would be ignored.
	// By using `httpproxy.FromEnvironment()`, we ensure proxy settings are read dynamically.
	origHTTPSProxyFromEnvironment := delegatingresolver.HTTPSProxyFromEnvironment
	delegatingresolver.HTTPSProxyFromEnvironment = func(req *http.Request) (*url.URL, error) {
		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
	}
	defer func() {
		delegatingresolver.HTTPSProxyFromEnvironment = origHTTPSProxyFromEnvironment
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.NewClient(unresolvedTargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s) failed: %v", unresolvedTargetURI, err)
	}
	defer conn.Close()

	// Send an empty RPC to the backend through the proxy.
	client := testgrpc.NewTestServiceClient(conn)
	client.EmptyCall(ctx, &testpb.Empty{})

	if !proxyCalled {
		t.Fatalf("Proxy not connected")
	}
}
