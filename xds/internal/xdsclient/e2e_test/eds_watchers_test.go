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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

const (
	edsHost1 = "1.foo.bar.com"
	edsHost2 = "2.foo.bar.com"
	edsHost3 = "3.foo.bar.com"
	edsPort1 = 1
	edsPort2 = 2
	edsPort3 = 3
)

// badEndpointsResource returns a endpoints resource for the given
// edsServiceName which contains an endpoint with a load_balancing weight of
// `0`. This is expected to be NACK'ed by the xDS client.
func badEndpointsResource(edsServiceName string, host string, ports []uint32) *v3endpointpb.ClusterLoadAssignment {
	e := e2e.DefaultEndpoint(edsServiceName, host, ports)
	e.Endpoints[0].LbEndpoints[0].LoadBalancingWeight = &wrapperspb.UInt32Value{Value: 0}
	return e
}

// xdsClient is expected to produce an error containing this string when an
// update is received containing an endpoints resource created using
// `badEndpointsResource`.
const wantEndpointsNACKErr = "EDS response contains an endpoint with zero weight"

// verifyEndpointsUpdate waits for an update to be received on the provided
// update channel and verifies that it matches the expected update.
//
// Returns an error if no update is received before the context deadline expires
// or the received update does not match the expected one.
func verifyEndpointsUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate xdsresource.EndpointsUpdateErrTuple) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for a endpoints resource from the management server: %v", err)
	}
	got := u.(xdsresource.EndpointsUpdateErrTuple)
	if wantUpdate.Err != nil {
		if gotType, wantType := xdsresource.ErrType(got.Err), xdsresource.ErrType(wantUpdate.Err); gotType != wantType {
			return fmt.Errorf("received update with error type %v, want %v", gotType, wantType)
		}
	}
	cmpOpts := []cmp.Option{cmpopts.EquateEmpty(), cmpopts.IgnoreFields(xdsresource.EndpointsUpdate{}, "Raw")}
	if diff := cmp.Diff(wantUpdate.Update, got.Update, cmpOpts...); diff != "" {
		return fmt.Errorf("received unepected diff in the endpoints resource update: (-want, got):\n%s", diff)
	}
	return nil
}

// verifyNoEndpointsUpdate verifies that no endpoints update is received on the
// provided update channel, and returns an error if an update is received.
//
// A very short deadline is used while waiting for the update, as this function
// is intended to be used when an update is not expected.
func verifyNoEndpointsUpdate(ctx context.Context, updateCh *testutils.Channel) error {
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := updateCh.Receive(sCtx); err != context.DeadlineExceeded {
		return fmt.Errorf("unexpected EndpointsUpdate: %v", u)
	}
	return nil
}

