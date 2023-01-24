/*
 *
 * Copyright 2022 gRPC authors.
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
 */

package transport_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

var strSort = func(s1, s2 string) bool { return s1 < s2 }

// TestTransport_BackoffAfterStreamFailure tests the case where the management
// server returns an error in the ADS streaming RPC. The test verifies the
// following:
// 1. Initial discovery request matches expectation.
// 2. RPC error is propagated via the stream error handler.
// 3. When the stream is closed, the transport backs off.
// 4. The same discovery request is sent on the newly created stream.
func (s) TestTransport_BackoffAfterStreamFailure(t *testing.T) {
	// Channels used for verifying different events in the test.
	streamCloseCh := make(chan struct{}, 1)                          // ADS stream is closed.
	streamRequestCh := make(chan *v3discoverypb.DiscoveryRequest, 1) // Discovery request is received.
	backoffCh := make(chan struct{}, 1)                              // Transport backoff after stream failure.
	streamErrCh := make(chan error, 1)                               // Stream error seen by the transport.

	// Create an xDS management server listening on a local port.
	streamErr := errors.New("ADS stream error")
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{
		// Push on a channel whenever the stream is closed.
		OnStreamClosed: func(int64) {
			select {
			case streamCloseCh <- struct{}{}:
			default:
			}
		},

		// Return an error everytime a request is sent on the stream. This
		// should cause the transport to backoff before attempting to recreate
		// the stream.
		OnStreamRequest: func(id int64, req *v3discoverypb.DiscoveryRequest) error {
			select {
			case streamRequestCh <- req:
			default:
			}
			return streamErr
		},
	})
	if err != nil {
		t.Fatalf("Failed to start xDS management server: %v", err)
	}
	defer mgmtServer.Stop()
	t.Logf("Started xDS management server on %s", mgmtServer.Address)

	// Override the backoff implementation to push on a channel that is read by
	// the test goroutine.
	transportBackoff := func(v int) time.Duration {
		select {
		case backoffCh <- struct{}{}:
		default:
		}
		return 0
	}

	// Construct the server config to represent the management server.
	nodeID := uuid.New().String()
	serverCfg := bootstrap.ServerConfig{
		ServerURI:    mgmtServer.Address,
		Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
		CredsType:    "insecure",
		TransportAPI: version.TransportV3,
		NodeProto:    &v3corepb.Node{Id: nodeID},
	}

	// Create a new transport. Since we are only testing backoff behavior here,
	// we can pass a no-op data model layer implementation.
	tr, err := transport.New(transport.Options{
		ServerCfg:     serverCfg,
		UpdateHandler: func(transport.ResourceUpdate) error { return nil }, // No data model layer validation.
		StreamErrorHandler: func(err error) {
			select {
			case streamErrCh <- err:
			default:
			}
		},
		Backoff: transportBackoff,
	})
	if err != nil {
		t.Fatalf("Failed to create xDS transport: %v", err)
	}
	defer tr.Close()

	// Send a discovery request through the transport.
	const resourceName = "resource name"
	tr.SendRequest(version.V3ListenerURL, []string{resourceName})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Verify that the initial discovery request matches expectation.
	var gotReq *v3discoverypb.DiscoveryRequest
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for discovery request on the stream")
	}
	wantReq := &v3discoverypb.DiscoveryRequest{
		VersionInfo:   "",
		Node:          &v3corepb.Node{Id: nodeID},
		ResourceNames: []string{resourceName},
		TypeUrl:       "type.googleapis.com/envoy.config.listener.v3.Listener",
		ResponseNonce: "",
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}

	// Verify that the received stream error is reported to the user.
	var gotErr error
	select {
	case gotErr = <-streamErrCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for stream error to be reported to the user")
	}
	if !strings.Contains(gotErr.Error(), streamErr.Error()) {
		t.Fatalf("Received stream error: %v, wantErr: %v", gotErr, streamErr)
	}

	// Verify that the stream is closed.
	select {
	case <-streamCloseCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for stream to be closed after an error")
	}

	// Verify that the transport backs off before recreating the stream.
	select {
	case <-backoffCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for transport to backoff after stream failure")
	}

	// Verify that the same discovery request is resent on the new stream.
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for discovery request on the stream")
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}
}

