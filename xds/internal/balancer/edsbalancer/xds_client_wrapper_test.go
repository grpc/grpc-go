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

package edsbalancer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	xdsinternal "google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/version"
)

var (
	testServiceName    = "test/foo"
	testEDSClusterName = "test/service/eds"
)

// Given a list of resource names, verifies that EDS requests for the same are
// received at the fake server.
func verifyExpectedRequests(fs *fakeserver.Server, resourceNames ...string) error {
	wantReq := &xdspb.DiscoveryRequest{
		TypeUrl: version.V2EndpointsURL,
		Node:    xdstestutils.EmptyNodeProtoV2,
	}
	for _, name := range resourceNames {
		if name != "" {
			wantReq.ResourceNames = []string{name}
		}

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		req, err := fs.XDSRequestChan.Receive(ctx)
		if err != nil {
			return fmt.Errorf("timed out when expecting request {%+v} at fake server", wantReq)
		}
		edsReq := req.(*fakeserver.Request)
		if edsReq.Err != nil {
			return fmt.Errorf("eds RPC failed with err: %v", edsReq.Err)
		}
		if !proto.Equal(edsReq.Req, wantReq) {
			return fmt.Errorf("got EDS request %v, expected: %v, diff: %s", edsReq.Req, wantReq, cmp.Diff(edsReq.Req, wantReq, cmp.Comparer(proto.Equal)))
		}
	}
	return nil
}

// TestClientWrapperWatchEDS verifies that the clientWrapper registers an
// EDS watch for expected resource upon receiving an update from the top-level
// edsBalancer.
//
// The test does the following:
// * Starts a fake xDS server.
// * Creates a clientWrapper.
// * Sends updates with different edsServiceNames and expects new watches to be
//   registered.
func (s) TestClientWrapperWatchEDS(t *testing.T) {
	fakeServer, cleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer cleanup()
	t.Logf("Started fake xDS server at %s...", fakeServer.Address)

	cw := newXDSClientWrapper(nil, balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}}, nil)
	defer cw.close()
	t.Logf("Started xDS client wrapper for endpoint %s...", testServiceName)

	oldBootstrapConfigNew := bootstrapConfigNew
	bootstrapConfigNew = func() (*bootstrap.Config, error) {
		return &bootstrap.Config{
			BalancerName: fakeServer.Address,
			Creds:        grpc.WithInsecure(),
			NodeProto:    xdstestutils.EmptyNodeProtoV2,
		}, nil
	}
	defer func() { bootstrapConfigNew = oldBootstrapConfigNew }()

	// Update with an non-empty edsServiceName should trigger an EDS watch for
	// the same.
	cw.handleUpdate(&EDSConfig{
		BalancerName:   fakeServer.Address,
		EDSServiceName: "foobar-1",
	}, nil)
	if err := verifyExpectedRequests(fakeServer, "foobar-1"); err != nil {
		t.Fatal(err)
	}

	// Also test the case where the edsServerName changes from one non-empty
	// name to another, and make sure a new watch is registered. The previously
	// registered watch will be cancelled, which will result in an EDS request
	// with no resource names being sent to the server.
	cw.handleUpdate(&EDSConfig{
		BalancerName:   fakeServer.Address,
		EDSServiceName: "foobar-2",
	}, nil)
	if err := verifyExpectedRequests(fakeServer, "", "foobar-2"); err != nil {
		t.Fatal(err)
	}
}

// TestClientWrapperHandleUpdateError verifies that the clientWrapper handles
// errors from the edsWatch callback appropriately.
//
// The test does the following:
// * Creates a clientWrapper.
// * Creates a fakeclient.Client and passes it to the clientWrapper in attributes.
// * Verifies the clientWrapper registers an EDS watch.
// * Forces the fakeclient.Client to invoke the registered EDS watch callback with
//   an error. Verifies that the wrapper does not invoke the top-level
//   edsBalancer with the received error.
func (s) TestClientWrapperHandleUpdateError(t *testing.T) {
	edsRespChan := testutils.NewChannel()
	newEDS := func(update xdsclient.EndpointsUpdate, err error) {
		edsRespChan.Send(&edsUpdate{resp: update, err: err})
	}

	cw := newXDSClientWrapper(newEDS, balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}}, nil)
	defer cw.close()

	xdsC := fakeclient.NewClient()
	cw.handleUpdate(&EDSConfig{EDSServiceName: testEDSClusterName}, attributes.New(xdsinternal.XDSClientID, xdsC))

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotCluster, err := xdsC.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	if gotCluster != testEDSClusterName {
		t.Fatalf("xdsClient.WatchEndpoints() called with cluster: %v, want %v", gotCluster, testEDSClusterName)
	}
	watchErr := errors.New("EDS watch callback error")
	xdsC.InvokeWatchEDSCallback(xdsclient.EndpointsUpdate{}, watchErr)

	// The callback is called with an error, expect no update from edsRespChan.
	//
	// TODO: check for loseContact() when errors indicating "lose contact" are
	// handled correctly.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotUpdate, err := edsRespChan.Receive(ctx)
	if err != nil {
		t.Fatalf("edsBalancer failed to get edsUpdate %v", err)
	}
	update := gotUpdate.(*edsUpdate)
	if !cmp.Equal(update.resp, (xdsclient.EndpointsUpdate{})) || update.err != watchErr {
		t.Fatalf("want update {nil, %v}, got %+v", watchErr, update)
	}
}

// TestClientWrapperGetsXDSClientInAttributes verfies the case where the
// clientWrapper receives the xdsClient to use in the attributes section of the
// update.
func (s) TestClientWrapperGetsXDSClientInAttributes(t *testing.T) {
	oldxdsclientNew := xdsclientNew
	xdsclientNew = func(_ xdsclient.Options) (xdsClientInterface, error) {
		t.Fatalf("unexpected call to xdsclientNew when xds_client is set in attributes")
		return nil, nil
	}
	defer func() { xdsclientNew = oldxdsclientNew }()

	cw := newXDSClientWrapper(nil, balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}}, nil)
	defer cw.close()

	xdsC1 := fakeclient.NewClient()
	cw.handleUpdate(&EDSConfig{EDSServiceName: testEDSClusterName}, attributes.New(xdsinternal.XDSClientID, xdsC1))

	// Verify that the eds watch is registered for the expected resource name.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotCluster, err := xdsC1.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	if gotCluster != testEDSClusterName {
		t.Fatalf("xdsClient.WatchEndpoints() called with cluster: %v, want %v", gotCluster, testEDSClusterName)
	}

	// Pass a new client in the attributes. Verify that the watch is
	// re-registered on the new client, and that the old client is not closed
	// (because clientWrapper only closes clients that it creates, it does not
	// close client that are passed through attributes).
	xdsC2 := fakeclient.NewClient()
	cw.handleUpdate(&EDSConfig{EDSServiceName: testEDSClusterName}, attributes.New(xdsinternal.XDSClientID, xdsC2))
	gotCluster, err = xdsC2.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	if gotCluster != testEDSClusterName {
		t.Fatalf("xdsClient.WatchEndpoints() called with cluster: %v, want %v", gotCluster, testEDSClusterName)
	}

	if err := xdsC1.WaitForClose(ctx); err != context.DeadlineExceeded {
		t.Fatalf("clientWrapper closed xdsClient received in attributes")
	}
}
