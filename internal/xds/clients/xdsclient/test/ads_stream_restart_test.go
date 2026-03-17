/*
 *
 * Copyright 2024 gRPC authors.
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

package xdsclient_test

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Tests that an ADS stream is restarted after a connection failure. Also
// verifies that if there were any watches registered before the connection
// failed, those resources are re-requested after the stream is restarted.
func (s) TestADS_ResourcesAreRequestedAfterStreamRestart(t *testing.T) {
	// Create a restartable listener that can simulate a broken ADS stream.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server that uses a couple of channels to inform
	// the test about the specific LDS and CDS resource names being requested.
	ldsResourcesCh := make(chan []string, 2)
	streamOpened := make(chan struct{}, 1)
	streamClosed := make(chan struct{}, 1)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			t.Logf("Received request for resources: %v of type %s", req.GetResourceNames(), req.GetTypeUrl())

			// Drain the resource name channels before writing to them to ensure
			// that the most recently requested names are made available to the
			// test.
			if req.GetTypeUrl() == version.V3ListenerURL {
				select {
				case <-ldsResourcesCh:
				default:
				}
				ldsResourcesCh <- req.GetResourceNames()
			}
			return nil
		},
		OnStreamClosed: func(int64, *v3corepb.Node) {
			select {
			case streamClosed <- struct{}{}:
			default:
			}

		},
		OnStreamOpen: func(context.Context, int64, string) error {
			select {
			case streamOpened <- struct{}{}:
			default:
			}
			return nil
		},
	})

	// Create a listener resource on the management server.
	const listenerName = "listener"
	const routeConfigName = "route-config"
	nodeID := uuid.New().String()
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, routeConfigName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS client pointing to the above server.
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	client := createXDSClient(t, mgmtServer.Address, nodeID, grpctransport.NewBuilder(configs))

	// Register a watch for a listener resource.
	lw := newListenerWatcher()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, listenerName, lw)
	defer ldsCancel()

	// Verify that an ADS stream is opened and an LDS request with the above
	// resource name is sent.
	select {
	case <-streamOpened:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for ADS stream to open")
	}
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerName}); err != nil {
		t.Fatal(err)
	}

	// Verify the update received by the watcher.
	wantListenerUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: routeConfigName,
		},
	}
	if err := verifyListenerUpdate(ctx, lw.updateCh, wantListenerUpdate); err != nil {
		t.Fatal(err)
	}

	// Create another listener resource on the management server, in addition
	// to the existing listener resource.
	const listenerName2 = "listener2"
	const routeConfigName2 = "route-config2"
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, routeConfigName), e2e.DefaultClientListener(listenerName2, routeConfigName2)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Register a watch for another listener resource, and verify that a LDS request
	// with the both listener resource names are sent.
	lw2 := newListenerWatcher()
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, listenerName2, lw2)
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerName, listenerName2}); err != nil {
		t.Fatal(err)
	}

	// Verify the update received by the watcher.
	wantListenerUpdate = listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: routeConfigName2,
		},
	}
	if err := verifyListenerUpdate(ctx, lw2.updateCh, wantListenerUpdate); err != nil {
		t.Fatal(err)
	}

	// Cancel the watch for the second listener resource, and verify that an LDS
	// request with only first listener resource names is sent.
	ldsCancel2()
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerName}); err != nil {
		t.Fatal(err)
	}

	// Stop the restartable listener and wait for the stream to close.
	lis.Stop()
	select {
	case <-streamClosed:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for ADS stream to close")
	}

	// Restart the restartable listener and wait for the stream to open.
	lis.Restart()
	select {
	case <-streamOpened:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for ADS stream to open")
	}

	// Verify that the first listener resource is requested again.
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerName}); err != nil {
		t.Fatal(err)
	}

	// Wait for a short duration and verify that no LDS request is sent, since
	// there are no resources being watched.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case names := <-ldsResourcesCh:
		t.Fatalf("LDS request sent for resource names %v, when expecting no request", names)
	}
}
