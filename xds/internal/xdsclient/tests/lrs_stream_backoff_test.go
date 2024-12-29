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
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/testing/protocmp"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Tests the case where the management server returns an error in the ADS
// streaming RPC. Verifies that the LRS stream is restarted after a backoff
// period, and that the previously requested resources are re-requested on the
// new stream.
func (s) TestLRS_BackoffAfterStreamFailure(t *testing.T) {
	// Setup channels for state tracking.
	streamCloseCh := make(chan struct{}, 1)
	resourceRequestCh := make(chan []string, 1)
	backoffCh := make(chan struct{}, 1)

	// Context with timeout for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Setup management server with simulated stream error.
	mgmtServer := setupManagementServer(t, ctx, streamCloseCh, resourceRequestCh, errors.New("LRS stream error"))

	// Configure backoff.
	streamBackoff := func(v int) time.Duration {
		select {
		case backoffCh <- struct{}{}:
		case <-ctx.Done():
		}
		return 500 * time.Millisecond
	}

	// Create xDS client with backoff behavior.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)
	client := createXDSClientWithBackoff(t, bc, streamBackoff)

	// Start watching a resource.
	const listenerName = "listener"
	lw := newListenerWatcher()
	ldsCancel := xdsresource.WatchListener(client, listenerName, lw)
	defer ldsCancel()

	// Verify the resource request.
	if err := waitForResourceNames(ctx, t, resourceRequestCh, []string{listenerName}); err != nil {
		t.Fatal(err)
	}

	// Verify the error callback with the watcher.
	verifyStreamError(ctx, t, lw, "LRS stream error")

	// Verify the stream is closed.
	if err := waitForChannelSignal(ctx, streamCloseCh); err != nil {
		t.Fatal("Stream closure not observed:", err)
	}

	// Verify backoff signal before restart.
	if err := waitForChannelSignal(ctx, backoffCh); err != nil {
		t.Fatal("Backoff signal not observed:", err)
	}

	// Verify resource re-request after stream restart.
	if err := waitForResourceNames(ctx, t, resourceRequestCh, []string{listenerName}); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where a stream breaks because the server goes down. Verifies
// that when the server comes back up, the same resources are re-requested, this
// time with the previously acked version and an empty nonce.
func (s) TestLRS_RetriesAfterBrokenStream(t *testing.T) {
	// Channels used for verifying different events in the test.
	streamRequestCh := make(chan *v3discoverypb.DiscoveryRequest, 1)   // Discovery request is received.
	streamResponseCh := make(chan *v3discoverypb.DiscoveryResponse, 1) // Discovery response is received.

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create an xDS management server listening on a local port.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local listener for the xDS management server: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener:                    lis,
		SupportLoadReportingService: true,
		// Push the received request on to a channel for the test goroutine to
		// verify that it matches expectations.
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			select {
			case streamRequestCh <- req:
			case <-ctx.Done():
			}
			return nil
		},
		// Push the response that the management server is about to send on to a
		// channel. The test goroutine to uses this to extract the version and
		// nonce, expected on subsequent requests.
		OnStreamResponse: func(_ context.Context, _ int64, _ *v3discoverypb.DiscoveryRequest, resp *v3discoverypb.DiscoveryResponse) {
			select {
			case streamResponseCh <- resp:
			case <-ctx.Done():
			}
		},
	})

	// Create a listener resource on the management server.
	const listenerName = "load-report"
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

	// Override the backoff implementation to always return 0, to reduce test
	// run time. Instead control when the backoff returns by blocking on a
	// channel, that the test closes.
	backoffCh := make(chan struct{})
	streamBackoff := func(v int) time.Duration {
		select {
		case backoffCh <- struct{}{}:
		case <-ctx.Done():
		}
		return 0
	}

	// Create an xDS client with bootstrap pointing to the above server.
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)
	client := createXDSClientWithBackoff(t, bc, streamBackoff)

	// Register a watch for a listener resource.
	lw := newListenerWatcher()
	ldsCancel := xdsresource.WatchListener(client, listenerName, lw)
	defer ldsCancel()

	// Verify that the initial discovery request matches expectation.
	var gotReq *v3discoverypb.DiscoveryRequest
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for discovery request on the stream")
	}
	wantReq := &v3discoverypb.DiscoveryRequest{
		VersionInfo: "",
		Node: &v3corepb.Node{
			Id:                   nodeID,
			UserAgentName:        "gRPC Go",
			UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
			ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw"},
		},
		ResourceNames: []string{listenerName},
		TypeUrl:       "type.googleapis.com/envoy.config.listener.v3.Listener",
		ResponseNonce: "",
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}

	// Capture the version and nonce from the response.
	var gotResp *v3discoverypb.DiscoveryResponse
	select {
	case gotResp = <-streamResponseCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for discovery response on the stream")
	}
	version := gotResp.GetVersionInfo()
	nonce := gotResp.GetNonce()

	// Verify that the ACK contains the appropriate version and nonce.
	wantReq.VersionInfo = version
	wantReq.ResponseNonce = nonce
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for the discovery request ACK on the stream")
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}

	// Verify the update received by the watcher.
	wantUpdate := listenerUpdateErrTuple{
		update: xdsresource.ListenerUpdate{
			RouteConfigName: routeConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, lw.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Bring down the management server to simulate a broken stream.
	lis.Stop()

	// Verify that the error callback on the watcher is not invoked.
	verifyNoListenerUpdate(ctx, lw.updateCh)

	// Wait for backoff to kick in, and unblock the first backoff attempt.
	select {
	case <-backoffCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for stream backoff")
	}

	// Bring up the management server. The test does not have prcecise control
	// over when new streams to the management server will start succeeding. The
	// ADS stream implementation will backoff as many times as required before
	// it can successfully create a new stream. Therefore, we need to receive on
	// the backoffCh as many times as required, and unblock the backoff
	// implementation.
	lis.Restart()
	go func() {
		for {
			select {
			case <-backoffCh:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Verify that the transport creates a new stream and sends out a new
	// request which contains the previously acked version, but an empty nonce.
	wantReq.ResponseNonce = ""
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for the discovery request ACK on the stream")
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}
}

// Tests the case where a resource is requested before the a valid ADS stream
// exists. Verifies that the a discovery request is sent out for the previously
// requested resource once a valid stream is created.
func (s) TestLRS_ResourceRequestedBeforeStreamCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Channels for verifying different events in the test.
	streamRequestCh := make(chan *v3discoverypb.DiscoveryRequest, 1)

	// Create an xDS management server listening on a local port.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local listener: %v", err)
	}
	defer l.Close()

	lis := testutils.NewRestartableListener(l)
	defer lis.Stop()

	streamErr := errors.New("LRS stream error")

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener:                    lis,
		SupportLoadReportingService: true,
		OnStreamRequest: func(id int64, req *v3discoverypb.DiscoveryRequest) error {
			// Capture only LoadStats requests.
			if req.GetTypeUrl() == version.V3ListenerURL {
				select {
				case streamRequestCh <- req:
				default:
				}
			}
			return streamErr
		},
	})
	// defer mgmtServer.Stop()

	// Bring down the management server before creating the transport.
	lis.Stop()

	// Override backoff to minimize test time.
	backoffCh := make(chan struct{}, 1)
	unblockBackoffCh := make(chan struct{})
	streamBackoff := func(v int) time.Duration {
		select {
		case backoffCh <- struct{}{}:
		default:
		}
		<-unblockBackoffCh
		return 0
	}

	// Create an xDS client with bootstrap pointing to the above server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)
	client := createXDSClientWithBackoff(t, bc, streamBackoff)
	if client == nil {
		t.Fatalf("Failed to create xDS client")
	}

	// Register a listener watch for the "load-report" resource.
	const listenerName = "load-report"
	lw := newListenerWatcher()
	ldsCancel := xdsresource.WatchListener(client, listenerName, lw)
	defer ldsCancel()

	// Wait for backoff to kick in.
	select {
	case <-backoffCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for stream backoff")
	}

	// Bring up the connection to the management server and unblock the backoff.
	lis.Restart()
	close(unblockBackoffCh)

	// Verify that the initial discovery request matches expectations.
	var gotReq *v3discoverypb.DiscoveryRequest
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for discovery request on the stream")
	}
	wantReq := &v3discoverypb.DiscoveryRequest{
		VersionInfo: "",
		Node: &v3corepb.Node{
			Id:                   nodeID,
			UserAgentName:        "gRPC Go",
			UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
			ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw"},
		},
		ResourceNames: []string{listenerName},
		TypeUrl:       "type.googleapis.com/envoy.config.listener.v3.Listener",
		ResponseNonce: "",
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}
}

