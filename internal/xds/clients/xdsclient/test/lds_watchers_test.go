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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/syncutil"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

type noopListenerWatcher struct{}

func (noopListenerWatcher) ResourceChanged(_ xdsclient.ResourceData, onDone func()) {
	onDone()
}
func (noopListenerWatcher) ResourceError(_ error, onDone func()) {
	onDone()
}
func (noopListenerWatcher) AmbientError(_ error, onDone func()) {
	onDone()
}

type listenerUpdateErrTuple struct {
	update      listenerUpdate
	resourceErr error
	ambientErr  error
}

type listenerWatcher struct {
	updateCh      *testutils.Channel // Messages of type listenerUpdate
	resourceErrCh *testutils.Channel // Messages of type resource error
	ambientErrCh  *testutils.Channel // Messages of type ambient error
}

func newListenerWatcher() *listenerWatcher {
	return &listenerWatcher{
		updateCh:      testutils.NewChannelWithSize(1),
		resourceErrCh: testutils.NewChannelWithSize(1),
		ambientErrCh:  testutils.NewChannelWithSize(1),
	}
}

func (lw *listenerWatcher) ResourceChanged(update xdsclient.ResourceData, onDone func()) {
	lisData, ok := update.(*listenerResourceData)
	if !ok {
		lw.resourceErrCh.Send(listenerUpdateErrTuple{resourceErr: fmt.Errorf("unexpected resource type: %T", update)})
		onDone()
		return
	}
	select {
	case <-lw.updateCh.C:
	default:
	}
	lw.updateCh.Send(listenerUpdateErrTuple{update: lisData.Resource})
	onDone()
}

func (lw *listenerWatcher) AmbientError(err error, onDone func()) {
	// When used with a go-control-plane management server that continuously
	// resends resources which are NACKed by the xDS client, using a `Replace()`
	// here and in OnResourceDoesNotExist() simplifies tests which will have
	// access to the most recently received error.
	lw.ambientErrCh.Replace(listenerUpdateErrTuple{ambientErr: err})
	onDone()
}

func (lw *listenerWatcher) ResourceError(err error, onDone func()) {
	lw.resourceErrCh.Replace(listenerUpdateErrTuple{resourceErr: err})
	onDone()
}

// badListenerResource returns a listener resource for the given name which does
// not contain the `RouteSpecifier` field in the HTTPConnectionManager, and
// hence is expected to be NACKed by the client.
func badListenerResource(t *testing.T, name string) *v3listenerpb.Listener {
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{})
	return &v3listenerpb.Listener{
		Name:        name,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
	}
}

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

// verifyListenerUpdate waits for a listenerUpdateErrTuple from the provided
// updateCh (typically the updateCh, resourceErrCh, or ambientErrCh of
// listenerWatcher) and verifies that it matches the expected wantUpdate tuple.
//
// It performs the following checks:
//   - Waits for an item on updateCh until the context deadline.
//   - If wantUpdate contains a resourceErr or ambientErr, it compares the
//     xdsresource.ErrorType of the received error with the expected error
//     type.
//   - If wantUpdate contains an update, it compares the received update with
//     the expected update, ignoring the Raw field.
//
// Returns an error if the context expires, or if the received tuple does not
// match the expected tuple according to the comparison logic.
func verifyListenerUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate listenerUpdateErrTuple) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for a listener resource from the management server: %v", err)
	}
	got := u.(listenerUpdateErrTuple)
	if wantUpdate.resourceErr != nil {
		if gotType, wantType := xdsresource.ErrType(got.resourceErr), xdsresource.ErrType(wantUpdate.resourceErr); gotType != wantType {
			return fmt.Errorf("received update with resource error type %v, want %v", gotType, wantType)
		}
	}
	if wantUpdate.ambientErr != nil {
		if gotType, wantType := xdsresource.ErrType(got.ambientErr), xdsresource.ErrType(wantUpdate.ambientErr); gotType != wantType {
			return fmt.Errorf("received update with ambient error type %v, want %v", gotType, wantType)
		}
	}
	cmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(listenerUpdate{}, "Raw"),
	}
	if diff := cmp.Diff(wantUpdate.update, got.update, cmpOpts...); diff != "" {
		return fmt.Errorf("received unexpected diff in the listener resource update: (-want, got):\n%s", diff)
	}
	return nil
}

