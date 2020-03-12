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

	"google.golang.org/grpc/xds/internal/testutils"
)

// TestClusterWatch covers the case where an update is received after a watch().
func (s) TestClusterWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	clusterErrCh := testutils.NewChannel()
	cancelWatch := c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(update)
		clusterErrCh.Send(err)
	})

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.newUpdate(cdsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != wantUpdate {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := clusterErrCh.Receive(); err != nil || e != nil {
		t.Errorf("unexpected clusterError: %v, error receiving from channel: %v", e, err)
	}

	// Another update for a different resource name.
	v2Client.r.newUpdate(cdsURL, map[string]interface{}{
		"randomName": ClusterUpdate{},
	})

	if u, err := clusterUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := clusterErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterError: %v, %v, want channel recv timeout", e, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	v2Client.r.newUpdate(cdsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := clusterErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterError: %v, %v, want channel recv timeout", e, err)
	}
}

// TestClusterTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestClusterTwoWatchSameResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var clusterUpdateChs, clusterErrChs []*testutils.Channel
	const count = 2

	var cancelLastWatch func()

	for i := 0; i < count; i++ {
		clusterUpdateCh := testutils.NewChannel()
		clusterUpdateChs = append(clusterUpdateChs, clusterUpdateCh)
		clusterErrCh := testutils.NewChannel()
		clusterErrChs = append(clusterErrChs, clusterErrCh)
		cancelLastWatch = c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
			clusterUpdateCh.Send(update)
			clusterErrCh.Send(err)
		})
	}

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.newUpdate(cdsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count; i++ {
		if u, err := clusterUpdateChs[i].Receive(); err != nil || u != wantUpdate {
			t.Errorf("i=%v, unexpected clusterUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := clusterErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected clusterError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	v2Client.r.newUpdate(cdsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	for i := 0; i < count-1; i++ {
		if u, err := clusterUpdateChs[i].Receive(); err != nil || u != wantUpdate {
			t.Errorf("i=%v, unexpected clusterUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := clusterErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected clusterError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	if u, err := clusterUpdateChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := clusterErrChs[count-1].TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterError: %v, %v, want channel recv timeout", e, err)
	}
}

// TestClusterThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestClusterThreeWatchDifferentResourceName(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	var clusterUpdateChs, clusterErrChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		clusterUpdateCh := testutils.NewChannel()
		clusterUpdateChs = append(clusterUpdateChs, clusterUpdateCh)
		clusterErrCh := testutils.NewChannel()
		clusterErrChs = append(clusterErrChs, clusterErrCh)
		c.WatchCluster(testCDSName+"1", func(update ClusterUpdate, err error) {
			clusterUpdateCh.Send(update)
			clusterErrCh.Send(err)
		})
	}

	// Third watch for a different name.
	clusterUpdateCh2 := testutils.NewChannel()
	clusterErrCh2 := testutils.NewChannel()
	c.WatchCluster(testCDSName+"2", func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(update)
		clusterErrCh2.Send(err)
	})

	wantUpdate1 := ClusterUpdate{ServiceName: testEDSName + "1"}
	wantUpdate2 := ClusterUpdate{ServiceName: testEDSName + "2"}
	v2Client.r.newUpdate(cdsURL, map[string]interface{}{
		testCDSName + "1": wantUpdate1,
		testCDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if u, err := clusterUpdateChs[i].Receive(); err != nil || u != wantUpdate1 {
			t.Errorf("i=%v, unexpected clusterUpdate: %v, error receiving from channel: %v", i, u, err)
		}
		if e, err := clusterErrChs[i].Receive(); err != nil || e != nil {
			t.Errorf("i=%v, unexpected clusterError: %v, error receiving from channel: %v", i, e, err)
		}
	}

	if u, err := clusterUpdateCh2.Receive(); err != nil || u != wantUpdate2 {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := clusterErrCh2.Receive(); err != nil || e != nil {
		t.Errorf("unexpected clusterError: %v, error receiving from channel: %v", e, err)
	}
}

// TestClusterWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestClusterWatchAfterCache(t *testing.T) {
	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	clusterErrCh := testutils.NewChannel()
	c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(update)
		clusterErrCh.Send(err)
	})

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.newUpdate(cdsURL, map[string]interface{}{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != wantUpdate {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := clusterErrCh.Receive(); err != nil || e != nil {
		t.Errorf("unexpected clusterError: %v, error receiving from channel: %v", e, err)
	}

	// Another watch for the resource in cache.
	clusterUpdateCh2 := testutils.NewChannel()
	clusterErrCh2 := testutils.NewChannel()
	c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(update)
		clusterErrCh2.Send(err)
	})

	// New watch should receives the update.
	if u, err := clusterUpdateCh2.Receive(); err != nil || u != wantUpdate {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := clusterErrCh2.Receive(); err != nil || e != nil {
		t.Errorf("unexpected clusterError: %v, error receiving from channel: %v", e, err)
	}

	// Old watch should see nothing.
	if u, err := clusterUpdateCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
	if e, err := clusterErrCh.TimedReceive(chanRecvTimeout); err != testutils.ErrRecvTimeout {
		t.Errorf("unexpected clusterError: %v, %v, want channel recv timeout", e, err)
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

	v2ClientCh, cleanup := overrideNewXDSV2Client()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	<-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	clusterErrCh := testutils.NewChannel()
	c.WatchCluster(testCDSName, func(u ClusterUpdate, err error) {
		clusterUpdateCh.Send(u)
		clusterErrCh.Send(err)
	})

	if u, err := clusterUpdateCh.TimedReceive(defaultWatchExpiryTimeout * 2); err != nil || (u != ClusterUpdate{}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
	if e, err := clusterErrCh.TimedReceive(defaultWatchExpiryTimeout * 2); err != nil || e == nil {
		t.Errorf("unexpected clusterError: %v, error receiving from channel: %v, want error watcher timeout", e, err)
	}
}