// TestTransport_RetriesAfterBrokenStream tests the case where a stream breaks
// because the server goes down. The test verifies the following:
//  1. Initial discovery request matches expectation.
//  2. Good response from the server leads to an ACK with appropriate version.
//  3. Management server going down, leads to stream failure.
//  4. Once the management server comes back up, the same resources are
//     re-requested, this time with an empty nonce.
func (s) TestTransport_RetriesAfterBrokenStream(t *testing.T) {
	// Channels used for verifying different events in the test.
	streamRequestCh := make(chan *v3discoverypb.DiscoveryRequest, 1)   // Discovery request is received.
	streamResponseCh := make(chan *v3discoverypb.DiscoveryResponse, 1) // Discovery response is received.
	streamErrCh := make(chan error, 1)                                 // Stream error seen by the transport.

	// Create an xDS management server listening on a local port.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local listener for the xDS management server: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{
		Listener: lis,
		// Push the received request on to a channel for the test goroutine to
		// verify that it matches expectations.
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			select {
			case streamRequestCh <- req:
			default:
			}
			return nil
		},
		// Push the response that the management server is about to send on to a
		// channel. The test goroutine to uses this to extract the version and
		// nonce, expected on subsequent requests.
		OnStreamResponse: func(_ context.Context, _ int64, _ *v3discoverypb.DiscoveryRequest, resp *v3discoverypb.DiscoveryResponse) {
			select {
			case streamResponseCh <- resp:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("Failed to start xDS management server: %v", err)
	}
	defer mgmtServer.Stop()
	t.Logf("Started xDS management server on %s", lis.Addr().String())

	// Configure the management server with appropriate resources.
	apiListener := &v3listenerpb.ApiListener{
		ApiListener: func() *anypb.Any {
			return testutils.MarshalAny(&v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
					Rds: &v3httppb.Rds{
						ConfigSource: &v3corepb.ConfigSource{
							ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
						},
						RouteConfigName: "route-configuration-name",
					},
				},
			})
		}(),
	}
	const resourceName1 = "resource name 1"
	const resourceName2 = "resource name 2"
	listenerResource1 := &v3listenerpb.Listener{
		Name:        resourceName1,
		ApiListener: apiListener,
	}
	listenerResource2 := &v3listenerpb.Listener{
		Name:        resourceName2,
		ApiListener: apiListener,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listenerResource1, listenerResource2},
		SkipValidation: true,
	})

	// Construct the server config to represent the management server.
	serverCfg := bootstrap.ServerConfig{
		ServerURI:    lis.Addr().String(),
		Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
		CredsType:    "insecure",
		TransportAPI: version.TransportV3,
		NodeProto:    &v3corepb.Node{Id: nodeID},
	}

	// Create a new transport. Since we are only testing backoff behavior here,
	// we can pass a no-op data model layer implementation.
	tr, err := transport.New(transport.Options{
		ServerCfg:     serverCfg,
		UpdateHandler: func(transport.ResourceUpdate) error { return nil }, // No data model layer validation.
		StreamErrorHandler: func(err error) {
			select {
			case streamErrCh <- err:
			default:
			}
		},
		Backoff: func(int) time.Duration { return time.Duration(0) }, // No backoff.
	})
	if err != nil {
		t.Fatalf("Failed to create xDS transport: %v", err)
	}
	defer tr.Close()

	// Send a discovery request through the transport.
	tr.SendRequest(version.V3ListenerURL, []string{resourceName1, resourceName2})

	// Verify that the initial discovery request matches expectation.
	var gotReq *v3discoverypb.DiscoveryRequest
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for discovery request on the stream")
	}
	wantReq := &v3discoverypb.DiscoveryRequest{
		VersionInfo:   "",
		Node:          &v3corepb.Node{Id: nodeID},
		ResourceNames: []string{resourceName1, resourceName2},
		TypeUrl:       "type.googleapis.com/envoy.config.listener.v3.Listener",
		ResponseNonce: "",
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform(), cmpopts.SortSlices(strSort)); diff != "" {
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
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform(), cmpopts.SortSlices(strSort)); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}

	// Bring down the management server to simulate a broken stream.
	lis.Stop()

	// We don't care about the exact error here and it can vary based on which
	// error gets reported first, the Recv() failure or the new stream creation
	// failure. So, all we check here is whether we get an error or not.
	select {
	case <-streamErrCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for stream error to be reported to the user")
	}

	// Bring up the connection to the management server.
	lis.Restart()

	// Verify that the transport creates a new stream and sends out a new
	// request which contains the previously acked version, but an empty nonce.
	wantReq.ResponseNonce = ""
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for the discovery request ACK on the stream")
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform(), cmpopts.SortSlices(strSort)); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}
}

