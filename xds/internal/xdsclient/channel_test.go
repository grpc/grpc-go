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

	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
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
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Lookup the listener resource type from the resource type map. This is used to
// parse listener resources used in this test.
var listenerType = xdsinternal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)

// xdsChannelForTest creates an xdsChannel to the specified serverURI for
// testing purposes.
func xdsChannelForTest(t *testing.T, serverURI, nodeID string, watchExpiryTimeout time.Duration) *xdsChannel {
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
	bootstrapCfg, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("Failed to create bootstrap configuration: %v", err)
	}

	// Create an xdsChannel that uses everything set up above.
	xc, err := newXDSChannel(xdsChannelOpts{
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

// verifyUpdateAndMetadata verifies that the event handler received the expected
// updates and metadata.  It checks that the received resource type matches the
// expected type, and that the received updates and metadata match the expected
// values. The function ignores the timestamp fields in the metadata, as those
// are expected to be different.
func verifyUpdateAndMetadata(ctx context.Context, t *testing.T, eh *testEventHandler, wantUpdates map[string]ads.DataAndErrTuple, wantMD xdsresource.UpdateMetadata) {
	t.Helper()

	gotTyp, gotUpdates, gotMD, err := eh.waitForUpdate(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for update callback to be invoked on the event handler")
	}

	if gotTyp != listenerType {
		t.Fatalf("Got resource type %v, want %v", gotTyp, listenerType)
	}
	opts := cmp.Options{
		protocmp.Transform(),
		cmpopts.EquateEmpty(),
		cmpopts.EquateErrors(),
		cmpopts.IgnoreFields(xdsresource.UpdateMetadata{}, "Timestamp"),
		cmpopts.IgnoreFields(xdsresource.UpdateErrorMetadata{}, "Timestamp"),
	}
	if diff := cmp.Diff(wantUpdates, gotUpdates, opts); diff != "" {
		t.Fatalf("Got unexpected diff in update (-want +got):\n%s\n want: %+v\n got: %+v", diff, wantUpdates, gotUpdates)
	}
	if diff := cmp.Diff(wantMD, gotMD, opts); diff != "" {
		t.Fatalf("Got unexpected diff in update (-want +got):\n%s\n want: %v\n got: %v", diff, wantMD, gotMD)
	}
}

// Tests different failure cases when creating a new xdsChannel. It checks that
// the xdsChannel creation fails when any of the required options (transport,
// serverConfig, bootstrapConfig, or resourceTypeGetter) are missing or nil.
func (s) TestChannel_New_FailureCases(t *testing.T) {
	type fakeTransport struct {
		transport.Transport
	}

	tests := []struct {
		name       string
		opts       xdsChannelOpts
		wantErrStr string
	}{
		{
			name:       "emptyTransport",
			opts:       xdsChannelOpts{},
			wantErrStr: "transport is nil",
		},
		{
			name:       "emptyServerConfig",
			opts:       xdsChannelOpts{transport: &fakeTransport{}},
			wantErrStr: "serverConfig is nil",
		},
		{
			name: "emptyBootstrapConfig",
			opts: xdsChannelOpts{
				transport:    &fakeTransport{},
				serverConfig: &bootstrap.ServerConfig{},
			},
			wantErrStr: "bootstrapConfig is nil",
		},
		{
			name: "emptyResourceTypeGetter",
			opts: xdsChannelOpts{
				transport:       &fakeTransport{},
				serverConfig:    &bootstrap.ServerConfig{},
				bootstrapConfig: &bootstrap.Config{},
			},
			wantErrStr: "resourceTypeGetter is nil",
		},
		{
			name: "emptyEventHandler",
			opts: xdsChannelOpts{
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
			if _, err := newXDSChannel(test.opts); err == nil || !strings.Contains(err.Error(), test.wantErrStr) {
				t.Fatalf("newXDSChannel() = %v, want %q", err, test.wantErrStr)
			}
		})
	}
}

// Tests different scenarios of the xdsChannel receiving a response from the
// management server. In all scenarios, the xdsChannel is expected to pass the
// received responses as-is to the resource parsing functionality specified by
// the resourceTypeGetter.
func (s) TestChannel_ADS_HandleResponseFromManagementServer(t *testing.T) {
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
							Domains: []string{"*"},
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
		wantMD                   xdsresource.UpdateMetadata
		wantErr                  error
	}{
		{
			desc:                   "one bad resource - deserialization failure",
			resourceNamesToRequest: []string{listenerName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources:   []*anypb.Any{badlyMarshaledResource},
			},
			wantUpdates: nil, // No updates expected as the response runs into unmarshaling errors.
			wantMD: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusNACKed,
				Version: "0",
				ErrState: &xdsresource.UpdateErrorMetadata{
					Version: "0",
					Err:     cmpopts.AnyError,
				},
			},
			wantErr: cmpopts.AnyError,
		},
		{
			desc:                   "one bad resource - validation failure",
			resourceNamesToRequest: []string{listenerName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: []*anypb.Any{testutils.MarshalAny(t, &v3listenerpb.Listener{
					Name: listenerName1,
					ApiListener: &v3listenerpb.ApiListener{
						ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
							RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
						}),
					},
				})},
			},
			wantUpdates: map[string]ads.DataAndErrTuple{
				listenerName1: {
					Err: cmpopts.AnyError,
				},
			},
			wantMD: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusNACKed,
				Version: "0",
				ErrState: &xdsresource.UpdateErrorMetadata{
					Version: "0",
					Err:     cmpopts.AnyError,
				},
			},
		},
		{
			desc:                   "two bad resources",
			resourceNamesToRequest: []string{listenerName1, listenerName2},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: []*anypb.Any{
					badlyMarshaledResource,
					testutils.MarshalAny(t, &v3listenerpb.Listener{
						Name: listenerName2,
						ApiListener: &v3listenerpb.ApiListener{
							ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
							}),
						},
					}),
				},
			},
			wantUpdates: map[string]ads.DataAndErrTuple{
				listenerName2: {
					Err: cmpopts.AnyError,
				},
			},
			wantMD: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusNACKed,
				Version: "0",
				ErrState: &xdsresource.UpdateErrorMetadata{
					Version: "0",
					Err:     cmpopts.AnyError,
				},
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
								Domains: []string{"*"},
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
			wantMD: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusACKed,
				Version: "0",
			},
		},
		{
			desc:                   "one good and one bad - deserialization failure",
			resourceNamesToRequest: []string{listenerName1, listenerName2},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: []*anypb.Any{
					badlyMarshaledResource,
					listener2,
				},
			},
			wantUpdates: map[string]ads.DataAndErrTuple{
				listenerName2: {
					Resource: &xdsresource.ListenerResourceData{Resource: xdsresource.ListenerUpdate{
						InlineRouteConfig: &xdsresource.RouteConfigUpdate{
							VirtualHosts: []*xdsresource.VirtualHost{{
								Domains: []string{"*"},
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
			wantMD: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusNACKed,
				Version: "0",
				ErrState: &xdsresource.UpdateErrorMetadata{
					Version: "0",
					Err:     cmpopts.AnyError,
				},
			},
		},
		{
			desc:                   "one good and one bad - validation failure",
			resourceNamesToRequest: []string{listenerName1, listenerName2},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				VersionInfo: "0",
				TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: []*anypb.Any{
					testutils.MarshalAny(t, &v3listenerpb.Listener{
						Name: listenerName1,
						ApiListener: &v3listenerpb.ApiListener{
							ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
							}),
						},
					}),
					listener2,
				},
			},
			wantUpdates: map[string]ads.DataAndErrTuple{
				listenerName1: {Err: cmpopts.AnyError},
				listenerName2: {
					Resource: &xdsresource.ListenerResourceData{Resource: xdsresource.ListenerUpdate{
						InlineRouteConfig: &xdsresource.RouteConfigUpdate{
							VirtualHosts: []*xdsresource.VirtualHost{{
								Domains: []string{"*"},
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
			wantMD: xdsresource.UpdateMetadata{
				Status:  xdsresource.ServiceStatusNACKed,
				Version: "0",
				ErrState: &xdsresource.UpdateErrorMetadata{
					Version: "0",
					Err:     cmpopts.AnyError,
				},
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
								Domains: []string{"*"},
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
								Domains: []string{"*"},
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
			wantMD: xdsresource.UpdateMetadata{
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
								Domains: []string{"*"},
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
								Domains: []string{"*"},
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
			wantMD: xdsresource.UpdateMetadata{
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
			mgmtServer, cleanup, err := fakeserver.StartServer(nil)
			if err != nil {
				t.Fatalf("Failed to start fake xDS server: %v", err)
			}
			defer cleanup()
			t.Logf("Started xDS management server on %s", mgmtServer.Address)
			mgmtServer.XDSResponseChan <- &fakeserver.Response{Resp: test.managementServerResponse}

			// Create an xdsChannel for the test with a long watch expiry timer
			// to ensure that watches don't expire for the duration of the test.
			nodeID := uuid.New().String()
			xc := xdsChannelForTest(t, mgmtServer.Address, nodeID, 2*defaultTestTimeout)
			defer xc.close()

			// Subscribe to the resources specified in the test table.
			for _, name := range test.resourceNamesToRequest {
				xc.subscribe(listenerType, name)
			}

			// Wait for an update callback on the event handler and verify the
			// contents of the update and the metadata.
			verifyUpdateAndMetadata(ctx, t, xc.eventHandler.(*testEventHandler), test.wantUpdates, test.wantMD)
		})
	}
}

// Tests that the xdsChannel correctly handles the expiry of a watch for a
// resource by ensuring that the watch expiry callback is invoked on the event
// handler with the expected resource type and name.
func (s) TestChannel_ADS_HandleResponseWatchExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server, but do not configure any resources on it.
	// This will result in the watch for a resource to timeout.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create an xdsChannel for the test with a short watch expiry timer to
	// ensure that the test does not run very long, as it needs to wait for the
	// watch to expire.
	nodeID := uuid.New().String()
	xc := xdsChannelForTest(t, mgmtServer.Address, nodeID, 2*defaultTestShortTimeout)
	defer xc.close()

	// Subscribe to a listener resource.
	const listenerName = "listener-name"
	xc.subscribe(listenerType, listenerName)

	// Wait for the watch expiry callback on the authority to be invoked and
	// verify that the watch expired for the expected resource name and type.
	eventHandler := xc.eventHandler.(*testEventHandler)
	gotTyp, gotName, err := eventHandler.waitForResourceDoesNotExist(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for the watch expiry callback to be invoked on the xDS client")
	}

	if gotTyp != listenerType {
		t.Fatalf("Got type %v, want %v", gotTyp, listenerType)
	}
	if gotName != listenerName {
		t.Fatalf("Got name %v, want %v", gotName, listenerName)
	}
}

// Tests that the xdsChannel correctly handles stream failures by ensuring that
// the stream failure callback is invoked on the event handler.
func (s) TestChannel_ADS_StreamFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server with a restartable listener to simulate
	// connection failures.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: lis})

	// Configure a listener resource on the management server.
	const listenerResourceName = "test-listener-resource"
	const routeConfigurationName = "test-route-configuration-resource"
	nodeID := uuid.New().String()
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerResourceName, routeConfigurationName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Create an xdsChannel for the test with a long watch expiry timer
	// to ensure that watches don't expire for the duration of the test.
	xc := xdsChannelForTest(t, mgmtServer.Address, nodeID, 2*defaultTestTimeout)
	defer xc.close()

	// Subscribe to the resource created above.
	xc.subscribe(listenerType, listenerResourceName)

	// Wait for an update callback on the event handler and verify the
	// contents of the update and the metadata.
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{Rds: &v3httppb.Rds{
			ConfigSource: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
			},
			RouteConfigName: routeConfigurationName,
		}},
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
	})
	listenerResource, err := anypb.New(&v3listenerpb.Listener{
		Name:        listenerResourceName,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("Failed to create listener resource: %v", err)
	}

	wantUpdates := map[string]ads.DataAndErrTuple{
		listenerResourceName: {
			Resource: &xdsresource.ListenerResourceData{
				Resource: xdsresource.ListenerUpdate{
					RouteConfigName: routeConfigurationName,
					HTTPFilters:     makeRouterFilterList(t),
					Raw:             listenerResource,
				},
			},
		},
	}
	wantMD := xdsresource.UpdateMetadata{
		Status:  xdsresource.ServiceStatusACKed,
		Version: "1",
	}

	eventHandler := xc.eventHandler.(*testEventHandler)
	verifyUpdateAndMetadata(ctx, t, eventHandler, wantUpdates, wantMD)

	lis.Stop()
	if err := eventHandler.waitForStreamFailure(ctx); err != nil {
		t.Fatalf("Timeout when waiting for the stream failure callback to be invoked on the xDS client: %v", err)
	}
}

// Tests the behavior of the xdsChannel when a resource is unsubscribed.
// Verifies that when a previously subscribed resource is unsubscribed, a
// request is sent without the previously subscribed resource name.
func (s) TestChannel_ADS_ResourceUnsubscribe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server that uses a channel to inform the test
	// about the specific LDS resource names being requested.
	ldsResourcesCh := make(chan []string, 1)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			t.Logf("Received request for resources: %v of type %s", req.GetResourceNames(), req.GetTypeUrl())

			if req.TypeUrl != version.V3ListenerURL {
				return fmt.Errorf("unexpected resource type URL: %q", req.TypeUrl)
			}

			// Make the most recently requested names available to the test.
			ldsResourcesCh <- req.GetResourceNames()
			return nil
		},
	})

	// Configure two listener resources on the management server.
	const listenerResourceName1 = "test-listener-resource-1"
	const routeConfigurationName1 = "test-route-configuration-resource-1"
	const listenerResourceName2 = "test-listener-resource-2"
	const routeConfigurationName2 = "test-route-configuration-resource-2"
	nodeID := uuid.New().String()
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(listenerResourceName1, routeConfigurationName1),
			e2e.DefaultClientListener(listenerResourceName2, routeConfigurationName2),
		},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Create an xdsChannel for the test with a long watch expiry timer
	// to ensure that watches don't expire for the duration of the test.
	xc := xdsChannelForTest(t, mgmtServer.Address, nodeID, 2*defaultTestTimeout)
	defer xc.close()

	// Subscribe to the resources created above and verify that a request is
	// sent for the same.
	xc.subscribe(listenerType, listenerResourceName1)
	xc.subscribe(listenerType, listenerResourceName2)
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerResourceName1, listenerResourceName2}); err != nil {
		t.Fatal(err)
	}

	// Wait for the above resources to be ACKed.
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerResourceName1, listenerResourceName2}); err != nil {
		t.Fatal(err)
	}

	// Unsubscribe to one of the resources created above, and ensure that the
	// other resource is still being requested.
	xc.unsubscribe(listenerType, listenerResourceName1)
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerResourceName2}); err != nil {
		t.Fatal(err)
	}

	// Since the version on the management server for the above resource is not
	// changed, we will not receive an update from it for the one resource that
	// we are still requesting.

	// Unsubscribe to the remaining resource, and ensure that no more resources
	// are being requested.
	xc.unsubscribe(listenerType, listenerResourceName2)
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{}); err != nil {
		t.Fatal(err)
	}
}

