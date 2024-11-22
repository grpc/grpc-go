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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/resolver/delegatingresolver"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

const (
	unresProxyURI = "example.com"

// defaultTestTimeout = 10 * time.Second
)

// overwriteAndRestore overwrite function httpProxyFromEnvironment and
// returns a function to restore the default values.
func overwrite(hpfe func(req *http.Request) (*url.URL, error)) func() {
	backHPFE := delegatingresolver.HTTPSProxyFromEnvironment
	delegatingresolver.HTTPSProxyFromEnvironment = hpfe
	return func() {
		delegatingresolver.HTTPSProxyFromEnvironment = backHPFE
	}
}

// TestGrpcDialWithProxy tests grpc.Dial with no resolver mentioned in targetURI
// (i.e it uses passthrough) with a proxy.
func (s) TestGrpcDialWithProxy(t *testing.T) {

	//Create and start a backend server
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(func() { backend.Stop() })
	backendAddr := backend.Address

	proxyLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	errCh := make(chan error, 1)
	defer func() { close(errCh) }()
	reqCheck := func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		//emchandwani: TODO :Change to the correct string
		if req.URL.Host != unresProxyURI {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, unresProxyURI)
		}
		return nil
	}
	fmt.Printf("backendaddr sent: %v\n", backendAddr)
	p := testutils.NewProxyServer(proxyLis, reqCheck, errCh, backendAddr)
	t.Cleanup(func() { p.Stop() })

	// Overwrite the function in the test and restore them in defer.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresProxyURI {
			return &url.URL{
				Scheme: "https",
				Host:   proxyLis.Addr().String(), //emchandwani: return unresolved string here, and add manual resolver to check the correct resolution
			}, nil
		}
		return nil, nil
	}
	defer overwrite(hpfe)()
	// set proxy env variable
	origHTTPSProxy := envconfig.HTTPSProxy
	envconfig.HTTPSProxy = "proxyexample.com"
	defer func() { envconfig.HTTPSProxy = origHTTPSProxy }()
	// Dial to proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	fmt.Printf("env config: %v\n", os.Getenv("HTTPS_PROXY"))
	conn, err := grpc.Dial(unresProxyURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial failed: %v", err)
	}
	defer conn.Close()
	// Send an RPC to the backend through the proxy.
	client := testpb.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}
	// Check if the proxy encountered any errors.
	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	default:
		t.Logf("proxy server succeeded")
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

// TestGrpcDialWithProxy tests grpc.Dial with dns resolver mentioned in targetURI
// (i.e it uses passthrough) with a proxy. DNS resolution should be done, to be compatible with the old behaviour.
func (s) TestGrpcDialWithProxyandResolution(t *testing.T) {

	//Create and start a backend server
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(func() { backend.Stop() })
	backendAddr := backend.Address

	proxyLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	errCh := make(chan error, 1)
	defer func() { close(errCh) }()
	reqCheck := func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		//emchandwani: TODO :Change to the correct string
		if req.URL.Host != unresProxyURI {
			fmt.Printf("unexpected URL.Host %q, want %q", req.URL.Host, backendAddr)

			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, backendAddr)
		}
		return nil
	}
	fmt.Printf("backendaddr sent: %v\n", backendAddr)
	p := testutils.NewProxyServer(proxyLis, reqCheck, errCh, backendAddr)
	t.Cleanup(func() { p.Stop() })

	// Overwrite the function in the test and restore them in defer.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresProxyURI {
			return &url.URL{
				Scheme: "https",
				Host:   "proxyURL.com", //emchandwani: return unresolved string here, and add manual resolver to check the correct resolution
			}, nil
		}
		return nil, nil
	}
	defer overwrite(hpfe)()
	// mrTarget := setupDNS(t)
	mrProxy := setupDNS(t)
	// set proxy env variable
	origHTTPSProxy := envconfig.HTTPSProxy
	envconfig.HTTPSProxy = "proxyexample.com"
	defer func() { envconfig.HTTPSProxy = origHTTPSProxy }()
	// Dial to proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	fmt.Printf("env config: %v\n", os.Getenv("HTTPS_PROXY"))
	conn, err := grpc.Dial(unresProxyURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial failed: %v", err)
	}
	// mrTarget.UpdateState(resolver.State{
	// 	Addresses: []resolver.Address{
	// 		{Addr: backendAddr},
	// 	}})

	mrProxy.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: proxyLis.Addr().String()},
		}})
	defer conn.Close()
	// Send an RPC to the backend through the proxy.
	client := testpb.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}
	// Check if the proxy encountered any errors.
	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	default:
		t.Logf("proxy server succeeded")
	}

}

// TestGrpcNewClientWithProxy tests grpc.NewClient with no resolver mentioned in targetURI with a proxy
func (s) TestGrpcNewClientWithProxy(t *testing.T) {
	//Create and start a backend server
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(func() { backend.Stop() })
	backendAddr := backend.Address

	proxyLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	errCh := make(chan error, 1)
	defer func() { close(errCh) }()
	reqCheck := func(req *http.Request) error {
		if req.Method != http.MethodConnect {
			return fmt.Errorf("unexpected Method %q, want %q", req.Method, http.MethodConnect)
		}
		if req.URL.Host != unresProxyURI {
			return fmt.Errorf("unexpected URL.Host %q, want %q", req.URL.Host, backendAddr)
		}
		return nil
	}
	fmt.Printf("backendaddr sent: %v\n", backendAddr)
	p := testutils.NewProxyServer(proxyLis, reqCheck, errCh, backendAddr)
	t.Cleanup(func() { p.Stop() })

	// Overwrite the function in the test and restore them in defer.
	hpfe := func(req *http.Request) (*url.URL, error) {
		if req.URL.Host == unresProxyURI {
			return &url.URL{
				Scheme: "https",
				Host:   proxyLis.Addr().String(), //emchandwani: return unresolved string here, and add manual resolver to check the correct resolution
			}, nil
		}
		return nil, nil
	}
	defer overwrite(hpfe)()
	// set proxy env variable
	origHTTPSProxy := envconfig.HTTPSProxy
	envconfig.HTTPSProxy = "proxyexample.com"
	defer func() { envconfig.HTTPSProxy = origHTTPSProxy }()
	// Dial to proxy server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	fmt.Printf("env config: %v\n", os.Getenv("HTTPS_PROXY"))
	conn, err := grpc.NewClient(unresProxyURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial failed: %v", err)
	}
	defer conn.Close()
	// Send an RPC to the backend through the proxy.
	client := testpb.NewTestServiceClient(conn)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}
	// Check if the proxy encountered any errors.
	select {
	case err := <-errCh:
		t.Fatalf("proxy server encountered an error: %v", err)
	default:
		t.Logf("proxy server succeeded")
	}
}
