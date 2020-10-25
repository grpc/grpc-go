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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
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
	defaultTestTimeout            = 5 * time.Second
	defaultTestShortTimeout       = 10 * time.Millisecond // For events expected to *not* happen.
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
			NodeProto:    xdstestutils.EmptyNodeProtoV2,
		},
		WatchExpiryTimeout: watchExpiryTimeout,
	}
}

type testAPIClient struct {
	addWatches    map[ResourceType]*testutils.Channel
	removeWatches map[ResourceType]*testutils.Channel
}

func overrideNewAPIClient() (*testutils.Channel, func()) {
	origNewAPIClient := newAPIClient
	ch := testutils.NewChannel()
	newAPIClient = func(apiVersion version.TransportAPI, cc *grpc.ClientConn, opts BuildOptions) (APIClient, error) {
		ret := newTestAPIClient()
		ch.Send(ret)
		return ret, nil
	}
	return ch, func() { newAPIClient = origNewAPIClient }
}

func newTestAPIClient() *testAPIClient {
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

func (c *testAPIClient) ReportLoad(ctx context.Context, cc *grpc.ClientConn, opts LoadReportingOptions) {
}

func (c *testAPIClient) Close() {}

// TestWatchCallAnotherWatch covers the case where watch() is called inline by a
// callback. It makes sure it doesn't cause a deadlock.
func (s) TestWatchCallAnotherWatch(t *testing.T) {
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

	clusterUpdateCh := testutils.NewChannel()
	firstTime := true
	client.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
		// Calls another watch inline, to ensure there's deadlock.
		client.WatchCluster("another-random-name", func(ClusterUpdate, error) {})

		if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); firstTime && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		firstTime = false
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ServiceName: testEDSName}
	client.NewClusters(map[string]ClusterUpdate{testCDSName: wantUpdate})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	wantUpdate2 := ClusterUpdate{ServiceName: testEDSName + "2"}
	client.NewClusters(map[string]ClusterUpdate{testCDSName: wantUpdate2})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}

func verifyListenerUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate ListenerUpdate) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for listener update: %v", err)
	}
	gotUpdate := u.(ldsUpdateErr)
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate) {
		return fmt.Errorf("unexpected endpointsUpdate: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}

func verifyRouteConfigUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate RouteConfigUpdate) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for route configuration update: %v", err)
	}
	gotUpdate := u.(rdsUpdateErr)
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate) {
		return fmt.Errorf("unexpected route config update: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}

func verifyServiceUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate ServiceUpdate) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for service update: %v", err)
	}
	gotUpdate := u.(serviceUpdateErr)
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate, cmpopts.EquateEmpty()) {
		return fmt.Errorf("unexpected service update: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}

func verifyClusterUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate ClusterUpdate) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for cluster update: %v", err)
	}
	gotUpdate := u.(clusterUpdateErr)
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate) {
		return fmt.Errorf("unexpected clusterUpdate: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}

func verifyEndpointsUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate EndpointsUpdate) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for endpoints update: %v", err)
	}
	gotUpdate := u.(endpointsUpdateErr)
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate, cmpopts.EquateEmpty()) {
		return fmt.Errorf("unexpected endpointsUpdate: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}
