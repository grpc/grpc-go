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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/protobuf/testing/protocmp"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

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
	// the test about the request and response messages being exchanged.
	streamRequestCh := testutils.NewChannel()
	streamResponseCh := testutils.NewChannel()
	streamOpened := testutils.NewChannel()
	streamClosed := testutils.NewChannel()
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			t.Logf("Received request for resources: %v of type %s", req.GetResourceNames(), req.GetTypeUrl())
			streamRequestCh.SendContext(ctx, req)
			return nil
		},
		OnStreamResponse: func(_ context.Context, _ int64, _ *v3discoverypb.DiscoveryRequest, resp *v3discoverypb.DiscoveryResponse) {
			streamResponseCh.SendContext(ctx, resp)
		},
		OnStreamClosed: func(int64, *v3corepb.Node) {
			streamClosed.SendContext(ctx, nil)
		},
		OnStreamOpen: func(context.Context, int64, string) error {
			streamOpened.SendContext(ctx, nil)
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
	if _, err = streamOpened.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for ADS stream to open")
	}

	// Verify that the initial discovery request matches expectation.
	r, err := streamRequestCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for the initial discovery request")
	}
	gotReq := r.(*v3discoverypb.DiscoveryRequest)
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
	r, err = streamResponseCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for a discovery response from the server")
	}
	gotResp := r.(*v3discoverypb.DiscoveryResponse)

	// Verify that the ACK contains the appropriate version and nonce.
	r, err = streamRequestCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for ACK")
	}
	gotReq = r.(*v3discoverypb.DiscoveryRequest)
	wantReq.VersionInfo = gotResp.GetVersionInfo()
	wantReq.ResponseNonce = gotResp.GetNonce()
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}

	// Verify the update received by the watcher.
	wantListenerUpdate := listenerUpdateErrTuple{
		update: &xdsresource.ListenerUpdate{
			RouteConfigName: routeConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, lw.updateCh, wantListenerUpdate); err != nil {
		t.Fatal(err)
	}

	// Stop the restartable listener and wait for the stream to close.
	lis.Stop()
	if _, err = streamClosed.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for ADS stream to close")
	}

	// Restart the restartable listener and wait for the stream to open.
	lis.Restart()
	if _, err = streamOpened.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for ADS stream to open")
	}

	// Verify that the listener resource is requested again.
	r, err = streamRequestCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for the initial discovery request")
	}
	gotReq = r.(*v3discoverypb.DiscoveryRequest)
	wantReq.ResponseNonce = ""
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}
}
