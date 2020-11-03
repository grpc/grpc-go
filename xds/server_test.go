/*
 *
 * Copyright 2020 gRPC authors.
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

package xds

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const (
	defaultTestTimeout       = 5 * time.Second
	defaultTestShortTimeout  = 10 * time.Millisecond
	testServerResourceNameID = "/path/to/resource"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type fakeGRPCServer struct {
	done              chan struct{}
	registerServiceCh *testutils.Channel
	serveCh           *testutils.Channel
	stopCh            *testutils.Channel
	gracefulStopCh    *testutils.Channel
}

func (f *fakeGRPCServer) RegisterService(*grpc.ServiceDesc, interface{}) {
	f.registerServiceCh.Send(nil)
}

func (f *fakeGRPCServer) Serve(net.Listener) error {
	f.serveCh.Send(nil)
	<-f.done
	return nil
}

func (f *fakeGRPCServer) Stop() {
	close(f.done)
	f.stopCh.Send(nil)
}
func (f *fakeGRPCServer) GracefulStop() {
	close(f.done)
	f.gracefulStopCh.Send(nil)
}

func newFakeGRPCServer() *fakeGRPCServer {
	return &fakeGRPCServer{
		done:              make(chan struct{}),
		registerServiceCh: testutils.NewChannel(),
		serveCh:           testutils.NewChannel(),
		stopCh:            testutils.NewChannel(),
		gracefulStopCh:    testutils.NewChannel(),
	}
}

func (s) TestNewServer(t *testing.T) {
	xdsCreds, err := xds.NewServerCredentials(xds.ServerOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create xds server credentials: %v", err)
	}

	tests := []struct {
		desc              string
		serverOpts        []grpc.ServerOption
		wantXDSCredsInUse bool
	}{
		{
			desc:       "without_xds_creds",
			serverOpts: []grpc.ServerOption{grpc.Creds(insecure.NewCredentials())},
		},
		{
			desc:              "with_xds_creds",
			serverOpts:        []grpc.ServerOption{grpc.Creds(xdsCreds)},
			wantXDSCredsInUse: true,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// The xds package adds a couple of server options (unary and stream
			// interceptors) to the server options passed in by the user.
			wantServerOpts := len(test.serverOpts) + 2

			origNewGRPCServer := newGRPCServer
			newGRPCServer = func(opts ...grpc.ServerOption) grpcServerInterface {
				if got := len(opts); got != wantServerOpts {
					t.Fatalf("%d ServerOptions passed to grpc.Server, want %d", got, wantServerOpts)
				}
				// Verify that the user passed ServerOptions are forwarded as is.
				if !reflect.DeepEqual(opts[2:], test.serverOpts) {
					t.Fatalf("got ServerOptions %v, want %v", opts[2:], test.serverOpts)
				}
				return grpc.NewServer(opts...)
			}
			defer func() {
				newGRPCServer = origNewGRPCServer
			}()

			s := NewGRPCServer(test.serverOpts...)
			defer s.Stop()

			if s.xdsCredsInUse != test.wantXDSCredsInUse {
				t.Fatalf("xdsCredsInUse is %v, want %v", s.xdsCredsInUse, test.wantXDSCredsInUse)
			}
		})
	}
}

func (s) TestRegisterService(t *testing.T) {
	fs := newFakeGRPCServer()

	origNewGRPCServer := newGRPCServer
	newGRPCServer = func(opts ...grpc.ServerOption) grpcServerInterface { return fs }
	defer func() { newGRPCServer = origNewGRPCServer }()

	s := NewGRPCServer()
	defer s.Stop()

	s.RegisterService(&grpc.ServiceDesc{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fs.registerServiceCh.Receive(ctx); err != nil {
		t.Fatalf("timeout when expecting RegisterService() to called on grpc.Server: %v", err)
	}
}

const (
	fakeProvider1Name = "fake-certificate-provider-1"
	fakeProvider2Name = "fake-certificate-provider-2"
	fakeConfig        = "my fake config"
)

var (
	fpb1, fpb2          *fakeProviderBuilder
	certProviderConfigs map[string]*certprovider.BuildableConfig
)

func init() {
	fpb1 = &fakeProviderBuilder{name: fakeProvider1Name}
	fpb2 = &fakeProviderBuilder{name: fakeProvider2Name}
	cfg1, _ := fpb1.ParseConfig(fakeConfig + "1111")
	cfg2, _ := fpb2.ParseConfig(fakeConfig + "2222")
	certProviderConfigs = map[string]*certprovider.BuildableConfig{
		"default1": cfg1,
		"default2": cfg2,
	}
	certprovider.Register(fpb1)
	certprovider.Register(fpb2)
}

// fakeProviderBuilder builds new instances of fakeProvider and interprets the
// config provided to it as a string.
type fakeProviderBuilder struct {
	name string
}

func (b *fakeProviderBuilder) ParseConfig(config interface{}) (*certprovider.BuildableConfig, error) {
	s, ok := config.(string)
	if !ok {
		return nil, fmt.Errorf("providerBuilder %s received config of type %T, want string", b.name, config)
	}
	return certprovider.NewBuildableConfig(b.name, []byte(s), func(certprovider.BuildOptions) certprovider.Provider {
		return &fakeProvider{
			Distributor: certprovider.NewDistributor(),
			config:      s,
		}
	}), nil
}

func (b *fakeProviderBuilder) Name() string {
	return b.name
}

// fakeProvider is an implementation of the Provider interface which provides a
// method for tests to invoke to push new key materials.
type fakeProvider struct {
	*certprovider.Distributor
	config string
}

// Close helps implement the Provider interface.
func (p *fakeProvider) Close() {
	p.Distributor.Stop()
}

// setupOverrides sets up overrides for bootstrap config, new xdsClient creation,
// new gRPC.Server creation, and certificate provider creation.
func setupOverrides() (*fakeGRPCServer, *testutils.Channel, *testutils.Channel, func()) {
	clientCh := testutils.NewChannel()
	origNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) {
		c := fakeclient.NewClient()
		c.SetBootstrapConfig(&bootstrap.Config{
			BalancerName:         "dummyBalancer",
			Creds:                grpc.WithTransportCredentials(insecure.NewCredentials()),
			NodeProto:            xdstestutils.EmptyNodeProtoV3,
			ServerResourceNameID: testServerResourceNameID,
			CertProviderConfigs:  certProviderConfigs,
		})
		clientCh.Send(c)
		return c, nil
	}

	fs := newFakeGRPCServer()
	origNewGRPCServer := newGRPCServer
	newGRPCServer = func(opts ...grpc.ServerOption) grpcServerInterface { return fs }

	providerCh := testutils.NewChannel()
	origBuildProvider := buildProvider
	buildProvider = func(c map[string]*certprovider.BuildableConfig, id, cert string, wi, wr bool) (certprovider.Provider, error) {
		p, err := origBuildProvider(c, id, cert, wi, wr)
		providerCh.Send(nil)
		return p, err
	}

	return fs, clientCh, providerCh, func() {
		newXDSClient = origNewXDSClient
		newGRPCServer = origNewGRPCServer
		buildProvider = origBuildProvider
	}
}

// setupOverridesForXDSCreds overrides only the xdsClient creation with a fake
// one. Tests that use xdsCredentials need a real grpc.Server instead of a fake
// one, because the xDS-enabled server needs to read configured creds from the
// underlying grpc.Server to confirm whether xdsCreds were configured.
func setupOverridesForXDSCreds(includeCertProviderCfg bool) (*testutils.Channel, *testutils.Channel, func()) {
	clientCh := testutils.NewChannel()
	origNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) {
		c := fakeclient.NewClient()
		bc := &bootstrap.Config{
			BalancerName:         "dummyBalancer",
			Creds:                grpc.WithTransportCredentials(insecure.NewCredentials()),
			NodeProto:            xdstestutils.EmptyNodeProtoV3,
			ServerResourceNameID: testServerResourceNameID,
		}
		if includeCertProviderCfg {
			bc.CertProviderConfigs = certProviderConfigs
		}
		c.SetBootstrapConfig(bc)
		clientCh.Send(c)
		return c, nil
	}

	providerCh := testutils.NewChannel()
	origBuildProvider := buildProvider
	buildProvider = func(c map[string]*certprovider.BuildableConfig, id, cert string, wi, wr bool) (certprovider.Provider, error) {
		p, err := origBuildProvider(c, id, cert, wi, wr)
		providerCh.Send(nil)
		return p, err
	}

	return clientCh, providerCh, func() {
		newXDSClient = origNewXDSClient
		buildProvider = origBuildProvider
	}
}

// TestServeSuccess tests the successful case of calling Serve().
// The following sequence of events happen:
// 1. Create a new GRPCServer and call Serve() in a goroutine.
// 2. Make sure an xdsClient is created, and an LDS watch is registered.
// 3. Push an error response from the xdsClient, and make sure that Serve() does
//    not exit.
// 4. Push a good response from the xdsClient, and make sure that Serve() on the
// 	  underlying grpc.Server is called.
func (s) TestServeSuccess(t *testing.T) {
	fs, clientCh, _, cleanup := setupOverrides()
	defer cleanup()

	server := NewGRPCServer()
	defer server.Stop()

	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xdstestutils.LocalTCPListener() failed: %v", err)
	}

	// Call Serve() in a goroutine, and push on a channel when Serve returns.
	serveDone := testutils.NewChannel()
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Error(err)
		}
		serveDone.Send(nil)
	}()

	// Wait for an xdsClient to be created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := clientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for new xdsClient to be created: %v", err)
	}
	client := c.(*fakeclient.Client)

	// Wait for a listener watch to be registered on the xdsClient.
	name, err := client.WaitForWatchListener(ctx)
	if err != nil {
		t.Fatalf("error when waiting for a ListenerWatch: %v", err)
	}
	wantName := fmt.Sprintf("%s?udpa.resource.listening_address=%s", client.BootstrapConfig().ServerResourceNameID, lis.Addr().String())
	if name != wantName {
		t.Fatalf("LDS watch registered for name %q, want %q", name, wantName)
	}

	// Push an error to the registered listener watch callback and make sure
	// that Serve does not return.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{}, errors.New("LDS error"))
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := serveDone.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Serve() returned after a bad LDS response")
	}

	// Push a good LDS response, and wait for Serve() to be invoked on the
	// underlying grpc.Server.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: "routeconfig"}, nil)
	if _, err := fs.serveCh.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Serve() to be invoked on the grpc.Server")
	}
}

// TestServeWithStop tests the case where Stop() is called before an LDS update
// is received. This should cause Serve() to exit before calling Serve() on the
// underlying grpc.Server.
func (s) TestServeWithStop(t *testing.T) {
	fs, clientCh, _, cleanup := setupOverrides()
	defer cleanup()

	// Note that we are not deferring the Stop() here since we explicitly call
	// it after the LDS watch has been registered.
	server := NewGRPCServer()

	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xdstestutils.LocalTCPListener() failed: %v", err)
	}

	// Call Serve() in a goroutine, and push on a channel when Serve returns.
	serveDone := testutils.NewChannel()
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Error(err)
		}
		serveDone.Send(nil)
	}()

	// Wait for an xdsClient to be created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := clientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for new xdsClient to be created: %v", err)
	}
	client := c.(*fakeclient.Client)

	// Wait for a listener watch to be registered on the xdsClient.
	name, err := client.WaitForWatchListener(ctx)
	if err != nil {
		server.Stop()
		t.Fatalf("error when waiting for a ListenerWatch: %v", err)
	}
	wantName := fmt.Sprintf("%s?udpa.resource.listening_address=%s", client.BootstrapConfig().ServerResourceNameID, lis.Addr().String())
	if name != wantName {
		server.Stop()
		t.Fatalf("LDS watch registered for name %q, wantPrefix %q", name, wantName)
	}

	// Call Stop() on the server before a listener update is received, and
	// expect Serve() to exit.
	server.Stop()
	if _, err := serveDone.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Serve() to exit")
	}

	// Make sure that Serve() on the underlying grpc.Server is not called.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := fs.serveCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Serve() called on underlying grpc.Server")
	}
}

// TestServeBootstrapFailure tests the case where xDS bootstrap fails and
// verifies that Serve() exits with a non-nil error.
func (s) TestServeBootstrapFailure(t *testing.T) {
	// Since we have not setup fakes for anything, this will attempt to do real
	// xDS bootstrap and that will fail because the bootstrap environment
	// variable is not set.
	server := NewGRPCServer()
	defer server.Stop()

	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xdstestutils.LocalTCPListener() failed: %v", err)
	}

	serveDone := testutils.NewChannel()
	go func() { serveDone.Send(server.Serve(lis)) }()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	v, err := serveDone.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for Serve() to exit: %v", err)
	}
	if err, ok := v.(error); !ok || err == nil {
		t.Fatal("Serve() did not exit with error")
	}
}

// TestServeBootstrapWithMissingCertProviders tests the case where the bootstrap
// config does not contain certificate provider configuration, but xdsCreds are
// passed to the server. Verifies that the call to Serve() fails.
func (s) TestServeBootstrapWithMissingCertProviders(t *testing.T) {
	_, _, cleanup := setupOverridesForXDSCreds(false)
	defer cleanup()

	xdsCreds, err := xds.NewServerCredentials(xds.ServerOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create xds server credentials: %v", err)
	}
	server := NewGRPCServer(grpc.Creds(xdsCreds))
	defer server.Stop()

	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xdstestutils.LocalTCPListener() failed: %v", err)
	}

	serveDone := testutils.NewChannel()
	go func() {
		err := server.Serve(lis)
		serveDone.Send(err)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	v, err := serveDone.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for Serve() to exit: %v", err)
	}
	if err, ok := v.(error); !ok || err == nil {
		t.Fatal("Serve() did not exit with error")
	}
}

// TestServeNewClientFailure tests the case where xds client creation fails and
// verifies that Server() exits with a non-nil error.
func (s) TestServeNewClientFailure(t *testing.T) {
	origNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) {
		return nil, errors.New("xdsClient creation failed")
	}
	defer func() { newXDSClient = origNewXDSClient }()

	server := NewGRPCServer()
	defer server.Stop()

	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xdstestutils.LocalTCPListener() failed: %v", err)
	}

	serveDone := testutils.NewChannel()
	go func() {
		err := server.Serve(lis)
		serveDone.Send(err)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	v, err := serveDone.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for Serve() to exit: %v", err)
	}
	if err, ok := v.(error); !ok || err == nil {
		t.Fatal("Serve() did not exit with error")
	}
}

// TestHandleListenerUpdate_NoXDSCreds tests the case where an xds-enabled gRPC
// server is not configured with xDS credentials. Verifies that the security
// config received as part of a Listener update is not acted upon.
func (s) TestHandleListenerUpdate_NoXDSCreds(t *testing.T) {
	fs, clientCh, providerCh, cleanup := setupOverrides()
	defer cleanup()

	server := NewGRPCServer()
	defer server.Stop()

	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xdstestutils.LocalTCPListener() failed: %v", err)
	}

	// Call Serve() in a goroutine, and push on a channel when Serve returns.
	serveDone := testutils.NewChannel()
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Error(err)
		}
		serveDone.Send(nil)
	}()

	// Wait for an xdsClient to be created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := clientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for new xdsClient to be created: %v", err)
	}
	client := c.(*fakeclient.Client)

	// Wait for a listener watch to be registered on the xdsClient.
	name, err := client.WaitForWatchListener(ctx)
	if err != nil {
		t.Fatalf("error when waiting for a ListenerWatch: %v", err)
	}
	wantName := fmt.Sprintf("%s?udpa.resource.listening_address=%s", client.BootstrapConfig().ServerResourceNameID, lis.Addr().String())
	if name != wantName {
		t.Fatalf("LDS watch registered for name %q, want %q", name, wantName)
	}

	// Push a good LDS response with security config, and wait for Serve() to be
	// invoked on the underlying grpc.Server. Also make sure that certificate
	// providers are not created.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{
		RouteConfigName: "routeconfig",
		SecurityCfg: &xdsclient.SecurityConfig{
			RootInstanceName:     "default1",
			IdentityInstanceName: "default2",
			RequireClientCert:    true,
		},
	}, nil)
	if _, err := fs.serveCh.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Serve() to be invoked on the grpc.Server")
	}

	// Make sure the security configuration is not acted upon.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := providerCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("certificate provider created when no xDS creds were specified")
	}
}

// TestHandleListenerUpdate_ErrorUpdate tests the case where an xds-enabled gRPC
// server is configured with xDS credentials, but receives a Listener update
// with an error. Verifies that no certificate providers are created.
func (s) TestHandleListenerUpdate_ErrorUpdate(t *testing.T) {
	clientCh, providerCh, cleanup := setupOverridesForXDSCreds(true)
	defer cleanup()

	xdsCreds, err := xds.NewServerCredentials(xds.ServerOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create xds server credentials: %v", err)
	}

	server := NewGRPCServer(grpc.Creds(xdsCreds))
	defer server.Stop()

	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xdstestutils.LocalTCPListener() failed: %v", err)
	}

	// Call Serve() in a goroutine, and push on a channel when Serve returns.
	serveDone := testutils.NewChannel()
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Error(err)
		}
		serveDone.Send(nil)
	}()

	// Wait for an xdsClient to be created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := clientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for new xdsClient to be created: %v", err)
	}
	client := c.(*fakeclient.Client)

	// Wait for a listener watch to be registered on the xdsClient.
	name, err := client.WaitForWatchListener(ctx)
	if err != nil {
		t.Fatalf("error when waiting for a ListenerWatch: %v", err)
	}
	wantName := fmt.Sprintf("%s?udpa.resource.listening_address=%s", client.BootstrapConfig().ServerResourceNameID, lis.Addr().String())
	if name != wantName {
		t.Fatalf("LDS watch registered for name %q, want %q", name, wantName)
	}

	// Push an error to the registered listener watch callback and make sure
	// that Serve does not return.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{
		RouteConfigName: "routeconfig",
		SecurityCfg: &xdsclient.SecurityConfig{
			RootInstanceName:     "default1",
			IdentityInstanceName: "default2",
			RequireClientCert:    true,
		},
	}, errors.New("LDS error"))
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := serveDone.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Serve() returned after a bad LDS response")
	}

	// Also make sure that no certificate providers are created.
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := providerCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("certificate provider created when no xDS creds were specified")
	}
}

func (s) TestHandleListenerUpdate_ClosedListener(t *testing.T) {
	clientCh, providerCh, cleanup := setupOverridesForXDSCreds(true)
	defer cleanup()

	xdsCreds, err := xds.NewServerCredentials(xds.ServerOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create xds server credentials: %v", err)
	}

	server := NewGRPCServer(grpc.Creds(xdsCreds))
	defer server.Stop()

	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xdstestutils.LocalTCPListener() failed: %v", err)
	}

	// Call Serve() in a goroutine, and push on a channel when Serve returns.
	serveDone := testutils.NewChannel()
	go func() { serveDone.Send(server.Serve(lis)) }()

	// Wait for an xdsClient to be created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := clientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for new xdsClient to be created: %v", err)
	}
	client := c.(*fakeclient.Client)

	// Wait for a listener watch to be registered on the xdsClient.
	name, err := client.WaitForWatchListener(ctx)
	if err != nil {
		t.Fatalf("error when waiting for a ListenerWatch: %v", err)
	}
	wantName := fmt.Sprintf("%s?udpa.resource.listening_address=%s", client.BootstrapConfig().ServerResourceNameID, lis.Addr().String())
	if name != wantName {
		t.Fatalf("LDS watch registered for name %q, want %q", name, wantName)
	}

	// Push a good update to the registered listener watch callback. This will
	// unblock the xds-enabled server which is waiting for a good listener
	// update before calling Serve() on the underlying grpc.Server.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{
		RouteConfigName: "routeconfig",
		SecurityCfg:     &xdsclient.SecurityConfig{IdentityInstanceName: "default2"},
	}, nil)
	if _, err := providerCh.Receive(ctx); err != nil {
		t.Fatal("error when waiting for certificate provider to be created")
	}

	// Close the listener passed to Serve(), and wait for the latter to return a
	// non-nil error.
	lis.Close()
	v, err := serveDone.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for Serve() to exit: %v", err)
	}
	if err, ok := v.(error); !ok || err == nil {
		t.Fatal("Serve() did not exit with error")
	}

	// Push another listener update and make sure that no certificate providers
	// are created.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{
		RouteConfigName: "routeconfig",
		SecurityCfg:     &xdsclient.SecurityConfig{IdentityInstanceName: "default1"},
	}, nil)
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := providerCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("certificate provider created when no xDS creds were specified")
	}
}