// Tests the load reporting functionality of the xdsChannel.  It creates an
// xdsChannel, starts load reporting, and verifies that an LRS streaming RPC is
// created. It then makes another call to the load reporting API and ensures
// that a new LRS stream is not created. Finally, it cancels the load reporting
// calls and ensures that the stream is closed when the last call is canceled.
//
// Note that this test does not actually report any load. That is already tested
// by an e2e style test in the xdsclient package.
func (s) TestChannel_LRS_ReportLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create a management server that serves LRS.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create an xdsChannel for the test. Node id and watch expiry timer don't
	// matter for LRS.
	xc := xdsChannelForTest(t, mgmtServer.Address, "", defaultTestTimeout)
	defer xc.close()

	// Start load reporting and verify that an LRS streaming RPC is created.
	_, stopLRS1 := xc.reportLoad()
	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for an LRS streaming RPC to be created: %v", err)
	}

	// Make another call to the load reporting API, and ensure that a new LRS
	// stream is not created.
	_, stopLRS2 := xc.reportLoad()
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := lrsServer.LRSStreamOpenChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("New LRS streaming RPC created when expected to use an existing one")
	}

	// Cancel the first load reporting call, and ensure that the stream does not
	// close (because we have another call open).
	stopLRS1()
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := lrsServer.LRSStreamCloseChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("LRS stream closed when expected to stay open")
	}

	// Cancel the second load reporting call, and ensure the stream is closed.
	stopLRS2()
	if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatal("Timeout waiting for LRS stream to close")
	}
}

