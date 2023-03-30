/*
 *
 * Copyright 2022 gRPC authors.
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

package xdsclient_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/bootstrap"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/fakeserver"
	"google.golang.org/grpc/xds/internal"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/types/known/anypb"

	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

var (
	// Resource type implementations retrieved from the resource type map in the
	// internal package, which is initialized when the individual resource types
	// are created.
	listenerResourceType    = internal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)
	routeConfigResourceType = internal.ResourceTypeMapForTesting[version.V3RouteConfigURL].(xdsresource.Type)
)

// TestWatchCallAnotherWatch tests the scenario where a watch is registered for
// a resource, and more watches are registered from the first watch's callback.
// The test verifies that this scenario does not lead to a deadlock.
func (s) TestWatchCallAnotherWatch(t *testing.T) {
	overrideFedEnvVar(t)

	// Start an xDS management server and set the option to allow it to respond
	// to requests which only specify a subset of the configured resources.
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, close, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Configure the management server to respond with route config resources.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Routes: []*v3routepb.RouteConfiguration{
			e2e.DefaultRouteConfig(rdsName, ldsName, cdsName),
			e2e.DefaultRouteConfig(rdsNameNewStyle, ldsNameNewStyle, cdsName),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Start a watch for one route configuration resource. From the watch
	// callback of the first resource, register two more watches (one for the
	// same resource name, which would be satisfied from the cache, and another
	// for a different resource name, which would be satisfied from the server).
	updateCh1 := testutils.NewChannel()
	updateCh2 := testutils.NewChannel()
	updateCh3 := testutils.NewChannel()
	rdsCancel1 := client.WatchRouteConfig(rdsName, func(u xdsresource.RouteConfigUpdate, err error) {
		updateCh1.Send(xdsresource.RouteConfigUpdateErrTuple{Update: u, Err: err})

		// Watch for the same resource name.
		rdsCancel2 := client.WatchRouteConfig(rdsName, func(u xdsresource.RouteConfigUpdate, err error) {
			updateCh2.Send(xdsresource.RouteConfigUpdateErrTuple{Update: u, Err: err})
		})
		t.Cleanup(rdsCancel2)
		// Watch for a different resource name.
		rdsCancel3 := client.WatchRouteConfig(rdsNameNewStyle, func(u xdsresource.RouteConfigUpdate, err error) {
			updateCh3.Send(xdsresource.RouteConfigUpdateErrTuple{Update: u, Err: err})
		})
		t.Cleanup(rdsCancel3)
	})
	t.Cleanup(rdsCancel1)

	// Verify the contents of the received update for the all watchers.
	wantUpdate12 := xdsresource.RouteConfigUpdateErrTuple{
		Update: xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{ldsName},
					Routes: []*xdsresource.Route{
						{
							Prefix:           newStringP("/"),
							ActionType:       xdsresource.RouteActionRoute,
							WeightedClusters: map[string]xdsresource.WeightedCluster{cdsName: {Weight: 100}},
						},
					},
				},
			},
		},
	}
	wantUpdate3 := xdsresource.RouteConfigUpdateErrTuple{
		Update: xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{ldsNameNewStyle},
					Routes: []*xdsresource.Route{
						{
							Prefix:           newStringP("/"),
							ActionType:       xdsresource.RouteActionRoute,
							WeightedClusters: map[string]xdsresource.WeightedCluster{cdsName: {Weight: 100}},
						},
					},
				},
			},
		},
	}
	if err := verifyRouteConfigUpdate(ctx, updateCh1, wantUpdate12); err != nil {
		t.Fatal(err)
	}
	if err := verifyRouteConfigUpdate(ctx, updateCh2, wantUpdate12); err != nil {
		t.Fatal(err)
	}
	if err := verifyRouteConfigUpdate(ctx, updateCh3, wantUpdate3); err != nil {
		t.Fatal(err)
	}
}

// TestNodeProtoSentOnlyInFirstRequest verifies that a non-empty node proto gets
// sent only on the first discovery request message on the ADS stream.
//
// It also verifies the same behavior holds after a stream restart.
func (s) TestNodeProtoSentOnlyInFirstRequest(t *testing.T) {
	overrideFedEnvVar(t)

	// Create a restartable listener which can close existing connections.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	// Start a fake xDS management server with the above restartable listener.
	//
	// We are unable to use the go-control-plane server here, because it caches
	// the node proto received in the first request message and adds it to
	// subsequent requests before invoking the OnStreamRequest() callback.
	// Therefore we cannot verify what is sent by the xDS client.
	mgmtServer, cleanup, err := fakeserver.StartServer(lis)
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer cleanup()

	// Create a bootstrap file in a temporary directory.
	nodeID := uuid.New().String()
	bootstrapContents, err := bootstrap.Contents(bootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}

	// Create an xDS client with the above bootstrap contents.
	client, close, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	const (
		serviceName     = "my-service-client-side-xds"
		routeConfigName = "route-" + serviceName
		clusterName     = "cluster-" + serviceName
	)

	// Register a watch for the Listener resource.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	watcher := xdstestutils.NewTestResourceWatcher()
	client.WatchResource(listenerResourceType, serviceName, watcher)

	// Ensure the watch results in a discovery request with an empty node proto.
	if err := readDiscoveryResponseAndCheckForNonEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}

	// Configure a listener resource on the fake xDS server.
	lisAny, err := anypb.New(e2e.DefaultClientListener(serviceName, routeConfigName))
	if err != nil {
		t.Fatalf("Failed to marshal listener resource into an Any proto: %v", err)
	}
	mgmtServer.XDSResponseChan <- &fakeserver.Response{
		Resp: &v3discoverypb.DiscoveryResponse{
			TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
			VersionInfo: "1",
			Resources:   []*anypb.Any{lisAny},
		},
	}

	// The xDS client is expected to ACK the Listener resource. The discovery
	// request corresponding to the ACK must contain a nil node proto.
	if err := readDiscoveryResponseAndCheckForEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}

	// Register a watch for a RouteConfiguration resource.
	client.WatchResource(routeConfigResourceType, routeConfigName, watcher)

	// Ensure the watch results in a discovery request with an empty node proto.
	if err := readDiscoveryResponseAndCheckForEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}

	// Configure the route configuration resource on the fake xDS server.
	rcAny, err := anypb.New(e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName))
	if err != nil {
		t.Fatalf("Failed to marshal route configuration resource into an Any proto: %v", err)
	}
	mgmtServer.XDSResponseChan <- &fakeserver.Response{
		Resp: &v3discoverypb.DiscoveryResponse{
			TypeUrl:     "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
			VersionInfo: "1",
			Resources:   []*anypb.Any{rcAny},
		},
	}

	// Ensure the discovery request for the ACK contains an empty node proto.
	if err := readDiscoveryResponseAndCheckForEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}

	// Stop the management server and expect the error callback to be invoked.
	lis.Stop()
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the connection error to be propagated to the watcher")
	case <-watcher.ErrorCh:
	}

	// Restart the management server.
	lis.Restart()

	// The xDS client is expected to re-request previously requested resources.
	// Hence, we expect two DiscoveryRequest messages (one for the Listener and
	// one for the RouteConfiguration resource). The first message should contain
	// a non-nil node proto and the second should contain a nil-proto.
	//
	// And since we don't push any responses on the response channel of the fake
	// server, we do not expect any ACKs here.
	if err := readDiscoveryResponseAndCheckForNonEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}
	if err := readDiscoveryResponseAndCheckForEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}
}

// readDiscoveryResponseAndCheckForEmptyNodeProto reads a discovery request
// message out of the provided reqCh. It returns an error if it fails to read a
// message before the context deadline expires, or if the read message contains
// a non-empty node proto.
func readDiscoveryResponseAndCheckForEmptyNodeProto(ctx context.Context, reqCh *testutils.Channel) error {
	v, err := reqCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("Timeout when waiting for a DiscoveryRequest message")
	}
	req := v.(*fakeserver.Request).Req.(*v3discoverypb.DiscoveryRequest)
	if node := req.GetNode(); node != nil {
		return fmt.Errorf("Node proto received in DiscoveryRequest message is %v, want empty node proto", node)
	}
	return nil
}

// readDiscoveryResponseAndCheckForNonEmptyNodeProto reads a discovery request
// message out of the provided reqCh. It returns an error if it fails to read a
// message before the context deadline expires, or if the read message contains
// an empty node proto.
func readDiscoveryResponseAndCheckForNonEmptyNodeProto(ctx context.Context, reqCh *testutils.Channel) error {
	v, err := reqCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("Timeout when waiting for a DiscoveryRequest message")
	}
	req := v.(*fakeserver.Request).Req.(*v3discoverypb.DiscoveryRequest)
	if node := req.GetNode(); node == nil {
		return fmt.Errorf("Empty node proto received in DiscoveryRequest message, want non-empty node proto")
	}
	return nil
}