// TestEDSWatch covers the case where a single endpoint exists for a single
// endpoints resource. The test verifies the following scenarios:
//  1. An update from the management server containing the resource being
//     watched should result in the invocation of the watch callback.
//  2. An update from the management server containing a resource *not* being
//     watched should not result in the invocation of the watch callback.
//  3. After the watch is cancelled, an update from the management server
//     containing the resource that was being watched should not result in the
//     invocation of the watch callback.
//
// The test is run for old and new style names.
func (s) TestEDSWatch(t *testing.T) {
	tests := []struct {
		desc                   string
		resourceName           string
		watchedResource        *v3endpointpb.ClusterLoadAssignment // The resource being watched.
		updatedWatchedResource *v3endpointpb.ClusterLoadAssignment // The watched resource after an update.
		notWatchedResource     *v3endpointpb.ClusterLoadAssignment // A resource which is not being watched.
		wantUpdate             xdsresource.EndpointsUpdateErrTuple
	}{
		{
			desc:                   "old style resource",
			resourceName:           edsName,
			watchedResource:        e2e.DefaultEndpoint(edsName, edsHost1, []uint32{edsPort1}),
			updatedWatchedResource: e2e.DefaultEndpoint(edsName, edsHost2, []uint32{edsPort2}),
			notWatchedResource:     e2e.DefaultEndpoint("unsubscribed-eds-resource", edsHost3, []uint32{edsPort3}),
			wantUpdate: xdsresource.EndpointsUpdateErrTuple{
				Update: xdsresource.EndpointsUpdate{
					Localities: []xdsresource.Locality{
						{
							Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost1, edsPort1), Weight: 1}},
							ID:        internal.LocalityID{SubZone: "subzone"},
							Priority:  0,
							Weight:    1,
						},
					},
				},
			},
		},
		{
			desc:                   "new style resource",
			resourceName:           edsNameNewStyle,
			watchedResource:        e2e.DefaultEndpoint(edsNameNewStyle, edsHost1, []uint32{edsPort1}),
			updatedWatchedResource: e2e.DefaultEndpoint(edsNameNewStyle, edsHost2, []uint32{edsPort2}),
			notWatchedResource:     e2e.DefaultEndpoint("unsubscribed-eds-resource", edsHost3, []uint32{edsPort3}),
			wantUpdate: xdsresource.EndpointsUpdateErrTuple{
				Update: xdsresource.EndpointsUpdate{
					Localities: []xdsresource.Locality{
						{
							Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost1, edsPort1), Weight: 1}},
							ID:        internal.LocalityID{SubZone: "subzone"},
							Priority:  0,
							Weight:    1,
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			overrideFedEnvVar(t)
			mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
			defer cleanup()

			// Create an xDS client with the above bootstrap contents.
			client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
			if err != nil {
				t.Fatalf("Failed to create xDS client: %v", err)
			}
			defer client.Close()

			// Register a watch for a endpoint resource and have the watch
			// callback push the received update on to a channel.
			updateCh := testutils.NewChannel()
			edsCancel := client.WatchEndpoints(test.resourceName, func(u xdsresource.EndpointsUpdate, err error) {
				updateCh.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
			})

			// Configure the management server to return a single endpoint
			// resource, corresponding to the one being watched.
			resources := e2e.UpdateOptions{
				NodeID:         nodeID,
				Endpoints:      []*v3endpointpb.ClusterLoadAssignment{test.watchedResource},
				SkipValidation: true,
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}

			// Verify the contents of the received update.
			if err := verifyEndpointsUpdate(ctx, updateCh, test.wantUpdate); err != nil {
				t.Fatal(err)
			}

			// Configure the management server to return an additional endpoint
			// resource, one that we are not interested in.
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Endpoints:      []*v3endpointpb.ClusterLoadAssignment{test.watchedResource, test.notWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoEndpointsUpdate(ctx, updateCh); err != nil {
				t.Fatal(err)
			}

			// Cancel the watch and update the resource corresponding to the original
			// watch.  Ensure that the cancelled watch callback is not invoked.
			edsCancel()
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Endpoints:      []*v3endpointpb.ClusterLoadAssignment{test.updatedWatchedResource, test.notWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoEndpointsUpdate(ctx, updateCh); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestEDSWatch_TwoWatchesForSameResourceName covers the case where two watchers
// exist for a single endpoint resource.  The test verifies the following
// scenarios:
//  1. An update from the management server containing the resource being
//     watched should result in the invocation of both watch callbacks.
//  2. After one of the watches is cancelled, a redundant update from the
//     management server should not result in the invocation of either of the
//     watch callbacks.
//  3. An update from the management server containing the resource being
//     watched should result in the invocation of the un-cancelled watch
//     callback.
//
// The test is run for old and new style names.
func (s) TestEDSWatch_TwoWatchesForSameResourceName(t *testing.T) {
	tests := []struct {
		desc                   string
		resourceName           string
		watchedResource        *v3endpointpb.ClusterLoadAssignment // The resource being watched.
		updatedWatchedResource *v3endpointpb.ClusterLoadAssignment // The watched resource after an update.
		wantUpdateV1           xdsresource.EndpointsUpdateErrTuple
		wantUpdateV2           xdsresource.EndpointsUpdateErrTuple
	}{
		{
			desc:                   "old style resource",
			resourceName:           edsName,
			watchedResource:        e2e.DefaultEndpoint(edsName, edsHost1, []uint32{edsPort1}),
			updatedWatchedResource: e2e.DefaultEndpoint(edsName, edsHost2, []uint32{edsPort2}),
			wantUpdateV1: xdsresource.EndpointsUpdateErrTuple{
				Update: xdsresource.EndpointsUpdate{
					Localities: []xdsresource.Locality{
						{
							Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost1, edsPort1), Weight: 1}},
							ID:        internal.LocalityID{SubZone: "subzone"},
							Priority:  0,
							Weight:    1,
						},
					},
				},
			},
			wantUpdateV2: xdsresource.EndpointsUpdateErrTuple{
				Update: xdsresource.EndpointsUpdate{
					Localities: []xdsresource.Locality{
						{
							Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost2, edsPort2), Weight: 1}},
							ID:        internal.LocalityID{SubZone: "subzone"},
							Priority:  0,
							Weight:    1,
						},
					},
				},
			},
		},
		{
			desc:                   "new style resource",
			resourceName:           edsNameNewStyle,
			watchedResource:        e2e.DefaultEndpoint(edsNameNewStyle, edsHost1, []uint32{edsPort1}),
			updatedWatchedResource: e2e.DefaultEndpoint(edsNameNewStyle, edsHost2, []uint32{edsPort2}),
			wantUpdateV1: xdsresource.EndpointsUpdateErrTuple{
				Update: xdsresource.EndpointsUpdate{
					Localities: []xdsresource.Locality{
						{
							Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost1, edsPort1), Weight: 1}},
							ID:        internal.LocalityID{SubZone: "subzone"},
							Priority:  0,
							Weight:    1,
						},
					},
				},
			},
			wantUpdateV2: xdsresource.EndpointsUpdateErrTuple{
				Update: xdsresource.EndpointsUpdate{
					Localities: []xdsresource.Locality{
						{
							Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost2, edsPort2), Weight: 1}},
							ID:        internal.LocalityID{SubZone: "subzone"},
							Priority:  0,
							Weight:    1,
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			overrideFedEnvVar(t)
			mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
			defer cleanup()

			// Create an xDS client with the above bootstrap contents.
			client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
			if err != nil {
				t.Fatalf("Failed to create xDS client: %v", err)
			}
			defer client.Close()

			// Register two watches for the same endpoint resource and have the
			// callbacks push the received updates on to a channel.
			updateCh1 := testutils.NewChannel()
			edsCancel1 := client.WatchEndpoints(test.resourceName, func(u xdsresource.EndpointsUpdate, err error) {
				updateCh1.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
			})
			defer edsCancel1()
			updateCh2 := testutils.NewChannel()
			edsCancel2 := client.WatchEndpoints(test.resourceName, func(u xdsresource.EndpointsUpdate, err error) {
				updateCh2.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
			})

			// Configure the management server to return a single endpoint
			// resource, corresponding to the one being watched.
			resources := e2e.UpdateOptions{
				NodeID:         nodeID,
				Endpoints:      []*v3endpointpb.ClusterLoadAssignment{test.watchedResource},
				SkipValidation: true,
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}

			// Verify the contents of the received update.
			if err := verifyEndpointsUpdate(ctx, updateCh1, test.wantUpdateV1); err != nil {
				t.Fatal(err)
			}
			if err := verifyEndpointsUpdate(ctx, updateCh2, test.wantUpdateV1); err != nil {
				t.Fatal(err)
			}

			// Cancel the second watch and force the management server to push a
			// redundant update for the resource being watched. Neither of the
			// two watch callbacks should be invoked.
			edsCancel2()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoEndpointsUpdate(ctx, updateCh1); err != nil {
				t.Fatal(err)
			}
			if err := verifyNoEndpointsUpdate(ctx, updateCh2); err != nil {
				t.Fatal(err)
			}

			// Update to the resource being watched. The un-cancelled callback
			// should be invoked while the cancelled one should not be.
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Endpoints:      []*v3endpointpb.ClusterLoadAssignment{test.updatedWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyEndpointsUpdate(ctx, updateCh1, test.wantUpdateV2); err != nil {
				t.Fatal(err)
			}
			if err := verifyNoEndpointsUpdate(ctx, updateCh2); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestEDSWatch_ThreeWatchesForDifferentResourceNames covers the case with three
// watchers (two watchers for one resource, and the third watcher for another
// resource), exist across two endpoint configuration resources.  The test verifies
// that an update from the management server containing both resources results
// in the invocation of all watch callbacks.
//
// The test is run with both old and new style names.
func (s) TestEDSWatch_ThreeWatchesForDifferentResourceNames(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for the same endpoint resource and have the
	// callbacks push the received updates on to a channel.
	updateCh1 := testutils.NewChannel()
	edsCancel1 := client.WatchEndpoints(edsName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh1.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel1()
	updateCh2 := testutils.NewChannel()
	edsCancel2 := client.WatchEndpoints(edsName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh2.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel2()

	// Register the third watch for a different endpoint resource.
	updateCh3 := testutils.NewChannel()
	edsCancel3 := client.WatchEndpoints(edsNameNewStyle, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh3.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel3()

	// Configure the management server to return two endpoint resources,
	// corresponding to the registered watches.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			e2e.DefaultEndpoint(edsName, edsHost1, []uint32{edsPort1}),
			e2e.DefaultEndpoint(edsNameNewStyle, edsHost1, []uint32{edsPort1}),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update for the all watchers. The two
	// resources returned differ only in the resource name. Therefore the
	// expected update is the same for all the watchers.
	wantUpdate := xdsresource.EndpointsUpdateErrTuple{
		Update: xdsresource.EndpointsUpdate{
			Localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost1, edsPort1), Weight: 1}},
					ID:        internal.LocalityID{SubZone: "subzone"},
					Priority:  0,
					Weight:    1,
				},
			},
		},
	}
	if err := verifyEndpointsUpdate(ctx, updateCh1, wantUpdate); err != nil {
		t.Fatal(err)
	}
	if err := verifyEndpointsUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}
	if err := verifyEndpointsUpdate(ctx, updateCh3, wantUpdate); err != nil {
		t.Fatal(err)
	}
}

