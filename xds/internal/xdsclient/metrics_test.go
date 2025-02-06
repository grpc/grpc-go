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

package xdsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/stats"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
)

type noopListenerWatcher struct{}

func (noopListenerWatcher) OnUpdate(_ *xdsresource.ListenerResourceData, onDone xdsresource.OnDoneFunc) {
	onDone()
}

func (noopListenerWatcher) OnError(_ error, onDone xdsresource.OnDoneFunc) {
	onDone()
}

func (noopListenerWatcher) OnResourceDoesNotExist(onDone xdsresource.OnDoneFunc) {
	onDone()
}

// TestResourceUpdateMetrics configures an xDS client, and a management server
// to send valid and invalid LDS updates, and verifies that the expected metrics
// for both good and bad updates are emitted.
func (s) TestResourceUpdateMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := stats.NewTestMetricsRecorder()
	l, err := testutils.LocalTCPListener()
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

	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		}]`, mgmtServer.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
		Authorities: map[string]json.RawMessage{
			"authority": []byte("{}"),
		},
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap configuration: %v", err)
	}

	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := NewPool(config)
	client, close, err := pool.NewClientForTesting(OptionsForTesting{
		Name:               t.Name(),
		WatchExpiryTimeout: defaultTestWatchExpiryTimeout,
		MetricsRecorder:    tmr,
	})
	if err != nil {
		t.Fatalf("Failed to create an xDS client: %v", err)
	}
	defer close()

	// Watch the valid listener configured on the management server. This should
	// cause a resource updates valid count to emit eventually.
	xdsresource.WatchListener(client, listenerResourceName, noopListenerWatcher{})
	mdWant := stats.MetricsData{
		Handle:    xdsClientResourceUpdatesValidMetric.Descriptor(),
		IntIncr:   1,
		LabelKeys: []string{"grpc.target", "grpc.xds.server", "grpc.xds.resource_type"},
		LabelVals: []string{"Test/ResourceUpdateMetrics", mgmtServer.Address, "ListenerResource"},
	}
	if err := tmr.WaitForInt64Count(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
	// Invalid should have no recording point.
	if got, _ := tmr.Metric("grpc.xds_client.resource_updates_invalid"); got != 0 {
		t.Fatalf("Unexpected data for metric \"grpc.xds_client.resource_updates_invalid\", got: %v, want: %v", got, 0)
	}

	// Update management server with a bad update. Eventually, tmr should
	// receive an invalid count received metric. The successful metric should
	// stay the same.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerResourceName, routeConfigurationName)},
		SkipValidation: true,
	}
	resources.Listeners[0].ApiListener = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	mdWant = stats.MetricsData{
		Handle:    xdsClientResourceUpdatesInvalidMetric.Descriptor(),
		IntIncr:   1,
		LabelKeys: []string{"grpc.target", "grpc.xds.server", "grpc.xds.resource_type"},
		LabelVals: []string{"Test/ResourceUpdateMetrics", mgmtServer.Address, "ListenerResource"},
	}
	if err := tmr.WaitForInt64Count(ctx, mdWant); err != nil {
		t.Fatal(err.Error())
	}
	// Valid should stay the same at 1.
	if got, _ := tmr.Metric("grpc.xds_client.resource_updates_valid"); got != 1 {
		t.Fatalf("Unexpected data for metric \"grpc.xds_client.resource_updates_invalid\", got: %v, want: %v", got, 1)
	}
}
