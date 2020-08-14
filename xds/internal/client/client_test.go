/*
 *
 * Copyright 2019 gRPC authors.
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

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/version"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	testXDSServer   = "xds-server"
	chanRecvTimeout = 100 * time.Millisecond

	testLDSName = "test-lds"
	testRDSName = "test-rds"
	testCDSName = "test-cds"
	testEDSName = "test-eds"

	defaultTestWatchExpiryTimeout = 500 * time.Millisecond
)

func clientOpts(balancerName string, overrideWatchExpiryTImeout bool) Options {
	watchExpiryTimeout := time.Duration(0)
	if overrideWatchExpiryTImeout {
		watchExpiryTimeout = defaultTestWatchExpiryTimeout
	}
	return Options{
		Config: bootstrap.Config{
			BalancerName: balancerName,
			Creds:        grpc.WithInsecure(),
			NodeProto:    testutils.EmptyNodeProtoV2,
		},
		WatchExpiryTimeout: watchExpiryTimeout,
	}
}

type testAPIClient struct {
	r UpdateHandler

	addWatches    map[ResourceType]*testutils.Channel
	removeWatches map[ResourceType]*testutils.Channel
}

func overrideNewAPIClient() (<-chan *testAPIClient, func()) {
	origNewAPIClient := newAPIClient
	ch := make(chan *testAPIClient, 1)
	newAPIClient = func(apiVersion version.TransportAPI, cc *grpc.ClientConn, opts BuildOptions) (APIClient, error) {
		ret := newTestAPIClient(opts.Parent)
		ch <- ret
		return ret, nil
	}
	return ch, func() { newAPIClient = origNewAPIClient }
}

func newTestAPIClient(r UpdateHandler) *testAPIClient {
	addWatches := map[ResourceType]*testutils.Channel{
		ListenerResource:    testutils.NewChannel(),
		RouteConfigResource: testutils.NewChannel(),
		ClusterResource:     testutils.NewChannel(),
		EndpointsResource:   testutils.NewChannel(),
	}
	removeWatches := map[ResourceType]*testutils.Channel{
		ListenerResource:    testutils.NewChannel(),
		RouteConfigResource: testutils.NewChannel(),
		ClusterResource:     testutils.NewChannel(),
		EndpointsResource:   testutils.NewChannel(),
	}
	return &testAPIClient{
		r:             r,
		addWatches:    addWatches,
		removeWatches: removeWatches,
	}
}

func (c *testAPIClient) AddWatch(resourceType ResourceType, resourceName string) {
	c.addWatches[resourceType].Send(resourceName)
}

func (c *testAPIClient) RemoveWatch(resourceType ResourceType, resourceName string) {
	c.removeWatches[resourceType].Send(resourceName)
}

func (c *testAPIClient) Close() {}

// TestWatchCallAnotherWatch covers the case where watch() is called inline by a
// callback. It makes sure it doesn't cause a deadlock.
func (s) TestWatchCallAnotherWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	firstTime := true
	c.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
		// Calls another watch inline, to ensure there's deadlock.
		c.WatchCluster("another-random-name", func(ClusterUpdate, error) {})
		if _, err := v2Client.addWatches[ClusterResource].Receive(); firstTime && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		firstTime = false
	})
	if _, err := v2Client.addWatches[ClusterResource].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	wantUpdate2 := ClusterUpdate{ServiceName: testEDSName + "2"}
	v2Client.r.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate2,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
}
