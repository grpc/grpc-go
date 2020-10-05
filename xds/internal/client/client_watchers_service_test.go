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

package client

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc/internal/testutils"
)

type serviceUpdateErr struct {
	u   ServiceUpdate
	err error
}

// TestServiceWatch covers the cases:
// - an update is received after a watch()
// - an update with routes received
func (s) TestServiceWatch(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := New(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	serviceUpdateCh := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ServiceUpdate{Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}},
	})
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	wantUpdate2 := ServiceUpdate{
		Routes: []*Route{{
			Prefix: newStringP(""),
			Action: map[string]uint32{testCDSName: 1},
		}},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {
			Routes: []*Route{{
				Prefix: newStringP(""),
				Action: map[string]uint32{testCDSName: 1},
			}},
		},
	})
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}

// TestServiceWatchLDSUpdate covers the case that after first LDS and first RDS
// response, the second LDS response trigger an new RDS watch, and an update of
// the old RDS watch doesn't trigger update to service callback.
func (s) TestServiceWatchLDSUpdate(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := New(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	serviceUpdateCh := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ServiceUpdate{Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}},
	})
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another LDS update with a different RDS_name.
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName + "2"}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	// Another update for the old name.
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}},
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := serviceUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected serviceUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// RDS update for the new name.
	wantUpdate2 := ServiceUpdate{Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName + "2": 1}}}}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName + "2": {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName + "2": 1}}}},
	})
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}

// TestServiceWatchSecond covers the case where a second WatchService() gets an
// error (because only one is allowed). But the first watch still receives
// updates.
func (s) TestServiceWatchSecond(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := New(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	serviceUpdateCh := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ServiceUpdate{Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}},
	})
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Call WatchService() again, with the same or different name.
	serviceUpdateCh2 := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh2.Send(serviceUpdateErr{u: update, err: err})
	})
	u, err := serviceUpdateCh2.Receive(ctx)
	if err != nil {
		t.Fatalf("failed to get serviceUpdate: %v", err)
	}
	uu := u.(serviceUpdateErr)
	if !cmp.Equal(uu.u, ServiceUpdate{}) {
		t.Errorf("unexpected serviceUpdate: %v, want %v", uu.u, ServiceUpdate{})
	}
	if uu.err == nil {
		t.Errorf("unexpected serviceError: <nil>, want error watcher timeout")
	}

	// Send update again, first callback should be called, second should
	// timeout.
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}},
	})
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := serviceUpdateCh2.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected serviceUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestServiceWatchWithNoResponseFromServer tests the case where the xDS server
// does not respond to the requests being sent out as part of registering a
// service update watcher. The callback will get an error.
func (s) TestServiceWatchWithNoResponseFromServer(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := New(clientOpts(testXDSServer, true))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	serviceUpdateCh := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	u, err := serviceUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("failed to get serviceUpdate: %v", err)
	}
	uu := u.(serviceUpdateErr)
	if !cmp.Equal(uu.u, ServiceUpdate{}) {
		t.Errorf("unexpected serviceUpdate: %v, want %v", uu.u, ServiceUpdate{})
	}
	if uu.err == nil {
		t.Errorf("unexpected serviceError: <nil>, want error watcher timeout")
	}
}

// TestServiceWatchEmptyRDS tests the case where the underlying apiClient
// receives an empty RDS response. The callback will get an error.
func (s) TestServiceWatchEmptyRDS(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := New(clientOpts(testXDSServer, true))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	serviceUpdateCh := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	client.NewRouteConfigs(map[string]RouteConfigUpdate{})
	u, err := serviceUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("failed to get serviceUpdate: %v", err)
	}
	uu := u.(serviceUpdateErr)
	if !cmp.Equal(uu.u, ServiceUpdate{}) {
		t.Errorf("unexpected serviceUpdate: %v, want %v", uu.u, ServiceUpdate{})
	}
	if uu.err == nil {
		t.Errorf("unexpected serviceError: <nil>, want error watcher timeout")
	}
}

// TestServiceWatchWithClientClose tests the case where xDS responses are
// received after the client is closed, and we make sure that the registered
// watcher callback is not invoked.
func (s) TestServiceWatchWithClientClose(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := New(clientOpts(testXDSServer, true))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	serviceUpdateCh := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	// Client is closed before it receives the RDS response.
	client.Close()
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := serviceUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected serviceUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestServiceNotCancelRDSOnSameLDSUpdate covers the case that if the second LDS
// update contains the same RDS name as the previous, the RDS watch isn't
// canceled and restarted.
func (s) TestServiceNotCancelRDSOnSameLDSUpdate(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := New(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	serviceUpdateCh := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}},
	})

	wantUpdate := ServiceUpdate{Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}}
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another LDS update with a the same RDS_name.
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := apiClient.removeWatches[RouteConfigResource].Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("unexpected rds watch cancel")
	}
}

// TestServiceResourceRemoved covers the cases:
// - an update is received after a watch()
// - another update is received, with one resource removed
//   - this should trigger callback with resource removed error
// - one more update without the removed resource
//   - the callback (above) shouldn't receive any update
func (s) TestServiceResourceRemoved(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := New(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	serviceUpdateCh := testutils.NewChannel()
	client.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}},
	})
	wantUpdate := ServiceUpdate{Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}}}
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Remove LDS resource, should cancel the RDS watch, and trigger resource
	// removed error.
	client.NewListeners(map[string]ListenerUpdate{})
	if _, err := apiClient.removeWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want watch to be canceled, got error %v", err)
	}
	if u, err := serviceUpdateCh.Receive(ctx); err != nil || ErrType(u.(serviceUpdateErr).err) != ErrorTypeResourceNotFound {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// Send RDS update for the removed LDS resource, expect no updates to
	// callback, because RDS should be canceled.
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName + "new": 1}}}},
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := serviceUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected serviceUpdate: %v, want receiving from channel timeout", u)
	}

	// Add LDS resource, but not RDS resource, should
	//  - start a new RDS watch
	//  - timeout on service channel, because RDS cache was cleared
	client.NewListeners(map[string]ListenerUpdate{testLDSName: {RouteConfigName: testRDSName}})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := serviceUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected serviceUpdate: %v, want receiving from channel timeout", u)
	}

	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName + "new2": 1}}}},
	})
	wantUpdate = ServiceUpdate{Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName + "new2": 1}}}}
	if err := verifyServiceUpdate(ctx, serviceUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}
}
