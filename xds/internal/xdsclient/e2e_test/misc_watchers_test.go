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

package e2e_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"

	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
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
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

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
	var rdsCancel2, rdsCancel3 func()
	rdsCancel1 := client.WatchRouteConfig(rdsName, func(u xdsresource.RouteConfigUpdate, err error) {
		updateCh1.Send(xdsresource.RouteConfigUpdateErrTuple{Update: u, Err: err})
		// Watch for the same resource name.
		rdsCancel2 = client.WatchRouteConfig(rdsName, func(u xdsresource.RouteConfigUpdate, err error) {
			updateCh2.Send(xdsresource.RouteConfigUpdateErrTuple{Update: u, Err: err})
		})
		t.Cleanup(rdsCancel2)
		// Watch for a different resource name.
		rdsCancel3 = client.WatchRouteConfig(rdsNameNewStyle, func(u xdsresource.RouteConfigUpdate, err error) {
			updateCh3.Send(xdsresource.RouteConfigUpdateErrTuple{Update: u, Err: err})
			rdsCancel3()
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
							WeightedClusters: map[string]xdsresource.WeightedCluster{cdsName: {Weight: 1}},
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
							WeightedClusters: map[string]xdsresource.WeightedCluster{cdsName: {Weight: 1}},
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