// TestEDSWatch_ResourceCaching covers the case where a watch is registered for
// a resource which is already present in the cache.  The test verifies that the
// watch callback is invoked with the contents from the cache, instead of a
// request being sent to the management server.
func (s) TestEDSWatch_ResourceCaching(t *testing.T) {
	overrideFedEnvVar(t)
	firstRequestReceived := false
	firstAckReceived := grpcsync.NewEvent()
	secondRequestReceived := grpcsync.NewEvent()

	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(id int64, req *v3discoverypb.DiscoveryRequest) error {
			// The first request has an empty version string.
			if !firstRequestReceived && req.GetVersionInfo() == "" {
				firstRequestReceived = true
				return nil
			}
			// The first ack has a non-empty version string.
			if !firstAckReceived.HasFired() && req.GetVersionInfo() != "" {
				firstAckReceived.Fire()
				return nil
			}
			// Any requests after the first request and ack, are not expected.
			secondRequestReceived.Fire()
			return nil
		},
	})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for an endpoint resource and have the watch callback
	// push the received update on to a channel.
	updateCh1 := testutils.NewChannel()
	edsCancel1 := client.WatchEndpoints(edsName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh1.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel1()

	// Configure the management server to return a single endpoint resource,
	// corresponding to the one we registered a watch for.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsName, edsHost1, []uint32{edsPort1})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update.
	wantUpdate := xdsresource.EndpointsUpdateErrTuple{
		Update: xdsresource.EndpointsUpdate{
			Localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost1, edsPort1), Weight: 1}},
					ID:        internal.LocalityID{SubZone: "subzone"},
					Priority:  0,
					Weight:    1,
				},
			},
		},
	}
	if err := verifyEndpointsUpdate(ctx, updateCh1, wantUpdate); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("timeout when waiting for receipt of ACK at the management server")
	case <-firstAckReceived.Done():
	}

	// Register another watch for the same resource. This should get the update
	// from the cache.
	updateCh2 := testutils.NewChannel()
	edsCancel2 := client.WatchEndpoints(edsName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh2.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel2()
	if err := verifyEndpointsUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// No request should get sent out as part of this watch.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case <-secondRequestReceived.Done():
		t.Fatal("xdsClient sent out request instead of using update from cache")
	}
}

