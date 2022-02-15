/*
 *
 * Copyright 2021 gRPC authors.
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
	"testing"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

func overrideFedEnvVar(t *testing.T) {
	oldFed := envconfig.XDSFederation
	envconfig.XDSFederation = true
	t.Cleanup(func() { envconfig.XDSFederation = oldFed })
}

func testFedTwoWatchDifferentContextParameterOrder(t *testing.T, typ xdsresource.ResourceType, update interface{}) {
	overrideFedEnvVar(t)
	var (
		// Two resource names only differ in context parameter __order__.
		resourceName1 = testutils.BuildResourceName(typ, testAuthority, "test-resource-name", nil) + "?a=1&b=2"
		resourceName2 = testutils.BuildResourceName(typ, testAuthority, "test-resource-name", nil) + "?b=2&a=1"
	)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, ctrlCh := testClientSetup(t, false)
	updateCh, _ := newWatch(t, client, typ, resourceName1)
	_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, typ, resourceName1)
	newWatchF, newUpdateF, verifyUpdateF := typeToTestFuncs(typ)

	// Start a watch on the second resource name.
	updateCh2, _ := newWatchF(client, resourceName2)

	// Send an update on the first resoruce, both watchers should be updated.
	newUpdateF(updateHandler, map[string]interface{}{resourceName1: update})
	verifyUpdateF(ctx, t, updateCh, update, nil)
	verifyUpdateF(ctx, t, updateCh2, update, nil)
}

// TestLDSFedTwoWatchDifferentContextParameterOrder covers the case with new style resource name
// - Two watches with the same query string, but in different order. The two
//   watches should watch the same resource.
// - The response has the same query string, but in different order. The watch
//   should still be notified.
func (s) TestLDSFedTwoWatchDifferentContextParameterOrder(t *testing.T) {
	testFedTwoWatchDifferentContextParameterOrder(t, xdsresource.ListenerResource, xdsresource.ListenerUpdate{RouteConfigName: testRDSName})
}

// TestRDSFedTwoWatchDifferentContextParameterOrder covers the case with new style resource name
// - Two watches with the same query string, but in different order. The two
//   watches should watch the same resource.
// - The response has the same query string, but in different order. The watch
//   should still be notified.
func (s) TestRDSFedTwoWatchDifferentContextParameterOrder(t *testing.T) {
	testFedTwoWatchDifferentContextParameterOrder(t, xdsresource.RouteConfigResource, xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsresource.WeightedCluster{testCDSName: {Weight: 1}}}},
			},
		},
	})
}

// TestClusterFedTwoWatchDifferentContextParameterOrder covers the case with new style resource name
// - Two watches with the same query string, but in different order. The two
//   watches should watch the same resource.
// - The response has the same query string, but in different order. The watch
//   should still be notified.
func (s) TestClusterFedTwoWatchDifferentContextParameterOrder(t *testing.T) {
	testFedTwoWatchDifferentContextParameterOrder(t, xdsresource.ClusterResource, xdsresource.ClusterUpdate{ClusterName: testEDSName})
}

// TestEndpointsFedTwoWatchDifferentContextParameterOrder covers the case with new style resource name
// - Two watches with the same query string, but in different order. The two
//   watches should watch the same resource.
// - The response has the same query string, but in different order. The watch
//   should still be notified.
func (s) TestEndpointsFedTwoWatchDifferentContextParameterOrder(t *testing.T) {
	testFedTwoWatchDifferentContextParameterOrder(t, xdsresource.EndpointsResource, xdsresource.EndpointsUpdate{Localities: []xdsresource.Locality{testLocalities[0]}})
}
