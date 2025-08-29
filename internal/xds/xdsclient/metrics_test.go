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
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/stats"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	_ "google.golang.org/grpc/internal/xds/httpfilter/router" // Register the router filter.
)

type noopListenerWatcher struct{}

func (noopListenerWatcher) ResourceChanged(_ *xdsresource.ListenerResourceData, onDone func()) {
	onDone()
}

func (noopListenerWatcher) ResourceError(_ error, onDone func()) {
	onDone()
}

func (noopListenerWatcher) AmbientError(_ error, onDone func()) {
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

// TestServerFailureMetrics_BeforeResponseRecv configures an xDS client, and a
// management server. It then register a watcher and stops the management
// server before sending a resource update, and verifies that the expected
// metrics for server failure are emitted.
func (s) TestServerFailureMetrics_BeforeResponseRecv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tmr := stats.NewTestMetricsRecorder()
	l, err := testutils.LocalTCPListener()
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

	const listenerResourceName = "test-listener-resource"

	// Watch for the listener on the above management server.
	xdsresource.WatchListener(client, listenerResourceName, noopListenerWatcher{})
	// Verify that an ADS stream is opened and an LDS request with the above
	// resource name is sent.
	select {
	case <-streamOpened:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for ADS stream to open")
	}

	// Close the listener and ensure that the ADS stream breaks. This should
	// cause a server failure count to emit eventually.
	lis.Stop()

	// Restart to prevent the attempt to create a new ADS stream after back off.
	lis.Restart()

	mdWant := stats.MetricsData{
		Handle:    xdsClientServerFailureMetric.Descriptor(),
		IntIncr:   1,
		LabelKeys: []string{"grpc.target", "grpc.xds.server"},
		LabelVals: []string{"Test/ServerFailureMetrics_BeforeResponseRecv", mgmtServer.Address},
	}
	if err := tmr.WaitForInt64Count(ctx, mdWant); err != nil {
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

	tmr := stats.NewTestMetricsRecorder()
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
	client, closePool, err := pool.NewClientForTesting(OptionsForTesting{
		Name:            t.Name(),
		MetricsRecorder: tmr,
	})
	if err != nil {
		t.Fatalf("Failed to create an xDS client: %v", err)
	}
	defer closePool()

	// Watch the valid listener configured on the management server. This should
	// cause a resource updates valid count to emit eventually.
	xdsresource.WatchListener(client, listenerResourceName, noopListenerWatcher{})
	mdSuccess := stats.MetricsData{
		Handle:    xdsClientResourceUpdatesValidMetric.Descriptor(),
		IntIncr:   1,
		LabelKeys: []string{"grpc.target", "grpc.xds.server", "grpc.xds.resource_type"},
		LabelVals: []string{"Test/ServerFailureMetrics_AfterResponseRecv", mgmtServer.Address, "ListenerResource"},
	}
	if err := tmr.WaitForInt64Count(ctx, mdSuccess); err != nil {
		t.Fatal(err.Error())
	}

	// When the client sends an ACK, the management server would reply with an
	// error, breaking the stream.
	mdFailure := stats.MetricsData{
		Handle:    xdsClientServerFailureMetric.Descriptor(),
		IntIncr:   1,
		LabelKeys: []string{"grpc.target", "grpc.xds.server"},
		LabelVals: []string{"Test/ServerFailureMetrics_AfterResponseRecv", mgmtServer.Address},
	}

	// Server failure should still have no recording point.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if err := tmr.WaitForInt64Count(sCtx, mdFailure); err == nil {
		t.Fatalf("tmr.WaitForInt64Count(%v) succeeded when expected to timeout.", mdFailure)
	} else if sCtx.Err() == nil {
		t.Fatalf("tmr.WaitForInt64Count(%v) = %v, want context deadline exceeded", mdFailure, err)
	}

	// Unblock stream creation and verify that an update is received
	// successfully.
	close(streamCreationQuota)
	if err := tmr.WaitForInt64Count(ctx, mdSuccess); err != nil {
		t.Fatal(err.Error())
	}
}
