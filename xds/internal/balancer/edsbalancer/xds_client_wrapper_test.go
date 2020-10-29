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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/internal/testutils"
	xdsinternal "google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

var (
	testServiceName    = "test/foo"
	testEDSClusterName = "test/service/eds"
)

// Given a list of resource names, verifies that EDS requests for the same are
// received at the fake server.
func verifyExpectedRequests(fc *fakeclient.Client, resourceNames ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	for _, name := range resourceNames {
		if name == "" {
			// ResourceName empty string indicates a cancel.
			if err := fc.WaitForCancelEDSWatch(ctx); err != nil {
				return fmt.Errorf("timed out when expecting resource %q", name)
			}
			return nil
		}

		resName, err := fc.WaitForWatchEDS(ctx)
		if err != nil {
			return fmt.Errorf("timed out when expecting resource %q, %p", name, fc)
		}
		if resName != name {
			return fmt.Errorf("got EDS request for resource %q, expected: %q", resName, name)
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
	xdsC := fakeclient.NewClientWithName(testBalancerNameFooBar)

	cw := newXDSClientWrapper(nil, nil)
	defer cw.close()
	t.Logf("Started xDS client wrapper for endpoint %s...", testServiceName)

	// Update with an non-empty edsServiceName should trigger an EDS watch for
	// the same.
	cw.handleUpdate(&EDSConfig{EDSServiceName: "foobar-1"}, attributes.New(xdsinternal.XDSClientID, xdsC))
	if err := verifyExpectedRequests(xdsC, "foobar-1"); err != nil {
		t.Fatal(err)
	}

	// Also test the case where the edsServerName changes from one non-empty
	// name to another, and make sure a new watch is registered. The previously
	// registered watch will be cancelled, which will result in an EDS request
	// with no resource names being sent to the server.
	cw.handleUpdate(&EDSConfig{EDSServiceName: "foobar-2"}, attributes.New(xdsinternal.XDSClientID, xdsC))
	if err := verifyExpectedRequests(xdsC, "", "foobar-2"); err != nil {
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

	cw := newXDSClientWrapper(newEDS, nil)
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
	cw := newXDSClientWrapper(nil, nil)
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