// Helper to setup management server with stream behavior.
func setupManagementServer(t *testing.T, ctx context.Context, streamCloseCh chan struct{}, resourceRequestCh chan []string, streamErr error) *e2e.ManagementServer {
	t.Helper()

	return e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		SupportLoadReportingService: true,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3ListenerURL {
				select {
				case resourceRequestCh <- req.GetResourceNames():
				case <-ctx.Done():
				}
			}
			return streamErr
		},
		OnStreamClosed: func(int64, *v3corepb.Node) {
			select {
			case streamCloseCh <- struct{}{}:
			case <-ctx.Done():
			}
		},
	})
}

// Helper to wait for a channel signal with context timeout.
func waitForChannelSignal(ctx context.Context, ch chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return errors.New("context deadline exceeded while waiting for channel signal")
	}
}

// Helper to verify stream errors in watcher.
func verifyStreamError(ctx context.Context, t *testing.T, lw *listenerWatcher, wantErr string) {
	t.Helper()

	u, err := lw.updateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for error callback: %v", err)
	}
	gotErr := u.(listenerUpdateErrTuple).err

	if !cmp.Equal(gotErr.Error(), wantErr) {
		t.Fatalf("Received error: %v, want: %v, diff (-got, +want):\n%s",
			gotErr.Error(), wantErr, cmp.Diff(gotErr.Error(), wantErr))
	}
}
