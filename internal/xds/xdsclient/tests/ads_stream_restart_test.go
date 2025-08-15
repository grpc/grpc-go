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
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// waitForResourceNames waits for the wantNames to be received on namesCh.
// Returns a non-nil error if the context expires before that.
func waitForResourceNames(ctx context.Context, t *testing.T, namesCh chan []string, wantNames []string) error {
	t.Helper()

	var lastRequestedNames []string
	for ; ; <-time.After(defaultTestShortTimeout) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for resources %v to be requested from the management server. Last requested resources: %v", wantNames, lastRequestedNames)
		case gotNames := <-namesCh:
			if cmp.Equal(gotNames, wantNames, cmpopts.EquateEmpty(), cmpopts.SortSlices(func(s1, s2 string) bool { return s1 < s2 })) {
				return nil
			}
			lastRequestedNames = gotNames
		}
	}
}

// Tests that an ADS stream is restarted after a connection failure. Also
// verifies that if there were any watches registered before the connection
// failed, those resources are re-requested after the stream is restarted.
func (s) TestADS_ResourcesAreRequestedAfterStreamRestart(t *testing.T) {
	// Create a restartable listener that can simulate a broken ADS stream.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server that uses a couple of channels to inform
	// the test about the specific LDS and CDS resource names being requested.
	ldsResourcesCh := make(chan []string, 1)
	cdsResourcesCh := make(chan []string, 1)
	streamOpened := make(chan struct{}, 1)
	streamClosed := make(chan struct{}, 1)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			t.Logf("Received request for resources: %v of type %s", req.GetResourceNames(), req.GetTypeUrl())

			// Drain the resource name channels before writing to them to ensure
			// that the most recently requested names are made available to the
			// test.
			switch req.GetTypeUrl() {
			case version.V3ClusterURL:
				select {
				case <-cdsResourcesCh:
				default:
				}
				cdsResourcesCh <- req.GetResourceNames()
			case version.V3ListenerURL:
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

	// Create bootstrap configuration pointing to the above management server.
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)

	// Create an xDS client with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	client, close, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{
		Name: t.Name(),
	})
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Register a watch for a listener resource.
	lw := newListenerWatcher()
	ldsCancel := xdsresource.WatchListener(client, listenerName, lw)
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
		update: xdsresource.ListenerUpdate{
			RouteConfigName: routeConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, lw.updateCh, wantListenerUpdate); err != nil {
		t.Fatal(err)
	}

	// Create a cluster resource on the management server, in addition to the
	// existing listener resource.
	const clusterName = "cluster"
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, routeConfigName)},
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, clusterName, e2e.SecurityLevelNone)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Register a watch for a cluster resource, and verify that a CDS request
	// with the above resource name is sent.
	cw := newClusterWatcher()
	cdsCancel := xdsresource.WatchCluster(client, clusterName, cw)
	if err := waitForResourceNames(ctx, t, cdsResourcesCh, []string{clusterName}); err != nil {
		t.Fatal(err)
	}

	// Verify the update received by the watcher.
	wantClusterUpdate := clusterUpdateErrTuple{
		update: xdsresource.ClusterUpdate{
			ClusterName:    clusterName,
			EDSServiceName: clusterName,
		},
	}
	if err := verifyClusterUpdate(ctx, cw.updateCh, wantClusterUpdate); err != nil {
		t.Fatal(err)
	}

	// Cancel the watch for the above cluster resource, and verify that a CDS
	// request with no resource names is sent.
	cdsCancel()
	if err := waitForResourceNames(ctx, t, cdsResourcesCh, []string{}); err != nil {
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

	// Verify that the listener resource is requested again.
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerName}); err != nil {
		t.Fatal(err)
	}

	// Wait for a short duration and verify that no CDS request is sent, since
	// there are no resources being watched.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case names := <-cdsResourcesCh:
		t.Fatalf("CDS request sent for resource names %v, when expecting no request", names)
	}
}
