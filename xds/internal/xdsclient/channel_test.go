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
 */

package xdsclient

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/fakeserver"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdsinternal "google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/grpc/xds/internal/httpfilter/router"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/ads"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/grpctransport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Tests different failure cases when creating a new xdsChannel. It checks that
// the xdsChannel creation fails when any of the required options (transport,
// serverConfig, bootstrapConfig, or resourceTypeGetter) are missing or nil.
func (s) TestChannel_New_FailureCases(t *testing.T) {
	type fakeTransport struct {
		transport.Interface
	}

	tests := []struct {
		name       string
		opts       channelOptions
		wantErrStr string
	}{
		{
			name:       "emptyTransport",
			opts:       channelOptions{},
			wantErrStr: "transport is nil",
		},
		{
			name:       "emptyServerConfig",
			opts:       channelOptions{transport: &fakeTransport{}},
			wantErrStr: "serverConfig is nil",
		},
		{
			name: "emptyBootstrapConfig",
			opts: channelOptions{
				transport:    &fakeTransport{},
				serverConfig: &bootstrap.ServerConfig{},
			},
			wantErrStr: "bootstrapConfig is nil",
		},
		{
			name: "emptyResourceTypeGetter",
			opts: channelOptions{
				transport:       &fakeTransport{},
				serverConfig:    &bootstrap.ServerConfig{},
				bootstrapConfig: &bootstrap.Config{},
			},
			wantErrStr: "resourceTypeGetter is nil",
		},
		{
			name: "emptyEventHandler",
			opts: channelOptions{
				transport:          &fakeTransport{},
				serverConfig:       &bootstrap.ServerConfig{},
				bootstrapConfig:    &bootstrap.Config{},
				resourceTypeGetter: func(string) xdsresource.Type { return nil },
			},
			wantErrStr: "eventHandler is nil",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newChannel(test.opts)
			if err == nil || !strings.Contains(err.Error(), test.wantErrStr) {
				t.Fatalf("newXDSChannel() = %v, want %q", err, test.wantErrStr)
			}
		})
	}
}

// startFakeManagementServer starts a fake xDS management server.
//
// We need a fake management server in scenarios where we want to test receipt
// of a resource which is expected to run into unmarshaling errors.
func startFakeManagementServer(t *testing.T) *fakeserver.Server {
	t.Helper()

	fs, cleanup, err := fakeserver.StartServer(nil)
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	t.Cleanup(cleanup)

	t.Logf("Started xDS management server on %s", fs.Address)
	return fs
}

// TODO(easwars): ensure that we have a test for the resource not-found error.
/*
	{
		desc:                     "empty response",
		resourceNamesToRequest:   []string{resourceName1},
		managementServerResponse: &v3discoverypb.DiscoveryResponse{},
		wantUpdates:              nil, // No updates expected as the response is empty.
		wantMetadata: xdsresource.UpdateMetadata{
			Status:   xdsresource.ServiceStatusNACKed,
			Version:  "0",
			ErrState: &xdsresource.UpdateErrorMetadata{Version: "0"},
		},
	},
*/

