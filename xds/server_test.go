// +build go1.12

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
	"strings"
	"testing"
	"time"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
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
	defaultTestTimeout                     = 5 * time.Second
	defaultTestShortTimeout                = 10 * time.Millisecond
	testServerListenerResourceNameTemplate = "/path/to/resource/%s/%s"
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

func splitHostPort(hostport string) (string, string) {
	addr, port, err := net.SplitHostPort(hostport)
	if err != nil {
		panic(fmt.Sprintf("listener address %q does not parse: %v", hostport, err))
	}
	return addr, port
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
	fpb1 = &fakeProviderBuilder{
		name:    fakeProvider1Name,
		buildCh: testutils.NewChannel(),
	}
	fpb2 = &fakeProviderBuilder{
		name:    fakeProvider2Name,
		buildCh: testutils.NewChannel(),
	}
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
	name    string
	buildCh *testutils.Channel
}

func (b *fakeProviderBuilder) ParseConfig(config interface{}) (*certprovider.BuildableConfig, error) {
	s, ok := config.(string)
	if !ok {
		return nil, fmt.Errorf("providerBuilder %s received config of type %T, want string", b.name, config)
	}
	return certprovider.NewBuildableConfig(b.name, []byte(s), func(certprovider.BuildOptions) certprovider.Provider {
		b.buildCh.Send(nil)
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

// setupOverrides sets up overrides for bootstrap config, new xdsClient creation
// and new gRPC.Server creation.
func setupOverrides() (*fakeGRPCServer, *testutils.Channel, func()) {
	clientCh := testutils.NewChannel()
	origNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) {
		c := fakeclient.NewClient()
		c.SetBootstrapConfig(&bootstrap.Config{
			BalancerName:                       "dummyBalancer",
			Creds:                              grpc.WithTransportCredentials(insecure.NewCredentials()),
			NodeProto:                          xdstestutils.EmptyNodeProtoV3,
			ServerListenerResourceNameTemplate: testServerListenerResourceNameTemplate,
			CertProviderConfigs:                certProviderConfigs,
		})
		clientCh.Send(c)
		return c, nil
	}

	fs := newFakeGRPCServer()
	origNewGRPCServer := newGRPCServer
	newGRPCServer = func(opts ...grpc.ServerOption) grpcServerInterface { return fs }

	return fs, clientCh, func() {
		newXDSClient = origNewXDSClient
		newGRPCServer = origNewGRPCServer
	}
}

// setupOverridesForXDSCreds overrides only the xdsClient creation with a fake
// one. Tests that use xdsCredentials need a real grpc.Server instead of a fake
// one, because the xDS-enabled server needs to read configured creds from the
// underlying grpc.Server to confirm whether xdsCreds were configured.
func setupOverridesForXDSCreds(includeCertProviderCfg bool) (*testutils.Channel, func()) {
	clientCh := testutils.NewChannel()
	origNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) {
		c := fakeclient.NewClient()
		bc := &bootstrap.Config{
			BalancerName:                       "dummyBalancer",
			Creds:                              grpc.WithTransportCredentials(insecure.NewCredentials()),
			NodeProto:                          xdstestutils.EmptyNodeProtoV3,
			ServerListenerResourceNameTemplate: testServerListenerResourceNameTemplate,
		}
		if includeCertProviderCfg {
			bc.CertProviderConfigs = certProviderConfigs
		}
		c.SetBootstrapConfig(bc)
		clientCh.Send(c)
		return c, nil
	}

	return clientCh, func() { newXDSClient = origNewXDSClient }
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
	fs, clientCh, cleanup := setupOverrides()
	defer cleanup()

	// Create a new xDS-enabled gRPC server and pass it a server option to get
	// notified about serving mode changes.
	modeChangeCh := testutils.NewChannel()
	modeChangeOption := ServingModeCallback(func(addr net.Addr, args ServingModeChangeArgs) {
		t.Logf("server mode change callback invoked for listener %q with mode %q and error %v", addr.String(), args.Mode, args.Err)
		modeChangeCh.Send(args.Mode)
	})
	server := NewGRPCServer(modeChangeOption)
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
	wantName := strings.Replace(testServerListenerResourceNameTemplate, "%s", lis.Addr().String(), -1)
	if name != wantName {
		t.Fatalf("LDS watch registered for name %q, want %q", name, wantName)
	}

	// Push an error to the registered listener watch callback and make sure
	// that Serve does not return.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{}, xdsclient.NewErrorf(xdsclient.ErrorTypeResourceNotFound, "LDS resource not found"))
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := serveDone.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Serve() returned after a bad LDS response")
	}

	// Make sure the serving mode changes appropriately.
	v, err := modeChangeCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for serving mode to change: %v", err)
	}
	if mode := v.(ServingMode); mode != ServingModeNotServing {
		t.Fatalf("server mode is %q, want %q", mode, ServingModeNotServing)
	}

	// Push a good LDS response, and wait for Serve() to be invoked on the
	// underlying grpc.Server.
	addr, port := splitHostPort(lis.Addr().String())
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{
		RouteConfigName: "routeconfig",
		InboundListenerCfg: &xdsclient.InboundListenerConfig{
			Address: addr,
			Port:    port,
		},
	}, nil)
	if _, err := fs.serveCh.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Serve() to be invoked on the grpc.Server")
	}

	// Make sure the serving mode changes appropriately.
	v, err = modeChangeCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for serving mode to change: %v", err)
	}
	if mode := v.(ServingMode); mode != ServingModeServing {
		t.Fatalf("server mode is %q, want %q", mode, ServingModeServing)
	}

	// Push an update to the registered listener watch callback with a Listener
	// resource whose host:port does not match the actual listening address and
	// port. This will push the listener to "not-serving" mode.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{
		RouteConfigName: "routeconfig",
		InboundListenerCfg: &xdsclient.InboundListenerConfig{
			Address: "10.20.30.40",
			Port:    "666",
		},
	}, nil)
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := serveDone.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Serve() returned after a bad LDS response")
	}

	// Make sure the serving mode changes appropriately.
	v, err = modeChangeCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for serving mode to change: %v", err)
	}
	if mode := v.(ServingMode); mode != ServingModeNotServing {
		t.Fatalf("server mode is %q, want %q", mode, ServingModeNotServing)
	}
}