func verifyListenerResourceError(ctx context.Context, updateCh *testutils.Channel, wantErr, wantNodeID string) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for a listener error from the management server: %v", err)
	}
	gotErr := u.(listenerUpdateErrTuple).resourceErr
	return verifyListenerError(ctx, gotErr, wantErr, wantNodeID)
}

func verifyListenerError(_ context.Context, gotErr error, wantErr, wantNodeID string) error {
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantErr) {
		return fmt.Errorf("update received with error: %v, want %q", gotErr, wantErr)
	}
	if !strings.Contains(gotErr.Error(), wantNodeID) {
		return fmt.Errorf("update received with error: %v, want error with node ID: %q", gotErr, wantNodeID)
	}
	return nil
}

func verifyAmbientErrorType(ctx context.Context, updateCh *testutils.Channel, wantErrType xdsresource.ErrorType, wantNodeID string) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for a listener error from the management server: %v", err)
	}
	gotErr := u.(listenerUpdateErrTuple).ambientErr
	return verifyErrorType(gotErr, wantErrType, wantNodeID)
}

func verifyResourceErrorType(ctx context.Context, updateCh *testutils.Channel, wantErrType xdsresource.ErrorType, wantNodeID string) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for a listener error from the management server: %v", err)
	}
	gotErr := u.(listenerUpdateErrTuple).resourceErr
	return verifyErrorType(gotErr, wantErrType, wantNodeID)
}

func verifyErrorType(gotErr error, wantErrType xdsresource.ErrorType, wantNodeID string) error {
	if got, want := xdsresource.ErrType(gotErr), wantErrType; got != want {
		return fmt.Errorf("update received with error %v of type: %v, want %v", gotErr, got, want)
	}
	if !strings.Contains(gotErr.Error(), wantNodeID) {
		return fmt.Errorf("update received with error: %v, want error with node ID: %q", gotErr, wantNodeID)
	}
	return nil
}

