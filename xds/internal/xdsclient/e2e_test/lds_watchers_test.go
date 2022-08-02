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
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	_ "google.golang.org/grpc/xds"                                          // To ensure internal.NewXDSResolverWithConfigForTesting is set.
	_ "google.golang.org/grpc/xds/internal/httpfilter/router"               // Register the router filter.
	_ "google.golang.org/grpc/xds/internal/xdsclient/controller/version/v3" // Register the v3 xDS API client.
)

func overrideFedEnvVar(t *testing.T) {
	oldFed := envconfig.XDSFederation
	envconfig.XDSFederation = true
	t.Cleanup(func() { envconfig.XDSFederation = oldFed })
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestWatchExpiryTimeout = 500 * time.Millisecond
	defaultTestTimeout            = 5 * time.Second
	defaultTestShortTimeout       = 10 * time.Millisecond // For events expected to *not* happen.

	ldsName         = "xdsclient-test-lds-resource"
	rdsName         = "xdsclient-test-rds-resource"
	ldsNameNewStyle = "xdstp:///envoy.config.listener.v3.Listener/xdsclient-test-lds-resource"
	rdsNameNewStyle = "xdstp:///envoy.config.listener.v3.Listener/xdsclient-test-rds-resource"
)

// badListenerResource returns a listener resource for the given name which does
// not contain the `RouteSpecifier` field in the HTTPConnectionManager, and
// hence is expected to be NACKed by the client.
func badListenerResource(name string) *v3listenerpb.Listener {
	hcm := testutils.MarshalAny(&v3httppb.HttpConnectionManager{
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
	})
	return &v3listenerpb.Listener{
		Name:        name,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	}
}

// xdsClient is expected to produce an error containing this string when an
// update is received containing a listener created using `badListenerResource`.
const wantNACKErr = "no RouteSpecifier"

// verifyNoListenerUpdate verifies that no listener update is received on the
// provided update channel, and returns an error if an update is received.
//
// A very short deadline is used while waiting for the update, as this function
// is intended to be used when an update is not expected.
func verifyNoListenerUpdate(ctx context.Context, updateCh *testutils.Channel) error {
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := updateCh.Receive(sCtx); err != context.DeadlineExceeded {
		return fmt.Errorf("unexpected ListenerUpdate: %v", u)
	}
	return nil
}

// verifyListenerUpdate waits for an update to be received on the provided
// update channel and verifies that it matches the expected update.
//
// Returns an error if no update is received before the context deadline expires
// or the received update does not match the expected one.
func verifyListenerUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate xdsresource.ListenerUpdateErrTuple) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for a listener resource from the management server: %v", err)
	}
	got := u.(xdsresource.ListenerUpdateErrTuple)
	if wantUpdate.Err != nil {
		if gotType, wantType := xdsresource.ErrType(got.Err), xdsresource.ErrType(wantUpdate.Err); gotType != wantType {
			return fmt.Errorf("received update with error type %v, want %v", gotType, wantType)
		}
	}
	cmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
		cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
	}
	if diff := cmp.Diff(wantUpdate.Update, got.Update, cmpOpts...); diff != "" {
		return fmt.Errorf("received unepected diff in the listener resource update: (-want, got):\n%s", diff)
	}
	return nil
}

