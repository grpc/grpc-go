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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/testutils"
)

type rdsUpdateErr struct {
	u   rdsUpdate
	err error
}

// TestRDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name (which doesn't trigger callback)
// - an upate is received after cancel()
func (s) TestRDSWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	rdsUpdateCh := testutils.NewChannel()
	cancelWatch := c.watchRDS(testRDSName, func(update rdsUpdate, err error) {
		rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := rdsUpdate{weightedCluster: map[string]uint32{testCDSName: 1}}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdate{}, rdsUpdateErr{})) {
		t.Errorf("unexpected rdsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another update for a different resource name.
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		"randomName": {},
	})

	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestRDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestRDSTwoWatchSameResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
		cancelLastWatch = c.watchRDS(testRDSName, func(update rdsUpdate, err error) {
			rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[rdsURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	wantUpdate := rdsUpdate{weightedCluster: map[string]uint32{testCDSName: 1}}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdate{}, rdsUpdateErr{})) {
			t.Errorf("i=%v, unexpected rdsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdate{}, rdsUpdateErr{})) {
			t.Errorf("i=%v, unexpected rdsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := rdsUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestRDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestRDSThreeWatchDifferentResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
		c.watchRDS(testRDSName+"1", func(update rdsUpdate, err error) {
			rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
		})
		if _, err := v2Client.addWatches[rdsURL].Receive(); i == 0 && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
	}

	// Third watch for a different name.
	rdsUpdateCh2 := testutils.NewChannel()
	c.watchRDS(testRDSName+"2", func(update rdsUpdate, err error) {
		rdsUpdateCh2.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := rdsUpdate{weightedCluster: map[string]uint32{testCDSName + "1": 1}}
	wantUpdate2 := rdsUpdate{weightedCluster: map[string]uint32{testCDSName + "2": 1}}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName + "1": wantUpdate1,
		testRDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate1, nil}, cmp.AllowUnexported(rdsUpdate{}, rdsUpdateErr{})) {
			t.Errorf("i=%v, unexpected rdsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := rdsUpdateCh2.Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate2, nil}, cmp.AllowUnexported(rdsUpdate{}, rdsUpdateErr{})) {
		t.Errorf("unexpected rdsUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestRDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestRDSWatchAfterCache(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	rdsUpdateCh := testutils.NewChannel()
	c.watchRDS(testRDSName, func(update rdsUpdate, err error) {
		rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := v2Client.addWatches[rdsURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := rdsUpdate{weightedCluster: map[string]uint32{testCDSName: 1}}
	v2Client.r.newRDSUpdate(map[string]rdsUpdate{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdate{}, rdsUpdateErr{})) {
		t.Errorf("unexpected rdsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another watch for the resource in cache.
	rdsUpdateCh2 := testutils.NewChannel()
	c.watchRDS(testRDSName, func(update rdsUpdate, err error) {
		rdsUpdateCh2.Send(rdsUpdateErr{u: update, err: err})
	})
	if n, err := v2Client.addWatches[rdsURL].Receive(); err == nil {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if u, err := rdsUpdateCh2.Receive(); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdate{}, rdsUpdateErr{})) {
		t.Errorf("unexpected rdsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Old watch should see nothing.
	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}
