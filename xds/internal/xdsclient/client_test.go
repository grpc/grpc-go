// +build go1.12

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

package xdsclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/protobuf/testing/protocmp"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	testXDSServer = "xds-server"

	testLDSName = "test-lds"
	testRDSName = "test-rds"
	testCDSName = "test-cds"
	testEDSName = "test-eds"

	defaultTestWatchExpiryTimeout = 500 * time.Millisecond
	defaultTestTimeout            = 5 * time.Second
	defaultTestShortTimeout       = 10 * time.Millisecond // For events expected to *not* happen.
)

var (
	cmpOpts = cmp.Options{
		cmpopts.EquateEmpty(),
		cmp.Comparer(func(a, b time.Time) bool { return true }),
		cmp.Comparer(func(x, y error) bool {
			if x == nil || y == nil {
				return x == nil && y == nil
			}
			return x.Error() == y.Error()
		}),
		protocmp.Transform(),
	}

	// When comparing NACK UpdateMetadata, we only care if error is nil, but not
	// the details in error.
	errPlaceHolder       = fmt.Errorf("error whose details don't matter")
	cmpOptsIgnoreDetails = cmp.Options{
		cmp.Comparer(func(a, b time.Time) bool { return true }),
		cmp.Comparer(func(x, y error) bool {
			return (x == nil) == (y == nil)
		}),
	}
)

func clientOpts(balancerName string, overrideWatchExpiryTimeout bool) (*bootstrap.Config, time.Duration) {
	watchExpiryTimeout := defaultWatchExpiryTimeout
	if overrideWatchExpiryTimeout {
		watchExpiryTimeout = defaultTestWatchExpiryTimeout
	}
	return &bootstrap.Config{
		BalancerName: balancerName,
		Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
		NodeProto:    xdstestutils.EmptyNodeProtoV2,
	}, watchExpiryTimeout
}

type testAPIClient struct {
	done          *grpcsync.Event
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
		done:          grpcsync.NewEvent(),
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

func (c *testAPIClient) reportLoad(context.Context, *grpc.ClientConn, loadReportingOptions) {
}

func (c *testAPIClient) Close() {
	c.done.Fire()
}

// TestWatchCallAnotherWatch covers the case where watch() is called inline by a
// callback. It makes sure it doesn't cause a deadlock.
func (s) TestWatchCallAnotherWatch(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
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

	wantUpdate := ClusterUpdate{ClusterName: testEDSName}
	client.NewClusters(map[string]ClusterUpdate{testCDSName: wantUpdate}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	wantUpdate2 := ClusterUpdate{ClusterName: testEDSName + "2"}
	client.NewClusters(map[string]ClusterUpdate{testCDSName: wantUpdate2}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}
}

func verifyListenerUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate ListenerUpdate, wantErr error) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for listener update: %v", err)
	}
	gotUpdate := u.(ldsUpdateErr)
	if wantErr != nil {
		if gotUpdate.err != wantErr {
			return fmt.Errorf("unexpected error: %v, want %v", gotUpdate.err, wantErr)
		}
		return nil
	}
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate) {
		return fmt.Errorf("unexpected endpointsUpdate: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}

func verifyRouteConfigUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate RouteConfigUpdate, wantErr error) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for route configuration update: %v", err)
	}
	gotUpdate := u.(rdsUpdateErr)
	if wantErr != nil {
		if gotUpdate.err != wantErr {
			return fmt.Errorf("unexpected error: %v, want %v", gotUpdate.err, wantErr)
		}
		return nil
	}
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate) {
		return fmt.Errorf("unexpected route config update: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}

func verifyClusterUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate ClusterUpdate, wantErr error) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for cluster update: %v", err)
	}
	gotUpdate := u.(clusterUpdateErr)
	if wantErr != nil {
		if gotUpdate.err != wantErr {
			return fmt.Errorf("unexpected error: %v, want %v", gotUpdate.err, wantErr)
		}
		return nil
	}
	if !cmp.Equal(gotUpdate.u, wantUpdate) {
		return fmt.Errorf("unexpected clusterUpdate: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}

func verifyEndpointsUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate EndpointsUpdate, wantErr error) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for endpoints update: %v", err)
	}
	gotUpdate := u.(endpointsUpdateErr)
	if wantErr != nil {
		if gotUpdate.err != wantErr {
			return fmt.Errorf("unexpected error: %v, want %v", gotUpdate.err, wantErr)
		}
		return nil
	}
	if gotUpdate.err != nil || !cmp.Equal(gotUpdate.u, wantUpdate, cmpopts.EquateEmpty()) {
		return fmt.Errorf("unexpected endpointsUpdate: (%v, %v), want: (%v, nil)", gotUpdate.u, gotUpdate.err, wantUpdate)
	}
	return nil
}

// Test that multiple New() returns the same Client. And only when the last
// client is closed, the underlying client is closed.
func (s) TestClientNewSingleton(t *testing.T) {
	oldBootstrapNewConfig := bootstrapNewConfig
	bootstrapNewConfig = func() (*bootstrap.Config, error) {
		return &bootstrap.Config{
			BalancerName: testXDSServer,
			Creds:        grpc.WithInsecure(),
			NodeProto:    xdstestutils.EmptyNodeProtoV2,
		}, nil
	}
	defer func() { bootstrapNewConfig = oldBootstrapNewConfig }()

	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	// The first New(). Should create a Client and a new APIClient.
	client, err := newRefCounted()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	clientImpl := client.clientImpl
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testAPIClient)

	// Call New() again. They should all return the same client implementation,
	// and should not create new API client.
	const count = 9
	for i := 0; i < count; i++ {
		tc, terr := newRefCounted()
		if terr != nil {
			client.Close()
			t.Fatalf("%d-th call to New() failed with error: %v", i, terr)
		}
		if tc.clientImpl != clientImpl {
			client.Close()
			tc.Close()
			t.Fatalf("%d-th call to New() got a different client %p, want %p", i, tc.clientImpl, clientImpl)
		}

		sctx, scancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
		defer scancel()
		_, err := apiClientCh.Receive(sctx)
		if err == nil {
			client.Close()
			t.Fatalf("%d-th call to New() created a new API client", i)
		}
	}

	// Call Close(). Nothing should be actually closed until the last ref calls
	// Close().
	for i := 0; i < count; i++ {
		client.Close()
		if clientImpl.done.HasFired() {
			t.Fatalf("%d-th call to Close(), unexpected done in the client implemenation", i)
		}
		if apiClient.done.HasFired() {
			t.Fatalf("%d-th call to Close(), unexpected done in the API client", i)
		}
	}

	// Call the last Close(). The underlying implementation and API Client
	// should all be closed.
	client.Close()
	if !clientImpl.done.HasFired() {
		t.Fatalf("want client implementation to be closed, got not done")
	}
	if !apiClient.done.HasFired() {
		t.Fatalf("want API client to be closed, got not done")
	}

	// Call New() again after the previous Client is actually closed. Should
	// create a Client and a new APIClient.
	client2, err2 := newRefCounted()
	if err2 != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client2.Close()
	c2, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient2 := c2.(*testAPIClient)

	// The client wrapper with ref count should be the same.
	if client2 != client {
		t.Fatalf("New() after Close() should return the same client wrapper, got different %p, %p", client2, client)
	}
	if client2.clientImpl == clientImpl {
		t.Fatalf("New() after Close() should return different client implementation, got the same %p", client2.clientImpl)
	}
	if apiClient2 == apiClient {
		t.Fatalf("New() after Close() should return different API client, got the same %p", apiClient2)
	}
}
