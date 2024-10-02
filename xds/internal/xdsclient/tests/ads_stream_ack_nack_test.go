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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Tests simple ACK and NACK scenarios on the ADS stream:
//  1. When a good response is received, i.e. once that is expected to be ACKed,
//     the test verifies that an ACK is sent matching the version and nonce from
//     the response.
//  2. When a subsequent bad response is received, i.e. once is expected to be
//     NACKed, the test verifies that a NACK is sent matching the previously
//     ACKed version and current nonce from the response.
//  3. When a subsequent goos response is received, the test verifies that an
//     ACK is sent matching the version and nonce from the current response.
func (s) TestADS_ACK_NACK_Simple(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create an xDS management server listening on a local port. Configure the
	// request and response handlers to push on channels that are inspected by
	// the test goroutine to verify ack version and nonce.
	streamRequestCh := testutils.NewChannel()
	streamResponseCh := testutils.NewChannel()
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			streamRequestCh.Send(req)
			return nil
		},
		OnStreamResponse: func(_ context.Context, _ int64, _ *v3discoverypb.DiscoveryRequest, resp *v3discoverypb.DiscoveryResponse) {
			// The go-control-plane management server continuously resends the
			// same resource if NACKed by the client. Hence, we need to use
			// `Replace` here instead of `Send`.
			streamResponseCh.Replace(resp)
		},
	})

	// Create a listener resource on the management server.
	const listenerName = "listener"
	const routeConfigName = "route-config"
	nodeID := uuid.New().String()
	listenerResource := e2e.DefaultClientListener(listenerName, routeConfigName)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listenerResource},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS client with bootstrap pointing to the above server.
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)
	client := createXDSClient(t, bc)

	// Register a watch for a listener resource.
	lw := newListenerWatcher()
	ldsCancel := xdsresource.WatchListener(client, listenerName, lw)
	defer ldsCancel()

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
		t.Fatal("Timeout when waiting for the discovery response from client")
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
	wantUpdate := listenerUpdateErrTuple{
		update: xdsresource.ListenerUpdate{
			RouteConfigName: routeConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, lw.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Update the management server with a listener resource that contains an
	// empty HTTP connection manager within the apiListener, which will cause
	// the resource to be NACKed.
	badListener := proto.Clone(listenerResource).(*v3listenerpb.Listener)
	badListener.ApiListener.ApiListener = nil
	mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{badListener},
		SkipValidation: true,
	})

	r, err = streamResponseCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for the discovery response from client")
	}
	gotResp = r.(*v3discoverypb.DiscoveryResponse)

	var wantNackErr = errors.New("unexpected http connection manager resource type")
	if err := verifyListenerUpdate(ctx, lw.updateCh, listenerUpdateErrTuple{err: wantNackErr}); err != nil {
		t.Fatal(err)
	}

	// Verify that the NACK contains the appropriate version, nonce and error.
	// We expect the version to not change as this is a NACK.
	r, err = streamRequestCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for ACK")
	}
	gotReq = r.(*v3discoverypb.DiscoveryRequest)
	if gotNonce, wantNonce := gotReq.GetResponseNonce(), gotResp.GetNonce(); gotNonce != wantNonce {
		t.Errorf("Unexpected nonce in discovery request, got: %v, want: %v", gotNonce, wantNonce)
	}
	if gotErr := gotReq.GetErrorDetail(); gotErr == nil || !strings.Contains(gotErr.GetMessage(), wantNackErr.Error()) {
		t.Fatalf("Unexpected error in discovery request, got: %v, want: %v", gotErr.GetMessage(), wantNackErr)
	}

	// Update the management server to send a good resource again.
	mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listenerResource},
		SkipValidation: true,
	})

	// The envoy-go-control-plane management server keeps resending the same
	// resource as long as we keep NACK'ing it. So, we will see the bad resource
	// sent to us a few times here, before receiving the good resource.
	for {
		r, err = streamResponseCh.Receive(ctx)
		if err != nil {
			t.Fatal("Timeout when waiting for the discovery response from client")
		}
		gotResp = r.(*v3discoverypb.DiscoveryResponse)

		// Verify that the ACK contains the appropriate version and nonce.
		r, err = streamRequestCh.Receive(ctx)
		if err != nil {
			t.Fatal("Timeout when waiting for ACK")
		}
		gotReq = r.(*v3discoverypb.DiscoveryRequest)
		wantReq.VersionInfo = gotResp.GetVersionInfo()
		wantReq.ResponseNonce = gotResp.GetNonce()
		wantReq.ErrorDetail = nil
		diff := cmp.Diff(gotReq, wantReq, protocmp.Transform())
		if diff == "" {
			break
		}
		t.Logf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}

	// Verify the update received by the watcher.
	for ; ctx.Err() == nil; <-time.After(100 * time.Millisecond) {
		if err := verifyListenerUpdate(ctx, lw.updateCh, wantUpdate); err != nil {
			t.Logf("Failed to verify listener update, err: %v", err)
			continue
		}
		break
	}
	if ctx.Err() != nil {
		t.Fatal("Timeout when waiting for listener update")
	}
}

