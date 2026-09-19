/*
 *
 * Copyright 2025 gRPC authors.
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
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/syncutil"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/metrics"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// TestResourceUpdateMetrics configures an xDS client, and a management server
// to send valid and invalid LDS updates, and verifies that the expected metrics
// for both good and bad updates are emitted.
func (s) TestResourceUpdateMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := newTestMetricsReporter()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: l})
	const listenerResourceName = "test-listener-resource"
	const routeConfigurationName = "test-route-configuration-resource"
	nodeID := uuid.New().String()
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerResourceName, routeConfigurationName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp resource names used in this test do not specify an
		// authority. These will end up looking up an entry with the
		// empty key in the authorities map. Having an entry with an
		// empty key and empty configuration, results in these
		// resources also using the top-level configuration.
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
		MetricsReporter: tmr,
	}
	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Watch the valid listener configured on the management server. This should
	// cause a resource update valid metric to emit eventually.
	client.WatchResource(listenerType.TypeURL, listenerResourceName, noopListenerWatcher{})
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	// Update management server with a bad update. This should cause a resource
	// update invalid metric to emit eventually.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerResourceName, routeConfigurationName)},
		SkipValidation: true,
	}
	resources.Listeners[0].ApiListener = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateInvalid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	// Resource update valid metric should have not emitted.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if err := tmr.waitForMetric(sCtx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err == nil {
		t.Fatal("tmr.WaitForInt64Count(ctx, mdWant) succeeded when expected to timeout.")
	}
}

// TestServerFailureMetrics_BeforeResponseRecv configures an xDS client, and a
// management server. It then register a watcher and stops the management
// server before sending a resource update, and verifies that the expected
// metric for server failure is emitted.
func (s) TestServerFailureMetrics_BeforeResponseRecv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := newTestMetricsReporter()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	lis := testutils.NewRestartableListener(l)
	streamOpened := make(chan struct{}, 1)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamOpen: func(context.Context, int64, string) error {
			select {
			case streamOpened <- struct{}{}:
			default:
			}
			return nil
		},
	})

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp resource names used in this test do not specify an
		// authority. These will end up looking up an entry with the
		// empty key in the authorities map. Having an entry with an
		// empty key and empty configuration, results in these
		// resources also using the top-level configuration.
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
		MetricsReporter: tmr,
	}
	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	const listenerResourceName = "test-listener-resource"

	// Watch for the listener on the above management server.
	client.WatchResource(listenerType.TypeURL, listenerResourceName, noopListenerWatcher{})
	// Verify that an ADS stream is opened and an LDS request with the above
	// resource name is sent.
	select {
	case <-streamOpened:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for ADS stream to open")
	}

	// Close the listener and ensure that the ADS stream breaks. This should
	// cause a server failure metric to emit eventually.
	lis.Stop()

	// Restart to prevent the attempt to create a new ADS stream after back off.
	lis.Restart()

	if err := tmr.waitForMetric(ctx, &metrics.ServerFailure{ServerURI: mgmtServer.Address}); err != nil {
		t.Fatal(err)
	}
}

// TestServerFailureMetrics_AfterResponseRecv configures an xDS client and a
// management server to send a valid LDS update, and verifies that the
// successful update metric is emitted. When the client ACKs the update, the
// server returns an error, breaking the stream. The test then verifies that the
// server failure metric is not emitted, because the ADS stream was closed after
// a response was received on the stream. Finally, the test waits for the client
// to establish a new stream and verifies that the client emits a metric after
// receiving a successful update.
func (s) TestServerFailureMetrics_AfterResponseRecv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := newTestMetricsReporter()
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	streamCreationQuota := make(chan struct{}, 1)
	streamCreationQuota <- struct{}{}

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamOpen: func(context.Context, int64, string) error {
			// The following select block is used to block stream creation after
			// the first stream has failed, but while we are waiting to verify
			// that the failure metric is not reported.
			select {
			case <-streamCreationQuota:
			case <-ctx.Done():
			}
			return nil
		},
		OnStreamRequest: func(streamID int64, req *v3discoverypb.DiscoveryRequest) error {
			// We only want the ACK on the first stream to return an error
			// (leading to stream closure), without effecting subsequent stream
			// attempts.
			if streamID == 1 && req.GetVersionInfo() != "" {
				return errors.New("test configured error")
			}
			return nil
		}},
	)
	const listenerResourceName = "test-listener-resource"
	const routeConfigurationName = "test-route-configuration-resource"
	nodeID := uuid.New().String()
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerResourceName, routeConfigurationName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp resource names used in this test do not specify an
		// authority. These will end up looking up an entry with the
		// empty key in the authorities map. Having an entry with an
		// empty key and empty configuration, results in these
		// resources also using the top-level configuration.
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
		MetricsReporter: tmr,
	}
	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Watch the valid listener configured on the management server. This should
	// cause a resource update valid metric to emit eventually.
	client.WatchResource(listenerType.TypeURL, listenerResourceName, noopListenerWatcher{})
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	// When the client sends an ACK, the management server would reply with an
	// error, breaking the stream.
	// Server failure should still have no recording point.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	failureMetric := &metrics.ServerFailure{ServerURI: mgmtServer.Address}
	if err := tmr.waitForMetric(sCtx, failureMetric); err == nil {
		t.Fatalf("tmr.waitForMetric(%v) succeeded when expected to timeout.", failureMetric)
	} else if sCtx.Err() == nil {
		t.Fatalf("tmr.WaitForInt64Count(%v) = %v, want context deadline exceeded", failureMetric, err)
	}
	// Unblock stream creation and verify that an update is received
	// successfully.
	close(streamCreationQuota)
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}
}

// TestConnectedMetric verifies the "grpc.xds_client.connected" metric state
// transitions. It begins by ensuring no metrics are reported before connection
// is attempted. Then it establishes a connection by watching a valid resource
// and verifies the connected state pulses to 1. Finally, it stops the
// management server and verifies the state drops back to 0.
func (s) TestConnectedMetric(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := newTestMetricsReporter()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: l})
	nodeID := uuid.New().String()

	xdsClientConfig := xdsclient.Config{
		Servers: []xdsclient.ServerConfig{{
			ServerIdentifier: clients.ServerIdentifier{
				ServerURI:  mgmtServer.Address,
				Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
			},
		}},
		Node: clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(map[string]grpctransport.Config{
			"insecure": {Credentials: insecure.NewBundle()},
		}),
		ResourceTypes: map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType},
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
		MetricsReporter: tmr,
	}
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	tmr.triggerAsyncMetrics()
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if err := tmr.waitForSpecificMetric(sCtx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address}); err == nil {
		t.Fatal("XDSClientConnected metric reported before any watch was started")
	}

	const listenerName = "test-listener-resource"
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, "route-config")},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}
	client.WatchResource(listenerType.TypeURL, listenerName, noopListenerWatcher{})

	// Wait for the update to ensure we are connected.
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	// Now trigger async metrics.
	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 1}); err != nil {
		t.Fatal(err)
	}

	mgmtServer.Stop()

	// Wait for the synchronous server failure metric to confirm disconnect.
	if err := tmr.waitForSpecificMetric(ctx, &metrics.ServerFailure{ServerURI: mgmtServer.Address}); err != nil {
		t.Fatal(err)
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 0}); err != nil {
		t.Fatal(err)
	}

	// Verify async metric reporters are unregistered when client closes.
	client.Close()

	if count := tmr.numAsyncReporters(); count != 0 {
		t.Fatalf("Async reporter not unregistered after client close, count: %d", count)
	}

	// Drain the channel of any leftover metrics from previous pulses.
	tmr.Drain()

	tmr.triggerAsyncMetrics()
	// No metrics should be reported now because there are no reporters.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := tmr.Receive(sCtx); err == nil {
		t.Fatal("Metrics reported after all reporters were unregistered")
	}
}

// TestResourceMetrics verifies that the xDS client correctly tracks resource
// states (acked, nacked_but_cached) in "grpc.xds_client.resources" metric. It
// watches a resource, pushes a valid update from the management server, and
// asserts that the resource transitions to 'acked' state. Then it pushes an
// invalid update and asserts that the resource transitions to
// 'nacked_but_cached' state.
func (s) TestResourceMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := newTestMetricsReporter()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: l})
	nodeID := uuid.New().String()

	xdsClientConfig := xdsclient.Config{
		Servers: []xdsclient.ServerConfig{{
			ServerIdentifier: clients.ServerIdentifier{
				ServerURI:  mgmtServer.Address,
				Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
			},
		}},
		Node: clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(map[string]grpctransport.Config{
			"insecure": {Credentials: insecure.NewBundle()},
		}),
		ResourceTypes: map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType},
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
		MetricsReporter: tmr,
	}
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	const listenerName = "test-listener-resource"
	const routeConfigName = "test-route-configuration-resource"

	client.WatchResource(listenerType.TypeURL, listenerName, noopListenerWatcher{})

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, routeConfigName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	// Trigger async metrics.
	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "#old", // Default value for old-style non-xdstp as per gRFC A78
		ResourceType: "ListenerResource",
		CacheState:   "acked",
		Count:        1,
	}); err != nil {
		t.Fatal(err)
	}

	resources.Listeners[0].ApiListener = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	// Wait for Invalid update.
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateInvalid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "#old",
		ResourceType: "ListenerResource",
		CacheState:   "nacked_but_cached",
		Count:        1,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestResourceMetrics_Extended verifies complex resource state transitions
// (requested, does_not_exist, nacked) in "grpc.xds_client.resources" metric
// across multiple resources. It watches several resources, pushes a partial
// update (some valid, some invalid) to transition them to 'acked' and 'nacked'
// states, then management server removes a resource and asserts that active
// watchers transition back to 'requested' while omitted resources transition
// to 'does_not_exist'.
func (s) TestResourceMetrics_Extended(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := newTestMetricsReporter()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: l})
	nodeID := uuid.New().String()

	xdsClientConfig := xdsclient.Config{
		Servers: []xdsclient.ServerConfig{{
			ServerIdentifier: clients.ServerIdentifier{
				ServerURI:  mgmtServer.Address,
				Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
			},
		}},
		Node: clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(map[string]grpctransport.Config{
			"insecure": {Credentials: insecure.NewBundle()},
		}),
		ResourceTypes: map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType},
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
		MetricsReporter: tmr,
	}
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	resRequested1 := "res-requested-1"
	resRequested2 := "res-requested-2"
	resNacked1 := "res-nacked-1"
	resNacked2 := "res-nacked-2"
	resRemoved := "res-not-exist"

	resList := []string{resRequested1, resRequested2, resNacked1, resNacked2, resRemoved}
	for _, res := range resList {
		client.WatchResource(listenerType.TypeURL, res, noopListenerWatcher{})
	}

	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(resNacked1, "route-config"),
			e2e.DefaultClientListener(resNacked2, "route-config"),
			e2e.DefaultClientListener(resRemoved, "route-config"),
		},
		SkipValidation: true,
	}
	resources.Listeners[0].ApiListener = nil
	resources.Listeners[1].ApiListener = nil

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	// Wait for the resource update to be accepted.
	if err := tmr.waitForSpecificMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}
	// Push an empty update to remove all resources from the management server.
	resourcesEmpty := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resourcesEmpty); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "#old",
		ResourceType: "ListenerResource",
		CacheState:   "requested",
		Count:        2,
	}); err != nil {
		t.Fatalf("Failed to verify requested count: %v", err)
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "#old",
		ResourceType: "ListenerResource",
		CacheState:   "nacked",
		Count:        2,
	}); err != nil {
		t.Fatalf("Failed to verify nacked count: %v", err)
	}

	// Wait for the xDS client to process the empty update and trigger
	// ResourceNotFound for omitted resources immediately.
	lw := newListenerWatcher()
	client.WatchResource(listenerType.TypeURL, resRemoved, lw)

	// Verify that resources missing from the authoritative update transition to
	// the does_not_exist state.
	if err := verifyResourceErrorType(ctx, lw.resourceErrCh, xdsresource.ErrorTypeResourceNotFound, ""); err != nil {
		t.Fatal(err)
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "#old",
		ResourceType: "ListenerResource",
		CacheState:   "does_not_exist",
		Count:        1,
	}); err != nil {
		t.Fatalf("Failed to verify does_not_exist count: %v", err)
	}
}

// TestConnectedMetric_Reconnection verifies the behavior of the
// "grpc.xds_client.connected" metric as the xDS client goes through stream
// failures and reconnections. It uses a restartable listener to break the
// stream, a client-side stream interceptor to hold up stream creation
// attempts, and callbacks on the management server to block responses and to
// wait for requests received by the server. It verifies that the metric
// reports:
//   - 0 before the client connects to the server for the very first time
//   - 1 as soon as the very first stream is created, even before a response
//     is received on it
//   - 1 when a stream on which a response was previously received breaks,
//     and a new stream creation attempt is still in progress
//   - 0 when a stream creation attempt fails
//   - 0 on a newly created stream, until a response is received on it
//   - 1 once a response is received on the newly created stream
func (s) TestConnectedMetric_Reconnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	// Start a management server whose stream callbacks are wired up to the
	// fixture. This allows the test to block server responses and to wait
	// for requests received by the server.
	fixture := newReconnectionTestFixture(t)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener:         lis,
		OnStreamRequest:  fixture.onStreamRequest,
		OnStreamResponse: fixture.onStreamResponse,
	})
	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	// Inject a stream interceptor that allows the test to hold up stream
	// creation attempts from the client.
	grpcNewClient := func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		opts = append(opts, grpc.WithStreamInterceptor(fixture.streamInterceptor))
		return grpc.NewClient(target, opts...)
	}
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle(), GRPCNewClient: grpcNewClient}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
		MetricsReporter: fixture.tmr,
	}

	// Stop the listener before creating the client to force initial stream
	// creation attempts to fail, and block server responses so that the test
	// controls when the first response is sent.
	lis.Stop()
	fixture.blockServerResponses()

	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	const listenerName = "test-listener-resource"
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, "route-config")},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	// Watch a resource to kick off stream creation attempts, which fail
	// because the listener is stopped, and verify that the metric reports 0.
	client.WatchResource(listenerType.TypeURL, listenerName, noopListenerWatcher{})
	if err := fixture.waitForConnectedValue(ctx, 0); err != nil {
		t.Fatalf("Connected metric check before the first connection: %v", err)
	}

	// Restart the listener and wait for the server to receive a request,
	// which guarantees that the client has finished creating the stream. The
	// very first stream is considered established as soon as it is created,
	// so the metric must report 1 even before a response is received.
	fixture.expectNextStreamRequest()
	lis.Restart()
	if err := fixture.waitForStreamRequest(ctx); err != nil {
		t.Fatalf("Waiting for the request on the first stream: %v", err)
	}
	if err := fixture.verifyConnectedValue(1); err != nil {
		t.Fatalf("Connected metric check after first stream creation: %v", err)
	}

	// Release the pending response and wait for the client to ACK it. The
	// ACK guarantees that the client has received a response on the stream:
	// only a stream on which a response was received remains established
	// when it breaks, until a new stream creation attempt fails.
	fixture.expectNextStreamRequest()
	fixture.allowServerResponses()
	if err := fixture.waitForStreamRequest(ctx); err != nil {
		t.Fatalf("Waiting for the client to ACK the response: %v", err)
	}

	// Stop the listener to break the stream, holding up the stream creation
	// attempt that follows. Verify that the metric still reports 1, since
	// the new attempt has not failed yet.
	fixture.blockStreamAttempts()
	lis.Stop()
	if err := fixture.waitForStreamAttemptBlocked(ctx); err != nil {
		t.Fatalf("Waiting for a stream creation attempt to be blocked: %v", err)
	}
	if err := fixture.verifyConnectedValue(1); err != nil {
		t.Fatalf("Connected metric check while stream creation is blocked: %v", err)
	}

	// Release the blocked attempt and let it fail, since the listener is
	// still stopped. Verify that the metric transitions to 0.
	fixture.releaseBlockedStreamAttempt()
	if err := fixture.waitForConnectedValue(ctx, 0); err != nil {
		t.Fatalf("Connected metric check after stream creation failure: %v", err)
	}

	// Restart the listener, with server responses blocked, and wait for the
	// server to receive a request on a new stream. Streams created after the
	// very first are not considered established until a response is received
	// on them, so the metric must still report 0.
	fixture.expectNextStreamRequest()
	fixture.blockServerResponses()
	lis.Restart()
	if err := fixture.waitForStreamRequest(ctx); err != nil {
		t.Fatalf("Waiting for the request on the new stream: %v", err)
	}
	if err := fixture.verifyConnectedValue(0); err != nil {
		t.Fatalf("Connected metric check before response on the new stream: %v", err)
	}

	// Release the pending response and verify that the metric transitions
	// back to 1 once the client receives it.
	fixture.allowServerResponses()
	if err := fixture.waitForConnectedValue(ctx, 1); err != nil {
		t.Fatalf("Connected metric check after response on the new stream: %v", err)
	}
}

// reconnectionTestFixture contains the synchronization machinery used by
// TestConnectedMetric_Reconnection to deterministically drive the xDS client
// through stream failures and reconnections.
type reconnectionTestFixture struct {
	// tmr is the metrics reporter registered with the xDS client under test.
	tmr *testMetricsReporter

	mu sync.Mutex // Guards all fields below.
	// reqReceived fires when the management server receives a request. It is
	// armed by expectNextStreamRequest, and is nil until then.
	reqReceived *syncutil.Event
	// sendResponse, when non-nil, holds back responses from the management
	// server until the channel is closed by allowServerResponses.
	sendResponse chan struct{}
	// attemptBlocked fires when a stream creation attempt from the client is
	// held up by the stream interceptor.
	attemptBlocked *syncutil.Event
	// attemptUnblock releases held up stream creation attempts when fired.
	// When nil, stream creation attempts proceed unhindered.
	attemptUnblock *syncutil.Event
}

func newReconnectionTestFixture(t *testing.T) *reconnectionTestFixture {
	f := &reconnectionTestFixture{tmr: newTestMetricsReporter()}
	// Guarantee that a blocked server response does not outlive the test: if
	// the test fails between blockServerResponses and allowServerResponses,
	// the management server's handler goroutine would otherwise remain
	// blocked in onStreamResponse and be flagged by the leak checker.
	t.Cleanup(f.allowServerResponses)
	return f
}

// onStreamRequest implements the management server's OnStreamRequest
// callback. It fires the reqReceived event, if armed via
// expectNextStreamRequest.
func (f *reconnectionTestFixture) onStreamRequest(int64, *v3discoverypb.DiscoveryRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reqReceived != nil {
		f.reqReceived.Fire()
	}
	return nil
}

// onStreamResponse implements the management server's OnStreamResponse
// callback. It holds back the response for as long as server responses are
// blocked. The given context is not the test's context: it comes from the
// management server's response, and for responses generated from its cache
// it is a background context that is never canceled. A test cleanup
// registered in newReconnectionTestFixture therefore releases blocked
// responses, guaranteeing that this callback eventually returns.
func (f *reconnectionTestFixture) onStreamResponse(ctx context.Context, _ int64, _ *v3discoverypb.DiscoveryRequest, _ *v3discoverypb.DiscoveryResponse) {
	f.mu.Lock()
	sendResponse := f.sendResponse
	f.mu.Unlock()

	if sendResponse == nil {
		return
	}
	select {
	case <-sendResponse:
	case <-ctx.Done():
	}
}

// streamInterceptor is a client-side stream interceptor that holds up stream
// creation attempts for as long as they are blocked via blockStreamAttempts.
func (f *reconnectionTestFixture) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	f.mu.Lock()
	blocked, unblock := f.attemptBlocked, f.attemptUnblock
	f.mu.Unlock()

	if unblock != nil {
		blocked.Fire()
		select {
		case <-unblock.Done():
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return streamer(ctx, desc, cc, method, opts...)
}

// expectNextStreamRequest arms the fixture to signal receipt of the next
// request by the management server, in a subsequent call to
// waitForStreamRequest.
func (f *reconnectionTestFixture) expectNextStreamRequest() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqReceived = syncutil.NewEvent()
}

// waitForStreamRequest blocks until the management server receives a request,
// following the previous call to expectNextStreamRequest.
func (f *reconnectionTestFixture) waitForStreamRequest(ctx context.Context) error {
	f.mu.Lock()
	reqReceived := f.reqReceived
	f.mu.Unlock()

	if reqReceived == nil {
		return errors.New("waitForStreamRequest called without a preceding call to expectNextStreamRequest")
	}
	select {
	case <-reqReceived.Done():
		return nil
	case <-ctx.Done():
		return errors.New("timeout waiting for the management server to receive a request")
	}
}

// blockServerResponses causes the management server to hold back responses
// until allowServerResponses is called.
func (f *reconnectionTestFixture) blockServerResponses() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendResponse = make(chan struct{})
}

// allowServerResponses releases responses held back by the management server,
// and allows subsequent responses to be sent unhindered.
func (f *reconnectionTestFixture) allowServerResponses() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendResponse != nil {
		close(f.sendResponse)
		f.sendResponse = nil
	}
}

// blockStreamAttempts causes stream creation attempts from the client to be
// held up in the stream interceptor until releaseBlockedStreamAttempt is
// called.
func (f *reconnectionTestFixture) blockStreamAttempts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attemptBlocked = syncutil.NewEvent()
	f.attemptUnblock = syncutil.NewEvent()
}

// waitForStreamAttemptBlocked blocks until a stream creation attempt from the
// client is held up in the stream interceptor.
func (f *reconnectionTestFixture) waitForStreamAttemptBlocked(ctx context.Context) error {
	f.mu.Lock()
	blocked := f.attemptBlocked
	f.mu.Unlock()

	if blocked == nil {
		return errors.New("waitForStreamAttemptBlocked called without a preceding call to blockStreamAttempts")
	}
	select {
	case <-blocked.Done():
		return nil
	case <-ctx.Done():
		return errors.New("timeout waiting for a stream creation attempt to be blocked")
	}
}

// releaseBlockedStreamAttempt releases the stream creation attempt currently
// held up in the stream interceptor, and allows subsequent attempts to
// proceed unhindered.
func (f *reconnectionTestFixture) releaseBlockedStreamAttempt() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attemptUnblock != nil {
		f.attemptUnblock.Fire()
	}
}

// connectedValue triggers a report of async metrics and returns the reported
// value of the "grpc.xds_client.connected" metric. The second return value
// is false if the metric was not reported, which is the case until the
// client creates a channel to the management server.
//
// Metrics reported earlier are discarded before triggering the report. And
// since this metric is only reported when async metrics reporting is
// triggered from this test, and reporting happens synchronously, the
// returned value is guaranteed to reflect the current state of the client.
func (f *reconnectionTestFixture) connectedValue() (int64, bool) {
	f.tmr.Drain()
	f.tmr.triggerAsyncMetrics()
	for {
		m, ok := f.tmr.receiveNonBlocking()
		if !ok {
			// The metric was not part of the triggered report.
			return 0, false
		}
		if cm, ok := m.(*metrics.XDSClientConnected); ok {
			return cm.Value, true
		}
	}
}

// verifyConnectedValue verifies that the "grpc.xds_client.connected" metric
// currently reports want.
func (f *reconnectionTestFixture) verifyConnectedValue(want int64) error {
	got, ok := f.connectedValue()
	if !ok {
		return errors.New("connected metric was not reported")
	}
	if got != want {
		return fmt.Errorf("connected metric reports %d, want %d", got, want)
	}
	return nil
}

// waitForConnectedValue polls until the "grpc.xds_client.connected" metric
// reports want, or the given context expires.
func (f *reconnectionTestFixture) waitForConnectedValue(ctx context.Context, want int64) error {
	var lastErr error
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if lastErr = f.verifyConnectedValue(want); lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for connected metric to report %d: %v", want, lastErr)
}

// TestResourceMetrics_AuthorityOldStyle verifies that the xDS client correctly
// falls back to '#old' in "grpc.xds_client.resources" metrics for legacy
// authorities. It watches a resource using default authority and asserts that
// the metric is reported under '#old' instead of an empty string.
func (s) TestResourceMetrics_AuthorityOldStyle(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	nodeID := uuid.New().String()

	serverCfg := xdsclient.ServerConfig{
		ServerIdentifier: clients.ServerIdentifier{
			ServerURI:  mgmtServer.Address,
			Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
		},
	}

	tmr := newTestMetricsReporter()
	xdsClientConfig := xdsclient.Config{
		Servers: []xdsclient.ServerConfig{serverCfg},
		Node:    clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(map[string]grpctransport.Config{
			"insecure": {Credentials: insecure.NewBundle()},
		}),
		ResourceTypes: map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType},
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{serverCfg}},
		},
		MetricsReporter: tmr,
	}

	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	const listenerName = "test-listener"

	client.WatchResource(listenerType.TypeURL, listenerName, newListenerWatcher())

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, "route-config")},
		SkipValidation: true,
	}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	// Verify that the client substitutes empty authority with '#old' in metrics.
	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "#old",
		ResourceType: "ListenerResource",
		CacheState:   "acked",
		Count:        1,
	}); err != nil {
		t.Fatalf("Failed to observe grpc.xds.authority '#old' metric substitution: %v", err)
	}
}