// Tests different scenarios of the xdsChannel receiving a response from the
// management server. In all scenarios, the xdsChannel is expected to pass the
// received responses as-is to the resource parsing functionality specified by
// the resourceTypeGetter.
func (s) TestChannel_HandleResponseFromManagementServer(t *testing.T) {
	const (
		listenerName1 = "listener-name-1"
		listenerName2 = "listener-name-2"
		routeName     = "route-name"
		clusterName   = "cluster-name"
	)
	var (
		badlyMarshaledResource = &anypb.Any{
			TypeUrl: "type.googleapis.com/envoy.config.listener.v3.Listener",
			Value:   []byte{1, 2, 3, 4},
		}
		apiListener = &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
						Name: routeName,
						VirtualHosts: []*v3routepb.VirtualHost{{
							Domains: []string{listenerName1},
							Routes: []*v3routepb.Route{{
								Match: &v3routepb.RouteMatch{
									PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
								},
								Action: &v3routepb.Route_Route{
									Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
									}}}}}}},
				},
				HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
				CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
					MaxStreamDuration: durationpb.New(time.Second),
				},
			}),
		}
		listener1 = testutils.MarshalAny(t, &v3listenerpb.Listener{
			Name:        listenerName1,
			ApiListener: apiListener,
		})
		listener2 = testutils.MarshalAny(t, &v3listenerpb.Listener{
			Name:        listenerName2,
			ApiListener: apiListener,
		})
	)

	tests := []struct {
		desc                     string
		resourceNamesToRequest   []string
		managementServerResponse *v3discoverypb.DiscoveryResponse
		wantUpdates              map[string]ads.DataAndErrTuple
		wantMetadata             xdsresource.UpdateMetadata
	}{
		{
			desc:                   "badly marshaled response",
			resourceNamesToRequest: []string{listenerName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources:   []*anypb.Any{badlyMarshaledResource},
			},
			wantUpdates: nil, // No updates expected as the response runs into unmarshaling errors.
			wantMetadata: xdsresource.UpdateMetadata{
				Status:   xdsresource.ServiceStatusNACKed,
				Version:  "0",
				ErrState: &xdsresource.UpdateErrorMetadata{Version: "0"},
			},
		},
		{
			desc:                   "one good resource",
			resourceNamesToRequest: []string{listenerName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources:   []*anypb.Any{listener1},
			},
			wantUpdates: map[string]ads.DataAndErrTuple{
				listenerName1: {
					Resource: &xdsresource.ListenerResourceData{Resource: xdsresource.ListenerUpdate{
						InlineRouteConfig: &xdsresource.RouteConfigUpdate{
							VirtualHosts: []*xdsresource.VirtualHost{{
								Domains: []string{listenerName1},
								Routes: []*xdsresource.Route{{
									Prefix:           newStringP("/"),
									WeightedClusters: map[string]xdsresource.WeightedCluster{clusterName: {Weight: 1}},
									ActionType:       xdsresource.RouteActionRoute},
								},
							}}},
						MaxStreamDuration: time.Second,
						Raw:               listener1,
						HTTPFilters:       makeRouterFilterList(t),
					}},
				},
			},
			wantMetadata: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusACKed,
				Version: "0",
			},
		},
		{
			desc:                   "two good resources",
			resourceNamesToRequest: []string{listenerName1, listenerName2},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources:   []*anypb.Any{listener1, listener2},
			},
			wantUpdates: map[string]ads.DataAndErrTuple{
				listenerName1: {
					Resource: &xdsresource.ListenerResourceData{Resource: xdsresource.ListenerUpdate{
						InlineRouteConfig: &xdsresource.RouteConfigUpdate{
							VirtualHosts: []*xdsresource.VirtualHost{{
								Domains: []string{listenerName1},
								Routes: []*xdsresource.Route{{
									Prefix:           newStringP("/"),
									WeightedClusters: map[string]xdsresource.WeightedCluster{clusterName: {Weight: 1}},
									ActionType:       xdsresource.RouteActionRoute},
								},
							}}},
						MaxStreamDuration: time.Second,
						Raw:               listener1,
						HTTPFilters:       makeRouterFilterList(t),
					}},
				},
				listenerName2: {
					Resource: &xdsresource.ListenerResourceData{Resource: xdsresource.ListenerUpdate{
						InlineRouteConfig: &xdsresource.RouteConfigUpdate{
							VirtualHosts: []*xdsresource.VirtualHost{{
								Domains: []string{listenerName2},
								Routes: []*xdsresource.Route{{
									Prefix:           newStringP("/"),
									WeightedClusters: map[string]xdsresource.WeightedCluster{clusterName: {Weight: 1}},
									ActionType:       xdsresource.RouteActionRoute},
								},
							}}},
						MaxStreamDuration: time.Second,
						Raw:               listener2,
						HTTPFilters:       makeRouterFilterList(t),
					}},
				},
			},
			wantMetadata: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusACKed,
				Version: "0",
			},
		},
		{
			desc:                   "two resources when we requested one",
			resourceNamesToRequest: []string{listenerName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources:   []*anypb.Any{listener1, listener2},
			},
			wantUpdates: map[string]ads.DataAndErrTuple{
				listenerName1: {
					Resource: &xdsresource.ListenerResourceData{Resource: xdsresource.ListenerUpdate{
						InlineRouteConfig: &xdsresource.RouteConfigUpdate{
							VirtualHosts: []*xdsresource.VirtualHost{{
								Domains: []string{listenerName1},
								Routes: []*xdsresource.Route{{
									Prefix:           newStringP("/"),
									WeightedClusters: map[string]xdsresource.WeightedCluster{clusterName: {Weight: 1}},
									ActionType:       xdsresource.RouteActionRoute},
								},
							}}},
						MaxStreamDuration: time.Second,
						Raw:               listener1,
						HTTPFilters:       makeRouterFilterList(t),
					}},
				},
				listenerName2: {
					Resource: &xdsresource.ListenerResourceData{Resource: xdsresource.ListenerUpdate{
						InlineRouteConfig: &xdsresource.RouteConfigUpdate{
							VirtualHosts: []*xdsresource.VirtualHost{{
								Domains: []string{listenerName2},
								Routes: []*xdsresource.Route{{
									Prefix:           newStringP("/"),
									WeightedClusters: map[string]xdsresource.WeightedCluster{clusterName: {Weight: 1}},
									ActionType:       xdsresource.RouteActionRoute},
								},
							}}},
						MaxStreamDuration: time.Second,
						Raw:               listener2,
						HTTPFilters:       makeRouterFilterList(t),
					}},
				},
			},
			wantMetadata: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusACKed,
				Version: "0",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			// Start a fake xDS management server and configure the response it
			// would send to its client.
			mgmtServer := startFakeManagementServer(t)
			mgmtServer.XDSResponseChan <- &fakeserver.Response{Resp: test.managementServerResponse}

			// Lookup the listener resource type from the resource type map. All
			// resources used in this test are listeners resources.
			listenerType := xdsinternal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)

			// Create an xdsChannel for the test with a long watch expiry timer
			// to ensure that watches don't expire for the duration of the test.
			xc := xdsChannelForTest(t, mgmtServer.Address, listenerType, 2*defaultTestTimeout)
			defer xc.close()

			// Subscribe to the resources specified in the test table.
			for _, name := range test.resourceNamesToRequest {
				xc.subscribe(listenerType, name)
			}

			// Wait for an update callback on the authority and verify the
			// contents of the update and the metadata.
			eventHandler := xc.eventHandler.(*testEventHandler)
			gotTyp, gotUpdates, gotMetadata, err := eventHandler.waitForUpdate(ctx)
			if err != nil {
				t.Fatal("Timeout when waiting for update callback to be invoked on the authority")
			}

			if gotTyp != listenerType {
				t.Fatalf("Got type %v, want %v", gotTyp, listenerType)
			}
			if diff := cmp.Diff(gotUpdates, test.wantUpdates, cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("Got unexpected diff in update (-want +got):\n%s\n want: %+v\n got: %+v", diff, gotUpdates, test.wantUpdates)
			}
			opts := cmp.Options{
				cmpopts.IgnoreFields(xdsresource.UpdateMetadata{}, "Timestamp"),
				cmpopts.IgnoreFields(xdsresource.UpdateErrorMetadata{}, "Timestamp"),
				cmpopts.IgnoreFields(xdsresource.UpdateErrorMetadata{}, "Err"),
			}
			if diff := cmp.Diff(gotMetadata, test.wantMetadata, opts...); diff != "" {
				t.Fatalf("Got unexpected diff in update metadata (-want +got):\n%s\n want: %+v\n got: %+v", diff, gotMetadata, test.wantMetadata)
			}
		})
	}
}

