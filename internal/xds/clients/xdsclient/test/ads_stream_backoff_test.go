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
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	xdsclientinternal "google.golang.org/grpc/internal/xds/clients/xdsclient/internal"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/testing/protocmp"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
)

func overrideStreamBackOff(t *testing.T, streamBackOff func(int) time.Duration) {
	originalStreamBackoff := xdsclientinternal.StreamBackoff
	xdsclientinternal.StreamBackoff = streamBackOff
	t.Cleanup(func() { xdsclientinternal.StreamBackoff = originalStreamBackoff })
}

// Creates an xDS client with the given management server address, nodeID and backoff function.
func createXDSClientWithBackoff(t *testing.T, mgmtServerAddress string, nodeID string, streamBackoff func(int) time.Duration) *xdsclient.XDSClient {
	t.Helper()
	overrideStreamBackOff(t, streamBackoff)
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	return createXDSClient(t, mgmtServerAddress, nodeID, grpctransport.NewBuilder(configs))
}

// Tests the case where the management server returns an error in the ADS
// streaming RPC. Verifies that the ADS stream is restarted after a backoff
// period, and that the previously requested resources are re-requested on the
// new stream.
func (s) TestADS_BackoffAfterStreamFailure(t *testing.T) {
	// Channels used for verifying different events in the test.
	streamCloseCh := make(chan struct{}, 1)  // ADS stream is closed.
	ldsResourcesCh := make(chan []string, 1) // Listener resource names in the discovery request.
	backoffCh := make(chan struct{}, 1)      // Backoff after stream failure.

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create an xDS management server that returns RPC errors.
	streamErr := errors.New("ADS stream error")
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			// Push the requested resource names on to a channel.
			if req.GetTypeUrl() == version.V3ListenerURL {
				t.Logf("Received LDS request for resources: %v", req.GetResourceNames())
				select {
				case ldsResourcesCh <- req.GetResourceNames():
				case <-ctx.Done():
				}
			}
			// Return an error everytime a request is sent on the stream. This
			// should cause the transport to backoff before attempting to
			// recreate the stream.
			return streamErr
		},
		// Push on a channel whenever the stream is closed.
		OnStreamClosed: func(int64, *v3corepb.Node) {
			select {
			case streamCloseCh <- struct{}{}:
			case <-ctx.Done():
			}
		},
	})

	// Override the backoff implementation to push on a channel that is read by
	// the test goroutine.
	backoffCtx, backoffCancel := context.WithCancel(ctx)
	streamBackoff := func(int) time.Duration {
		select {
		case backoffCh <- struct{}{}:
		case <-backoffCtx.Done():
		}
		return 0
	}
	defer backoffCancel()

	// Create an xDS client with bootstrap pointing to the above server.
	nodeID := uuid.New().String()
	client := createXDSClientWithBackoff(t, mgmtServer.Address, nodeID, streamBackoff)

	// Register a watch for a listener resource.
	const listenerName = "listener"
	lw := newListenerWatcher()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, listenerName, lw)
	defer ldsCancel()

	// Verify that an ADS stream is created and an LDS request with the above
	// resource name is sent.
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerName}); err != nil {
		t.Fatal(err)
	}

	// Verify that the received stream error is reported to the watcher.
	if err := verifyListenerResourceError(ctx, lw.resourceErrCh, streamErr.Error(), nodeID); err != nil {
		t.Fatal(err)
	}

	// Verify that the stream is closed.
	select {
	case <-streamCloseCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for stream to be closed after an error")
	}

	// Verify that the ADS stream backs off before recreating the stream.
	select {
	case <-backoffCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for ADS stream to backoff after stream failure")
	}

	// Verify that the same resource name is re-requested on the new stream.
	if err := waitForResourceNames(ctx, t, ldsResourcesCh, []string{listenerName}); err != nil {
		t.Fatal(err)
	}

	// To prevent indefinite blocking during xDS client close, which is caused
	// by a blocking backoff channel write, cancel the backoff context early
	// given that the test is complete.
	backoffCancel()

}

// Tests the case where a stream breaks because the server goes down. Verifies
// that when the server comes back up, the same resources are re-requested, this
// time with the previously acked version and an empty nonce.
func (s) TestADS_RetriesAfterBrokenStream(t *testing.T) {
	// Channels used for verifying different events in the test.
	streamRequestCh := make(chan *v3discoverypb.DiscoveryRequest, 1)   // Discovery request is received.
	streamResponseCh := make(chan *v3discoverypb.DiscoveryResponse, 1) // Discovery response is received.

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create an xDS management server listening on a local port.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,
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

	// Override the backoff implementation to always return 0, to reduce test
	// run time. Instead control when the backoff returns by blocking on a
	// channel, that the test closes.
	backoffCh := make(chan struct{})
	streamBackoff := func(int) time.Duration {
		select {
		case backoffCh <- struct{}{}:
		case <-ctx.Done():
		}
		return 0
	}

	// Create an xDS client pointing to the above server.
	client := createXDSClientWithBackoff(t, mgmtServer.Address, nodeID, streamBackoff)

	// Register a watch for a listener resource.
	lw := newListenerWatcher()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, listenerName, lw)
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
			UserAgentName:        "user-agent",
			UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "0.0.0.0"},
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
		update: listenerUpdate{
			RouteConfigName: routeConfigName},
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
func (s) TestADS_ResourceRequestedBeforeStreamCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Channels used for verifying different events in the test.
	streamRequestCh := make(chan *v3discoverypb.DiscoveryRequest, 1) // Discovery request is received.

	// Create an xDS management server listening on a local port.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	streamErr := errors.New("ADS stream error")

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,

		// Return an error everytime a request is sent on the stream. This
		// should cause the transport to backoff before attempting to recreate
		// the stream.
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			select {
			case streamRequestCh <- req:
			default:
			}
			return streamErr
		},
	})

	// Bring down the management server before creating the transport. This
	// allows us to test the case where SendRequest() is called when there is no
	// stream to the management server.
	lis.Stop()

	// Override the backoff implementation to always return 0, to reduce test
	// run time. Instead control when the backoff returns by blocking on a
	// channel, that the test closes.
	backoffCh := make(chan struct{}, 1)
	unblockBackoffCh := make(chan struct{})
	streamBackoff := func(int) time.Duration {
		select {
		case backoffCh <- struct{}{}:
		default:
		}
		<-unblockBackoffCh
		return 0
	}

	// Create an xDS client with bootstrap pointing to the above server.
	nodeID := uuid.New().String()
	client := createXDSClientWithBackoff(t, mgmtServer.Address, nodeID, streamBackoff)

	// Register a watch for a listener resource.
	const listenerName = "listener"
	lw := newListenerWatcher()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, listenerName, lw)
	defer ldsCancel()

	// The above watch results in an attempt to create a new stream, which will
	// fail, and will result in backoff. Wait for backoff to kick in.
	select {
	case <-backoffCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for stream backoff")
	}

	// Bring up the connection to the management server, and unblock the backoff
	// implementation.
	lis.Restart()
	close(unblockBackoffCh)

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
			UserAgentName:        "user-agent",
			UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "0.0.0.0"},
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
