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
	"sync"
	"testing"

	"github.com/google/uuid"
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
		t.Fatal(err.Error())
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
		t.Fatal(err.Error())
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
		t.Fatal(err.Error())
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
		t.Fatal(err.Error())
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
		t.Fatal(err.Error())
	}
}

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

	// Initial State: Not connected (until watch starts or channel created?).
	// The client creates channels on demand. WatchResource triggers channel creation.

	// Watch a resource to trigger connection.
	client.WatchResource(listenerType.TypeURL, "foo", noopListenerWatcher{})

	// Wait for connection.
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
		t.Fatal(err.Error())
	}

	// Now trigger async metrics.
	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientConnected{ServerURI: mgmtServer.Address, Value: 1}); err != nil {
		t.Fatal(err.Error())
	}

	// Stop server to force disconnect.
	mgmtServer.Stop()

	// Wait a bit for disconnect to be detected.
	// xDS client should detect it on stream failure.
	// We can wait for ServerFailure metric (synchronous).
	if err := tmr.waitForSpecificMetric(ctx, &metrics.ServerFailure{ServerURI: mgmtServer.Address}); err != nil {
		t.Fatal(err.Error())
	}
}

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

	// Requested state.
	client.WatchResource(listenerType.TypeURL, listenerName, noopListenerWatcher{})

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, routeConfigName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	// Wait for Valid update.
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err.Error())
	}

	// Trigger async metrics.
	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "", // Default authority
		ResourceType: "ListenerResource",
		CacheState:   "acked",
		Count:        1,
	}); err != nil {
		t.Fatal(err.Error())
	}

	// Nacked but cached.
	// Update with bad resource.
	resources.Listeners[0].ApiListener = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	// Wait for Invalid update.
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateInvalid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err.Error())
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "",
		ResourceType: "ListenerResource",
		CacheState:   "nacked_but_cached",
		Count:        1,
	}); err != nil {
		t.Fatal(err.Error())
	}
}

// TestResourceMetrics_Extended tests multiple resources in different states:
// - 2 in "requested" state
// - 2 in "nacked" state
// - 1 in "does_not_exist" state
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

	// Resource names
	resRequested1 := "res-requested-1"
	resRequested2 := "res-requested-2"
	resNacked1 := "res-nacked-1"
	resNacked2 := "res-nacked-2"
	resNotExist := "res-not-exist"

	resList := []string{resRequested1, resRequested2, resNacked1, resNacked2, resNotExist}
	for _, res := range resList {
		client.WatchResource(listenerType.TypeURL, res, noopListenerWatcher{})
	}

	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(resNacked1, "route-config"),
			e2e.DefaultClientListener(resNacked2, "route-config"),
			e2e.DefaultClientListener(resNotExist, "route-config"),
		},
		SkipValidation: true,
	}
	// Make Nacked resources invalid
	resources.Listeners[0].ApiListener = nil
	resources.Listeners[1].ApiListener = nil

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	// Wait for Res5 to be valid.
	if err := tmr.waitForSpecificMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err.Error())
	}
	resourcesEmpty := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resourcesEmpty); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	// Verify "requested" count 2
	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "",
		ResourceType: "ListenerResource",
		CacheState:   "requested",
		Count:        2,
	}); err != nil {
		t.Fatalf("Failed to verify requested count: %v", err)
	}

	// Verify "nacked" count 2
	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "",
		ResourceType: "ListenerResource",
		CacheState:   "nacked",
		Count:        2,
	}); err != nil {
		t.Fatalf("Failed to verify nacked count: %v", err)
	}

	// Wait for the does_not_exist state.
	// We need to wait for the client to process the empty update and mark the resource as removed.
	// We use a watcher to detect the specific error.
	removed := make(chan struct{})
	var closeOnce sync.Once
	client.WatchResource(listenerType.TypeURL, resNotExist, &testWatcher{
		onError: func(err error) {
			if xdsresource.ErrType(err) == xdsresource.ErrorTypeResourceNotFound {
				closeOnce.Do(func() { close(removed) })
			}
		},
	})

	// Verify "does_not_exist" count 1
	select {
	case <-removed:
	case <-ctx.Done():
		t.Fatal("timeout waiting for resource removal")
	}

	tmr.triggerAsyncMetrics()
	if err := tmr.waitForSpecificMetric(ctx, &metrics.XDSClientResourceStats{
		Authority:    "",
		ResourceType: "ListenerResource",
		CacheState:   "does_not_exist",
		Count:        1,
	}); err != nil {
		t.Fatalf("Failed to verify does_not_exist count: %v", err)
	}
}

type testWatcher struct {
	onError func(error)
}

func (w *testWatcher) ResourceChanged(_ xdsclient.ResourceData, onDone func()) { onDone() }
func (w *testWatcher) ResourceError(err error, onDone func()) {
	if w.onError != nil {
		w.onError(err)
	}
	onDone()
}
func (w *testWatcher) AmbientError(_ error, onDone func()) { onDone() }
