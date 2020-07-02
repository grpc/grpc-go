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
)

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
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
	if _, err := v2Client.addWatches[edsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.newEDSUpdate(map[string]EndpointsUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := endpointsUpdateCh.Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate, nil}, endpointsCmpOpts...) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another update for a different resource name.
	v2Client.r.newEDSUpdate(map[string]EndpointsUpdate{
		"randomName": {},
	})

	if u, err := endpointsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.newEDSUpdate(map[string]EndpointsUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := endpointsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestEndpointsTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestEndpointsTwoWatchSameResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
		if _, err := v2Client.addWatches[edsURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.newEDSUpdate(map[string]EndpointsUpdate{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := endpointsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, endpointsUpdateErr{wantUpdate, nil}, endpointsCmpOpts...) {
			t.Errorf("i=%v, unexpected endpointsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.newEDSUpdate(map[string]EndpointsUpdate{
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
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
		if _, err := v2Client.addWatches[edsURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	// Third watch for a different name.
	endpointsUpdateCh2 := testutils.NewChannel()
	c.WatchEndpoints(testCDSName+"2", func(update EndpointsUpdate, err error) {
		endpointsUpdateCh2.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[edsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	wantUpdate2 := EndpointsUpdate{Localities: []Locality{testLocalities[1]}}
	v2Client.r.newEDSUpdate(map[string]EndpointsUpdate{
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
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
	if _, err := v2Client.addWatches[edsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.newEDSUpdate(map[string]EndpointsUpdate{
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
	if n, err := v2Client.addWatches[edsURL].Receive(); err == nil {
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

	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
	if _, err := v2Client.addWatches[edsURL].Receive(); err != nil {
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
