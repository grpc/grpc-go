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
	"net"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/grpc/xds/internal/clients/grpctransport"
	"google.golang.org/grpc/xds/internal/clients/internal/testutils/e2e"
	"google.golang.org/grpc/xds/internal/clients/xdsclient"
	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/xdsresource"
	"google.golang.org/grpc/xds/internal/clients/xdsclient/metrics"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
)

// TestResourceUpdateMetrics configures an xDS client, and a management server
// to send valid and invalid LDS updates, and verifies that the expected metrics
// for both good and bad updates are emitted.
func (s) TestResourceUpdateMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout*1000)
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

// TestServerFailureMetrics_AfterResponseRecv configures an xDS client, and a
// management server to send a valid LDS updates, and verifies that the
// resource update valid metric is emitted. It then closes the management server
// listener to close the ADS stream and verifies that the server failure metric
// is not emitted because the ADS stream was closed after having received a
// response on the stream.
func (s) TestServerFailureMetrics_AfterResponseRecv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := newTestMetricsReporter()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	lis := testutils.NewRestartableListener(l)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: lis})
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
	// cause a resource updates valid count to emit eventually.
	client.WatchResource(listenerType.TypeURL, listenerResourceName, noopListenerWatcher{})
	if err := tmr.waitForMetric(ctx, &metrics.ResourceUpdateValid{ServerURI: mgmtServer.Address, ResourceType: "ListenerResource"}); err != nil {
		t.Fatal(err.Error())
	}

	// Close the listener and ensure that the ADS stream breaks. This should
	// cause a server failure metric to emit eventually.
	lis.Stop()
	if ctx.Err() != nil {
		t.Fatalf("Timeout when waiting for ADS stream to close")
	}
	// Restart to prevent the attempt to create a new ADS stream after back off.
	lis.Restart()

	// Server failure should not have emitted.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if err := tmr.waitForMetric(sCtx, &metrics.ServerFailure{ServerURI: mgmtServer.Address}); err == nil {
		t.Fatal("tmr.WaitForInt64Count(ctx, mdWant) succeeded when expected to timeout.")
	}
}
