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
	"net"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
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

// TestConnectedMetric_Reconnection verifies "grpc.xds_client.connected" metric
// accuracy during flaky network conditions. It uses a restartable listener and
// a dial interceptor to pause connection attempts. It asserts that the
// connected state pulses to 1 when a stream succeeds, remains 1 immediately
// after failures before retries, transitions to 0 after subsequent failures,
// and returns to 1 only after successful stream recreation.
func (s) TestConnectedMetric_Reconnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	sendResponse := make(chan struct{})
	streamOpened := make(chan struct{}, 10)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamOpen: func(ctx context.Context, _ int64, _ string) error {
			select {
			case streamOpened <- struct{}{}:
			case <-ctx.Done():
			}
			return nil
		},
		OnStreamResponse: func(ctx context.Context, _ int64, _ *v3discoverypb.DiscoveryRequest, _ *v3discoverypb.DiscoveryResponse) {
			select {
			case <-sendResponse:
			case <-ctx.Done():
			}
		},
	})
	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}
	// streamAttemptCh is used by the interceptor to notify the test that a
	// stream is attempting to open.
	streamAttemptCh := testutils.NewChannel()
	// commandCh is used by the test to tell the interceptor whether to block or pass.
	commandCh := testutils.NewChannel()
	// blocked is used by the interceptor to signal the test that it has
	// successfully blocked the stream creation attempt.
	blocked := make(chan struct{})
	// unblock is used by the test to release the interceptor and allow the
	// stream creation to proceed.
	unblock := make(chan struct{})

	// customGRPCNewClient overrides the gRPC client creation to inject a stream
	// interceptor that can block ADS stream creation attempts for testing
	// reconnection behavior.
	customGRPCNewClient := func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		interceptor := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, cops ...grpc.CallOption) (grpc.ClientStream, error) {
			streamAttemptCh.Send(struct{}{}) // Notify test

			cmd, err := commandCh.Receive(ctx) // Wait for command
			if err != nil {
				return nil, err
			}

			if cmd.(string) == "block" {
				select {
				case blocked <- struct{}{}:
				case <-ctx.Done():
				}
				// Pause stream creation until released by the test.
				<-unblock
			}
			return streamer(ctx, desc, cc, method, cops...)
		}
		opts = append(opts, grpc.WithStreamInterceptor(interceptor))
		return grpc.NewClient(target, opts...)
	}

	tmr := newTestMetricsReporter()
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle(), GRPCNewClient: customGRPCNewClient}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
		MetricsReporter: tmr,
	}

	waitForStreamSuccess := func() {
		for {
			select {
			case <-streamOpened:
				return
			case <-streamAttemptCh.C:
				commandCh.Send("pass")
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for stream success")
			}
		}
	}

	// Stop listener to force initial connection failure.
	lis.Stop()

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

	// Initiate watch to trigger connection attempt.
	client.WatchResource(listenerType.TypeURL, listenerName, noopListenerWatcher{})

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 0}); err != nil {
		t.Fatalf("XDSClientConnected check failed at start - got: %v, want 0", err)
	}

	// Verify state transitions to connected (Value: 1) when stream succeeds.
	lis.Restart()

	waitForStreamSuccess()

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 1}); err != nil {
		t.Fatalf("XDSClientConnected check failed after 1st NewStream - got: %v, want 1", err)
	}

	// Push resource update to confirm the connection is functional.
	close(sendResponse)
	if err := tmr.waitForSpecificMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	// Verify state remains connected (Value: 1) immediately after failure before
	// NewStream retry.
	lis.Stop()

	if _, err := streamAttemptCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for 2nd stream attempt (block): %v", err)
	}
	commandCh.Send("block")

	<-blocked

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 1}); err != nil {
		t.Fatalf("XDSClientConnected check failed while NewStream is blocked - got: %v, want 1", err)
	}

	// Verify state transitions to disconnected (Value: 0) after NewStream attempt
	// fails.
	close(unblock)

	if err := tmr.waitForSpecificMetric(ctx, &metrics.ServerFailure{ServerURI: mgmtServer.Address}); err != nil {
		t.Fatal(err)
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 0}); err != nil {
		t.Fatalf("XDSClientConnected check failed after NewStream failure - got: %v, want 0", err)
	}

	sendResponse = make(chan struct{})

	// Verify state remains disconnected (Value: 0) while waiting for the first
	// response on the new stream.
	lis.Restart()

	// Clear channels to ensure fresh readings.
	for len(streamOpened) > 0 {
		<-streamOpened
	}

	waitForStreamSuccess()

	tmr.Drain()
	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 0}); err != nil {
		t.Fatalf("XDSClientConnected check failed after successful NewStream but before response - got: %v, want 0", err)
	}

	// Verify state returns to connected (Value: 1) after receiving the first
	// response on the new stream.
	close(sendResponse)

	if err := tmr.waitForSpecificMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err)
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 1}); err != nil {
		t.Fatalf("XDSClientConnected check failed after response- got: %v, want 1", err)
	}
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