// waitForResourceNames waits for the wantNames to be received on namesCh.
// Returns a non-nil error if the context expires before that.
func waitForResourceNames(ctx context.Context, t *testing.T, namesCh chan []string, wantNames []string) error {
	t.Helper()

	var lastRequestedNames []string
	for ; ; <-time.After(defaultTestShortTimeout) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for resources %v to be requested from the management server. Last requested resources: %v", wantNames, lastRequestedNames)
		case gotNames := <-namesCh:
			if cmp.Equal(gotNames, wantNames, cmpopts.EquateEmpty(), cmpopts.SortSlices(func(s1, s2 string) bool { return s1 < s2 })) {
				return nil
			}
			lastRequestedNames = gotNames
		}
	}
}

// newTestEventHandler creates a new testEventHandler instance with the
// necessary channels for testing the xdsChannel.
func newTestEventHandler() *testEventHandler {
	return &testEventHandler{
		typeCh:    make(chan xdsresource.Type, 1),
		updateCh:  make(chan map[string]ads.DataAndErrTuple, 1),
		mdCh:      make(chan xdsresource.UpdateMetadata, 1),
		nameCh:    make(chan string, 1),
		connErrCh: make(chan error, 1),
	}
}

// testEventHandler is a struct that implements the xdsChannelEventhandler
// interface.  It is used to receive events from an xdsChannel, and has multiple
// channels on which it makes these events available to the test.
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

func (ta *testEventHandler) waitForStreamFailure(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ta.connErrCh:
	}
	return nil
}

func (ta *testEventHandler) adsResourceUpdate(typ xdsresource.Type, updates map[string]ads.DataAndErrTuple, md xdsresource.UpdateMetadata, onDone func()) {
	ta.typeCh <- typ
	ta.updateCh <- updates
	ta.mdCh <- md
	onDone()
}

// waitForUpdate waits for the next resource update event from the xdsChannel.
// It returns the resource type, the resource updates, and the update metadata.
// If the context is canceled, it returns an error.
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

// waitForResourceDoesNotExist waits for the next resource-does-not-exist event
// from the xdsChannel. It returns the resource type and the resource name. If
// the context is canceled, it returns an error.
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