func (s) TestChannel_HandleResponseWatchExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server, but do not configure any resources on it.
	// This will result in the watch for a resource to timeout.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Lookup the listener resource type from the resource type map. All
	// resources used in this test are listeners resources.
	listenerType := xdsinternal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)

	// Create an xdsChannel for the test with a short watch expiry timer to
	// ensure that the test does not run very long, as it needs to wait for the
	// watch to expire.
	xc := xdsChannelForTest(t, mgmtServer.Address, listenerType, 2*defaultTestShortTimeout)
	defer xc.close()

	// Subscribe to a listener resource.
	const listenerName = "listener-name"
	xc.subscribe(listenerType, listenerName)

	// Wait for the watch expiry callback on the authority to be invoked and
	// verify that the watch expired for the expected resource name and type.
	eventHandler := xc.eventHandler.(*testEventHandler)
	gotTyp, gotName, err := eventHandler.waitForResourceDoesNotExist(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for the watch expiry callback to be invoked on the authority")
	}

	if gotTyp != listenerType {
		t.Fatalf("Got type %v, want %v", gotTyp, listenerType)
	}
	if gotName != listenerName {
		t.Fatalf("Got name %v, want %v", gotName, listenerName)
	}
}

func xdsChannelForTest(t *testing.T, serverURI string, listenerType xdsresource.Type, watchExpiryTimeout time.Duration) *xdsChannel {
	t.Helper()

	// Create server configuration for the above management server.
	serverCfg, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: serverURI})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}

	// Create a grpc transport to the above management server.
	tr, err := (&grpctransport.Builder{}).Build(transport.BuildOptions{ServerConfig: serverCfg})
	if err != nil {
		t.Fatalf("Failed to create a transport for server config %s: %v", serverCfg, err)
	}

	// Create bootstrap configuration with the top-level xds servers
	// field containing the server configuration for the above
	// management server.
	nodeID := uuid.New().String()
	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[{
					"server_uri": %q,
					"channel_creds": [{"type": "insecure"}]
				}]`, serverURI)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap contents: %v", err)
	}
	bootstrapCfg, err := bootstrap.NewConfigForTesting(contents)
	if err != nil {
		t.Fatalf("Failed to create bootstrap configuration: %v", err)
	}

	// Create an xdsChannel that uses everything set up above.
	xc, err := newChannel(channelOptions{
		transport:       tr,
		serverConfig:    serverCfg,
		bootstrapConfig: bootstrapCfg,
		resourceTypeGetter: func(typeURL string) xdsresource.Type {
			if typeURL != "type.googleapis.com/envoy.config.listener.v3.Listener" {
				return nil
			}
			return listenerType
		},
		eventHandler:       newTestEventHandler(),
		watchExpiryTimeout: watchExpiryTimeout,
	})
	if err != nil {
		t.Fatalf("Failed to create xdsChannel: %v", err)
	}
	t.Cleanup(func() { xc.close() })
	return xc
}

func newTestEventHandler() *testEventHandler {
	return &testEventHandler{
		typeCh:    make(chan xdsresource.Type, 1),
		updateCh:  make(chan map[string]ads.DataAndErrTuple, 1),
		mdCh:      make(chan xdsresource.UpdateMetadata, 1),
		nameCh:    make(chan string, 1),
		connErrCh: make(chan error, 1),
	}
}

// testEventHandler is a struct that implements the adsEventhandler interface.
// It is used to receive events from an xdsChannel, and has multiple channels on
// which it makes these events available to the test.
type testEventHandler struct {
	typeCh    chan xdsresource.Type               // Resource type of an update or resource-does-not-exist error.
	updateCh  chan map[string]ads.DataAndErrTuple // Resource updates.
	mdCh      chan xdsresource.UpdateMetadata     // Metadata from an update.
	nameCh    chan string                         // Name of the non-existent resource.
	connErrCh chan error                          // Connectivity error.

}

func (ta *testEventHandler) adsStreamFailure(err error) {
	ta.connErrCh <- err
}

func (ta *testEventHandler) adsResourceUpdate(typ xdsresource.Type, updates map[string]ads.DataAndErrTuple, md xdsresource.UpdateMetadata, onDone func()) {
	ta.typeCh <- typ
	ta.updateCh <- updates
	ta.mdCh <- md
	onDone()
}

func (ta *testEventHandler) waitForUpdate(ctx context.Context) (xdsresource.Type, map[string]ads.DataAndErrTuple, xdsresource.UpdateMetadata, error) {
	var typ xdsresource.Type
	var updates map[string]ads.DataAndErrTuple
	var md xdsresource.UpdateMetadata

	select {
	case typ = <-ta.typeCh:
	case <-ctx.Done():
		return nil, nil, xdsresource.UpdateMetadata{}, ctx.Err()
	}

	select {
	case updates = <-ta.updateCh:
	case <-ctx.Done():
		return nil, nil, xdsresource.UpdateMetadata{}, ctx.Err()
	}

	select {
	case md = <-ta.mdCh:
	case <-ctx.Done():
		return nil, nil, xdsresource.UpdateMetadata{}, ctx.Err()
	}
	return typ, updates, md, nil
}

func (ta *testEventHandler) adsResourceDoesNotExist(typ xdsresource.Type, name string) {
	ta.typeCh <- typ
	ta.nameCh <- name
}

func (ta *testEventHandler) waitForResourceDoesNotExist(ctx context.Context) (xdsresource.Type, string, error) {
	var typ xdsresource.Type
	var name string

	select {
	case typ = <-ta.typeCh:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}

	select {
	case name = <-ta.nameCh:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	return typ, name, nil
}

func newStringP(s string) *string {
	return &s
}

func makeRouterFilter(t *testing.T) xdsresource.HTTPFilter {
	routerBuilder := httpfilter.Get(router.TypeURL)
	routerConfig, _ := routerBuilder.ParseFilterConfig(testutils.MarshalAny(t, &v3routerpb.Router{}))
	return xdsresource.HTTPFilter{Name: "router", Filter: routerBuilder, Config: routerConfig}
}

func makeRouterFilterList(t *testing.T) []xdsresource.HTTPFilter {
	return []xdsresource.HTTPFilter{makeRouterFilter(t)}
}