// TestEDSWatch_ExpiryTimerFiresBeforeResponse tests the case where the client
// does not receive an EDS response for the request that it sends. The test
// verifies that the watch callback is invoked with an error once the
// watchExpiryTimer fires.
func (s) TestEDSWatch_ExpiryTimerFiresBeforeResponse(t *testing.T) {
	// No need to spin up a management server since we don't want the client to
	// receive a response for the watch being registered by the test.

	// Create an xDS client talking to a non-existent management server.
	client, err := xdsclient.NewWithConfigForTesting(&bootstrap.Config{
		XDSServer: &bootstrap.ServerConfig{
			ServerURI:    "dummy management server address",
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			TransportAPI: version.TransportV3,
			NodeProto:    &v3corepb.Node{},
		},
	}, defaultTestWatchExpiryTimeout, time.Duration(0))
	if err != nil {
		t.Fatalf("failed to create xds client: %v", err)
	}
	defer client.Close()

	// Register a watch for a resource which is expected to fail with an error
	// after the watch expiry timer fires.
	updateCh := testutils.NewChannel()
	edsCancel := client.WatchEndpoints(edsName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel()

	// Wait for the watch expiry timer to fire.
	<-time.After(defaultTestWatchExpiryTimeout)

	// Verify that an empty update with the expected error is received.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	wantErr := fmt.Errorf("watch for resource %q of type ClusterLoadAssignment timed out", rdsName)
	if err := verifyEndpointsUpdate(ctx, updateCh, xdsresource.EndpointsUpdateErrTuple{Err: wantErr}); err != nil {
		t.Fatal(err)
	}
}

// TestEDSWatch_ValidResponseCancelsExpiryTimerBehavior tests the case where the
// client receives a valid EDS response for the request that it sends. The test
// verifies that the behavior associated with the expiry timer (i.e, callback
// invocation with error) does not take place.
func (s) TestEDSWatch_ValidResponseCancelsExpiryTimerBehavior(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	defer mgmtServer.Stop()

	// Create an xDS client talking to the above management server.
	nodeID := uuid.New().String()
	client, err := xdsclient.NewWithConfigForTesting(&bootstrap.Config{
		XDSServer: &bootstrap.ServerConfig{
			ServerURI:    mgmtServer.Address,
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			TransportAPI: version.TransportV3,
			NodeProto:    &v3corepb.Node{Id: nodeID},
		},
	}, defaultTestWatchExpiryTimeout, time.Duration(0))
	if err != nil {
		t.Fatalf("failed to create xds client: %v", err)
	}
	defer client.Close()

	// Register a watch for an endpoint resource and have the watch callback
	// push the received update on to a channel.
	updateCh := testutils.NewChannel()
	edsCancel := client.WatchEndpoints(edsName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh.Send(xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel()

	// Configure the management server to return a single endpoint resource,
	// corresponding to the one we registered a watch for.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsName, edsHost1, []uint32{edsPort1})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update.
	wantUpdate := xdsresource.EndpointsUpdateErrTuple{
		Update: xdsresource.EndpointsUpdate{
			Localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost1, edsPort1), Weight: 1}},
					ID:        internal.LocalityID{SubZone: "subzone"},
					Priority:  0,
					Weight:    1,
				},
			},
		},
	}
	if err := verifyEndpointsUpdate(ctx, updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Wait for the watch expiry timer to fire, and verify that the callback is
	// not invoked.
	<-time.After(defaultTestWatchExpiryTimeout)
	if err := verifyNoEndpointsUpdate(ctx, updateCh); err != nil {
		t.Fatal(err)
	}
}

