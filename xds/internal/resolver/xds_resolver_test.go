/*
 *
 * Copyright 2019 gRPC authors.
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

package resolver

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	xdsbootstrap "google.golang.org/grpc/internal/testutils/xds/bootstrap"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/balancer/clustermanager"
	"google.golang.org/grpc/xds/internal/balancer/ringhash"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/grpc/xds/internal/httpfilter/router"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer" // To parse LB config
)

const (
	targetStr               = "target"
	routeStr                = "route"
	cluster                 = "cluster"
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 100 * time.Microsecond
)

var target = resolver.Target{URL: *testutils.MustParseURL("xds:///" + targetStr)}

var routerFilter = xdsresource.HTTPFilter{Name: "rtr", Filter: httpfilter.Get(router.TypeURL)}
var routerFilterList = []xdsresource.HTTPFilter{routerFilter}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestRegister(t *testing.T) {
	if resolver.Get(xdsScheme) == nil {
		t.Errorf("scheme %v is not registered", xdsScheme)
	}
}

// testClientConn is a fake implemetation of resolver.ClientConn that pushes
// state updates and errors returned by the resolver on to channels for
// consumption by tests.
type testClientConn struct {
	resolver.ClientConn
	stateCh *testutils.Channel
	errorCh *testutils.Channel
}

func (t *testClientConn) UpdateState(s resolver.State) error {
	t.stateCh.Replace(s)
	return nil
}

func (t *testClientConn) ReportError(err error) {
	t.errorCh.Replace(err)
}

func (t *testClientConn) ParseServiceConfig(jsonSC string) *serviceconfig.ParseResult {
	return internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
}

func newTestClientConn() *testClientConn {
	return &testClientConn{
		stateCh: testutils.NewChannel(),
		errorCh: testutils.NewChannel(),
	}
}

// TestResolverBuilder_ClientCreationFails tests the case where xDS client
// creation fails, and verifies that xDS resolver build fails as well.
func (s) TestResolverBuilder_ClientCreationFails(t *testing.T) {
	// Override xDS client creation function and return an error.
	origNewClient := newXDSClient
	newXDSClient = func() (xdsclient.XDSClient, func(), error) {
		return nil, nil, errors.New("failed to create xDS client")
	}
	defer func() {
		newXDSClient = origNewClient
	}()

	// Build an xDS resolver and expect it to fail.
	builder := resolver.Get(xdsScheme)
	if builder == nil {
		t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
	}
	if _, err := builder.Build(target, newTestClientConn(), resolver.BuildOptions{}); err == nil {
		t.Fatalf("builder.Build(%v) succeeded when expected to fail", target)
	}
}

// TestResolverBuilder_DifferentBootstrapConfigs tests the resolver builder's
// Build() method with different xDS bootstrap configurations.
func (s) TestResolverBuilder_DifferentBootstrapConfigs(t *testing.T) {
	tests := []struct {
		name         string
		bootstrapCfg *bootstrap.Config // Empty top-level xDS server config, will be set by test logic.
		target       resolver.Target
		buildOpts    resolver.BuildOptions
		wantErr      string
	}{
		{
			name:         "good",
			bootstrapCfg: &bootstrap.Config{},
			target:       target,
		},
		{
			name: "authority not defined in bootstrap",
			bootstrapCfg: &bootstrap.Config{
				ClientDefaultListenerResourceNameTemplate: "%s",
				Authorities: map[string]*bootstrap.Authority{
					"test-authority": {
						ClientListenerResourceNameTemplate: "xdstp://test-authority/%s",
					},
				},
			},
			target: resolver.Target{
				URL: url.URL{
					Host: "non-existing-authority",
					Path: "/" + targetStr,
				},
			},
			wantErr: `authority "non-existing-authority" is not found in the bootstrap file`,
		},
		{
			name:         "xDS creds specified without certificate providers in bootstrap",
			bootstrapCfg: &bootstrap.Config{},
			target:       target,
			buildOpts: resolver.BuildOptions{
				DialCreds: func() credentials.TransportCredentials {
					creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
					if err != nil {
						t.Fatalf("xds.NewClientCredentials() failed: %v", err)
					}
					return creds
				}(),
			},
			wantErr: `xdsCreds specified but certificate_providers config missing in bootstrap file`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
			if err != nil {
				t.Fatalf("Starting xDS management server: %v", err)
			}
			defer mgmtServer.Stop()

			// Add top-level xDS server config corresponding to the above
			// management server.
			test.bootstrapCfg.XDSServer = &bootstrap.ServerConfig{
				ServerURI:    mgmtServer.Address,
				Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
				TransportAPI: version.TransportV3,
			}

			// Override xDS client creation to use bootstrap configuration
			// specified by the test.
			origNewClient := newXDSClient
			newXDSClient = func() (xdsclient.XDSClient, func(), error) {
				// The watch timeout and idle authority timeout values passed to
				// NewWithConfigForTesing() are immaterial for this test, as we
				// are only testing the resolver build functionality.
				return xdsclient.NewWithConfigForTesting(test.bootstrapCfg, defaultTestTimeout, defaultTestTimeout)
			}
			defer func() {
				newXDSClient = origNewClient
			}()

			builder := resolver.Get(xdsScheme)
			if builder == nil {
				t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
			}

			r, err := builder.Build(test.target, newTestClientConn(), test.buildOpts)
			if gotErr, wantErr := err != nil, test.wantErr != ""; gotErr != wantErr {
				t.Fatalf("builder.Build(%v) returned err: %v, wantErr: %v", target, err, test.wantErr)
			}
			if test.wantErr != "" && !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("builder.Build(%v) returned err: %v, wantErr: %v", target, err, test.wantErr)
			}
			if err != nil {
				// This is the case where we expect an error and got it.
				return
			}
			r.Close()
		})
	}
}

type setupOpts struct {
	bootstrapC *bootstrap.Config
	target     resolver.Target
}

func testSetup(t *testing.T, opts setupOpts) (*xdsResolver, *fakeclient.Client, *testClientConn, func()) {
	t.Helper()

	fc := fakeclient.NewClient()
	if opts.bootstrapC != nil {
		fc.SetBootstrapConfig(opts.bootstrapC)
	}
	oldClientMaker := newXDSClient
	closeCh := make(chan struct{})
	newXDSClient = func() (xdsclient.XDSClient, func(), error) {
		return fc, grpcsync.OnceFunc(func() { close(closeCh) }), nil
	}
	cancel := func() {
		// Make sure the xDS client is closed, in all (successful or failed)
		// cases.
		select {
		case <-time.After(defaultTestTimeout):
			t.Fatalf("timeout waiting for close")
		case <-closeCh:
		}
		newXDSClient = oldClientMaker
	}
	builder := resolver.Get(xdsScheme)
	if builder == nil {
		t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
	}

	tcc := newTestClientConn()
	r, err := builder.Build(opts.target, tcc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("builder.Build(%v) returned err: %v", target, err)
	}
	return r.(*xdsResolver), fc, tcc, func() {
		r.Close()
		cancel()
	}
}

// waitForWatchListener waits for the WatchListener method to be called on the
// xdsClient within a reasonable amount of time, and also verifies that the
// watch is called with the expected target.
func waitForWatchListener(ctx context.Context, t *testing.T, xdsC *fakeclient.Client, wantTarget string) {
	t.Helper()

	gotTarget, err := xdsC.WaitForWatchListener(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchService failed with error: %v", err)
	}
	if gotTarget != wantTarget {
		t.Fatalf("xdsClient.WatchService() called with target: %v, want %v", gotTarget, wantTarget)
	}
}

// waitForWatchRouteConfig waits for the WatchRoute method to be called on the
// xdsClient within a reasonable amount of time, and also verifies that the
// watch is called with the expected target.
func waitForWatchRouteConfig(ctx context.Context, t *testing.T, xdsC *fakeclient.Client, wantTarget string) {
	t.Helper()

	gotTarget, err := xdsC.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchService failed with error: %v", err)
	}
	if gotTarget != wantTarget {
		t.Fatalf("xdsClient.WatchService() called with target: %v, want %v", gotTarget, wantTarget)
	}
}

// buildResolverForTarget builds an xDS resolver for the given target. It
// returns a testClientConn which allows inspection of resolver updates, and a
// function to close the resolver once the test is complete.
func buildResolverForTarget(t *testing.T, target resolver.Target) (*testClientConn, func()) {
	builder := resolver.Get(xdsScheme)
	if builder == nil {
		t.Fatalf("resolver.Get(%v) returned nil", xdsScheme)
	}

	tcc := newTestClientConn()
	r, err := builder.Build(target, tcc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("builder.Build(%v) returned err: %v", target, err)
	}
	return tcc, r.Close
}

// TestResolverResourceName builds an xDS resolver and verifies that the
// resource name specified in the discovery request matches expectations.
func (s) TestResolverResourceName(t *testing.T) {
	// Federation support is required when new style names are used.
	oldXDSFederation := envconfig.XDSFederation
	envconfig.XDSFederation = true
	defer func() { envconfig.XDSFederation = oldXDSFederation }()

	tests := []struct {
		name                         string
		listenerResourceNameTemplate string
		extraAuthority               string
		dialTarget                   string
		wantResourceName             string
	}{
		{
			name:                         "default %s old style",
			listenerResourceNameTemplate: "%s",
			dialTarget:                   "xds:///target",
			wantResourceName:             "target",
		},
		{
			name:                         "old style no percent encoding",
			listenerResourceNameTemplate: "/path/to/%s",
			dialTarget:                   "xds:///target",
			wantResourceName:             "/path/to/target",
		},
		{
			name:                         "new style with %s",
			listenerResourceNameTemplate: "xdstp://authority.com/%s",
			dialTarget:                   "xds:///0.0.0.0:8080",
			wantResourceName:             "xdstp://authority.com/0.0.0.0:8080",
		},
		{
			name:                         "new style percent encoding",
			listenerResourceNameTemplate: "xdstp://authority.com/%s",
			dialTarget:                   "xds:///[::1]:8080",
			wantResourceName:             "xdstp://authority.com/%5B::1%5D:8080",
		},
		{
			name:                         "new style different authority",
			listenerResourceNameTemplate: "xdstp://authority.com/%s",
			extraAuthority:               "test-authority",
			dialTarget:                   "xds://test-authority/target",
			wantResourceName:             "xdstp://test-authority/envoy.config.listener.v3.Listener/target",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the management server to push the requested resource name
			// on to a channel. No resources are configured on the management
			// server as part of this test, as we are only interested in the
			// resource name being requested.
			resourceNameCh := make(chan string, 1)
			mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{
				OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
					// When the resolver is being closed, the watch associated
					// with the listener resource will be cancelled, and it
					// might result in a discovery request with no resource
					// names. Hence, we only consider requests which contain a
					// resource name.
					var name string
					if len(req.GetResourceNames()) == 1 {
						name = req.GetResourceNames()[0]
					}
					select {
					case resourceNameCh <- name:
					default:
					}
					return nil
				},
			})
			if err != nil {
				t.Fatalf("Failed to start xDS management server: %v", err)
			}
			defer mgmtServer.Stop()

			// Create a bootstrap configuration with test options.
			opts := xdsbootstrap.Options{
				ServerURI: mgmtServer.Address,
				Version:   xdsbootstrap.TransportV3,
				ClientDefaultListenerResourceNameTemplate: tt.listenerResourceNameTemplate,
			}
			if tt.extraAuthority != "" {
				// In this test, we really don't care about having multiple
				// management servers. All we need to verify is whether the
				// resource name matches expectation.
				opts.Authorities = map[string]string{
					tt.extraAuthority: mgmtServer.Address,
				}
			}
			cleanup, err := xdsbootstrap.CreateFile(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()

			_, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL(tt.dialTarget)})
			defer rClose()

			// Verify the resource name in the discovery request being sent out.
			select {
			case gotResourceName := <-resourceNameCh:
				if gotResourceName != tt.wantResourceName {
					t.Fatalf("Received discovery request with resource name: %v, want %v", gotResourceName, tt.wantResourceName)
				}
			case <-time.After(defaultTestTimeout):
				t.Fatalf("Timeout when waiting for discovery request")
			}
		})
	}
}

// TestResolverWatchCallbackAfterClose tests the case where a service update
// from the underlying xDS client is received after the resolver is closed, and
// verifies that the update is not propagated to the ClientConn.
func (s) TestResolverWatchCallbackAfterClose(t *testing.T) {
	// Setup the management server that synchronizes with the test goroutine
	// using two channels. The management server signals the test goroutine when
	// it receives a discovery request for a route configuration resource. And
	// the test goroutine signals the management server when the resolver is
	// closed.
	waitForRouteConfigDiscoveryReqCh := make(chan struct{})
	waitForResolverCloseCh := make(chan struct{})
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3RouteConfigURL {
				close(waitForRouteConfigDiscoveryReqCh)
				<-waitForResolverCloseCh
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Failed to start xDS management server: %v", err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Configure listener and route configuration resources on the management
	// server.
	const serviceName = "my-service-client-side-xds"
	rdsName := "route-" + serviceName
	cdsName := "cluster-" + serviceName
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, rdsName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, serviceName, cdsName)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	// Wait for a discovery request for a route configuration resource.
	select {
	case <-waitForRouteConfigDiscoveryReqCh:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for a discovery request for a route configuration resource")
	}

	// Close the resolver and unblock the management server.
	rClose()
	close(waitForResolverCloseCh)

	// Verify that the update from the management server is not propagated to
	// the ClientConn. The xDS resolver, once closed, is expected to drop
	// updates from the xDS client.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := tcc.stateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("ClientConn received an update from the resolver that was closed: %v", err)
	}
}

// TestResolverCloseClosesXDSClient tests that the xDS resolver's Close method
// closes the xDS client.
func (s) TestResolverCloseClosesXDSClient(t *testing.T) {
	bootstrapCfg := &bootstrap.Config{
		XDSServer: &bootstrap.ServerConfig{
			ServerURI:    "dummy-management-server-address",
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			TransportAPI: version.TransportV3,
		},
	}

	// Override xDS client creation to use bootstrap configuration pointing to a
	// dummy management server. Also close a channel when the returned xDS
	// client is closed.
	closeCh := make(chan struct{})
	origNewClient := newXDSClient
	newXDSClient = func() (xdsclient.XDSClient, func(), error) {
		c, cancel, err := xdsclient.NewWithConfigForTesting(bootstrapCfg, defaultTestTimeout, defaultTestTimeout)
		return c, func() {
			close(closeCh)
			cancel()
		}, err
	}
	defer func() {
		newXDSClient = origNewClient
	}()

	_, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///my-service-client-side-xds")})
	rClose()

	select {
	case <-closeCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for xDS client to be closed")
	}
}

// TestResolverBadServiceUpdate tests the case where a resource returned by the
// management server is NACKed by the xDS client, which then returns an update
// containing an error to the resolver. Verifies that the update is propagated
// to the ClientConn by the resolver. It also tests the cases where the resolver
// gets a good update subsequently, and another error after the good update.
func (s) TestResolverBadServiceUpdate(t *testing.T) {
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	// Configure a listener resource that is expected to be NACKed because it
	// does not contain the `RouteSpecifier` field in the HTTPConnectionManager.
	hcm := testutils.MarshalAny(&v3httppb.HttpConnectionManager{
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
	})
	lis := &v3listenerpb.Listener{
		Name:        serviceName,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	}
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{lis},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	wantErr := "no RouteSpecifier"
	val, err := tcc.errorCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for error to be propagated to the ClientConn")
	}
	gotErr := val.(error)
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantErr) {
		t.Fatalf("Received error from resolver %q, want %q", gotErr, wantErr)
	}

	// Configure good listener and route configuration resources on the
	// management server.
	rdsName := "route-" + serviceName
	cdsName := "cluster-" + serviceName
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, rdsName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, serviceName, cdsName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Expect a good update from the resolver.
	val, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState := val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}

	// Configure another bad resource on the management server.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{lis},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Expect an error update from the resolver.
	val, err = tcc.errorCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for error to be propagated to the ClientConn")
	}
	gotErr = val.(error)
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantErr) {
		t.Fatalf("Received error from resolver %q, want %q", gotErr, wantErr)
	}
}

// TestResolverGoodServiceUpdate tests the case where the resource returned by
// the management server is ACKed by the xDS client, which then returns a good
// service update to the resolver. The test verifies that the service config
// returned by the resolver matches expectations, and that the config selector
// returned by the resolver picks clusters based on the route configuration
// received from the management server.
func (s) TestResolverGoodServiceUpdate(t *testing.T) {
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	ldsName := serviceName
	rdsName := "route-" + serviceName
	for _, tt := range []struct {
		routeConfig       *v3routepb.RouteConfiguration
		wantServiceConfig string
		wantClusters      map[string]bool
	}{
		{
			// A route configuration with a single cluster.
			routeConfig: &v3routepb.RouteConfiguration{
				Name: rdsName,
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{ldsName},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
								Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
									{
										Name:   "test-cluster-1",
										Weight: &wrapperspb.UInt32Value{Value: 100},
									},
								},
							}},
						}},
					}},
				}},
			},
			wantServiceConfig: `
{
  "loadBalancingConfig": [{
    "xds_cluster_manager_experimental": {
      "children": {
        "cluster:test-cluster-1": {
          "childPolicy": [{
			"cds_experimental": {
			  "cluster": "test-cluster-1"
			}
		  }]
        }
      }
    }
  }]
}`,
			wantClusters: map[string]bool{"cluster:test-cluster-1": true},
		},
		{
			// A route configuration with a two new clusters.
			routeConfig: &v3routepb.RouteConfiguration{
				Name: rdsName,
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{ldsName},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
								Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
									{
										Name:   "cluster_1",
										Weight: &wrapperspb.UInt32Value{Value: 75},
									},
									{
										Name:   "cluster_2",
										Weight: &wrapperspb.UInt32Value{Value: 25},
									},
								},
							}},
						}},
					}},
				}},
			},
			// This update contains the cluster from the previous update as well
			// as this update, as the previous config selector still references
			// the old cluster when the new one is pushed.
			wantServiceConfig: `
{
  "loadBalancingConfig": [{
    "xds_cluster_manager_experimental": {
      "children": {
        "cluster:test-cluster-1": {
          "childPolicy": [{
			"cds_experimental": {
			  "cluster": "test-cluster-1"
			}
		  }]
        },
        "cluster:cluster_1": {
          "childPolicy": [{
			"cds_experimental": {
			  "cluster": "cluster_1"
			}
		  }]
        },
        "cluster:cluster_2": {
          "childPolicy": [{
			"cds_experimental": {
			  "cluster": "cluster_2"
			}
		  }]
        }
      }
    }
  }]
}`,
			wantClusters: map[string]bool{"cluster:cluster_1": true, "cluster:cluster_2": true},
		},
		{
			// A redundant route configuration update.
			// TODO(easwars): Do we need this, or can we do something else? Because the xds client might swallow this update.
			routeConfig: &v3routepb.RouteConfiguration{
				Name: rdsName,
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{ldsName},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
								Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
									{
										Name:   "cluster_1",
										Weight: &wrapperspb.UInt32Value{Value: 75},
									},
									{
										Name:   "cluster_2",
										Weight: &wrapperspb.UInt32Value{Value: 25},
									},
								},
							}},
						}},
					}},
				}},
			},
			// With this redundant update, the old config selector has been
			// stopped, so there are no more references to the first cluster.
			// Only the second update's clusters should remain.
			wantServiceConfig: `
{
  "loadBalancingConfig": [{
    "xds_cluster_manager_experimental": {
      "children": {
        "cluster:cluster_1": {
          "childPolicy": [{
			"cds_experimental": {
			  "cluster": "cluster_1"
			}
		  }]
        },
        "cluster:cluster_2": {
          "childPolicy": [{
			"cds_experimental": {
			  "cluster": "cluster_2"
			}
		  }]
        }
      }
    }
  }]
}`,
			wantClusters: map[string]bool{"cluster:cluster_1": true, "cluster:cluster_2": true},
		},
	} {

		// Configure the management server with a good listener resource and a
		// route configuration resource, as specified by the test case.
		resources := e2e.UpdateOptions{
			NodeID:         nodeID,
			Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
			Routes:         []*v3routepb.RouteConfiguration{tt.routeConfig},
			SkipValidation: true,
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		if err := mgmtServer.Update(ctx, resources); err != nil {
			t.Fatal(err)
		}

		// Read the update pushed by the resolver to the ClientConn.
		val, err := tcc.stateCh.Receive(ctx)
		if err != nil {
			t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
		}
		rState := val.(resolver.State)
		if err := rState.ServiceConfig.Err; err != nil {
			t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
		}

		wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(tt.wantServiceConfig)
		if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
			t.Errorf("Received unexpected service config")
			t.Error("got: ", cmp.Diff(nil, rState.ServiceConfig.Config))
			t.Fatal("want: ", cmp.Diff(nil, wantSCParsed.Config))
		}

		cs := iresolver.GetConfigSelector(rState)
		if cs == nil {
			t.Fatal("Received nil config selector in update from resolver")
		}

		pickedClusters := make(map[string]bool)
		// Odds of picking 75% cluster 100 times in a row: 1 in 3E-13.  And
		// with the random number generator stubbed out, we can rely on this
		// to be 100% reproducible.
		for i := 0; i < 100; i++ {
			res, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
			if err != nil {
				t.Fatalf("cs.SelectConfig(): %v", err)
			}
			cluster := clustermanager.GetPickedClusterForTesting(res.Context)
			pickedClusters[cluster] = true
			res.OnCommitted()
		}
		if !cmp.Equal(pickedClusters, tt.wantClusters) {
			t.Errorf("Picked clusters: %v; want: %v", pickedClusters, tt.wantClusters)
		}
	}
}

// TestResolverRequestHash tests a case where a resolver receives a RouteConfig update
// with a HashPolicy specifying to generate a hash. The configSelector generated should
// successfully generate a Hash.
func (s) TestResolverRequestHash(t *testing.T) {
	oldRH := envconfig.XDSRingHash
	envconfig.XDSRingHash = true
	defer func() { envconfig.XDSRingHash = oldRH }()

	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	ldsName := serviceName
	rdsName := "route-" + serviceName
	// Configure the management server with a good listener resource and a
	// route configuration resource that specifies a hash policy.
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		Routes: []*v3routepb.RouteConfiguration{{
			Name: rdsName,
			VirtualHosts: []*v3routepb.VirtualHost{{
				Domains: []string{ldsName},
				Routes: []*v3routepb.Route{{
					Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
							Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
								{
									Name:   "test-cluster-1",
									Weight: &wrapperspb.UInt32Value{Value: 100},
								},
							},
						}},
						HashPolicy: []*v3routepb.RouteAction_HashPolicy{{
							PolicySpecifier: &v3routepb.RouteAction_HashPolicy_Header_{
								Header: &v3routepb.RouteAction_HashPolicy_Header{
									HeaderName: ":path",
								},
							},
							Terminal: true,
						}},
					}},
				}},
			}},
		}},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the update pushed by the resolver to the ClientConn.
	val, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState := val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}

	// Selecting a config when there was a hash policy specified in the route
	// that will be selected should put a request hash in the config's context.
	res, err := cs.SelectConfig(iresolver.RPCInfo{
		Context: metadata.NewOutgoingContext(ctx, metadata.Pairs(":path", "/products")),
		Method:  "/service/method",
	})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}
	gotHash := ringhash.GetRequestHashForTesting(res.Context)
	wantHash := xxhash.Sum64String("/products")
	if gotHash != wantHash {
		t.Fatalf("Got request hash: %v, want: %v", gotHash, wantHash)
	}
}

// TestResolverRemovedWithRPCs tests the case where resources are removed from
// the management server, causing it to send an empty update to the xDS client,
// which returns a resource-not-found error to the xDS resolver. The test
// verifies that an ongoing RPC is handled properly when this happens.
func (s) TestResolverRemovedWithRPCs(t *testing.T) {
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	ldsName := serviceName
	rdsName := "route-" + serviceName
	// Configure the management server with a good listener and route
	// configuration resource.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, ldsName, "test-cluster-1")},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the update pushed by the resolver to the ClientConn.
	val, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState := val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(`
{
	"loadBalancingConfig": [
		{
		  "xds_cluster_manager_experimental": {
			"children": {
			  "cluster:test-cluster-1": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "test-cluster-1"
					}
				  }
				]
			  }
			}
		  }
		}
	  ]
}`)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}

	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}
	res, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}

	// Delete the resources on the management server. This should result in a
	// resource-not-found error from the xDS client.
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{NodeID: nodeID}); err != nil {
		t.Fatal(err)
	}

	// The RPC started earlier is still in progress. So, the xDS resolver will
	// not produce an empty service config at this point. Instead it will retain
	// the cluster to which the RPC is ongoing in the service config, but will
	// return an erroring config selector which will fail new RPCs.
	val, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState = val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}
	cs = iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}
	_, err = cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("cs.SelectConfig() returned: %v, want: %v", err, codes.Unavailable)
	}

	// "Finish the RPC"; this could cause a panic if the resolver doesn't
	// handle it correctly.
	res.OnCommitted()

	// Now that the RPC is committed, the xDS resolver is expected to send an
	// update with an empty service config.
	val, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState = val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantSCParsed = internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(`{}`)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}
}

// TestResolverRemovedResource tests the case where resources returned by the
// management server are removed. The test verifies that the resolver pushes the
// expected config selector and service config in this case.
func (s) TestResolverRemovedResource(t *testing.T) {
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	// Configure the management server with a good listener and route
	// configuration resource.
	ldsName := serviceName
	rdsName := "route-" + serviceName
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, ldsName, "test-cluster-1")},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the update pushed by the resolver to the ClientConn.
	val, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState := val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(`
{
	"loadBalancingConfig": [
		{
		  "xds_cluster_manager_experimental": {
			"children": {
			  "cluster:test-cluster-1": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "test-cluster-1"
					}
				  }
				]
			  }
			}
		  }
		}
	  ]
}`)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}

	// "Make an RPC" by invoking the config selector.
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}

	res, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}

	// "Finish the RPC"; this could cause a panic if the resolver doesn't
	// handle it correctly.
	res.OnCommitted()

	// Delete the resources on the management server, resulting in a
	// resource-not-found error from the xDS client.
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{NodeID: nodeID}); err != nil {
		t.Fatal(err)
	}

	// The channel should receive the existing service config with the original
	// cluster but with an erroring config selector.
	val, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState = val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}

	// "Make another RPC" by invoking the config selector.
	cs = iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}

	res, err = cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("cs.SelectConfig() got %v, %v, expected UNAVAILABLE error", res, err)
	}

	// In the meantime, an empty ServiceConfig update should have been sent.
	val, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState = val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantSCParsed = internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)("{}")
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}
}

// TestResolverWRR tests the case where the route configuration returned by the
// management server contains a set of weighted clusters. The test performs a
// bunch of RPCs using the cluster specifier returned by the resolver, and
// verifies the cluster distribution.
func (s) TestResolverWRR(t *testing.T) {
	defer func(oldNewWRR func() wrr.WRR) { newWRR = oldNewWRR }(newWRR)
	newWRR = testutils.NewTestWRR

	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	ldsName := serviceName
	rdsName := "route-" + serviceName
	// Configure the management server with a good listener resource and a
	// route configuration resource.
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		Routes: []*v3routepb.RouteConfiguration{{
			Name: rdsName,
			VirtualHosts: []*v3routepb.VirtualHost{{
				Domains: []string{ldsName},
				Routes: []*v3routepb.Route{{
					Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
							Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
								{
									Name:   "A",
									Weight: &wrapperspb.UInt32Value{Value: 75},
								},
								{
									Name:   "B",
									Weight: &wrapperspb.UInt32Value{Value: 25},
								},
							},
						}},
					}},
				}},
			}},
		}},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the update pushed by the resolver to the ClientConn.
	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}

	// Make RPCs are verify WRR behavior in the cluster specifier.
	picks := map[string]int{}
	for i := 0; i < 100; i++ {
		res, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
		if err != nil {
			t.Fatalf("cs.SelectConfig(): %v", err)
		}
		picks[clustermanager.GetPickedClusterForTesting(res.Context)]++
		res.OnCommitted()
	}
	want := map[string]int{"cluster:A": 75, "cluster:B": 25}
	if !cmp.Equal(picks, want) {
		t.Errorf("Picked clusters: %v; want: %v", picks, want)
	}
}

// TestResolverMaxStreamDuration tests the case where the resolver receives max
// stream duration as part of the listener and route configuration resources.
// The test verifies that the RPC timeout returned by the config selector
// matches expectations. A non-nil max stream duration (this includes an
// explicit zero value) in a matching route overrides the value specified in the
// listener resource.
func (s) TestResolverMaxStreamDuration(t *testing.T) {
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	// Configure the management server with a listener resource that specifies a
	// max stream duration as part of its HTTP connection manager. Also
	// configure a route configuration resource, which has multiple routes with
	// different values of max stream duration.
	ldsName := serviceName
	rdsName := "route-" + serviceName
	hcm := testutils.MarshalAny(&v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{Rds: &v3httppb.Rds{
			ConfigSource: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
			},
			RouteConfigName: rdsName,
		}},
		HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
		CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
			MaxStreamDuration: durationpb.New(1 * time.Second),
		},
	})
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{{
			Name:        ldsName,
			ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
			FilterChains: []*v3listenerpb.FilterChain{{
				Name: "filter-chain-name",
				Filters: []*v3listenerpb.Filter{{
					Name:       wellknown.HTTPConnectionManager,
					ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
				}},
			}},
		}},
		Routes: []*v3routepb.RouteConfiguration{{
			Name: rdsName,
			VirtualHosts: []*v3routepb.VirtualHost{{
				Domains: []string{ldsName},
				Routes: []*v3routepb.Route{
					{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/foo"}},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
								Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
									{
										Name:   "A",
										Weight: &wrapperspb.UInt32Value{Value: 100},
									},
								}},
							},
							MaxStreamDuration: &v3routepb.RouteAction_MaxStreamDuration{
								MaxStreamDuration: durationpb.New(5 * time.Second),
							},
						}},
					},
					{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/bar"}},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
								Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
									{
										Name:   "B",
										Weight: &wrapperspb.UInt32Value{Value: 100},
									},
								}},
							},
							MaxStreamDuration: &v3routepb.RouteAction_MaxStreamDuration{
								MaxStreamDuration: durationpb.New(0 * time.Second),
							},
						}},
					},
					{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
								Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
									{
										Name:   "C",
										Weight: &wrapperspb.UInt32Value{Value: 100},
									},
								}},
							},
						}},
					},
				},
			}},
		}},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the update pushed by the resolver to the ClientConn.
	gotState, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState := gotState.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}

	testCases := []struct {
		name   string
		method string
		want   *time.Duration
	}{{
		name:   "RDS setting",
		method: "/foo/method",
		want:   newDurationP(5 * time.Second),
	}, {
		name:   "explicit zero in RDS; ignore LDS",
		method: "/bar/method",
		want:   nil,
	}, {
		name:   "no config in RDS; fallback to LDS",
		method: "/baz/method",
		want:   newDurationP(time.Second),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := iresolver.RPCInfo{
				Method:  tc.method,
				Context: ctx,
			}
			res, err := cs.SelectConfig(req)
			if err != nil {
				t.Errorf("cs.SelectConfig(%v): %v", req, err)
				return
			}
			res.OnCommitted()
			got := res.MethodConfig.Timeout
			if !cmp.Equal(got, tc.want) {
				t.Errorf("For method %q: res.MethodConfig.Timeout = %v; want %v", tc.method, got, tc.want)
			}
		})
	}
}

// TestResolverDelayedOnCommitted tests that clusters remain in service
// config if RPCs are in flight.
func (s) TestResolverDelayedOnCommitted(t *testing.T) {
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	// Configure the management server with a good listener and route
	// configuration resource.
	ldsName := serviceName
	rdsName := "route-" + serviceName
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, ldsName, "old-cluster")},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the update pushed by the resolver to the ClientConn.
	val, err := tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState := val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(`
{
	"loadBalancingConfig": [
		{
		  "xds_cluster_manager_experimental": {
			"children": {
			  "cluster:old-cluster": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "old-cluster"
					} 
				  } 
				] 
			  } 
			} 
		  } 
		} 
	  ] 
}`)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}

	// Make an RPC, but do not commit it yet.
	cs := iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}
	resOld, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}
	if cluster := clustermanager.GetPickedClusterForTesting(resOld.Context); cluster != "cluster:old-cluster" {
		t.Fatalf("Picked cluster is %q, want %q", cluster, "cluster:old-cluster")
	}

	// Delay resOld.OnCommitted(). As long as there are pending RPCs to removed
	// clusters, they still appear in the service config.

	// Update the route configuration resource on the management server to
	// return a new cluster.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, ldsName, "new-cluster")},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the update pushed by the resolver to the ClientConn and ensure the
	// old cluster is present in the service config. Also ensure that the newly
	// returned config selector does not hold a reference to the old cluster.
	val, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState = val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantSCParsed = internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(`
{
	"loadBalancingConfig": [
		{
		  "xds_cluster_manager_experimental": {
			"children": {
			  "cluster:old-cluster": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "old-cluster"
					} 
				  } 
				] 
			  }, 
			  "cluster:new-cluster": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "new-cluster"
					} 
				  } 
				] 
			  } 
			} 
		  } 
		} 
	  ] 
}`)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s\nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}

	cs = iresolver.GetConfigSelector(rState)
	if cs == nil {
		t.Fatal("Received nil config selector in update from resolver")
	}
	resNew, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}
	if cluster := clustermanager.GetPickedClusterForTesting(resNew.Context); cluster != "cluster:new-cluster" {
		t.Fatalf("Picked cluster is %q, want %q", cluster, "cluster:new-cluster")
	}

	// Invoke OnCommitted on the old RPC; should lead to a service config update
	// that deletes the old cluster, as the old cluster no longer has any
	// pending RPCs.
	resOld.OnCommitted()

	val, err = tcc.stateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for an update from the resolver: %v", err)
	}
	rState = val.(resolver.State)
	if err := rState.ServiceConfig.Err; err != nil {
		t.Fatalf("Received error in service config: %v", rState.ServiceConfig.Err)
	}
	wantSCParsed = internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(`
{
	"loadBalancingConfig": [
		{
		  "xds_cluster_manager_experimental": {
			"children": {
			  "cluster:new-cluster": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "new-cluster"
					} 
				  } 
				] 
			  } 
			} 
		  } 
		} 
	  ] 
}`)
	if !internal.EqualServiceConfigForTesting(rState.ServiceConfig.Config, wantSCParsed.Config) {
		t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, rState.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
	}
}

// TestResolverMultipleLDSUpdates tests the case where two LDS updates with the
// same RDS name to watch are received without an RDS in between. Those LDS
// updates shouldn't trigger a service config update.
func (s) TestResolverMultipleLDSUpdates(t *testing.T) {
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	cleanup, err := xdsbootstrap.CreateFile(xdsbootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
		Version:   xdsbootstrap.TransportV3,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Build an xDS resolver that uses the above bootstrap configuration
	// Creating the xDS resolver should result in creation of the xDS client.
	const serviceName = "my-service-client-side-xds"
	tcc, rClose := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + serviceName)})
	defer rClose()

	// Configure the management server with a listener resource, but no route
	// configuration resource.
	ldsName := serviceName
	rdsName := "route-" + serviceName
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Ensure there is no update from the resolver.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	gotState, err := tcc.stateCh.Receive(sCtx)
	if err == nil {
		t.Fatalf("Received update from resolver %v when none expected", gotState)
	}

	// Configure the management server with a listener resource that points to
	// the same route configuration resource but has different values for some
	// other fields. There is still no route configuration resource on the
	// management server.
	hcm := testutils.MarshalAny(&v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{Rds: &v3httppb.Rds{
			ConfigSource: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
			},
			RouteConfigName: rdsName,
		}},
		HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
		CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
			MaxStreamDuration: durationpb.New(1 * time.Second),
		},
	})
	resources = e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{{
			Name:        ldsName,
			ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
			FilterChains: []*v3listenerpb.FilterChain{{
				Name: "filter-chain-name",
				Filters: []*v3listenerpb.Filter{{
					Name:       wellknown.HTTPConnectionManager,
					ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
				}},
			}},
		}},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Ensure that there is no update from the resolver.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	gotState, err = tcc.stateCh.Receive(sCtx)
	if err == nil {
		t.Fatalf("Received update from resolver %v when none expected", gotState)
	}
}

type filterBuilder struct {
	httpfilter.Filter // embedded as we do not need to implement registry / parsing in this test.
	path              *[]string
}

var _ httpfilter.ClientInterceptorBuilder = &filterBuilder{}

func (fb *filterBuilder) BuildClientInterceptor(config, override httpfilter.FilterConfig) (iresolver.ClientInterceptor, error) {
	if config == nil {
		panic("unexpected missing config")
	}
	*fb.path = append(*fb.path, "build:"+config.(filterCfg).s)
	err := config.(filterCfg).newStreamErr
	if override != nil {
		*fb.path = append(*fb.path, "override:"+override.(filterCfg).s)
		err = override.(filterCfg).newStreamErr
	}

	return &filterInterceptor{path: fb.path, s: config.(filterCfg).s, err: err}, nil
}

type filterInterceptor struct {
	path *[]string
	s    string
	err  error
}

func (fi *filterInterceptor) NewStream(ctx context.Context, ri iresolver.RPCInfo, done func(), newStream func(ctx context.Context, done func()) (iresolver.ClientStream, error)) (iresolver.ClientStream, error) {
	*fi.path = append(*fi.path, "newstream:"+fi.s)
	if fi.err != nil {
		return nil, fi.err
	}
	d := func() {
		*fi.path = append(*fi.path, "done:"+fi.s)
		done()
	}
	cs, err := newStream(ctx, d)
	if err != nil {
		return nil, err
	}
	return &clientStream{ClientStream: cs, path: fi.path, s: fi.s}, nil
}

type clientStream struct {
	iresolver.ClientStream
	path *[]string
	s    string
}

type filterCfg struct {
	httpfilter.FilterConfig
	s            string
	newStreamErr error
}

func (s) TestXDSResolverHTTPFilters(t *testing.T) {
	var path []string
	testCases := []struct {
		name         string
		ldsFilters   []xdsresource.HTTPFilter
		vhOverrides  map[string]httpfilter.FilterConfig
		rtOverrides  map[string]httpfilter.FilterConfig
		clOverrides  map[string]httpfilter.FilterConfig
		rpcRes       map[string][][]string
		selectErr    string
		newStreamErr string
	}{
		{
			name: "no router filter",
			ldsFilters: []xdsresource.HTTPFilter{
				{Name: "foo", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo1"}},
			},
			rpcRes: map[string][][]string{
				"1": {
					{"build:foo1", "override:foo2", "build:bar1", "override:bar2", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
				},
			},
			selectErr: "no router filter present",
		},
		{
			name: "ignored after router filter",
			ldsFilters: []xdsresource.HTTPFilter{
				{Name: "foo", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo1"}},
				routerFilter,
				{Name: "foo2", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo2"}},
			},
			rpcRes: map[string][][]string{
				"1": {
					{"build:foo1", "newstream:foo1", "done:foo1"},
				},
				"2": {
					{"build:foo1", "newstream:foo1", "done:foo1"},
					{"build:foo1", "newstream:foo1", "done:foo1"},
					{"build:foo1", "newstream:foo1", "done:foo1"},
				},
			},
		},
		{
			name: "NewStream error; ensure earlier interceptor Done is still called",
			ldsFilters: []xdsresource.HTTPFilter{
				{Name: "foo", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo1"}},
				{Name: "bar", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "bar1", newStreamErr: errors.New("bar newstream err")}},
				routerFilter,
			},
			rpcRes: map[string][][]string{
				"1": {
					{"build:foo1", "build:bar1", "newstream:foo1", "newstream:bar1" /* <err in bar1 NewStream> */, "done:foo1"},
				},
				"2": {
					{"build:foo1", "build:bar1", "newstream:foo1", "newstream:bar1" /* <err in bar1 NewSteam> */, "done:foo1"},
				},
			},
			newStreamErr: "bar newstream err",
		},
		{
			name: "all overrides",
			ldsFilters: []xdsresource.HTTPFilter{
				{Name: "foo", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "foo1", newStreamErr: errors.New("this is overridden to nil")}},
				{Name: "bar", Filter: &filterBuilder{path: &path}, Config: filterCfg{s: "bar1"}},
				routerFilter,
			},
			vhOverrides: map[string]httpfilter.FilterConfig{"foo": filterCfg{s: "foo2"}, "bar": filterCfg{s: "bar2"}},
			rtOverrides: map[string]httpfilter.FilterConfig{"foo": filterCfg{s: "foo3"}, "bar": filterCfg{s: "bar3"}},
			clOverrides: map[string]httpfilter.FilterConfig{"foo": filterCfg{s: "foo4"}, "bar": filterCfg{s: "bar4"}},
			rpcRes: map[string][][]string{
				"1": {
					{"build:foo1", "override:foo2", "build:bar1", "override:bar2", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
					{"build:foo1", "override:foo2", "build:bar1", "override:bar2", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
				},
				"2": {
					{"build:foo1", "override:foo3", "build:bar1", "override:bar3", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
					{"build:foo1", "override:foo4", "build:bar1", "override:bar4", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
					{"build:foo1", "override:foo3", "build:bar1", "override:bar3", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
					{"build:foo1", "override:foo4", "build:bar1", "override:bar4", "newstream:foo1", "newstream:bar1", "done:bar1", "done:foo1"},
				},
			},
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			xdsR, xdsC, tcc, cancel := testSetup(t, setupOpts{target: target})
			defer xdsR.Close()
			defer cancel()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			waitForWatchListener(ctx, t, xdsC, targetStr)

			xdsC.InvokeWatchListenerCallback(xdsresource.ListenerUpdate{
				RouteConfigName: routeStr,
				HTTPFilters:     tc.ldsFilters,
			}, nil)
			if i == 0 {
				waitForWatchRouteConfig(ctx, t, xdsC, routeStr)
			}

			defer func(oldNewWRR func() wrr.WRR) { newWRR = oldNewWRR }(newWRR)
			newWRR = testutils.NewTestWRR

			// Invoke the watchAPI callback with a good service update and wait for the
			// UpdateState method to be called on the ClientConn.
			xdsC.InvokeWatchRouteConfigCallback("", xdsresource.RouteConfigUpdate{
				VirtualHosts: []*xdsresource.VirtualHost{
					{
						Domains: []string{targetStr},
						Routes: []*xdsresource.Route{{
							Prefix: newStringP("1"), WeightedClusters: map[string]xdsresource.WeightedCluster{
								"A": {Weight: 1},
								"B": {Weight: 1},
							},
						}, {
							Prefix: newStringP("2"), WeightedClusters: map[string]xdsresource.WeightedCluster{
								"A": {Weight: 1},
								"B": {Weight: 1, HTTPFilterConfigOverride: tc.clOverrides},
							},
							HTTPFilterConfigOverride: tc.rtOverrides,
						}},
						HTTPFilterConfigOverride: tc.vhOverrides,
					},
				},
			}, nil)

			gotState, err := tcc.stateCh.Receive(ctx)
			if err != nil {
				t.Fatalf("Error waiting for UpdateState to be called: %v", err)
			}
			rState := gotState.(resolver.State)
			if err := rState.ServiceConfig.Err; err != nil {
				t.Fatalf("ClientConn.UpdateState received error in service config: %v", rState.ServiceConfig.Err)
			}

			cs := iresolver.GetConfigSelector(rState)
			if cs == nil {
				t.Fatal("received nil config selector")
			}

			for method, wants := range tc.rpcRes {
				// Order of wants is non-deterministic.
				remainingWant := make([][]string, len(wants))
				copy(remainingWant, wants)
				for n := range wants {
					path = nil

					res, err := cs.SelectConfig(iresolver.RPCInfo{Method: method, Context: context.Background()})
					if tc.selectErr != "" {
						if err == nil || !strings.Contains(err.Error(), tc.selectErr) {
							t.Errorf("SelectConfig(_) = _, %v; want _, Contains(%v)", err, tc.selectErr)
						}
						if err == nil {
							res.OnCommitted()
						}
						continue
					}
					if err != nil {
						t.Fatalf("Unexpected error from cs.SelectConfig(_): %v", err)
					}
					var doneFunc func()
					_, err = res.Interceptor.NewStream(context.Background(), iresolver.RPCInfo{}, func() {}, func(ctx context.Context, done func()) (iresolver.ClientStream, error) {
						doneFunc = done
						return nil, nil
					})
					if tc.newStreamErr != "" {
						if err == nil || !strings.Contains(err.Error(), tc.newStreamErr) {
							t.Errorf("NewStream(...) = _, %v; want _, Contains(%v)", err, tc.newStreamErr)
						}
						if err == nil {
							res.OnCommitted()
							doneFunc()
						}
						continue
					}
					if err != nil {
						t.Fatalf("unexpected error from Interceptor.NewStream: %v", err)

					}
					res.OnCommitted()
					doneFunc()

					// Confirm the desired path is found in remainingWant, and remove it.
					pass := false
					for i := range remainingWant {
						if reflect.DeepEqual(path, remainingWant[i]) {
							remainingWant[i] = remainingWant[len(remainingWant)-1]
							remainingWant = remainingWant[:len(remainingWant)-1]
							pass = true
							break
						}
					}
					if !pass {
						t.Errorf("%q:%v - path:\n%v\nwant one of:\n%v", method, n, path, remainingWant)
					}
				}
			}
		})
	}
}

func newDurationP(d time.Duration) *time.Duration {
	return &d
}