// TestServeWithStop tests the case where Stop() is called before an LDS update
// is received. This should cause Serve() to exit before calling Serve() on the
// underlying grpc.Server.
func (s) TestServeWithStop(t *testing.T) {
	fs, clientCh, cleanup := setupOverrides()
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
	wantName := strings.Replace(testServerListenerResourceNameTemplate, "%s", lis.Addr().String(), -1)
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

// TestServeBootstrapConfigInvalid tests the cases where the bootstrap config
// does not contain expected fields. Verifies that the call to Serve() fails.
func (s) TestServeBootstrapConfigInvalid(t *testing.T) {
	tests := []struct {
		desc            string
		bootstrapConfig *bootstrap.Config
	}{
		{
			desc:            "bootstrap config is missing",
			bootstrapConfig: nil,
		},
		{
			desc: "certificate provider config is missing",
			bootstrapConfig: &bootstrap.Config{
				BalancerName:                       "dummyBalancer",
				Creds:                              grpc.WithTransportCredentials(insecure.NewCredentials()),
				NodeProto:                          xdstestutils.EmptyNodeProtoV3,
				ServerListenerResourceNameTemplate: testServerListenerResourceNameTemplate,
			},
		},
		{
			desc: "server_listener_resource_name_template is missing",
			bootstrapConfig: &bootstrap.Config{
				BalancerName:        "dummyBalancer",
				Creds:               grpc.WithTransportCredentials(insecure.NewCredentials()),
				NodeProto:           xdstestutils.EmptyNodeProtoV3,
				CertProviderConfigs: certProviderConfigs,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// Override the xdsClient creation with one that returns a fake
			// xdsClient with the specified bootstrap configuration.
			clientCh := testutils.NewChannel()
			origNewXDSClient := newXDSClient
			newXDSClient = func() (xdsClientInterface, error) {
				c := fakeclient.NewClient()
				c.SetBootstrapConfig(test.bootstrapConfig)
				clientCh.Send(c)
				return c, nil
			}
			defer func() { newXDSClient = origNewXDSClient }()

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
		})
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
	fs, clientCh, cleanup := setupOverrides()
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
	wantName := strings.Replace(testServerListenerResourceNameTemplate, "%s", lis.Addr().String(), -1)
	if name != wantName {
		t.Fatalf("LDS watch registered for name %q, want %q", name, wantName)
	}

	// Push a good LDS response with security config, and wait for Serve() to be
	// invoked on the underlying grpc.Server. Also make sure that certificate
	// providers are not created.
	fcm, err := xdsclient.NewFilterChainManager(&v3listenerpb.Listener{
		FilterChains: []*v3listenerpb.FilterChain{
			{
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
							CommonTlsContext: &v3tlspb.CommonTlsContext{
								TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
									InstanceName:    "identityPluginInstance",
									CertificateName: "identityCertName",
								},
							},
						}),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("xdsclient.NewFilterChainManager() failed with error: %v", err)
	}
	addr, port := splitHostPort(lis.Addr().String())
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{
		RouteConfigName: "routeconfig",
		InboundListenerCfg: &xdsclient.InboundListenerConfig{
			Address:      addr,
			Port:         port,
			FilterChains: fcm,
		},
	}, nil)
	if _, err := fs.serveCh.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Serve() to be invoked on the grpc.Server")
	}

	// Make sure the security configuration is not acted upon.
	if err := verifyCertProviderNotCreated(); err != nil {
		t.Fatal(err)
	}
}

// TestHandleListenerUpdate_ErrorUpdate tests the case where an xds-enabled gRPC
// server is configured with xDS credentials, but receives a Listener update
// with an error. Verifies that no certificate providers are created.
func (s) TestHandleListenerUpdate_ErrorUpdate(t *testing.T) {
	clientCh, cleanup := setupOverridesForXDSCreds(true)
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
	wantName := strings.Replace(testServerListenerResourceNameTemplate, "%s", lis.Addr().String(), -1)
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

	// Also make sure that no certificate providers are created.
	if err := verifyCertProviderNotCreated(); err != nil {
		t.Fatal(err)
	}
}

func verifyCertProviderNotCreated() error {
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := fpb1.buildCh.Receive(sCtx); err != context.DeadlineExceeded {
		return errors.New("certificate provider created when no xDS creds were specified")
	}
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := fpb2.buildCh.Receive(sCtx); err != context.DeadlineExceeded {
		return errors.New("certificate provider created when no xDS creds were specified")
	}
	return nil
}