// TestTransport_ResourceRequestedBeforeStreamCreation tests the case where a
// resource is requested before the transport has a valid stream. Verifies that
// the transport sends out the request once it has a valid stream.
func (s) TestTransport_ResourceRequestedBeforeStreamCreation(t *testing.T) {
	// Channels used for verifying different events in the test.
	streamRequestCh := make(chan *v3discoverypb.DiscoveryRequest, 1) // Discovery request is received.

	// Create an xDS management server listening on a local port.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local listener for the xDS management server: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	streamErr := errors.New("ADS stream error")

	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{
		Listener: lis,

		// Return an error everytime a request is sent on the stream. This
		// should cause the transport to backoff before attempting to recreate
		// the stream.
		OnStreamRequest: func(id int64, req *v3discoverypb.DiscoveryRequest) error {
			select {
			case streamRequestCh <- req:
			default:
			}
			return streamErr
		},
	})
	if err != nil {
		t.Fatalf("Failed to start xDS management server: %v", err)
	}
	defer mgmtServer.Stop()
	t.Logf("Started xDS management server on %s", lis.Addr().String())

	// Bring down the management server before creating the transport. This
	// allows us to test the case where SendRequest() is called when there is no
	// stream to the management server.
	lis.Stop()

	// Construct the server config to represent the management server.
	nodeID := uuid.New().String()
	serverCfg := bootstrap.ServerConfig{
		ServerURI:    lis.Addr().String(),
		Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
		CredsType:    "insecure",
		TransportAPI: version.TransportV3,
		NodeProto:    &v3corepb.Node{Id: nodeID},
	}

	// Create a new transport. Since we are only testing backoff behavior here,
	// we can pass a no-op data model layer implementation.
	tr, err := transport.New(transport.Options{
		ServerCfg:          serverCfg,
		UpdateHandler:      func(transport.ResourceUpdate) error { return nil }, // No data model layer validation.
		StreamErrorHandler: func(error) {},                                      // No stream error handling.
		Backoff:            func(int) time.Duration { return time.Duration(0) }, // No backoff.
	})
	if err != nil {
		t.Fatalf("Failed to create xDS transport: %v", err)
	}
	defer tr.Close()

	// Send a discovery request through the transport.
	const resourceName = "resource name"
	tr.SendRequest(version.V3ListenerURL, []string{resourceName})

	// Wait until the transport has attempted to connect to the management
	// server and has seen the connection fail. In this case, since the
	// connection is down, and the transport creates streams with WaitForReady()
	// set to true, stream creation will never fail (unless the context
	// expires), and therefore we cannot rely on the stream error handler.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if tr.ChannelConnectivityStateForTesting() == connectivity.TransientFailure {
			break
		}
	}

	lis.Restart()

	// Verify that the initial discovery request matches expectation.
	var gotReq *v3discoverypb.DiscoveryRequest
	select {
	case gotReq = <-streamRequestCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for discovery request on the stream")
	}
	wantReq := &v3discoverypb.DiscoveryRequest{
		VersionInfo:   "",
		Node:          &v3corepb.Node{Id: nodeID},
		ResourceNames: []string{resourceName},
		TypeUrl:       "type.googleapis.com/envoy.config.listener.v3.Listener",
		ResponseNonce: "",
	}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}
}