// TestEDSWatch_NACKError covers the case where an update from the management
// server is NACK'ed by the xdsclient. The test verifies that the error is
// propagated to the watcher.
func (s) TestEDSWatch_NACKError(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for a route configuration resource and have the watch
	// callback push the received update on to a channel.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	updateCh := testutils.NewChannel()
	edsCancel := client.WatchEndpoints(edsName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh.SendContext(ctx, xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel()

	// Configure the management server to return a single route configuration
	// resource which is expected to be NACKed by the client.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{badEndpointsResource(edsName, edsHost1, []uint32{edsPort1})},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify that the expected error is propagated to the watcher.
	u, err := updateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for an endpoints resource from the management server: %v", err)
	}
	gotErr := u.(xdsresource.EndpointsUpdateErrTuple).Err
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantEndpointsNACKErr) {
		t.Fatalf("update received with error: %v, want %q", gotErr, wantEndpointsNACKErr)
	}
}

// TestEDSWatch_PartialValid covers the case where a response from the
// management server contains both valid and invalid resources and is expected
// to be NACK'ed by the xdsclient. The test verifies that watchers corresponding
// to the valid resource receive the update, while watchers corresponding to the
// invalid resource receive an error.
func (s) TestEDSWatch_PartialValid(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for two endpoint resources. The first watch is
	// expected to receive an error because the received resource is NACKed.
	// The second watch is expected to get a good update.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	badResourceName := rdsName
	updateCh1 := testutils.NewChannel()
	edsCancel1 := client.WatchEndpoints(badResourceName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh1.SendContext(ctx, xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel1()
	goodResourceName := ldsNameNewStyle
	updateCh2 := testutils.NewChannel()
	edsCancel2 := client.WatchEndpoints(goodResourceName, func(u xdsresource.EndpointsUpdate, err error) {
		updateCh2.SendContext(ctx, xdsresource.EndpointsUpdateErrTuple{Update: u, Err: err})
	})
	defer edsCancel2()

	// Configure the management server to return two endpoints resources,
	// corresponding to the registered watches.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			badEndpointsResource(badResourceName, edsHost1, []uint32{edsPort1}),
			e2e.DefaultEndpoint(goodResourceName, edsHost1, []uint32{edsPort1}),
		},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify that the expected error is propagated to the watcher which
	// requested for the bad resource.
	u, err := updateCh1.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for an endpoints resource from the management server: %v", err)
	}
	gotErr := u.(xdsresource.EndpointsUpdateErrTuple).Err
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantEndpointsNACKErr) {
		t.Fatalf("update received with error: %v, want %q", gotErr, wantEndpointsNACKErr)
	}

	// Verify that the watcher watching the good resource receives an update.
	wantUpdate := xdsresource.EndpointsUpdateErrTuple{
		Update: xdsresource.EndpointsUpdate{
			Localities: []xdsresource.Locality{
				{
					Endpoints: []xdsresource.Endpoint{{Address: fmt.Sprintf("%s:%d", edsHost1, edsPort1), Weight: 1}},
					ID:        internal.LocalityID{SubZone: "subzone"},
					Priority:  0,
					Weight:    1,
				},
			},
		},
	}
	if err := verifyEndpointsUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}
}
