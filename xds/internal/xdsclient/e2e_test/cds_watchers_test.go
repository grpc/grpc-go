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
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// badClusterResource returns a cluster resource for the given name which
// contains a config_source_specifier for the `lrs_server` field which is not
// set to `self`, and hence is expected to be NACKed by the client.
func badClusterResource(clusterName, edsServiceName string, secLevel e2e.SecurityLevel) *v3clusterpb.Cluster {
	cluster := e2e.DefaultCluster(clusterName, edsServiceName, secLevel)
	cluster.LrsServer = &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{}}
	return cluster
}

// xdsClient is expected to produce an error containing this string when an
// update is received containing a cluster created using `badClusterResource`.
const wantClusterNACKErr = "unsupported config_source_specifier"

// verifyClusterUpdate waits for an update to be received on the provided update
// channel and verifies that it matches the expected update.
//
// Returns an error if no update is received before the context deadline expires
// or the received update does not match the expected one.
func verifyClusterUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate xdsresource.ClusterUpdateErrTuple) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for a cluster resource from the management server: %v", err)
	}
	got := u.(xdsresource.ClusterUpdateErrTuple)
	if wantUpdate.Err != nil {
		if gotType, wantType := xdsresource.ErrType(got.Err), xdsresource.ErrType(wantUpdate.Err); gotType != wantType {
			return fmt.Errorf("received update with error type %v, want %v", gotType, wantType)
		}
	}
	cmpOpts := []cmp.Option{cmpopts.EquateEmpty(), cmpopts.IgnoreFields(xdsresource.ClusterUpdate{}, "Raw")}
	if diff := cmp.Diff(wantUpdate.Update, got.Update, cmpOpts...); diff != "" {
		return fmt.Errorf("received unepected diff in the cluster resource update: (-want, got):\n%s", diff)
	}
	return nil
}

// verifyNoClusterUpdate verifies that no cluster update is received on the
// provided update channel, and returns an error if an update is received.
//
// A very short deadline is used while waiting for the update, as this function
// is intended to be used when an update is not expected.
func verifyNoClusterUpdate(ctx context.Context, updateCh *testutils.Channel) error {
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := updateCh.Receive(sCtx); err != context.DeadlineExceeded {
		return fmt.Errorf("received unexpected ClusterUpdate when expecting none: %v", u)
	}
	return nil
}

