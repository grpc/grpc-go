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

// TestRDSWatch covers the case where an update is received after a watch().
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
	rdsErrCh := testutils.NewChannel()
	cancelWatch := c.watchRDS(testRDSName, func(update rdsUpdate, err error) {
		rdsUpdateCh.Send(update)
		rdsErrCh.Send(err)
	})

	wantUpdate := rdsUpdate{clusterName: testCDSName}
	v2Client.r.newUpdate(rdsURL, map[string]interface{}{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.Receive(); err != nil || u != wantUpdate {
		t.Errorf("unexpected rdsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := rdsErrCh.Receive(); err != nil || e != nil {
		t.Errorf("unexpected rdsError: %v, error receiving from channel: %v", e, err)
	}

	// Another update for a different resource name.
	v2Client.r.newUpdate(rdsURL, map[string]interface{}{
		"randomName": rdsUpdate{},
	})

	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := rdsErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsError: %v, %v, want channel recv timeout", e, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.newUpdate(rdsURL, map[string]interface{}{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := rdsErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsError: %v, %v, want channel recv timeout", e, err)
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

	var rdsUpdateChs, rdsErrChs []*testutils.Channel
	const count = 2

	var cancelLastWatch func()

	for i := 0; i < count; i++ {
		rdsUpdateCh := testutils.NewChannel()
		rdsUpdateChs = append(rdsUpdateChs, rdsUpdateCh)
		rdsErrCh := testutils.NewChannel()
		rdsErrChs = append(rdsErrChs, rdsErrCh)
		cancelLastWatch = c.watchRDS(testRDSName, func(update rdsUpdate, err error) {
			rdsUpdateCh.Send(update)
			rdsErrCh.Send(err)
		})
	}

	wantUpdate := rdsUpdate{clusterName: testCDSName}
	v2Client.r.newUpdate(rdsURL, map[string]interface{}{
		testRDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || u != wantUpdate {
			t.Errorf("i=%v, unexpected rdsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := rdsErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected rdsError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.newUpdate(rdsURL, map[string]interface{}{
		testRDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || u != wantUpdate {
			t.Errorf("i=%v, unexpected rdsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := rdsErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected rdsError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	if u, err := rdsUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := rdsErrChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsError: %v, %v, want channel recv timeout", e, err)
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

	var rdsUpdateChs, rdsErrChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		rdsUpdateCh := testutils.NewChannel()
		rdsUpdateChs = append(rdsUpdateChs, rdsUpdateCh)
		rdsErrCh := testutils.NewChannel()
		rdsErrChs = append(rdsErrChs, rdsErrCh)
		c.watchRDS(testRDSName+"1", func(update rdsUpdate, err error) {
			rdsUpdateCh.Send(update)
			rdsErrCh.Send(err)
		})
	}

	// Third watch for a different name.
	rdsUpdateCh2 := testutils.NewChannel()
	rdsErrCh2 := testutils.NewChannel()
	c.watchRDS(testRDSName+"2", func(update rdsUpdate, err error) {
		rdsUpdateCh2.Send(update)
		rdsErrCh2.Send(err)
	})

	wantUpdate1 := rdsUpdate{clusterName: testCDSName + "1"}
	wantUpdate2 := rdsUpdate{clusterName: testCDSName + "2"}
	v2Client.r.newUpdate(rdsURL, map[string]interface{}{
		testRDSName + "1": wantUpdate1,
		testRDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := rdsUpdateChs[i].Receive(); err != nil || u != wantUpdate1 {
			t.Errorf("i=%v, unexpected rdsUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := rdsErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected rdsError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	if u, err := rdsUpdateCh2.Receive(); err != nil || u != wantUpdate2 {
		t.Errorf("unexpected rdsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := rdsErrCh2.Receive(); err != nil || e != nil {
		t.Errorf("unexpected rdsError: %v, error receiving from channel: %v", e, err)
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
	rdsErrCh := testutils.NewChannel()
	c.watchRDS(testRDSName, func(update rdsUpdate, err error) {
		rdsUpdateCh.Send(update)
		rdsErrCh.Send(err)
	})

	wantUpdate := rdsUpdate{clusterName: testCDSName}
	v2Client.r.newUpdate(rdsURL, map[string]interface{}{
		testRDSName: wantUpdate,
	})

	if u, err := rdsUpdateCh.Receive(); err != nil || u != wantUpdate {
		t.Errorf("unexpected rdsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := rdsErrCh.Receive(); err != nil || e != nil {
		t.Errorf("unexpected rdsError: %v, error receiving from channel: %v", e, err)
	}

	// Another watch for the resource in cache.
	rdsUpdateCh2 := testutils.NewChannel()
	rdsErrCh2 := testutils.NewChannel()
	c.watchRDS(testRDSName, func(update rdsUpdate, err error) {
		rdsUpdateCh2.Send(update)
		rdsErrCh2.Send(err)
	})

	// New watch should receives the update.
	if u, err := rdsUpdateCh2.Receive(); err != nil || u != wantUpdate {
		t.Errorf("unexpected rdsUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := rdsErrCh2.Receive(); err != nil || e != nil {
		t.Errorf("unexpected rdsError: %v, error receiving from channel: %v", e, err)
	}

	// Old watch should see nothing.
	if u, err := rdsUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := rdsErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected rdsError: %v, %v, want channel recv timeout", e, err)
	}
}