// TestLDSWatch covers the case where a single watcher exists for a single
// listener resource. The test verifies the following scenarios:
// 1. An update from the management server containing the resource being
//    watched should result in the invocation of the watch callback.
// 2. An update from the management server containing a resource *not* being
//    watched should not result in the invocation of the watch callback.
// 3. After the watch is cancelled, an update from the management server
//    containing the resource that was being watched should not result in the
//    invocation of the watch callback.
//
// The test is run for old and new style names.
func (s) TestLDSWatch(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, nil)
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	tests := []struct {
		desc                   string
		resourceName           string
		watchedResource        *v3listenerpb.Listener // The resource being watched.
		updatedWatchedResource *v3listenerpb.Listener // The watched resource after an update.
		notWatchedResource     *v3listenerpb.Listener // A resource which is not being watched.
		wantUpdate             xdsresource.ListenerUpdateErrTuple
	}{
		{
			desc:                   "old style resource",
			resourceName:           ldsName,
			watchedResource:        e2e.DefaultClientListener(ldsName, rdsName),
			updatedWatchedResource: e2e.DefaultClientListener(ldsName, "new-rds-resource"),
			notWatchedResource:     e2e.DefaultClientListener("unsubscribed-lds-resource", rdsName),
			wantUpdate: xdsresource.ListenerUpdateErrTuple{
				Update: xdsresource.ListenerUpdate{
					RouteConfigName: rdsName,
					HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
				},
			},
		},
		{
			desc:                   "new style resource",
			resourceName:           ldsNameNewStyle,
			watchedResource:        e2e.DefaultClientListener(ldsNameNewStyle, rdsNameNewStyle),
			updatedWatchedResource: e2e.DefaultClientListener(ldsNameNewStyle, "new-rds-resource"),
			notWatchedResource:     e2e.DefaultClientListener("unsubscribed-lds-resource", rdsNameNewStyle),
			wantUpdate: xdsresource.ListenerUpdateErrTuple{
				Update: xdsresource.ListenerUpdate{
					RouteConfigName: rdsNameNewStyle,
					HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// Register a watch for a listener resource and have the watch
			// callback push the received update on to a channel.
			updateCh := testutils.NewChannel()
			ldsCancel := client.WatchListener(test.resourceName, func(u xdsresource.ListenerUpdate, err error) {
				updateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
			})

			// Configure the management server to return a single listener
			// resource, corresponding to the one we registered a watch for.
			resources := e2e.UpdateOptions{
				NodeID:         nodeID,
				Listeners:      []*v3listenerpb.Listener{test.watchedResource},
				SkipValidation: true,
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}

			// Verify the contents of the received update.
			if err := verifyListenerUpdate(ctx, updateCh, test.wantUpdate); err != nil {
				t.Fatal(err)
			}

			// Configure the management server to return an additional listener
			// resource, one that we are not interested in.
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Listeners:      []*v3listenerpb.Listener{test.watchedResource, test.notWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoListenerUpdate(ctx, updateCh); err != nil {
				t.Fatal(err)
			}

			// Cancel the watch and update the resource corresponding to the original
			// watch.  Ensure that the cancelled watch callback is not invoked.
			ldsCancel()
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Listeners:      []*v3listenerpb.Listener{test.updatedWatchedResource, test.notWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoListenerUpdate(ctx, updateCh); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLDSWatch_TwoWatchesForSameResourceName covers the case where two watchers
// exist for a single listener resource.  The test verifies the following
// scenarios:
// 1. An update from the management server containing the resource being
//    watched should result in the invocation of both watch callbacks.
// 2. After one of the watches is cancelled, a redundant update from the
//    management server should not result in the invocation of either of the
//    watch callbacks.
// 3. An update from the management server containing the resource being
//    watched should result in the invocation of the un-cancelled watch
//    callback.
//
// The test is run for old and new style names.
func (s) TestLDSWatch_TwoWatchesForSameResourceName(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, nil)
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	tests := []struct {
		desc                   string
		resourceName           string
		watchedResource        *v3listenerpb.Listener // The resource being watched.
		updatedWatchedResource *v3listenerpb.Listener // The watched resource after an update.
		wantUpdateV1           xdsresource.ListenerUpdateErrTuple
		wantUpdateV2           xdsresource.ListenerUpdateErrTuple
	}{
		{
			desc:                   "old style resource",
			resourceName:           ldsName,
			watchedResource:        e2e.DefaultClientListener(ldsName, rdsName),
			updatedWatchedResource: e2e.DefaultClientListener(ldsName, "new-rds-resource"),
			wantUpdateV1: xdsresource.ListenerUpdateErrTuple{
				Update: xdsresource.ListenerUpdate{
					RouteConfigName: rdsName,
					HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
				},
			},
			wantUpdateV2: xdsresource.ListenerUpdateErrTuple{
				Update: xdsresource.ListenerUpdate{
					RouteConfigName: "new-rds-resource",
					HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
				},
			},
		},
		{
			desc:                   "new style resource",
			resourceName:           ldsNameNewStyle,
			watchedResource:        e2e.DefaultClientListener(ldsNameNewStyle, rdsNameNewStyle),
			updatedWatchedResource: e2e.DefaultClientListener(ldsNameNewStyle, "new-rds-resource"),
			wantUpdateV1: xdsresource.ListenerUpdateErrTuple{
				Update: xdsresource.ListenerUpdate{
					RouteConfigName: rdsNameNewStyle,
					HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
				},
			},
			wantUpdateV2: xdsresource.ListenerUpdateErrTuple{
				Update: xdsresource.ListenerUpdate{
					RouteConfigName: "new-rds-resource",
					HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// Register two watches for the same listener resource and have the
			// callbacks push the received updates on to a channel.
			updateCh1 := testutils.NewChannel()
			ldsCancel1 := client.WatchListener(test.resourceName, func(u xdsresource.ListenerUpdate, err error) {
				updateCh1.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
			})
			defer ldsCancel1()
			updateCh2 := testutils.NewChannel()
			ldsCancel2 := client.WatchListener(test.resourceName, func(u xdsresource.ListenerUpdate, err error) {
				updateCh2.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
			})

			// Configure the management server to return a single listener
			// resource, corresponding to the one we registered watches for.
			resources := e2e.UpdateOptions{
				NodeID:         nodeID,
				Listeners:      []*v3listenerpb.Listener{test.watchedResource},
				SkipValidation: true,
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}

			// Verify the contents of the received update.
			if err := verifyListenerUpdate(ctx, updateCh1, test.wantUpdateV1); err != nil {
				t.Fatal(err)
			}
			if err := verifyListenerUpdate(ctx, updateCh2, test.wantUpdateV1); err != nil {
				t.Fatal(err)
			}

			// Cancel the second watch and force the management server to push a
			// redundant update for the resource being watched. Neither of the
			// two watch callbacks should be invoked.
			ldsCancel2()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoListenerUpdate(ctx, updateCh1); err != nil {
				t.Fatal(err)
			}
			if err := verifyNoListenerUpdate(ctx, updateCh2); err != nil {
				t.Fatal(err)
			}

			// Update to the resource being watched. The un-cancelled callback
			// should be invoked while the cancelled one should not be.
			resources = e2e.UpdateOptions{
				NodeID:         nodeID,
				Listeners:      []*v3listenerpb.Listener{test.updatedWatchedResource},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyListenerUpdate(ctx, updateCh1, test.wantUpdateV2); err != nil {
				t.Fatal(err)
			}
			if err := verifyNoListenerUpdate(ctx, updateCh2); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLDSWatch_ThreeWatchesForDifferentResourceNames covers the case with three
// watchers (two watchers for one resource, and the third watcher for another
// resource), exist across two listener resources.  The test verifies that an
// update from the management server containing both resources results in the
// invocation of all watch callbacks.
//
// The test is run with both old and new style names.
func (s) TestLDSWatch_ThreeWatchesForDifferentResourceNames(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, nil)
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for the same listener resource and have the
	// callbacks push the received updates on to a channel.
	updateCh1 := testutils.NewChannel()
	ldsCancel1 := client.WatchListener(ldsName, func(u xdsresource.ListenerUpdate, err error) {
		updateCh1.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel1()
	updateCh2 := testutils.NewChannel()
	ldsCancel2 := client.WatchListener(ldsName, func(u xdsresource.ListenerUpdate, err error) {
		updateCh2.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel2()

	// Register the third watch for a different listener resource.
	updateCh3 := testutils.NewChannel()
	ldsCancel3 := client.WatchListener(ldsNameNewStyle, func(u xdsresource.ListenerUpdate, err error) {
		updateCh3.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel3()

	// Configure the management server to return two listener resources,
	// corresponding to the registered watches.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(ldsName, rdsName),
			e2e.DefaultClientListener(ldsNameNewStyle, rdsName),
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
	wantUpdate := xdsresource.ListenerUpdateErrTuple{
		Update: xdsresource.ListenerUpdate{
			RouteConfigName: rdsName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, updateCh1, wantUpdate); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, updateCh3, wantUpdate); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_ResourceCaching covers the case where a watch is registered for
// a resource which is already present in the cache.  The test verifies that the
// watch callback is invoked with the contents from the cache, instead of a
// request being sent to the management server.
func (s) TestLDSWatch_ResourceCaching(t *testing.T) {
	overrideFedEnvVar(t)
	firstRequestReceived := false
	firstAckReceived := grpcsync.NewEvent()
	secondRequestReceived := grpcsync.NewEvent()

	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, &e2e.ManagementServerOptions{
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

	// Register a watch for a listener resource and have the watch
	// callback push the received update on to a channel.
	updateCh1 := testutils.NewChannel()
	ldsCancel1 := client.WatchListener(ldsName, func(u xdsresource.ListenerUpdate, err error) {
		updateCh1.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel1()

	// Configure the management server to return a single listener
	// resource, corresponding to the one we registered a watch for.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update.
	wantUpdate := xdsresource.ListenerUpdateErrTuple{
		Update: xdsresource.ListenerUpdate{
			RouteConfigName: rdsName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, updateCh1, wantUpdate); err != nil {
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
	ldsCancel2 := client.WatchListener(ldsName, func(u xdsresource.ListenerUpdate, err error) {
		updateCh2.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel2()
	if err := verifyListenerUpdate(ctx, updateCh2, wantUpdate); err != nil {
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

// TestLDSWatch_ResourceRemoved covers the cases where a resource being watched
// is removed from the management server. The test verifies the following
// scenarios:
// 1. Removing a resource should trigger the watch callback with a resource
//    removed error. It should not trigger the watch callback for an unrelated
//    resource.
// 2. An update to another resource should result in the invocation of the watch
//    callback associated with that resource.  It should not result in the
//    invocation of the watch callback associated with the deleted resource.
//
// The test is run with both old and new style names.
func (s) TestLDSWatch_ResourceRemoved(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, nil)
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for two listener resources and have the
	// callbacks push the received updates on to a channel.
	resourceName1 := ldsName
	updateCh1 := testutils.NewChannel()
	ldsCancel1 := client.WatchListener(resourceName1, func(u xdsresource.ListenerUpdate, err error) {
		updateCh1.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel1()

	resourceName2 := ldsNameNewStyle
	updateCh2 := testutils.NewChannel()
	ldsCancel2 := client.WatchListener(resourceName2, func(u xdsresource.ListenerUpdate, err error) {
		updateCh2.Send(xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel2()

	// Configure the management server to return two listener resources,
	// corresponding to the registered watches.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(resourceName1, rdsName),
			e2e.DefaultClientListener(resourceName2, rdsName),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update for both watchers. The two
	// resources returned differ only in the resource name. Therefore the
	// expected update is the same for both watchers.
	wantUpdate := xdsresource.ListenerUpdateErrTuple{
		Update: xdsresource.ListenerUpdate{
			RouteConfigName: rdsName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, updateCh1, wantUpdate); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Remove the first listener resource on the management server.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(resourceName2, rdsName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// The first watcher should receive a resource removed error, while the
	// second watcher should not see an update.
	if err := verifyListenerUpdate(ctx, updateCh1, xdsresource.ListenerUpdateErrTuple{
		Err: xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, ""),
	}); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoListenerUpdate(ctx, updateCh2); err != nil {
		t.Fatal(err)
	}

	// Update the second listener resource on the management server. The first
	// watcher should not see an update, while the second watcher should.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(resourceName2, "new-rds-resource")},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}
	if err := verifyNoListenerUpdate(ctx, updateCh1); err != nil {
		t.Fatal(err)
	}
	wantUpdate = xdsresource.ListenerUpdateErrTuple{
		Update: xdsresource.ListenerUpdate{
			RouteConfigName: "new-rds-resource",
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_NACKError covers the case where an update from the management
// server is NACK'ed by the xdsclient. The test verifies that the error is
// propagated to the watcher.
func (s) TestLDSWatch_NACKError(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, nil)
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for a listener resource and have the watch
	// callback push the received update on to a channel.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	updateCh := testutils.NewChannel()
	ldsCancel := client.WatchListener(ldsName, func(u xdsresource.ListenerUpdate, err error) {
		updateCh.SendContext(ctx, xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel()

	// Configure the management server to return a single listener resource
	// which is expected to be NACKed by the client.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{badListenerResource(ldsName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify that the expected error is propagated to the watcher.
	u, err := updateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for a listener resource from the management server: %v", err)
	}
	gotErr := u.(xdsresource.ListenerUpdateErrTuple).Err
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantNACKErr) {
		t.Fatalf("update received with error: %v, want %q", gotErr, wantNACKErr)
	}
}

// TestLDSWatch_PartialValid covers the case where a response from the
// management server contains both valid and invalid resources and is expected
// to be NACK'ed by the xdsclient. The test verifies that watchers corresponding
// to the valid resource receive the update, while watchers corresponding to the
// invalid resource receive an error.
func (s) TestLDSWatch_PartialValid(t *testing.T) {
	overrideFedEnvVar(t)
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, nil)
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for listener resources. The first watch is expected
	// to receive an error because the received resource is NACKed. The second
	// watch is expected to get a good update.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	badResourceName := ldsName
	updateCh1 := testutils.NewChannel()
	ldsCancel1 := client.WatchListener(badResourceName, func(u xdsresource.ListenerUpdate, err error) {
		updateCh1.SendContext(ctx, xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel1()
	goodResourceName := ldsNameNewStyle
	updateCh2 := testutils.NewChannel()
	ldsCancel2 := client.WatchListener(goodResourceName, func(u xdsresource.ListenerUpdate, err error) {
		updateCh2.SendContext(ctx, xdsresource.ListenerUpdateErrTuple{Update: u, Err: err})
	})
	defer ldsCancel2()

	// Configure the management with server two listener resources. One of these
	// is a bad resource causing the update to be NACKed.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			badListenerResource(badResourceName),
			e2e.DefaultClientListener(goodResourceName, rdsName),
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
		t.Fatalf("timeout when waiting for a listener resource from the management server: %v", err)
	}
	gotErr := u.(xdsresource.ListenerUpdateErrTuple).Err
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantNACKErr) {
		t.Fatalf("update received with error: %v, want %q", gotErr, wantNACKErr)
	}

	// Verify that the watcher watching the good resource receives a good
	// update.
	wantUpdate := xdsresource.ListenerUpdateErrTuple{
		Update: xdsresource.ListenerUpdate{
			RouteConfigName: rdsName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, updateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}
}