// TestCDSWatch covers the case where a single watcher exists for a single
// cluster resource. The test verifies the following scenarios:
//  1. An update from the management server containing the resource being
//     watched should result in the invocation of the watch callback.
//  2. An update from the management server containing a resource *not* being
//     watched should not result in the invocation of the watch callback.
//  3. After the watch is cancelled, an update from the management server
//     containing the resource that was being watched should not result in the
//     invocation of the watch callback.
//
// The test is run for old and new style names.
func (s) TestCDSWatch(t *testing.T) {
	tests := []struct {
		desc                   string
		resourceName           string
		watchedResource        *v3clusterpb.Cluster // The resource being watched.
		updatedWatchedResource *v3clusterpb.Cluster // The watched resource after an update.
		notWatchedResource     *v3clusterpb.Cluster // A resource which is not being watched.
		wantUpdate             xdsresource.ClusterUpdateErrTuple
	}{
		{
			desc:                   "old style resource",
			resourceName:           cdsName,
			watchedResource:        e2e.DefaultCluster(cdsName, edsName, e2e.SecurityLevelNone),
			updatedWatchedResource: e2e.DefaultCluster(cdsName, "new-eds-resource", e2e.SecurityLevelNone),
			notWatchedResource:     e2e.DefaultCluster("unsubscribed-cds-resource", edsName, e2e.SecurityLevelNone),
			wantUpdate: xdsresource.ClusterUpdateErrTuple{
				Update: xdsresource.ClusterUpdate{
					ClusterName:    cdsName,
					EDSServiceName: edsName,
				},
			},
		},
		{
			desc:                   "new style resource",
			resourceName:           cdsNameNewStyle,
			watchedResource:        e2e.DefaultCluster(cdsNameNewStyle, edsNameNewStyle, e2e.SecurityLevelNone),
			updatedWatchedResource: e2e.DefaultCluster(cdsNameNewStyle, "new-eds-resource", e2e.SecurityLevelNone),
			notWatchedResource:     e2e.DefaultCluster("unsubscribed-cds-resource", edsNameNewStyle, e2e.SecurityLevelNone),
			wantUpdate: xdsresource.ClusterUpdateErrTuple{
				Update: xdsresource.ClusterUpdate{
					ClusterName:    cdsNameNewStyle,
					EDSServiceName: edsNameNewStyle,
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

			// Register a watch for a cluster resource and have the watch
			// callback push the received update on to a channel.
			updateCh := testutils.NewChannel()
			cdsCancel := client.WatchCluster(test.resourceName, func(u xdsresource.ClusterUpdate, err error) {
				updateCh.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
			})

			// Configure the management server to return a single cluster
			// resource, corresponding to the one we registered a watch for.
			resources := e2e.UpdateOptions{
				NodeID:         nodeID,
				Clusters:       []*v3clusterpb.Cluster{test.watchedResource},
				SkipValidation: true,
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}

			// Verify the contents of the received update.
			if err := verifyClusterUpdate(ctx, updateCh, test.wantUpdate); err != nil {
				t.Fatal(err)
			}

			// Configure the management server to return an additional cluster
			// resource, one that we are not interested in.
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Clusters:       []*v3clusterpb.Cluster{test.watchedResource, test.notWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoClusterUpdate(ctx, updateCh); err != nil {
				t.Fatal(err)
			}

			// Cancel the watch and update the resource corresponding to the original
			// watch.  Ensure that the cancelled watch callback is not invoked.
			cdsCancel()
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Clusters:       []*v3clusterpb.Cluster{test.updatedWatchedResource, test.notWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoClusterUpdate(ctx, updateCh); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestCDSWatch_TwoWatchesForSameResourceName covers the case where two watchers
// exist for a single cluster resource.  The test verifies the following
// scenarios:
//  1. An update from the management server containing the resource being
//     watched should result in the invocation of both watch callbacks.
//  2. After one of the watches is cancelled, a redundant update from the
//     management server should not result in the invocation of either of the
//     watch callbacks.
//  3. A new update from the management server containing the resource being
//     watched should result in the invocation of the un-cancelled watch
//     callback.
//
// The test is run for old and new style names.
func (s) TestCDSWatch_TwoWatchesForSameResourceName(t *testing.T) {
	tests := []struct {
		desc                   string
		resourceName           string
		watchedResource        *v3clusterpb.Cluster // The resource being watched.
		updatedWatchedResource *v3clusterpb.Cluster // The watched resource after an update.
		wantUpdateV1           xdsresource.ClusterUpdateErrTuple
		wantUpdateV2           xdsresource.ClusterUpdateErrTuple
	}{
		{
			desc:                   "old style resource",
			resourceName:           cdsName,
			watchedResource:        e2e.DefaultCluster(cdsName, edsName, e2e.SecurityLevelNone),
			updatedWatchedResource: e2e.DefaultCluster(cdsName, "new-eds-resource", e2e.SecurityLevelNone),
			wantUpdateV1: xdsresource.ClusterUpdateErrTuple{
				Update: xdsresource.ClusterUpdate{
					ClusterName:    cdsName,
					EDSServiceName: edsName,
				},
			},
			wantUpdateV2: xdsresource.ClusterUpdateErrTuple{
				Update: xdsresource.ClusterUpdate{
					ClusterName:    cdsName,
					EDSServiceName: "new-eds-resource",
				},
			},
		},
		{
			desc:                   "new style resource",
			resourceName:           cdsNameNewStyle,
			watchedResource:        e2e.DefaultCluster(cdsNameNewStyle, edsNameNewStyle, e2e.SecurityLevelNone),
			updatedWatchedResource: e2e.DefaultCluster(cdsNameNewStyle, "new-eds-resource", e2e.SecurityLevelNone),
			wantUpdateV1: xdsresource.ClusterUpdateErrTuple{
				Update: xdsresource.ClusterUpdate{
					ClusterName:    cdsNameNewStyle,
					EDSServiceName: edsNameNewStyle,
				},
			},
			wantUpdateV2: xdsresource.ClusterUpdateErrTuple{
				Update: xdsresource.ClusterUpdate{
					ClusterName:    cdsNameNewStyle,
					EDSServiceName: "new-eds-resource",
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

			// Register two watches for the same cluster resource and have the
			// callbacks push the received updates on to a channel.
			updateCh1 := testutils.NewChannel()
			cdsCancel1 := client.WatchCluster(test.resourceName, func(u xdsresource.ClusterUpdate, err error) {
				updateCh1.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
			})
			defer cdsCancel1()
			updateCh2 := testutils.NewChannel()
			cdsCancel2 := client.WatchCluster(test.resourceName, func(u xdsresource.ClusterUpdate, err error) {
				updateCh2.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
			})

			// Configure the management server to return a single cluster
			// resource, corresponding to the one we registered watches for.
			resources := e2e.UpdateOptions{
				NodeID:         nodeID,
				Clusters:       []*v3clusterpb.Cluster{test.watchedResource},
				SkipValidation: true,
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}

			// Verify the contents of the received update.
			if err := verifyClusterUpdate(ctx, updateCh1, test.wantUpdateV1); err != nil {
				t.Fatal(err)
			}
			if err := verifyClusterUpdate(ctx, updateCh2, test.wantUpdateV1); err != nil {
				t.Fatal(err)
			}

			// Cancel the second watch and force the management server to push a
			// redundant update for the resource being watched. Neither of the
			// two watch callbacks should be invoked.
			cdsCancel2()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoClusterUpdate(ctx, updateCh1); err != nil {
				t.Fatal(err)
			}
			if err := verifyNoClusterUpdate(ctx, updateCh2); err != nil {
				t.Fatal(err)
			}

			// Update to the resource being watched. The un-cancelled callback
			// should be invoked while the cancelled one should not be.
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Clusters:       []*v3clusterpb.Cluster{test.updatedWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyClusterUpdate(ctx, updateCh1, test.wantUpdateV2); err != nil {
				t.Fatal(err)
			}
			if err := verifyNoClusterUpdate(ctx, updateCh2); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestCDSWatch_ThreeWatchesForDifferentResourceNames covers the case where
// three watchers (two watchers for one resource, and the third watcher for
// another resource) exist across two cluster resources (one with an old style
// name and one with a new style name).  The test verifies that an update from
// the management server containing both resources results in the invocation of
// all watch callbacks.
func (s) TestCDSWatch_ThreeWatchesForDifferentResourceNames(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for the same cluster resource and have the
	// callbacks push the received updates on to a channel.
	updateCh1 := testutils.NewChannel()
	cdsCancel1 := client.WatchCluster(cdsName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh1.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel1()
	updateCh2 := testutils.NewChannel()
	cdsCancel2 := client.WatchCluster(cdsName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh2.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel2()

	// Register the third watch for a different cluster resource, and push the
	// received updates onto a channel.
	updateCh3 := testutils.NewChannel()
	cdsCancel3 := client.WatchCluster(cdsNameNewStyle, func(u xdsresource.ClusterUpdate, err error) {
		updateCh3.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel3()

	// Configure the management server to return two cluster resources,
	// corresponding to the registered watches.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			e2e.DefaultCluster(cdsName, edsName, e2e.SecurityLevelNone),
			e2e.DefaultCluster(cdsNameNewStyle, edsNameNewStyle, e2e.SecurityLevelNone),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update for the all watchers.
	wantUpdate12 := xdsresource.ClusterUpdateErrTuple{
		Update: xdsresource.ClusterUpdate{
			ClusterName:    cdsName,
			EDSServiceName: edsName,
		},
	}
	wantUpdate3 := xdsresource.ClusterUpdateErrTuple{
		Update: xdsresource.ClusterUpdate{
			ClusterName:    cdsNameNewStyle,
			EDSServiceName: edsNameNewStyle,
		},
	}
	if err := verifyClusterUpdate(ctx, updateCh1, wantUpdate12); err != nil {
		t.Fatal(err)
	}
	if err := verifyClusterUpdate(ctx, updateCh2, wantUpdate12); err != nil {
		t.Fatal(err)
	}
	if err := verifyClusterUpdate(ctx, updateCh3, wantUpdate3); err != nil {
		t.Fatal(err)
	}
}

// TestCDSWatch_ResourceCaching covers the case where a watch is registered for
// a resource which is already present in the cache.  The test verifies that the
// watch callback is invoked with the contents from the cache, instead of a
// request being sent to the management server.
func (s) TestCDSWatch_ResourceCaching(t *testing.T) {
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

	// Register a watch for a cluster resource and have the watch
	// callback push the received update on to a channel.
	updateCh1 := testutils.NewChannel()
	cdsCancel1 := client.WatchCluster(cdsName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh1.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel1()

	// Configure the management server to return a single cluster
	// resource, corresponding to the one we registered a watch for.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(cdsName, edsName, e2e.SecurityLevelNone)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update.
	wantUpdate := xdsresource.ClusterUpdateErrTuple{
		Update: xdsresource.ClusterUpdate{
			ClusterName:    cdsName,
			EDSServiceName: edsName,
		},
	}
	if err := verifyClusterUpdate(ctx, updateCh1, wantUpdate); err != nil {
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
	cdsCancel2 := client.WatchCluster(cdsName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh2.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel2()
	if err := verifyClusterUpdate(ctx, updateCh2, wantUpdate); err != nil {
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

// TestCDSWatch_ExpiryTimerFiresBeforeResponse tests the case where the client
// does not receive an CDS response for the request that it sends. The test
// verifies that the watch callback is invoked with an error once the
// watchExpiryTimer fires.
func (s) TestCDSWatch_ExpiryTimerFiresBeforeResponse(t *testing.T) {
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

	// Register a watch for a resource which is expected to be invoked with an
	// error after the watch expiry timer fires.
	updateCh := testutils.NewChannel()
	cdsCancel := client.WatchCluster(cdsName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel()

	// Wait for the watch expiry timer to fire.
	<-time.After(defaultTestWatchExpiryTimeout)

	// Verify that an empty update with the expected error is received.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	wantErr := fmt.Errorf("watch for resource %q of type Cluster timed out", cdsName)
	if err := verifyClusterUpdate(ctx, updateCh, xdsresource.ClusterUpdateErrTuple{Err: wantErr}); err != nil {
		t.Fatal(err)
	}
}

// TestCDSWatch_ValidResponseCancelsExpiryTimerBehavior tests the case where the
// client receives a valid LDS response for the request that it sends. The test
// verifies that the behavior associated with the expiry timer (i.e, callback
// invocation with error) does not take place.
func (s) TestCDSWatch_ValidResponseCancelsExpiryTimerBehavior(t *testing.T) {
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

	// Register a watch for a cluster resource and have the watch
	// callback push the received update on to a channel.
	updateCh := testutils.NewChannel()
	cdsCancel := client.WatchCluster(cdsName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel()

	// Configure the management server to return a single cluster resource,
	// corresponding to the one we registered a watch for.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(cdsName, edsName, e2e.SecurityLevelNone)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update.
	wantUpdate := xdsresource.ClusterUpdateErrTuple{
		Update: xdsresource.ClusterUpdate{
			ClusterName:    cdsName,
			EDSServiceName: edsName,
		},
	}
	if err := verifyClusterUpdate(ctx, updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Wait for the watch expiry timer to fire, and verify that the callback is
	// not invoked.
	<-time.After(defaultTestWatchExpiryTimeout)
	if err := verifyNoClusterUpdate(ctx, updateCh); err != nil {
		t.Fatal(err)
	}
}

// TestCDSWatch_ResourceRemoved covers the cases where two watchers exists for
// two different resources (one with an old style name and one with a new style
// name). One of these resources being watched is removed from the management
// server. The test verifies the following scenarios:
//  1. Removing a resource should trigger the watch callback associated with that
//     resource with a resource removed error. It should not trigger the watch
//     callback for an unrelated resource.
//  2. An update to other resource should result in the invocation of the watch
//     callback associated with that resource.  It should not result in the
//     invocation of the watch callback associated with the deleted resource.
func (s) TesCDSWatch_ResourceRemoved(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for two cluster resources and have the
	// callbacks push the received updates on to a channel.
	resourceName1 := cdsName
	updateCh1 := testutils.NewChannel()
	cdsCancel1 := client.WatchCluster(resourceName1, func(u xdsresource.ClusterUpdate, err error) {
		updateCh1.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel1()
	resourceName2 := cdsNameNewStyle
	updateCh2 := testutils.NewChannel()
	cdsCancel2 := client.WatchCluster(resourceName2, func(u xdsresource.ClusterUpdate, err error) {
		updateCh2.Send(xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel2()

	// Configure the management server to return two cluster resources,
	// corresponding to the registered watches.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			e2e.DefaultCluster(resourceName1, edsName, e2e.SecurityLevelNone),
			e2e.DefaultCluster(resourceName2, edsNameNewStyle, e2e.SecurityLevelNone),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update for both watchers.
	wantUpdate1 := xdsresource.ClusterUpdateErrTuple{
		Update: xdsresource.ClusterUpdate{
			ClusterName:    resourceName1,
			EDSServiceName: edsName,
		},
	}
	wantUpdate2 := xdsresource.ClusterUpdateErrTuple{
		Update: xdsresource.ClusterUpdate{
			ClusterName:    resourceName2,
			EDSServiceName: edsNameNewStyle,
		},
	}
	if err := verifyClusterUpdate(ctx, updateCh1, wantUpdate1); err != nil {
		t.Fatal(err)
	}
	if err := verifyClusterUpdate(ctx, updateCh2, wantUpdate2); err != nil {
		t.Fatal(err)
	}

	// Remove the first cluster resource on the management server.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(resourceName2, edsNameNewStyle, e2e.SecurityLevelNone)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// The first watcher should receive a resource removed error, while the
	// second watcher should not receive an update.
	if err := verifyClusterUpdate(ctx, updateCh1, xdsresource.ClusterUpdateErrTuple{Err: xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "")}); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoClusterUpdate(ctx, updateCh2); err != nil {
		t.Fatal(err)
	}

	// Update the second cluster resource on the management server. The first
	// watcher should not receive an update, while the second watcher should.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(resourceName2, "new-eds-resource", e2e.SecurityLevelNone)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}
	if err := verifyNoClusterUpdate(ctx, updateCh1); err != nil {
		t.Fatal(err)
	}
	wantUpdate := xdsresource.ClusterUpdateErrTuple{
		Update: xdsresource.ClusterUpdate{
			ClusterName:    resourceName2,
			EDSServiceName: "new-eds-resource",
		},
	}
	if err := verifyClusterUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}
}

// TestCDSWatch_NACKError covers the case where an update from the management
// server is NACK'ed by the xdsclient. The test verifies that the error is
// propagated to the watcher.
func (s) TestCDSWatch_NACKError(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for a cluster resource and have the watch
	// callback push the received update on to a channel.
	updateCh := testutils.NewChannel()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cdsCancel := client.WatchCluster(cdsName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh.SendContext(ctx, xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel()

	// Configure the management server to return a single cluster resource
	// which is expected to be NACK'ed by the client.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{badClusterResource(cdsName, edsName, e2e.SecurityLevelNone)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify that the expected error is propagated to the watcher.
	u, err := updateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for a cluster resource from the management server: %v", err)
	}
	gotErr := u.(xdsresource.ClusterUpdateErrTuple).Err
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantClusterNACKErr) {
		t.Fatalf("update received with error: %v, want %q", gotErr, wantClusterNACKErr)
	}
}

// TestCDSWatch_PartialValid covers the case where a response from the
// management server contains both valid and invalid resources and is expected
// to be NACK'ed by the xdsclient. The test verifies that watchers corresponding
// to the valid resource receive the update, while watchers corresponding to the
// invalid resource receive an error.
func (s) TestCDSWatch_PartialValid(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for cluster resources. The first watch is expected
	// to receive an error because the received resource is NACK'ed. The second
	// watch is expected to get a good update.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	badResourceName := cdsName
	updateCh1 := testutils.NewChannel()
	cdsCancel1 := client.WatchCluster(badResourceName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh1.SendContext(ctx, xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel1()
	goodResourceName := cdsNameNewStyle
	updateCh2 := testutils.NewChannel()
	cdsCancel2 := client.WatchCluster(goodResourceName, func(u xdsresource.ClusterUpdate, err error) {
		updateCh2.SendContext(ctx, xdsresource.ClusterUpdateErrTuple{Update: u, Err: err})
	})
	defer cdsCancel2()

	// Configure the management server with two cluster resources. One of these
	// is a bad resource causing the update to be NACKed.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			badClusterResource(badResourceName, edsName, e2e.SecurityLevelNone),
			e2e.DefaultCluster(goodResourceName, edsName, e2e.SecurityLevelNone)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify that the expected error is propagated to the watcher which is
	// watching the bad resource.
	u, err := updateCh1.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for a cluster resource from the management server: %v", err)
	}
	gotErr := u.(xdsresource.ClusterUpdateErrTuple).Err
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantClusterNACKErr) {
		t.Fatalf("update received with error: %v, want %q", gotErr, wantClusterNACKErr)
	}

	// Verify that the watcher watching the good resource receives a good
	// update.
	wantUpdate := xdsresource.ClusterUpdateErrTuple{
		Update: xdsresource.ClusterUpdate{
			ClusterName:    goodResourceName,
			EDSServiceName: edsName,
		},
	}
	if err := verifyClusterUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}
}
