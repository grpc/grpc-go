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
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/testutils"
)

var (
	testLocalities = []Locality{
		{
			Endpoints: []Endpoint{{Address: "addr1:314"}},
			ID:        internal.Locality{SubZone: "locality-1"},
			Priority:  1,
			Weight:    1,
		},
		{
			Endpoints: []Endpoint{{Address: "addr2:159"}},
			ID:        internal.Locality{SubZone: "locality-2"},
			Priority:  0,
			Weight:    1,
		},
	}
)

// TestEndpointsWatch covers the case where an update is received after a watch().
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
	endpointsErrCh := testutils.NewChannel()
	cancelWatch := c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(update)
		endpointsErrCh.Send(err)
	})

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.newUpdate(edsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	if u, err := endpointsUpdateCh.Receive(); err != nil || !cmp.Equal(u, wantUpdate) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := endpointsErrCh.Receive(); err != nil || e != nil {
		t.Errorf("unexpected endpointsError: %v, error receiving from channel: %v", e, err)
	}

	// Another update for a different resource name.
	v2Client.r.newUpdate(edsURL, map[string]interface{}{
		"randomName": EndpointsUpdate{},
	})

	if u, err := endpointsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := endpointsErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsError: %v, %v, want channel recv timeout", e, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.newUpdate(edsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	if u, err := endpointsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := endpointsErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsError: %v, %v, want channel recv timeout", e, err)
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

	var endpointsUpdateChs, endpointsErrChs []*testutils.Channel
	const count = 2

	var cancelLastWatch func()

	for i := 0; i < count; i++ {
		endpointsUpdateCh := testutils.NewChannel()
		endpointsUpdateChs = append(endpointsUpdateChs, endpointsUpdateCh)
		endpointsErrCh := testutils.NewChannel()
		endpointsErrChs = append(endpointsErrChs, endpointsErrCh)
		cancelLastWatch = c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
			endpointsUpdateCh.Send(update)
			endpointsErrCh.Send(err)
		})
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.newUpdate(edsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := endpointsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, wantUpdate) {
			t.Errorf("i=%v, unexpected endpointsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := endpointsErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected endpointsError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.newUpdate(edsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := endpointsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, wantUpdate) {
			t.Errorf("i=%v, unexpected endpointsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := endpointsErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected endpointsError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	if u, err := endpointsUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := endpointsErrChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsError: %v, %v, want channel recv timeout", e, err)
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

	var endpointsUpdateChs, endpointsErrChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		endpointsUpdateCh := testutils.NewChannel()
		endpointsUpdateChs = append(endpointsUpdateChs, endpointsUpdateCh)
		endpointsErrCh := testutils.NewChannel()
		endpointsErrChs = append(endpointsErrChs, endpointsErrCh)
		c.WatchEndpoints(testCDSName+"1", func(update EndpointsUpdate, err error) {
			endpointsUpdateCh.Send(update)
			endpointsErrCh.Send(err)
		})
	}

	// Third watch for a different name.
	endpointsUpdateCh2 := testutils.NewChannel()
	endpointsErrCh2 := testutils.NewChannel()
	c.WatchEndpoints(testCDSName+"2", func(update EndpointsUpdate, err error) {
		endpointsUpdateCh2.Send(update)
		endpointsErrCh2.Send(err)
	})

	wantUpdate1 := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	wantUpdate2 := EndpointsUpdate{Localities: []Locality{testLocalities[1]}}
	v2Client.r.newUpdate(edsURL, map[string]interface{}{
		testCDSName + "1": wantUpdate1,
		testCDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := endpointsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, wantUpdate1) {
			t.Errorf("i=%v, unexpected endpointsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := endpointsErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected endpointsError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	if u, err := endpointsUpdateCh2.Receive(); err != nil || !cmp.Equal(u, wantUpdate2) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := endpointsErrCh2.Receive(); err != nil || e != nil {
		t.Errorf("unexpected endpointsError: %v, error receiving from channel: %v", e, err)
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
	endpointsErrCh := testutils.NewChannel()
	c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(update)
		endpointsErrCh.Send(err)
	})

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	v2Client.r.newUpdate(edsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	if u, err := endpointsUpdateCh.Receive(); err != nil || !cmp.Equal(u, wantUpdate) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := endpointsErrCh.Receive(); err != nil || e != nil {
		t.Errorf("unexpected endpointsError: %v, error receiving from channel: %v", e, err)
	}

	// Another watch for the resource in cache.
	endpointsUpdateCh2 := testutils.NewChannel()
	endpointsErrCh2 := testutils.NewChannel()
	c.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh2.Send(update)
		endpointsErrCh2.Send(err)
	})

	// New watch should receives the update.
	if u, err := endpointsUpdateCh2.Receive(); err != nil || !cmp.Equal(u, wantUpdate) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := endpointsErrCh2.Receive(); err != nil || e != nil {
		t.Errorf("unexpected endpointsError: %v, error receiving from channel: %v", e, err)
	}

	// Old watch should see nothing.
	if u, err := endpointsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := endpointsErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected endpointsError: %v, %v, want channel recv timeout", e, err)
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

	<-v2ClientCh

	endpointsUpdateCh := testutils.NewChannel()
	endpointsErrCh := testutils.NewChannel()
	c.WatchEndpoints(testCDSName, func(u EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(u)
		endpointsErrCh.Send(err)
	})

	if u, err := endpointsUpdateCh.TimedReceive(defaultWatchExpiryTimeout * 2); err != nil || !cmp.Equal(u, EndpointsUpdate{}) {
		t.Errorf("unexpected endpointsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := endpointsErrCh.TimedReceive(defaultWatchExpiryTimeout * 2); err != nil || e == nil {
		t.Errorf("unexpected endpointsError: %v, error receiving from channel: %v, want error watcher timeout", e, err)
	}
}