// Tests the case where the first response is invalid. The test verifies that
// the NACK contains an empty version string.
func (s) TestADS_ACK_NACK_InvalidFirstResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create an xDS management server listening on a local port. Configure the
	// request and response handlers to push on channels that are inspected by
	// the test goroutine to verify ack version and nonce.
	streamRequestCh := testutils.NewChannel()
	streamResponseCh := testutils.NewChannel()
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			streamRequestCh.Send(req)
			return nil
		},
		OnStreamResponse: func(_ context.Context, _ int64, _ *v3discoverypb.DiscoveryRequest, resp *v3discoverypb.DiscoveryResponse) {
			streamResponseCh.Send(resp)
		},
	})

	// Create a listener resource on the management server that is expected to
	// be NACKed by the xDS client.
	const listenerName = "listener"
	const routeConfigName = "route-config"
	nodeID := uuid.New().String()
	listenerResource := e2e.DefaultClientListener(listenerName, routeConfigName)
	listenerResource.ApiListener.ApiListener = nil
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listenerResource},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS client with bootstrap pointing to the above server.
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)
	client := createXDSClient(t, bc)

	// Register a watch for a listener resource.
	lw := newListenerWatcher()
	ldsCancel := xdsresource.WatchListener(client, listenerName, lw)
	defer ldsCancel()

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
		t.Fatal("Timeout when waiting for the discovery response from client")
	}
	gotResp := r.(*v3discoverypb.DiscoveryResponse)

	// Verify that the error is propagated to the watcher.
	var wantNackErr = errors.New("unexpected http connection manager resource type")
	if err := verifyListenerUpdate(ctx, lw.updateCh, listenerUpdateErrTuple{err: wantNackErr}); err != nil {
		t.Fatal(err)
	}

	// NACK should contain the appropriate error, nonce, but empty version.
	r, err = streamRequestCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for ACK")
	}
	gotReq = r.(*v3discoverypb.DiscoveryRequest)
	if gotVersion, wantVersion := gotReq.GetVersionInfo(), ""; gotVersion != wantVersion {
		t.Errorf("Unexpected version in discovery request, got: %v, want: %v", gotVersion, wantVersion)
	}
	if gotNonce, wantNonce := gotReq.GetResponseNonce(), gotResp.GetNonce(); gotNonce != wantNonce {
		t.Errorf("Unexpected nonce in discovery request, got: %v, want: %v", gotNonce, wantNonce)
	}
	if gotErr := gotReq.GetErrorDetail(); gotErr == nil || !strings.Contains(gotErr.GetMessage(), wantNackErr.Error()) {
		t.Fatalf("Unexpected error in discovery request, got: %v, want: %v", gotErr.GetMessage(), wantNackErr)
	}
}

// Tests the scenario where the xDS client is no longer interested in a
// resource. The following sequence of events are tested:
//  1. A resource is requested and a good response is received. The test verifies
//     that an ACK is sent for this resource.
//  2. The previously requested resource is no longer requested. The test
//     verifies that a request with no resource names is sent out.
//  3. The same resource is requested again. The test verifies that the request
//     is sent with the previously ACKed version.
func (s) TestADS_ACK_NACK_ResourceIsNotRequestedAnymore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create an xDS management server listening on a local port. Configure the
	// request and response handlers to push on channels that are inspected by
	// the test goroutine to verify ack version and nonce.
	streamRequestCh := testutils.NewChannel()
	streamResponseCh := testutils.NewChannel()
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			streamRequestCh.Send(req)
			return nil
		},
		OnStreamResponse: func(_ context.Context, _ int64, _ *v3discoverypb.DiscoveryRequest, resp *v3discoverypb.DiscoveryResponse) {
			streamResponseCh.Send(resp)
		},
	})

	// Create a listener resource on the management server.
	const listenerName = "listener"
	const routeConfigName = "route-config"
	nodeID := uuid.New().String()
	listenerResource := e2e.DefaultClientListener(listenerName, routeConfigName)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listenerResource},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS client with bootstrap pointing to the above server.
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)
	client, close, err := xdsclient.NewForTesting(xdsclient.OptionsForTesting{
		Name:     t.Name(),
		Contents: bc,
	})
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Register a watch for a listener resource.
	lw := newListenerWatcher()
	ldsCancel := xdsresource.WatchListener(client, listenerName, lw)
	defer ldsCancel()

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
		t.Fatal("Timeout when waiting for the discovery response from client")
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
	wantUpdate := listenerUpdateErrTuple{
		update: xdsresource.ListenerUpdate{
			RouteConfigName: routeConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
	}
	if err := verifyListenerUpdate(ctx, lw.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Cancel the watch on the listener resource. This should result in a
	// discovery request with no resource names.
	ldsCancel()

	// Verify that the discovery request matches expectation.
	r, err = streamRequestCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for discovery request")
	}
	gotReq = r.(*v3discoverypb.DiscoveryRequest)
	wantReq.ResourceNames = nil
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}

	// Register a watch for the same listener resource.
	lw = newListenerWatcher()
	ldsCancel = xdsresource.WatchListener(client, listenerName, lw)
	defer ldsCancel()

	// Verify that the discovery request contains the version from the
	// previously received response.
	r, err = streamRequestCh.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for discovery request")
	}
	gotReq = r.(*v3discoverypb.DiscoveryRequest)
	wantReq.ResourceNames = []string{listenerName}
	if diff := cmp.Diff(gotReq, wantReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in received discovery request, diff (-got, +want):\n%s", diff)
	}

	// TODO(https://github.com/envoyproxy/go-control-plane/issues/1002): Once
	// this bug is fixed, we need to verify that the update is received by the
	// watcher.
}
