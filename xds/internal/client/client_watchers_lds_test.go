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

	"google.golang.org/grpc/xds/internal/testutils"
)

type ldsUpdateErr struct {
	u   ldsUpdate
	err error
}

// TestLDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name (which doesn't trigger callback)
// - an upate is received after cancel()
func (s) TestLDSWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	ldsUpdateCh := testutils.NewChannel()
	cancelWatch := c.watchLDS(testLDSName, func(update ldsUpdate, err error) {
		ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
	})

	wantUpdate := ldsUpdate{routeName: testRDSName}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: wantUpdate,
	})

	if u, err := ldsUpdateCh.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected ldsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another update for a different resource name.
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		"randomName": {},
	})

	if u, err := ldsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected ldsUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: wantUpdate,
	})

	if u, err := ldsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected ldsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestLDSTwoWatchSameResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
		cancelLastWatch = c.watchLDS(testLDSName, func(update ldsUpdate, err error) {
			ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
		})
	}

	wantUpdate := ldsUpdate{routeName: testRDSName}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := ldsUpdateChs[i].Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
			t.Errorf("i=%v, unexpected ldsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := ldsUpdateChs[i].Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
			t.Errorf("i=%v, unexpected ldsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := ldsUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected ldsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestLDSThreeWatchDifferentResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
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
		c.watchLDS(testLDSName+"1", func(update ldsUpdate, err error) {
			ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
		})
	}

	// Third watch for a different name.
	ldsUpdateCh2 := testutils.NewChannel()
	c.watchLDS(testLDSName+"2", func(update ldsUpdate, err error) {
		ldsUpdateCh2.Send(ldsUpdateErr{u: update, err: err})
	})

	wantUpdate1 := ldsUpdate{routeName: testRDSName + "1"}
	wantUpdate2 := ldsUpdate{routeName: testRDSName + "2"}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName + "1": wantUpdate1,
		testLDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := ldsUpdateChs[i].Receive(); err != nil || u != (ldsUpdateErr{wantUpdate1, nil}) {
			t.Errorf("i=%v, unexpected ldsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
	}

	if u, err := ldsUpdateCh2.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected ldsUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// TestLDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestLDSWatchAfterCache(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	ldsUpdateCh := testutils.NewChannel()
	c.watchLDS(testLDSName, func(update ldsUpdate, err error) {
		ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
	})

	wantUpdate := ldsUpdate{routeName: testRDSName}
	v2Client.r.newLDSUpdate(map[string]ldsUpdate{
		testLDSName: wantUpdate,
	})

	if u, err := ldsUpdateCh.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected ldsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Another watch for the resource in cache.
	ldsUpdateCh2 := testutils.NewChannel()
	c.watchLDS(testLDSName, func(update ldsUpdate, err error) {
		ldsUpdateCh2.Send(ldsUpdateErr{u: update, err: err})
	})

	// New watch should receives the update.
	if u, err := ldsUpdateCh2.Receive(); err != nil || u != (ldsUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected ldsUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Old watch should see nothing.
	if u, err := ldsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected ldsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}
