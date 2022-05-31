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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
	"google.golang.org/grpc/xds/internal/xdsclient/pubsub"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
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
	testXDSServer          = "xds-server"
	testXDSServerAuthority = "xds-server-authority"

	testAuthority  = "test-authority"
	testAuthority2 = "test-authority-2"
	testLDSName    = "test-lds"
	testRDSName    = "test-rds"
	testCDSName    = "test-cds"
	testEDSName    = "test-eds"

	defaultTestWatchExpiryTimeout = 500 * time.Millisecond
	defaultTestTimeout            = 5 * time.Second
	defaultTestShortTimeout       = 10 * time.Millisecond // For events expected to *not* happen.
)

func newStringP(s string) *string {
	return &s
}

func clientOpts() *bootstrap.Config {
	return &bootstrap.Config{
		XDSServer: &bootstrap.ServerConfig{
			ServerURI: testXDSServer,
			Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
			NodeProto: xdstestutils.EmptyNodeProtoV2,
		},
		Authorities: map[string]*bootstrap.Authority{
			testAuthority: {
				XDSServer: &bootstrap.ServerConfig{
					ServerURI: testXDSServerAuthority,
					Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
					NodeProto: xdstestutils.EmptyNodeProtoV2,
				},
			},
		},
	}
}

type testController struct {
	// config is the config this controller is created with.
	config *bootstrap.ServerConfig

	done          *grpcsync.Event
	addWatches    map[xdsresource.ResourceType]*testutils.Channel
	removeWatches map[xdsresource.ResourceType]*testutils.Channel
}

func overrideNewController(t *testing.T) *testutils.Channel {
	origNewController := newController
	ch := testutils.NewChannel()
	newController = func(config *bootstrap.ServerConfig, pubsub *pubsub.Pubsub, validator xdsresource.UpdateValidatorFunc, logger *grpclog.PrefixLogger, _ func(int) time.Duration) (controllerInterface, error) {
		ret := newTestController(config)
		ch.Send(ret)
		return ret, nil
	}
	t.Cleanup(func() { newController = origNewController })
	return ch
}

func newTestController(config *bootstrap.ServerConfig) *testController {
	addWatches := map[xdsresource.ResourceType]*testutils.Channel{
		xdsresource.ListenerResource:    testutils.NewChannel(),
		xdsresource.RouteConfigResource: testutils.NewChannel(),
		xdsresource.ClusterResource:     testutils.NewChannel(),
		xdsresource.EndpointsResource:   testutils.NewChannel(),
	}
	removeWatches := map[xdsresource.ResourceType]*testutils.Channel{
		xdsresource.ListenerResource:    testutils.NewChannel(),
		xdsresource.RouteConfigResource: testutils.NewChannel(),
		xdsresource.ClusterResource:     testutils.NewChannel(),
		xdsresource.EndpointsResource:   testutils.NewChannel(),
	}
	return &testController{
		config:        config,
		done:          grpcsync.NewEvent(),
		addWatches:    addWatches,
		removeWatches: removeWatches,
	}
}

func (c *testController) AddWatch(resourceType xdsresource.ResourceType, resourceName string) {
	c.addWatches[resourceType].Send(resourceName)
}

func (c *testController) RemoveWatch(resourceType xdsresource.ResourceType, resourceName string) {
	c.removeWatches[resourceType].Send(resourceName)
}

func (c *testController) ReportLoad(server string) (*load.Store, func()) {
	panic("ReportLoad is not implemented")
}

func (c *testController) Close() {
	c.done.Fire()
}

