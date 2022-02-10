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

package xdsclient

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// TestRDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name (which doesn't trigger callback)
// - an update is received after cancel()
func (s) TestRDSWatch(t *testing.T) {
	testWatch(t, xdsresource.RouteConfigResource, xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsresource.WeightedCluster{testCDSName: {Weight: 1}}}},
			},
		},
	}, testRDSName)
}

// TestRDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestRDSTwoWatchSameResourceName(t *testing.T) {
	testTwoWatchSameResourceName(t, xdsresource.RouteConfigResource, xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsresource.WeightedCluster{testCDSName: {Weight: 1}}}},
			},
		},
	}, testRDSName)
}

// TestRDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestRDSThreeWatchDifferentResourceName(t *testing.T) {
	testThreeWatchDifferentResourceName(t, xdsresource.RouteConfigResource,
		xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{testLDSName},
					Routes:  []*xdsresource.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsresource.WeightedCluster{testCDSName + "1": {Weight: 1}}}},
				},
			},
		}, testRDSName+"1",
		xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{testLDSName},
					Routes:  []*xdsresource.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsresource.WeightedCluster{testCDSName + "2": {Weight: 1}}}},
				},
			},
		}, testRDSName+"2",
	)
}

// TestRDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestRDSWatchAfterCache(t *testing.T) {
	testWatchAfterCache(t, xdsresource.RouteConfigResource, xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsresource.WeightedCluster{testCDSName: {Weight: 1}}}},
			},
		},
	}, testRDSName)
}

// TestRouteWatchNACKError covers the case that an update is NACK'ed, and the
// watcher should also receive the error.
func (s) TestRouteWatchNACKError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, ctrlCh := testClientSetup(t, false)
	rdsUpdateCh, _ := newWatch(t, client, xdsresource.RouteConfigResource, testRDSName)
	_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, xdsresource.RouteConfigResource, testRDSName)

	wantError := fmt.Errorf("testing error")
	updateHandler.NewRouteConfigs(map[string]xdsresource.RouteConfigUpdateErrTuple{testRDSName: {Err: wantError}}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh, xdsresource.RouteConfigUpdate{}, wantError); err != nil {
		t.Fatal(err)
	}
}

// TestRouteWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalida resources should receive an error.
func (s) TestRouteWatchPartialValid(t *testing.T) {
	testWatchPartialValid(t, xdsresource.RouteConfigResource, xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*xdsresource.Route{{Prefix: newStringP(""), WeightedClusters: map[string]xdsresource.WeightedCluster{testCDSName: {Weight: 1}}}},
			},
		},
	}, testRDSName)
}