// TestLDSWatch covers the case where a single watcher exists for a single
// listener resource. The test verifies the following scenarios:
//  1. An update from the management server containing the resource being
//     watched should result in the invocation of the watch callback.
//  2. An update from the management server containing a resource *not* being
//     watched should not result in the invocation of the watch callback.
//  3. After the watch is cancelled, an update from the management server
//     containing the resource that was being watched should not result in the
//     invocation of the watch callback.
//
// The test is run for old and new style names.
func (s) TestLDSWatch(t *testing.T) {
	tests := []struct {
		desc                   string
		resourceName           string
		watchedResource        *v3listenerpb.Listener // The resource being watched.
		updatedWatchedResource *v3listenerpb.Listener // The watched resource after an update.
		notWatchedResource     *v3listenerpb.Listener // A resource which is not being watched.
		wantUpdate             listenerUpdateErrTuple
	}{
		{
			desc:                   "old style resource",
			resourceName:           ldsName,
			watchedResource:        e2e.DefaultClientListener(ldsName, rdsName),
			updatedWatchedResource: e2e.DefaultClientListener(ldsName, "new-rds-resource"),
			notWatchedResource:     e2e.DefaultClientListener("unsubscribed-lds-resource", rdsName),
			wantUpdate: listenerUpdateErrTuple{
				update: listenerUpdate{
					RouteConfigName: rdsName,
				},
			},
		},
		{
			desc:                   "new style resource",
			resourceName:           ldsNameNewStyle,
			watchedResource:        e2e.DefaultClientListener(ldsNameNewStyle, rdsNameNewStyle),
			updatedWatchedResource: e2e.DefaultClientListener(ldsNameNewStyle, "new-rds-resource"),
			notWatchedResource:     e2e.DefaultClientListener("unsubscribed-lds-resource", rdsNameNewStyle),
			wantUpdate: listenerUpdateErrTuple{
				update: listenerUpdate{
					RouteConfigName: rdsNameNewStyle,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

			nodeID := uuid.New().String()

			resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
			si := clients.ServerIdentifier{
				ServerURI:  mgmtServer.Address,
				Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
			}

			configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
			xdsClientConfig := xdsclient.Config{
				Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
				Node:             clients.Node{ID: nodeID},
				TransportBuilder: grpctransport.NewBuilder(configs),
				ResourceTypes:    resourceTypes,
				// Xdstp resource names used in this test do not specify an
				// authority. These will end up looking up an entry with the
				// empty key in the authorities map. Having an entry with an
				// empty key and empty configuration, results in these
				// resources also using the top-level configuration.
				Authorities: map[string]xdsclient.Authority{
					"": {XDSServers: []xdsclient.ServerConfig{}},
				},
			}

			// Create an xDS client with the above config.
			client, err := xdsclient.New(xdsClientConfig)
			if err != nil {
				t.Fatalf("Failed to create xDS client: %v", err)
			}
			defer client.Close()

			// Register a watch for a listener resource and have the watch
			// callback push the received update on to a channel.
			lw := newListenerWatcher()
			ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, test.resourceName, lw)

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
			if err := verifyListenerUpdate(ctx, lw.updateCh, test.wantUpdate); err != nil {
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
			if err := verifyNoListenerUpdate(ctx, lw.updateCh); err != nil {
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
			if err := verifyNoListenerUpdate(ctx, lw.updateCh); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLDSWatch_TwoWatchesForSameResourceName covers the case where two watchers
// exist for a single listener resource.  The test verifies the following
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
func (s) TestLDSWatch_TwoWatchesForSameResourceName(t *testing.T) {
	tests := []struct {
		desc                   string
		resourceName           string
		watchedResource        *v3listenerpb.Listener // The resource being watched.
		updatedWatchedResource *v3listenerpb.Listener // The watched resource after an update.
		wantUpdateV1           listenerUpdateErrTuple
		wantUpdateV2           listenerUpdateErrTuple
	}{
		{
			desc:                   "old style resource",
			resourceName:           ldsName,
			watchedResource:        e2e.DefaultClientListener(ldsName, rdsName),
			updatedWatchedResource: e2e.DefaultClientListener(ldsName, "new-rds-resource"),
			wantUpdateV1: listenerUpdateErrTuple{
				update: listenerUpdate{
					RouteConfigName: rdsName,
				},
			},
			wantUpdateV2: listenerUpdateErrTuple{
				update: listenerUpdate{
					RouteConfigName: "new-rds-resource",
				},
			},
		},
		{
			desc:                   "new style resource",
			resourceName:           ldsNameNewStyle,
			watchedResource:        e2e.DefaultClientListener(ldsNameNewStyle, rdsNameNewStyle),
			updatedWatchedResource: e2e.DefaultClientListener(ldsNameNewStyle, "new-rds-resource"),
			wantUpdateV1: listenerUpdateErrTuple{
				update: listenerUpdate{
					RouteConfigName: rdsNameNewStyle,
				},
			},
			wantUpdateV2: listenerUpdateErrTuple{
				update: listenerUpdate{
					RouteConfigName: "new-rds-resource",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

			nodeID := uuid.New().String()

			resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
			si := clients.ServerIdentifier{
				ServerURI:  mgmtServer.Address,
				Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
			}

			configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
			xdsClientConfig := xdsclient.Config{
				Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
				Node:             clients.Node{ID: nodeID},
				TransportBuilder: grpctransport.NewBuilder(configs),
				ResourceTypes:    resourceTypes,
				// Xdstp resource names used in this test do not specify an
				// authority. These will end up looking up an entry with the
				// empty key in the authorities map. Having an entry with an
				// empty key and empty configuration, results in these
				// resources also using the top-level configuration.
				Authorities: map[string]xdsclient.Authority{
					"": {XDSServers: []xdsclient.ServerConfig{}},
				},
			}

			// Create an xDS client with the above config.
			client, err := xdsclient.New(xdsClientConfig)
			if err != nil {
				t.Fatalf("Failed to create xDS client: %v", err)
			}
			defer client.Close()

			// Register two watches for the same listener resource and have the
			// callbacks push the received updates on to a channel.
			lw1 := newListenerWatcher()
			ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, test.resourceName, lw1)
			defer ldsCancel1()
			lw2 := newListenerWatcher()
			ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, test.resourceName, lw2)

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
			if err := verifyListenerUpdate(ctx, lw1.updateCh, test.wantUpdateV1); err != nil {
				t.Fatal(err)
			}
			if err := verifyListenerUpdate(ctx, lw2.updateCh, test.wantUpdateV1); err != nil {
				t.Fatal(err)
			}

			// Cancel the second watch and force the management server to push a
			// redundant update for the resource being watched. Neither of the
			// two watch callbacks should be invoked.
			ldsCancel2()
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
			}
			if err := verifyNoListenerUpdate(ctx, lw1.updateCh); err != nil {
				t.Fatal(err)
			}
			if err := verifyNoListenerUpdate(ctx, lw2.updateCh); err != nil {
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
			if err := verifyListenerUpdate(ctx, lw1.updateCh, test.wantUpdateV2); err != nil {
				t.Fatal(err)
			}
			if err := verifyNoListenerUpdate(ctx, lw2.updateCh); err != nil {
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
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()
	authority := makeAuthorityName(t.Name())

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp style resource names used in this test use a slash removed
		// version of t.Name as their authority, and the empty config
		// results in the top-level xds server configuration being used for
		// this authority.
		Authorities: map[string]xdsclient.Authority{
			authority: {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for the same listener resource and have the
	// callbacks push the received updates on to a channel.
	lw1 := newListenerWatcher()
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw1)
	defer ldsCancel1()
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw2)
	defer ldsCancel2()

	// Register the third watch for a different listener resource.
	ldsNameNewStyle := makeNewStyleLDSName(authority)
	lw3 := newListenerWatcher()
	ldsCancel3 := client.WatchResource(xdsresource.V3ListenerURL, ldsNameNewStyle, lw3)
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
	wantUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw1.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, lw2.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, lw3.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_ResourceCaching covers the case where a watch is registered for
// a resource which is already present in the cache.  The test verifies that the
// watch callback is invoked with the contents from the cache, instead of a
// request being sent to the management server.
func (s) TestLDSWatch_ResourceCaching(t *testing.T) {
	firstRequestReceived := false
	firstAckReceived := syncutil.NewEvent()
	secondRequestReceived := syncutil.NewEvent()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
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

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for a listener resource and have the watch
	// callback push the received update on to a channel.
	lw1 := newListenerWatcher()
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw1)
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
	wantUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw1.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("timeout when waiting for receipt of ACK at the management server")
	case <-firstAckReceived.Done():
	}

	// Register another watch for the same resource. This should get the update
	// from the cache.
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw2)
	defer ldsCancel2()
	if err := verifyListenerUpdate(ctx, lw2.updateCh, wantUpdate); err != nil {
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

// TestLDSWatch_ExpiryTimerFiresBeforeResponse tests the case where the client
// does not receive an LDS response for the request that it sends. The test
// verifies that the watch callback is invoked with an error once the
// watchExpiryTimer fires.
func (s) TestLDSWatch_ExpiryTimerFiresBeforeResponse(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Override the default watch expiry timeout.
		WatchExpiryTimeout: defaultTestWatchExpiryTimeout,
	}

	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for a resource which is expected to fail with an error
	// after the watch expiry timer fires.
	lw := newListenerWatcher()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw)
	defer ldsCancel()

	// Wait for the watch expiry timer to fire.
	<-time.After(defaultTestWatchExpiryTimeout)

	// Verify that an empty update with the expected resource error is
	// received.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	wantErr := xdsresource.NewError(xdsresource.ErrorTypeResourceNotFound, "")
	if err := verifyListenerUpdate(ctx, lw.resourceErrCh, listenerUpdateErrTuple{resourceErr: wantErr}); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_ValidResponseCancelsExpiryTimerBehavior tests the case where the
// client receives a valid LDS response for the request that it sends. The test
// verifies that the behavior associated with the expiry timer (i.e, callback
// invocation with error) does not take place.
func (s) TestLDSWatch_ValidResponseCancelsExpiryTimerBehavior(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Override the default watch expiry timeout.
		WatchExpiryTimeout: defaultTestWatchExpiryTimeout,
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for a listener resource and have the watch
	// callback push the received update on to a channel.
	lw := newListenerWatcher()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw)
	defer ldsCancel()

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
	wantUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Wait for the watch expiry timer to fire, and verify that the callback is
	// not invoked.
	<-time.After(defaultTestWatchExpiryTimeout)
	if err := verifyNoListenerUpdate(ctx, lw.updateCh); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_ResourceRemoved covers the cases where a resource being watched
// is removed from the management server. The test verifies the following
// scenarios:
//  1. Removing a resource should trigger the watch callback with a resource
//     removed error. It should not trigger the watch callback for an unrelated
//     resource.
//  2. An update to another resource should result in the invocation of the watch
//     callback associated with that resource.  It should not result in the
//     invocation of the watch callback associated with the deleted resource.
//
// The test is run with both old and new style names.
func (s) TestLDSWatch_ResourceRemoved(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()
	authority := makeAuthorityName(t.Name())

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp style resource names used in this test use a slash removed
		// version of t.Name as their authority, and the empty config
		// results in the top-level xds server configuration being used for
		// this authority.
		Authorities: map[string]xdsclient.Authority{
			authority: {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for two listener resources and have the
	// callbacks push the received updates on to a channel.
	resourceName1 := ldsName
	lw1 := newListenerWatcher()
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, resourceName1, lw1)
	defer ldsCancel1()

	resourceName2 := makeNewStyleLDSName(authority)
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, resourceName2, lw2)
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
	wantUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw1.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, lw2.updateCh, wantUpdate); err != nil {
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

	// The first watcher should receive a resource error for resource removal,
	// while the second watcher should not see an update.
	if err := verifyListenerUpdate(ctx, lw1.resourceErrCh, listenerUpdateErrTuple{
		resourceErr: xdsresource.NewError(xdsresource.ErrorTypeResourceNotFound, ""),
	}); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoListenerUpdate(ctx, lw2.updateCh); err != nil {
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
	if err := verifyNoListenerUpdate(ctx, lw1.updateCh); err != nil {
		t.Fatal(err)
	}
	wantUpdate = listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: "new-rds-resource",
		},
	}
	if err := verifyListenerUpdate(ctx, lw2.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_NewWatcherForRemovedResource covers the case where a new
// watcher registers for a resource that has been removed. The test verifies
// the following scenarios:
//  1. When a resource is deleted by the management server, any active
//     watchers of that resource should be notified with a "resource removed"
//     error through their watch callback.
//  2. If a new watcher attempts to register for a resource that has already
//     been deleted, its watch callback should be immediately invoked with a
//     "resource removed" error.
func (s) TestLDSWatch_NewWatcherForRemovedResource(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register watch for the listener resource and have the
	// callbacks push the received updates on to a channel.
	lw1 := newListenerWatcher()
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw1)
	defer ldsCancel1()

	// Configure the management server to return listener resource,
	// corresponding to the registered watch.
	resource := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resource); err != nil {
		t.Fatalf("Failed to update management server with resource: %v, err: %v", resource, err)
	}

	// Verify the contents of the received update for existing watch.
	wantUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw1.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Remove the listener resource on the management server.
	resource = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resource); err != nil {
		t.Fatalf("Failed to update management server with resource: %v, err: %v", resource, err)
	}

	// The existing watcher should receive a resource error for resource
	// removal.
	updateError := listenerUpdateErrTuple{resourceErr: xdsresource.NewError(xdsresource.ErrorTypeResourceNotFound, "")}
	if err := verifyListenerUpdate(ctx, lw1.resourceErrCh, updateError); err != nil {
		t.Fatal(err)
	}

	// New watchers attempting to register for a deleted resource should also
	// receive a "resource removed" error.
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw2)
	defer ldsCancel2()
	if err := verifyListenerUpdate(ctx, lw2.resourceErrCh, updateError); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_NACKError covers the case where an update from the management
// server is NACKed by the xdsclient. The test verifies that the error is
// propagated to the existing watcher. After NACK, if a new watcher registers
// for the resource, error is propagated to the new watcher as well.
func (s) TestLDSWatch_NACKError(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for a listener resource and have the watch
	// callback push the received update on to a channel.
	lw := newListenerWatcher()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw)
	defer ldsCancel()

	// Configure the management server to return a single listener resource
	// which is expected to be NACKed by the client.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{badListenerResource(t, ldsName)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify that the expected error is propagated to the existing watcher.
	// Since the resource is not cached, it should be received as resource
	// error.
	if err := verifyResourceErrorType(ctx, lw.resourceErrCh, xdsresource.ErrorTypeNACKed, nodeID); err != nil {
		t.Fatal(err)
	}

	// Verify that the expected error is propagated to the new watcher as well.
	// Since the resource is not cached, it should be received as resource
	// error.
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw2)
	defer ldsCancel2()
	if err := verifyResourceErrorType(ctx, lw2.resourceErrCh, xdsresource.ErrorTypeNACKed, nodeID); err != nil {
		t.Fatal(err)
	}
}

// Tests the scenario where a watch registered for a resource results in a good
// update followed by a bad update. This results in the resource cache
// containing both the old good update and the latest NACK error. The test
// verifies that a when a new watch is registered for the same resource, the new
// watcher receives the good update followed by the NACK error.
func (s) TestLDSWatch_ResourceCaching_NACKError(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register a watch for a listener resource and have the watch
	// callback push the received update on to a channel.
	lw1 := newListenerWatcher()
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw1)
	defer ldsCancel1()

	// Configure the management server to return a single listener
	// resource, corresponding to the one we registered a watch for.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1000*defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update.
	wantUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw1.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Configure the management server to return a single listener resource
	// which is expected to be NACKed by the client.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{badListenerResource(t, ldsName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify that the expected error is propagated to the existing watcher.
	// Since the resource is cached, it should be received as ambient error.
	if err := verifyAmbientErrorType(ctx, lw1.ambientErrCh, xdsresource.ErrorTypeNACKed, nodeID); err != nil {
		t.Fatal(err)
	}

	// Register another watch for the same resource. This should get the update
	// and error from the cache.
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw2)
	defer ldsCancel2()
	if err := verifyListenerUpdate(ctx, lw2.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
	// Verify that the expected error is propagated to the existing watcher.
	// Since the resource is cached, it should be received as ambient error.
	if err := verifyAmbientErrorType(ctx, lw2.ambientErrCh, xdsresource.ErrorTypeNACKed, nodeID); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_PartialValid covers the case where a response from the
// management server contains both valid and invalid resources and is expected
// to be NACKed by the xdsclient. The test verifies that watchers corresponding
// to the valid resource receive the update, while watchers corresponding to the
// invalid resource receive an error.
func (s) TestLDSWatch_PartialValid(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()
	authority := makeAuthorityName(t.Name())

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp style resource names used in this test use a slash removed
		// version of t.Name as their authority, and the empty config
		// results in the top-level xds server configuration being used for
		// this authority.
		Authorities: map[string]xdsclient.Authority{
			authority: {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
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
	lw1 := newListenerWatcher()
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, badResourceName, lw1)
	defer ldsCancel1()
	goodResourceName := makeNewStyleLDSName(authority)
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, goodResourceName, lw2)
	defer ldsCancel2()

	// Configure the management server with two listener resources. One of these
	// is a bad resource causing the update to be NACKed.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			badListenerResource(t, badResourceName),
			e2e.DefaultClientListener(goodResourceName, rdsName),
		},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify that the expected error is propagated to the watcher which
	// requested for the bad resource.
	// Verify that the expected error is propagated to the existing watcher.
	// Since the resource is not cached, it should be received as resource
	// error.
	if err := verifyResourceErrorType(ctx, lw1.resourceErrCh, xdsresource.ErrorTypeNACKed, nodeID); err != nil {
		t.Fatal(err)
	}

	// Verify that the watcher watching the good resource receives a good
	// update.
	wantUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw2.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatch_PartialResponse covers the case where a response from the
// management server does not contain all requested resources. LDS responses are
// supposed to contain all requested resources, and the absence of one usually
// indicates that the management server does not know about it. In cases where
// the server has never responded with this resource before, the xDS client is
// expected to wait for the watch timeout to expire before concluding that the
// resource does not exist on the server
func (s) TestLDSWatch_PartialResponse(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()
	authority := makeAuthorityName(t.Name())

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp style resource names used in this test use a slash removed
		// version of t.Name as their authority, and the empty config
		// results in the top-level xds server configuration being used for
		// this authority.
		Authorities: map[string]xdsclient.Authority{
			authority: {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Register two watches for two listener resources and have the
	// callbacks push the received updates on to a channel.
	resourceName1 := ldsName
	lw1 := newListenerWatcher()
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, resourceName1, lw1)
	defer ldsCancel1()

	resourceName2 := makeNewStyleLDSName(authority)
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, resourceName2, lw2)
	defer ldsCancel2()

	// Configure the management server to return only one of the two listener
	// resources, corresponding to the registered watches.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(resourceName1, rdsName),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update for first watcher.
	wantUpdate1 := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw1.updateCh, wantUpdate1); err != nil {
		t.Fatal(err)
	}

	// Verify that the second watcher does not get an update with an error.
	if err := verifyNoListenerUpdate(ctx, lw2.updateCh); err != nil {
		t.Fatal(err)
	}

	// Configure the management server to return two listener resources,
	// corresponding to the registered watches.
	resources = e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(resourceName1, rdsName),
			e2e.DefaultClientListener(resourceName2, rdsName),
		},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify the contents of the received update for the second watcher.
	wantUpdate2 := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw2.updateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}

	// Verify that the first watcher gets no update, as the first resource did
	// not change.
	if err := verifyNoListenerUpdate(ctx, lw1.updateCh); err != nil {
		t.Fatal(err)
	}
}
