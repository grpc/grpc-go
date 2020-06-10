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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
)

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
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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

	if _, err := v2Client.addWatches[ldsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName},
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestServiceWatchLDSUpdate covers the case that after first LDS and first RDS
// response, the second LDS response trigger an new RDS watch, and an update of
// the old RDS watch doesn't trigger update to service callback.
func (s) TestServiceWatchLDSUpdate(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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

	if _, err := v2Client.addWatches[ldsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName},
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another LDS update with a different RDS_name.
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName + "2"},
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	// Another update for the old name.
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected serviceUpdate: %v, %v, want channel recv timeout", u, err)
	}

	wantUpdate2 := ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName + "2": 1}}
	// RDS update for the new name.
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName + "2": {weightedCluster: map[string]uint32{testCDSName + "2": 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate2, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestServiceWatchSecond covers the case where a second WatchService() gets an
// error (because only one is allowed). But the first watch still receives
// updates.
func (s) TestServiceWatchSecond(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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

	if _, err := v2Client.addWatches[ldsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName},
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName: 1}},
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
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName},
	})
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName: 1}},
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
	fakeServer, cleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer cleanup()

	xdsClient, err := New(clientOpts(fakeServer.Address))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer xdsClient.Close()
	t.Log("Created an xdsClient...")

	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	callbackCh := testutils.NewChannel()
	cancelWatch := xdsClient.WatchService(goodLDSTarget1, func(su ServiceUpdate, err error) {
		if su.WeightedCluster != nil {
			callbackCh.Send(fmt.Errorf("got WeightedCluster: %+v, want nil", su.WeightedCluster))
			return
		}
		if err == nil {
			callbackCh.Send(errors.New("xdsClient.WatchService returned error non-nil error"))
			return
		}
		callbackCh.Send(nil)
	})
	defer cancelWatch()
	t.Log("Registered a watcher for service updates...")

	// Wait for one request from the client, but send no reponses.
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	waitForNilErr(t, callbackCh)
}

// TestServiceWatchEmptyRDS tests the case where the underlying v2Client
// receives an empty RDS response. The callback will get an error.
func (s) TestServiceWatchEmptyRDS(t *testing.T) {
	fakeServer, cleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer cleanup()

	xdsClient, err := New(clientOpts(fakeServer.Address))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer xdsClient.Close()
	t.Log("Created an xdsClient...")

	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	callbackCh := testutils.NewChannel()
	cancelWatch := xdsClient.WatchService(goodLDSTarget1, func(su ServiceUpdate, err error) {
		if su.WeightedCluster != nil {
			callbackCh.Send(fmt.Errorf("got WeightedCluster: %+v, want nil", su.WeightedCluster))
			return
		}
		if err == nil {
			callbackCh.Send(errors.New("xdsClient.WatchService returned error non-nil error"))
			return
		}
		callbackCh.Send(nil)
	})
	defer cancelWatch()
	t.Log("Registered a watcher for service updates...")

	// Make the fakeServer send LDS response.
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}

	// Make the fakeServer send an empty RDS response.
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an RDS request")
	}
	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: noVirtualHostsInRDSResponse}
	waitForNilErr(t, callbackCh)
}

// TestServiceWatchWithClientClose tests the case where xDS responses are
// received after the client is closed, and we make sure that the registered
// watcher callback is not invoked.
func (s) TestServiceWatchWithClientClose(t *testing.T) {
	fakeServer, cleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer cleanup()

	xdsClient, err := New(clientOpts(fakeServer.Address))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	defer xdsClient.Close()
	t.Log("Created an xdsClient...")

	callbackCh := testutils.NewChannel()
	cancelWatch := xdsClient.WatchService(goodLDSTarget1, func(su ServiceUpdate, err error) {
		callbackCh.Send(errors.New("watcher callback invoked after client close"))
	})
	defer cancelWatch()
	t.Log("Registered a watcher for service updates...")

	// Make the fakeServer send LDS response.
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}

	xdsClient.Close()
	t.Log("Closing the xdsClient...")

	// Push an RDS response from the fakeserver
	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodRDSResponse1}
	if cbErr, err := callbackCh.Receive(); err != testutils.ErrRecvTimeout {
		t.Fatal(cbErr)
	}
}

// TestServiceNotCancelRDSOnSameLDSUpdate covers the case that if the second LDS
// update contains the same RDS name as the previous, the RDS watch isn't
// canceled and restarted.
func (s) TestServiceNotCancelRDSOnSameLDSUpdate(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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

	if _, err := v2Client.addWatches[ldsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName},
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another LDS update with a the same RDS_name.
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName},
	})
	if v, err := v2Client.removeWatches[rdsURL].Receive(); err == nil {
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
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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

	if _, err := v2Client.addWatches[ldsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName},
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName: 1}},
	})

	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{wantUpdate, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Remove LDS resource, should cancel the RDS watch, and trigger resource
	// removed error.
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{})
	if _, err := v2Client.removeWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want watch to be canceled, got error %v", err)
	}
	if u, err := serviceUpdateCh.Receive(); err != nil || ErrType(u.(serviceUpdateErr).err) != ErrorTypeResourceNotFound {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// Send RDS update for the removed LDS resource, expect no updates to
	// callback, because RDS should be canceled.
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName + "new": 1}},
	})
	if u, err := serviceUpdateCh.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected serviceUpdate: %v, want receiving from channel timeout", u)
	}

	// Add LDS resource, but not RDS resource, should
	//  - start a new RDS watch
	//  - timeout on service channel, because RDS cache was cleared
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: {routeName: testRDSName},
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	if u, err := serviceUpdateCh.Receive(); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected serviceUpdate: %v, want receiving from channel timeout", u)
	}

	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: {weightedCluster: map[string]uint32{testCDSName + "new2": 1}},
	})
	if u, err := serviceUpdateCh.Receive(); err != nil || !cmp.Equal(u, serviceUpdateErr{ServiceUpdate{WeightedCluster: map[string]uint32{testCDSName + "new2": 1}}, nil}, serviceCmpOpts...) {
		t.Errorf("unexpected serviceUpdate: %v, error receiving from channel: %v", u, err)
	}
}
