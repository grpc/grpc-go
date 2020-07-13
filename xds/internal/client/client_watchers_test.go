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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/version"
)

type clusterUpdateErr struct {
	u   ClusterUpdate
	err error
}

// TestClusterWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name
// - an upate is received after cancel()
func (s) TestClusterWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	// TODO: add a timeout to this recv.
	// Note that this won't be necessary if we finish the TODO below to call
	// Client directly instead of v2Client.r.
	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	cancelWatch := c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	// This is calling v2Client.r to send the update, but r is set to Client, so
	// this is same as calling Client to update. The one thing this covers is
	// that `NewXDSV2Client` is called with the right parent.
	//
	// TODO: in a future cleanup, this (and the same thing in other tests) can
	// be changed call Client directly.
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another update, with an extra resource for a different resource name.
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName:  wantUpdate,
		"randomName": {},
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected clusterUpdate: %+v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestClusterTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestClusterTwoWatchSameResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var clusterUpdateChs []*testutils.Channel
	const count = 2

	var cancelLastWatch func()

	for i := 0; i < count; i++ {
		clusterUpdateCh := testutils.NewChannel()
		clusterUpdateChs = append(clusterUpdateChs, clusterUpdateCh)
		cancelLastWatch = c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
			clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := clusterUpdateChs[i].Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
			t.Errorf("i=%v, unexpected clusterUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := clusterUpdateChs[i].Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
			t.Errorf("i=%v, unexpected clusterUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := clusterUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestClusterThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestClusterThreeWatchDifferentResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var clusterUpdateChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		clusterUpdateCh := testutils.NewChannel()
		clusterUpdateChs = append(clusterUpdateChs, clusterUpdateCh)
		c.WatchCluster(testCDSName+"1", func(update ClusterUpdate, err error) {
			clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	// Third watch for a different name.
	clusterUpdateCh2 := testutils.NewChannel()
	c.WatchCluster(testCDSName+"2", func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ClusterUpdate{ServiceName: testEDSName + "1"}
	wantUpdate2 := ClusterUpdate{ServiceName: testEDSName + "2"}
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName + "1": wantUpdate1,
		testCDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := clusterUpdateChs[i].Receive(); err != nil || u != (clusterUpdateErr{wantUpdate1, nil}) {
			t.Errorf("i=%v, unexpected clusterUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := clusterUpdateCh2.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestClusterWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestClusterWatchAfterCache(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another watch for the resource in cache.
	clusterUpdateCh2 := testutils.NewChannel()
	c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(clusterUpdateErr{u: update, err: err})
	})
	if n, err := v2Client.addWatches[version.V2ClusterURL].Receive(); err == nil {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if u, err := clusterUpdateCh2.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Old watch should see nothing.
	if u, err := clusterUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestClusterWatchExpiryTimer tests the case where the client does not receive
// an CDS response for the request that it sends out. We want the watch callback
// to be invoked with an error once the watchExpiryTimer fires.
func (s) TestClusterWatchExpiryTimer(t *testing.T) {
	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	c.WatchCluster(testCDSName, func(u ClusterUpdate, err error) {
		clusterUpdateCh.Send(clusterUpdateErr{u: u, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	u, err := clusterUpdateCh.TimedReceive(defaultWatchExpiryTimeout * 2)
	if err != nil {
		t.Fatalf("failed to get clusterUpdate: %v", err)
	}
	uu := u.(clusterUpdateErr)
	if uu.u != (ClusterUpdate{}) {
		t.Errorf("unexpected clusterUpdate: %v, want %v", uu.u, ClusterUpdate{})
	}
	if uu.err == nil {
		t.Errorf("unexpected clusterError: <nil>, want error watcher timeout")
	}
}

// TestClusterWatchExpiryTimerStop tests the case where the client does receive
// an CDS response for the request that it sends out. We want no error even
// after expiry timeout.
func (s) TestClusterWatchExpiryTimerStop(t *testing.T) {
	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	c.WatchCluster(testCDSName, func(u ClusterUpdate, err error) {
		clusterUpdateCh.Send(clusterUpdateErr{u: u, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Wait for an error, the error should never happen.
	u, err := clusterUpdateCh.TimedReceive(defaultWatchExpiryTimeout * 2)
	if err != testutils.ErrRecvTimeout {
		t.Fatalf("got unexpected: %v, %v, want recv timeout", u.(clusterUpdateErr).u, u.(clusterUpdateErr).err)
	}
}

// TestClusterResourceRemoved covers the cases:
// - an update is received after a watch()
// - another update is received, with one resource removed
//   - this should trigger callback with resource removed error
// - one more update without the removed resource
//   - the callback (above) shouldn't receive any update
func (s) TestClusterResourceRemoved(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh1 := testutils.NewChannel()
	c.WatchCluster(testCDSName+"1", func(update ClusterUpdate, err error) {
		clusterUpdateCh1.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	// Another watch for a different name.
	clusterUpdateCh2 := testutils.NewChannel()
	c.WatchCluster(testCDSName+"2", func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ClusterURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ClusterUpdate{ServiceName: testEDSName + "1"}
	wantUpdate2 := ClusterUpdate{ServiceName: testEDSName + "2"}
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName + "1": wantUpdate1,
		testCDSName + "2": wantUpdate2,
	})

	if u, err := clusterUpdateCh1.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate1, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	if u, err := clusterUpdateCh2.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Send another update to remove resource 1.
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName + "2": wantUpdate2,
	})

	// watcher 1 should get an error.
	if u, err := clusterUpdateCh1.Receive(); err != nil || ErrType(u.(clusterUpdateErr).err) != ErrorTypeResourceNotFound {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// watcher 2 should get the same update again.
	if u, err := clusterUpdateCh2.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Send one more update without resource 1.
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName + "2": wantUpdate2,
	})

	// watcher 1 should get an error.
	if u, err := clusterUpdateCh1.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterUpdate: %v, want receiving from channel timeout", u)
	}

	// watcher 2 should get the same update again.
	if u, err := clusterUpdateCh2.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
}

var (
	testLocalities = []Locality{
		{
			Endpoints: []Endpoint{{Address: "addr1:314"}},
			ID:        internal.LocalityID{SubZone: "locality-1"},
			Priority:  1,
			Weight:    1,
		},
		{
			Endpoints: []Endpoint{{Address: "addr2:159"}},
			ID:        internal.LocalityID{SubZone: "locality-2"},
			Priority:  0,
			Weight:    1,
		},
	}
	endpointsCmpOpts = []cmp.Option{cmp.AllowUnexported(endpointsUpdateErr{}), cmpopts.EquateEmpty()}
)

type endpointsUpdateErr struct {
	u   EndpointsUpdate
	err error
}

// TestEndpointsWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name (which doesn't trigger callback)
// - an upate is received after cancel()
func (s) TestEndpointsWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	endpointsUpdateCh := testutils.NewChannel()
	cancelWatch := c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2EndpointsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.NewEndpoints(map[string]EndpointsUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := endpointsUpdateCh.Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate, nil}, endpointsCmpOpts...) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another update for a different resource name.
	v2Client.r.NewEndpoints(map[string]EndpointsUpdate{
		"randomName": {},
	})

	if u, err := endpointsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.NewEndpoints(map[string]EndpointsUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := endpointsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestEndpointsTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestEndpointsTwoWatchSameResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var endpointsUpdateChs []*testutils.Channel
	const count = 2

	var cancelLastWatch func()

	for i := 0; i < count; i++ {
		endpointsUpdateCh := testutils.NewChannel()
		endpointsUpdateChs = append(endpointsUpdateChs, endpointsUpdateCh)
		cancelLastWatch = c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
			endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[version.V2EndpointsURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.NewEndpoints(map[string]EndpointsUpdate{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := endpointsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate, nil}, endpointsCmpOpts...) {
			t.Errorf("i=%v, unexpected endpointsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.NewEndpoints(map[string]EndpointsUpdate{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := endpointsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate, nil}, endpointsCmpOpts...) {
			t.Errorf("i=%v, unexpected endpointsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := endpointsUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestEndpointsThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestEndpointsThreeWatchDifferentResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var endpointsUpdateChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		endpointsUpdateCh := testutils.NewChannel()
		endpointsUpdateChs = append(endpointsUpdateChs, endpointsUpdateCh)
		c.WatchEndpoints(testCDSName+"1", func(update EndpointsUpdate, err error) {
			endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[version.V2EndpointsURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	// Third watch for a different name.
	endpointsUpdateCh2 := testutils.NewChannel()
	c.WatchEndpoints(testCDSName+"2", func(update EndpointsUpdate, err error) {
		endpointsUpdateCh2.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2EndpointsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	wantUpdate2 := EndpointsUpdate{Localities: []Locality{testLocalities[1]}}
	v2Client.r.NewEndpoints(map[string]EndpointsUpdate{
		testCDSName + "1": wantUpdate1,
		testCDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := endpointsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate1, nil}, endpointsCmpOpts...) {
			t.Errorf("i=%v, unexpected endpointsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := endpointsUpdateCh2.Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate2, nil}, endpointsCmpOpts...) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestEndpointsWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestEndpointsWatchAfterCache(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	endpointsUpdateCh := testutils.NewChannel()
	c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2EndpointsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.NewEndpoints(map[string]EndpointsUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := endpointsUpdateCh.Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate, nil}, endpointsCmpOpts...) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another watch for the resource in cache.
	endpointsUpdateCh2 := testutils.NewChannel()
	c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh2.Send(endpointsUpdateErr{u: update, err: err})
	})
	if n, err := v2Client.addWatches[version.V2EndpointsURL].Receive(); err == nil {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if u, err := endpointsUpdateCh2.Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate, nil}, endpointsCmpOpts...) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Old watch should see nothing.
	if u, err := endpointsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestEndpointsWatchExpiryTimer tests the case where the client does not receive
// an CDS response for the request that it sends out. We want the watch callback
// to be invoked with an error once the watchExpiryTimer fires.
func (s) TestEndpointsWatchExpiryTimer(t *testing.T) {
	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	endpointsUpdateCh := testutils.NewChannel()
	c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2EndpointsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	u, err := endpointsUpdateCh.TimedReceive(defaultWatchExpiryTimeout * 2)
	if err != nil {
		t.Fatalf("failed to get endpointsUpdate: %v", err)
	}
	uu := u.(endpointsUpdateErr)
	if !cmp.Equal(uu.u, EndpointsUpdate{}, endpointsCmpOpts...) {
		t.Errorf("unexpected endpointsUpdate: %v, want %v", uu.u, EndpointsUpdate{})
	}
	if uu.err == nil {
		t.Errorf("unexpected endpointsError: <nil>, want error watcher timeout")
	}
}

type ldsUpdateErr struct {
	u   ListenerUpdate
	err error
}

// TestLDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name
// - an upate is received after cancel()
func (s) TestLDSWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	ldsUpdateCh := testutils.NewChannel()
	cancelWatch := c.watchLDS(testLDSName, func(update ListenerUpdate, err error) {
		ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ListenerUpdate{RouteConfigName: testRDSName}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: wantUpdate,
	})

	if u, err := ldsUpdateCh.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another update, with an extra resource for a different resource name.
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName:  wantUpdate,
		"randomName": {},
	})

	if u, err := ldsUpdateCh.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: wantUpdate,
	})

	if u, err := ldsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestLDSTwoWatchSameResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var ldsUpdateChs []*testutils.Channel
	const count = 2

	var cancelLastWatch func()

	for i := 0; i < count; i++ {
		ldsUpdateCh := testutils.NewChannel()
		ldsUpdateChs = append(ldsUpdateChs, ldsUpdateCh)
		cancelLastWatch = c.watchLDS(testLDSName, func(update ListenerUpdate, err error) {
			ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	wantUpdate := ListenerUpdate{RouteConfigName: testRDSName}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := ldsUpdateChs[i].Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
			t.Errorf("i=%v, unexpected ListenerUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := ldsUpdateChs[i].Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
			t.Errorf("i=%v, unexpected ListenerUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := ldsUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestLDSThreeWatchDifferentResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var ldsUpdateChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		ldsUpdateCh := testutils.NewChannel()
		ldsUpdateChs = append(ldsUpdateChs, ldsUpdateCh)
		c.watchLDS(testLDSName+"1", func(update ListenerUpdate, err error) {
			ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	// Third watch for a different name.
	ldsUpdateCh2 := testutils.NewChannel()
	c.watchLDS(testLDSName+"2", func(update ListenerUpdate, err error) {
		ldsUpdateCh2.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ListenerUpdate{RouteConfigName: testRDSName + "1"}
	wantUpdate2 := ListenerUpdate{RouteConfigName: testRDSName + "2"}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName + "1": wantUpdate1,
		testLDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := ldsUpdateChs[i].Receive(); err != nil || u != (ldsUpdateErr{wantUpdate1, nil}) {
			t.Errorf("i=%v, unexpected ListenerUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := ldsUpdateCh2.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestLDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestLDSWatchAfterCache(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	ldsUpdateCh := testutils.NewChannel()
	c.watchLDS(testLDSName, func(update ListenerUpdate, err error) {
		ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ListenerUpdate{RouteConfigName: testRDSName}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: wantUpdate,
	})

	if u, err := ldsUpdateCh.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another watch for the resource in cache.
	ldsUpdateCh2 := testutils.NewChannel()
	c.watchLDS(testLDSName, func(update ListenerUpdate, err error) {
		ldsUpdateCh2.Send(ldsUpdateErr{u: update, err: err})
	})
	if n, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err == nil {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if u, err := ldsUpdateCh2.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Old watch should see nothing.
	if u, err := ldsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSResourceRemoved covers the cases:
// - an update is received after a watch()
// - another update is received, with one resource removed
//   - this should trigger callback with resource removed error
// - one more update without the removed resource
//   - the callback (above) shouldn't receive any update
func (s) TestLDSResourceRemoved(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	ldsUpdateCh1 := testutils.NewChannel()
	c.watchLDS(testLDSName+"1", func(update ListenerUpdate, err error) {
		ldsUpdateCh1.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	// Another watch for a different name.
	ldsUpdateCh2 := testutils.NewChannel()
	c.watchLDS(testLDSName+"2", func(update ListenerUpdate, err error) {
		ldsUpdateCh2.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ListenerUpdate{RouteConfigName: testEDSName + "1"}
	wantUpdate2 := ListenerUpdate{RouteConfigName: testEDSName + "2"}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName + "1": wantUpdate1,
		testLDSName + "2": wantUpdate2,
	})

	if u, err := ldsUpdateCh1.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate1, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v", u, err)
	}

	if u, err := ldsUpdateCh2.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Send another update to remove resource 1.
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName + "2": wantUpdate2,
	})

	// watcher 1 should get an error.
	if u, err := ldsUpdateCh1.Receive(); err != nil || ErrType(u.(ldsUpdateErr).err) != ErrorTypeResourceNotFound {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// watcher 2 should get the same update again.
	if u, err := ldsUpdateCh2.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Send one more update without resource 1.
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName + "2": wantUpdate2,
	})

	// watcher 1 should get an error.
	if u, err := ldsUpdateCh1.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected ListenerUpdate: %v, want receiving from channel timeout", u)
	}

	// watcher 2 should get the same update again.
	if u, err := ldsUpdateCh2.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v", u, err)
	}
}

type rdsUpdateErr struct {
	u   RouteConfigUpdate
	err error
}

// TestRDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name (which doesn't trigger callback)
// - an upate is received after cancel()
func (s) TestRDSWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	rdsUpdateCh := testutils.NewChannel()
	cancelWatch := c.watchRDS(testRDSName, func(update RouteConfigUpdate, err error) {
		rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := RouteConfigUpdate{WeightedCluster: map[string]uint32{testCDSName: 1}}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdateErr{})) {
		t.Errorf("unexpected RouteConfigUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another update for a different resource name.
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		"randomName": {},
	})

	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestRDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestRDSTwoWatchSameResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var rdsUpdateChs []*testutils.Channel
	const count = 2

	var cancelLastWatch func()

	for i := 0; i < count; i++ {
		rdsUpdateCh := testutils.NewChannel()
		rdsUpdateChs = append(rdsUpdateChs, rdsUpdateCh)
		cancelLastWatch = c.watchRDS(testRDSName, func(update RouteConfigUpdate, err error) {
			rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	wantUpdate := RouteConfigUpdate{WeightedCluster: map[string]uint32{testCDSName: 1}}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdateErr{})) {
			t.Errorf("i=%v, unexpected RouteConfigUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdateErr{})) {
			t.Errorf("i=%v, unexpected RouteConfigUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := rdsUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestRDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestRDSThreeWatchDifferentResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var rdsUpdateChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		rdsUpdateCh := testutils.NewChannel()
		rdsUpdateChs = append(rdsUpdateChs, rdsUpdateCh)
		c.watchRDS(testRDSName+"1", func(update RouteConfigUpdate, err error) {
			rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	// Third watch for a different name.
	rdsUpdateCh2 := testutils.NewChannel()
	c.watchRDS(testRDSName+"2", func(update RouteConfigUpdate, err error) {
		rdsUpdateCh2.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := RouteConfigUpdate{WeightedCluster: map[string]uint32{testCDSName + "1": 1}}
	wantUpdate2 := RouteConfigUpdate{WeightedCluster: map[string]uint32{testCDSName + "2": 1}}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName + "1": wantUpdate1,
		testRDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate1, nil}, cmp.AllowUnexported(rdsUpdateErr{})) {
			t.Errorf("i=%v, unexpected RouteConfigUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := rdsUpdateCh2.Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate2, nil}, cmp.AllowUnexported(rdsUpdateErr{})) {
		t.Errorf("unexpected RouteConfigUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestRDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestRDSWatchAfterCache(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	rdsUpdateCh := testutils.NewChannel()
	c.watchRDS(testRDSName, func(update RouteConfigUpdate, err error) {
		rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := RouteConfigUpdate{WeightedCluster: map[string]uint32{testCDSName: 1}}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdateErr{})) {
		t.Errorf("unexpected RouteConfigUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another watch for the resource in cache.
	rdsUpdateCh2 := testutils.NewChannel()
	c.watchRDS(testRDSName, func(update RouteConfigUpdate, err error) {
		rdsUpdateCh2.Send(rdsUpdateErr{u: update, err: err})
	})
	if n, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err == nil {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if u, err := rdsUpdateCh2.Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdateErr{})) {
		t.Errorf("unexpected RouteConfigUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Old watch should see nothing.
	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

type serviceUpdateErr struct {
	u   ServiceUpdate
	err error
}

var serviceCmpOpts = []cmp.Option{cmp.AllowUnexported(serviceUpdateErr{}), cmpopts.EquateEmpty()}

// TestServiceWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name (which doesn't trigger callback)
// - an upate is received after cancel()
func (s) TestServiceWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	serviceUpdateCh := testutils.NewChannel()
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})

	wantUpdate := ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName: 1}}

	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestServiceWatchLDSUpdate covers the case that after first LDS and first RDS
// response, the second LDS response trigger an new RDS watch, and an update of
// the old RDS watch doesn't trigger update to service callback.
func (s) TestServiceWatchLDSUpdate(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	serviceUpdateCh := testutils.NewChannel()
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})

	wantUpdate := ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName: 1}}

	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another LDS update with a different RDS_name.
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName + "2"},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	// Another update for the old name.
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected serviceUpdate: %v, %v, want channel recv timeout", u, err)
	}

	wantUpdate2 := ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName + "2": 1}}
	// RDS update for the new name.
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName + "2": {WeightedCluster: map[string]uint32{testCDSName + "2": 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate2, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestServiceWatchSecond covers the case where a second WatchService() gets an
// error (because only one is allowed). But the first watch still receives
// updates.
func (s) TestServiceWatchSecond(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	serviceUpdateCh := testutils.NewChannel()
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})

	wantUpdate := ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName: 1}}

	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}

	serviceUpdateCh2 := testutils.NewChannel()
	// Call WatchService() again, with the same or different name.
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh2.Send(serviceUpdateErr{u: update, err: err})
	})

	u, err := serviceUpdateCh2.Receive()
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
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}

	if u, err := serviceUpdateCh2.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected serviceUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestServiceWatchWithNoResponseFromServer tests the case where the xDS server
// does not respond to the requests being sent out as part of registering a
// service update watcher. The callback will get an error.
func (s) TestServiceWatchWithNoResponseFromServer(t *testing.T) {
	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	serviceUpdateCh := testutils.NewChannel()
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	u, err := serviceUpdateCh.TimedReceive(defaultWatchExpiryTimeout * 2)
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

// TestServiceWatchEmptyRDS tests the case where the underlying v2Client
// receives an empty RDS response. The callback will get an error.
func (s) TestServiceWatchEmptyRDS(t *testing.T) {
	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	serviceUpdateCh := testutils.NewChannel()
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})

	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{})
	u, err := serviceUpdateCh.TimedReceive(defaultWatchExpiryTimeout * 2)
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
	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	serviceUpdateCh := testutils.NewChannel()
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})

	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	// Client is closed before it receives the RDS response.
	c.Close()
	if u, err := serviceUpdateCh.TimedReceive(defaultWatchExpiryTimeout * 2); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected serviceUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestServiceNotCancelRDSOnSameLDSUpdate covers the case that if the second LDS
// update contains the same RDS name as the previous, the RDS watch isn't
// canceled and restarted.
func (s) TestServiceNotCancelRDSOnSameLDSUpdate(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	serviceUpdateCh := testutils.NewChannel()
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})

	wantUpdate := ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName: 1}}

	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another LDS update with a the same RDS_name.
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if v, err := v2Client.removeWatches[version.V2RouteConfigURL].Receive(); err == nil {
		t.Fatalf("unexpected rds watch cancel: %v", v)
	}
}

// TestServiceResourceRemoved covers the cases:
// - an update is received after a watch()
// - another update is received, with one resource removed
//   - this should trigger callback with resource removed error
// - one more update without the removed resource
//   - the callback (above) shouldn't receive any update
func (s) TestServiceResourceRemoved(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	serviceUpdateCh := testutils.NewChannel()
	c.WatchService(testLDSName, func(update ServiceUpdate, err error) {
		serviceUpdateCh.Send(serviceUpdateErr{u: update, err: err})
	})

	wantUpdate := ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName: 1}}

	if _, err := v2Client.addWatches[version.V2ListenerURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Remove LDS resource, should cancel the RDS watch, and trigger resource
	// removed error.
	v2Client.r.NewListeners(map[string]ListenerUpdate{})
	if _, err := v2Client.removeWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want watch to be canceled, got error %v", err)
	}
	if u, err := serviceUpdateCh.Receive(); err != nil || ErrType(u.(serviceUpdateErr).err) != ErrorTypeResourceNotFound {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// Send RDS update for the removed LDS resource, expect no updates to
	// callback, because RDS should be canceled.
	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName + "new": 1}},
	})
	if u, err := serviceUpdateCh.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected serviceUpdate: %v, want receiving from channel timeout", u)
	}

	// Add LDS resource, but not RDS resource, should
	//  - start a new RDS watch
	//  - timeout on service channel, because RDS cache was cleared
	v2Client.r.NewListeners(map[string]ListenerUpdate{
		testLDSName: {RouteConfigName: testRDSName},
	})
	if _, err := v2Client.addWatches[version.V2RouteConfigURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	if u, err := serviceUpdateCh.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected serviceUpdate: %v, want receiving from channel timeout", u)
	}

	v2Client.r.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName: {WeightedCluster: map[string]uint32{testCDSName + "new2": 1}},
	})
	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName + "new2": 1}}, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}
}