// TestWatchCallAnotherWatch covers the case where watch() is called inline by a
// callback. It makes sure it doesn't cause a deadlock.
func (s) TestWatchCallAnotherWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Start a watch for some resource, so that the controller and update
	// handlers are built for this authority. The test needs these to make an
	// inline watch in a callback.
	client, ctrlCh := testClientSetup(t, false)
	newWatch(t, client, xdsresource.ClusterResource, "doesnot-matter")
	controller, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, xdsresource.ClusterResource, "doesnot-matter")

	clusterUpdateCh := testutils.NewChannel()
	firstTime := true
	client.WatchCluster(testCDSName, func(update xdsresource.ClusterUpdate, err error) {
		clusterUpdateCh.Send(xdsresource.ClusterUpdateErrTuple{Update: update, Err: err})
		// Calls another watch inline, to ensure there's deadlock.
		client.WatchCluster("another-random-name", func(xdsresource.ClusterUpdate, error) {})

		if _, err := controller.addWatches[xdsresource.ClusterResource].Receive(ctx); firstTime && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		firstTime = false
	})
	if _, err := controller.addWatches[xdsresource.ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := xdsresource.ClusterUpdate{ClusterName: testEDSName}
	updateHandler.NewClusters(map[string]xdsresource.ClusterUpdateErrTuple{testCDSName: {Update: wantUpdate}}, xdsresource.UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// The second update needs to be different in the underlying resource proto
	// for the watch callback to be invoked.
	wantUpdate2 := xdsresource.ClusterUpdate{ClusterName: testEDSName + "2", Raw: &anypb.Any{}}
	updateHandler.NewClusters(map[string]xdsresource.ClusterUpdateErrTuple{testCDSName: {Update: wantUpdate2}}, xdsresource.UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}
}

func verifyListenerUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate xdsresource.ListenerUpdate, wantErr error) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for listener update: %v", err)
	}
	gotUpdate := u.(xdsresource.ListenerUpdateErrTuple)
	if wantErr != nil {
		if !strings.Contains(gotUpdate.Err.Error(), wantErr.Error()) {
			return fmt.Errorf("unexpected error: %v, want %v", gotUpdate.Err, wantErr)
		}
		return nil
	}
	if gotUpdate.Err != nil || !cmp.Equal(gotUpdate.Update, wantUpdate, protocmp.Transform()) {
		return fmt.Errorf("unexpected endpointsUpdate: (%v, %v), want: (%v, nil)", gotUpdate.Update, gotUpdate.Err, wantUpdate)
	}
	return nil
}

func verifyRouteConfigUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate xdsresource.RouteConfigUpdate, wantErr error) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for route configuration update: %v", err)
	}
	gotUpdate := u.(xdsresource.RouteConfigUpdateErrTuple)
	if wantErr != nil {
		if !strings.Contains(gotUpdate.Err.Error(), wantErr.Error()) {
			return fmt.Errorf("unexpected error: %v, want %v", gotUpdate.Err, wantErr)
		}
		return nil
	}
	if gotUpdate.Err != nil || !cmp.Equal(gotUpdate.Update, wantUpdate, protocmp.Transform()) {
		return fmt.Errorf("unexpected route config update: (%v, %v), want: (%v, nil)", gotUpdate.Update, gotUpdate.Err, wantUpdate)
	}
	return nil
}

func verifyClusterUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate xdsresource.ClusterUpdate, wantErr error) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for cluster update: %v", err)
	}
	gotUpdate := u.(xdsresource.ClusterUpdateErrTuple)
	if wantErr != nil {
		if !strings.Contains(gotUpdate.Err.Error(), wantErr.Error()) {
			return fmt.Errorf("unexpected error: %v, want %v", gotUpdate.Err, wantErr)
		}
		return nil
	}
	if !cmp.Equal(gotUpdate.Update, wantUpdate, protocmp.Transform()) {
		return fmt.Errorf("unexpected clusterUpdate: (%v, %v), want: (%v, nil)", gotUpdate.Update, gotUpdate.Err, wantUpdate)
	}
	return nil
}

func verifyEndpointsUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate xdsresource.EndpointsUpdate, wantErr error) error {
	u, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for endpoints update: %v", err)
	}
	gotUpdate := u.(xdsresource.EndpointsUpdateErrTuple)
	if wantErr != nil {
		if !strings.Contains(gotUpdate.Err.Error(), wantErr.Error()) {
			return fmt.Errorf("unexpected error: %v, want %v", gotUpdate.Err, wantErr)
		}
		return nil
	}
	if gotUpdate.Err != nil || !cmp.Equal(gotUpdate.Update, wantUpdate, cmpopts.EquateEmpty(), protocmp.Transform()) {
		return fmt.Errorf("unexpected endpointsUpdate: (%v, %v), want: (%v, nil)", gotUpdate.Update, gotUpdate.Err, wantUpdate)
	}
	return nil
}
