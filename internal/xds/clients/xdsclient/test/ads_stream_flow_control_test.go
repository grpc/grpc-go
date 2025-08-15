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
	"slices"
	"sort"
	"testing"
	"time"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
)

// blockingListenerWatcher implements xdsresource.ListenerWatcher. It writes to
// a channel when it receives a callback from the watch. It also makes the
// DoneNotifier passed to the callback available to the test, thereby enabling
// the test to block this watcher for as long as required.
type blockingListenerWatcher struct {
	doneNotifierCh chan func()   // DoneNotifier passed to the callback.
	updateCh       chan struct{} // Written to when an update is received.
	ambientErrCh   chan struct{} // Written to when an ambient error is received.
	resourceErrCh  chan struct{} // Written to when a resource error is received.
}

func newBLockingListenerWatcher() *blockingListenerWatcher {
	return &blockingListenerWatcher{
		doneNotifierCh: make(chan func(), 1),
		updateCh:       make(chan struct{}, 1),
		ambientErrCh:   make(chan struct{}, 1),
		resourceErrCh:  make(chan struct{}, 1),
	}
}

func (lw *blockingListenerWatcher) ResourceChanged(_ xdsclient.ResourceData, done func()) {
	// Notify receipt of the update.
	select {
	case lw.updateCh <- struct{}{}:
	default:
	}

	select {
	case lw.doneNotifierCh <- done:
	default:
	}
}

func (lw *blockingListenerWatcher) ResourceError(_ error, done func()) {
	// Notify receipt of an error.
	select {
	case lw.resourceErrCh <- struct{}{}:
	default:
	}

	select {
	case lw.doneNotifierCh <- done:
	default:
	}
}

func (lw *blockingListenerWatcher) AmbientError(_ error, done func()) {
	// Notify receipt of an error.
	select {
	case lw.ambientErrCh <- struct{}{}:
	default:
	}

	select {
	case lw.doneNotifierCh <- done:
	default:
	}
}

type transportBuilder struct {
	adsStreamCh chan *stream
}

func (b *transportBuilder) Build(si clients.ServerIdentifier) (clients.Transport, error) {
	cc, err := grpc.NewClient(si.ServerURI, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.ForceCodec(&byteCodec{})))
	if err != nil {
		return nil, err
	}

	return &transport{cc: cc, adsStreamCh: b.adsStreamCh}, nil
}

type transport struct {
	cc          *grpc.ClientConn
	adsStreamCh chan *stream
}

func (t *transport) NewStream(ctx context.Context, method string) (clients.Stream, error) {
	s, err := t.cc.NewStream(ctx, &grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, method)
	if err != nil {
		return nil, err
	}

	stream := &stream{
		stream: s,
		recvCh: make(chan struct{}, 1),
		doneCh: make(chan struct{}),
	}
	t.adsStreamCh <- stream

	return stream, nil
}

func (t *transport) Close() {
	t.cc.Close()
}

type stream struct {
	stream grpc.ClientStream

	recvCh chan struct{}
	doneCh <-chan struct{}
}

func (s *stream) Send(msg []byte) error {
	return s.stream.SendMsg(msg)
}

func (s *stream) Recv() ([]byte, error) {
	select {
	case s.recvCh <- struct{}{}:
	case <-s.doneCh:
		return nil, errors.New("Recv() called after the test has finished")
	}

	var typedRes []byte
	if err := s.stream.RecvMsg(&typedRes); err != nil {
		return nil, err
	}
	return typedRes, nil
}

type byteCodec struct{}

func (c *byteCodec) Marshal(v any) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return b, nil
	}
	return nil, fmt.Errorf("transport: message is %T, but must be a []byte", v)
}

func (c *byteCodec) Unmarshal(data []byte, v any) error {
	if b, ok := v.(*[]byte); ok {
		*b = data
		return nil
	}
	return fmt.Errorf("transport: target is %T, but must be *[]byte", v)
}

func (c *byteCodec) Name() string {
	return "transport.byteCodec"
}

// Tests ADS stream level flow control with a single resource. The test does the
// following:
//   - Starts a management server and configures a listener resource on it.
//   - Creates an xDS client to the above management server, starts a couple of
//     listener watchers for the above resource, and verifies that the update
//     reaches these watchers.
//   - These watchers don't invoke the onDone callback until explicitly
//     triggered by the test. This allows the test to verify that the next
//     Recv() call on the ADS stream does not happen until both watchers have
//     completely processed the update, i.e invoke the onDone callback.
//   - Resource is updated on the management server, and the test verifies that
//     the update reaches the watchers.
func (s) TestADSFlowControl_ResourceUpdates_SingleResource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	// Create an xDS client pointing to the above server with a test transport
	// that allow monitoring the underlying stream through adsStreamCh.
	adsStreamCh := make(chan *stream, 1)
	client := createXDSClient(t, mgmtServer.Address, nodeID, &transportBuilder{adsStreamCh: adsStreamCh})

	// Configure two watchers for the same listener resource.
	const listenerResourceName = "test-listener-resource"
	const routeConfigurationName = "test-route-configuration-resource"
	watcher1 := newBLockingListenerWatcher()
	cancel1 := client.WatchResource(xdsresource.V3ListenerURL, listenerResourceName, watcher1)
	defer cancel1()
	watcher2 := newBLockingListenerWatcher()
	cancel2 := client.WatchResource(xdsresource.V3ListenerURL, listenerResourceName, watcher2)
	defer cancel2()

	// Wait for the ADS stream to be created.
	var adsStream *stream
	select {
	case adsStream = <-adsStreamCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for ADS stream to be created")
	}

	// Configure the listener resource on the management server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerResourceName, routeConfigurationName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Ensure that there is a read on the stream.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for ADS stream to be read from")
	}

	// Wait for the update to reach the watchers.
	select {
	case <-watcher1.updateCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for update to reach watcher 1")
	}
	select {
	case <-watcher2.updateCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for update to reach watcher 2")
	}

	// Update the listener resource on the management server to point to a new
	// route configuration resource.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerResourceName, "new-route")},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Unblock one watcher.
	onDone := <-watcher1.doneNotifierCh
	onDone()

	// Wait for a short duration and ensure that there is no read on the stream.
	select {
	case <-adsStream.recvCh:
		t.Fatal("Recv() called on the ADS stream before all watchers have processed the previous update")
	case <-time.After(defaultTestShortTimeout):
	}

	// Unblock the second watcher.
	onDone = <-watcher2.doneNotifierCh
	onDone()

	// Ensure that there is a read on the stream, now that the previous update
	// has been consumed by all watchers.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for Recv() to be called on the ADS stream after all watchers have processed the previous update")
	}

	// Wait for the new update to reach the watchers.
	select {
	case <-watcher1.updateCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for update to reach watcher 1")
	}
	select {
	case <-watcher2.updateCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for update to reach watcher 2")
	}

	// At this point, the xDS client is shut down (and the associated transport
	// is closed) without the watchers invoking their respective onDone
	// callbacks. This verifies that the closing a transport that has pending
	// watchers does not block.
}

// Tests ADS stream level flow control with a multiple resources. The test does
// the following:
//   - Starts a management server and configures two listener resources on it.
//   - Creates an xDS client to the above management server, starts a couple of
//     listener watchers for the two resources, and verifies that the update
//     reaches these watchers.
//   - These watchers don't invoke the onDone callback until explicitly
//     triggered by the test. This allows the test to verify that the next
//     Recv() call on the ADS stream does not happen until both watchers have
//     completely processed the update, i.e invoke the onDone callback.
func (s) TestADSFlowControl_ResourceUpdates_MultipleResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	const listenerResourceName1 = "test-listener-resource-1"
	const listenerResourceName2 = "test-listener-resource-2"
	wantResourceNames := []string{listenerResourceName1, listenerResourceName2}
	requestCh := make(chan struct{}, 1)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() != version.V3ListenerURL {
				return nil
			}
			gotResourceNames := req.GetResourceNames()
			sort.Slice(gotResourceNames, func(i, j int) bool { return req.ResourceNames[i] < req.ResourceNames[j] })
			if slices.Equal(gotResourceNames, wantResourceNames) {
				// The two resource names will be part of the initial request
				// and also the ACK. Hence, we need to make this write
				// non-blocking.
				select {
				case requestCh <- struct{}{}:
				default:
				}
			}
			return nil
		},
	})

	nodeID := uuid.New().String()

	// Create an xDS client pointing to the above server with a test transport
	// that allow monitoring the underlying stream through adsStreamCh.
	adsStreamCh := make(chan *stream, 1)
	client := createXDSClient(t, mgmtServer.Address, nodeID, &transportBuilder{adsStreamCh: adsStreamCh})

	// Configure two watchers for two different listener resources.
	const routeConfigurationName1 = "test-route-configuration-resource-1"
	watcher1 := newBLockingListenerWatcher()
	cancel1 := client.WatchResource(xdsresource.V3ListenerURL, listenerResourceName1, watcher1)
	defer cancel1()
	const routeConfigurationName2 = "test-route-configuration-resource-2"
	watcher2 := newBLockingListenerWatcher()
	cancel2 := client.WatchResource(xdsresource.V3ListenerURL, listenerResourceName2, watcher2)
	defer cancel2()

	// Wait for the wrapped ADS stream to be created.
	var adsStream *stream
	select {
	case adsStream = <-adsStreamCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for ADS stream to be created")
	}

	// Ensure that there is a read on the stream.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for ADS stream to be read from")
	}

	// Wait for both resource names to be requested.
	select {
	case <-requestCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for both resource names to be requested")
	}

	// Configure the listener resources on the management server.
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(listenerResourceName1, routeConfigurationName1),
			e2e.DefaultClientListener(listenerResourceName2, routeConfigurationName2),
		},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// At this point, we expect the management server to send both resources in
	// the same response. So, both watchers would be notified at the same time,
	// and no more Recv() calls should happen until both of them have invoked
	// their respective onDone() callbacks.

	// The order of callback invocations among the two watchers is not
	// guaranteed. So, we select on both of them and unblock the first watcher
	// whose callback is invoked.
	var otherWatcherUpdateCh chan struct{}
	var otherWatcherDoneCh chan func()
	select {
	case <-watcher1.updateCh:
		onDone := <-watcher1.doneNotifierCh
		onDone()
		otherWatcherUpdateCh = watcher2.updateCh
		otherWatcherDoneCh = watcher2.doneNotifierCh
	case <-watcher2.updateCh:
		onDone := <-watcher2.doneNotifierCh
		onDone()
		otherWatcherUpdateCh = watcher1.updateCh
		otherWatcherDoneCh = watcher1.doneNotifierCh
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update to reach first watchers")
	}

	// Wait for a short duration and ensure that there is no read on the stream.
	select {
	case <-adsStream.recvCh:
		t.Fatal("Recv() called on the ADS stream before all watchers have processed the previous update")
	case <-time.After(defaultTestShortTimeout):
	}

	// Wait for the update on the second watcher and unblock it.
	select {
	case <-otherWatcherUpdateCh:
		onDone := <-otherWatcherDoneCh
		onDone()
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update to reach second watcher")
	}

	// Ensure that there is a read on the stream, now that the previous update
	// has been consumed by all watchers.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for Recv() to be called on the ADS stream after all watchers have processed the previous update")
	}
}

// Test ADS stream flow control with a single resource that is expected to be
// NACKed by the xDS client and the watcher's ResourceError() callback is
// expected to be invoked because resource is not cached. Verifies that no
// further reads are attempted until the error is completely processed by the
// watcher.
func (s) TestADSFlowControl_ResourceErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	// Create an xDS client pointing to the above server with a test transport
	// that allow monitoring the underlying stream through adsStreamCh.
	adsStreamCh := make(chan *stream, 1)
	client := createXDSClient(t, mgmtServer.Address, nodeID, &transportBuilder{adsStreamCh: adsStreamCh})

	// Configure a watcher for a listener resource.
	const listenerResourceName = "test-listener-resource"
	watcher := newBLockingListenerWatcher()
	cancel = client.WatchResource(xdsresource.V3ListenerURL, listenerResourceName, watcher)
	defer cancel()

	// Wait for the stream to be created.
	var adsStream *stream
	select {
	case adsStream = <-adsStreamCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for ADS stream to be created")
	}

	// Configure the management server to return a single listener resource
	// which is expected to be NACKed by the client.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{badListenerResource(t, listenerResourceName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Ensure that there is a read on the stream.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for ADS stream to be read from")
	}

	// Wait for the resource error to reach the watcher.
	select {
	case <-watcher.resourceErrCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for error to reach watcher")
	}

	// Wait for a short duration and ensure that there is no read on the stream.
	select {
	case <-adsStream.recvCh:
		t.Fatal("Recv() called on the ADS stream before all watchers have processed the previous update")
	case <-time.After(defaultTestShortTimeout):
	}

	// Unblock one watcher.
	onDone := <-watcher.doneNotifierCh
	onDone()

	// Ensure that there is a read on the stream, now that the previous error
	// has been consumed by the watcher.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for Recv() to be called on the ADS stream after all watchers have processed the previous update")
	}
}

// Test ADS stream flow control with a single resource that is deleted from the
// management server and therefore the watcher's ResourceError()
// callback is expected to be invoked. Verifies that no further reads are
// attempted until the callback is completely handled by the watcher.
func (s) TestADSFlowControl_ResourceDoesNotExist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	// Create an xDS client pointing to the above server with a test transport
	// that allow monitoring the underlying stream through adsStreamCh.
	adsStreamCh := make(chan *stream, 1)
	client := createXDSClient(t, mgmtServer.Address, nodeID, &transportBuilder{adsStreamCh: adsStreamCh})

	// Configure a watcher for a listener resource.
	const listenerResourceName = "test-listener-resource"
	const routeConfigurationName = "test-route-configuration-resource"
	watcher := newBLockingListenerWatcher()
	cancel = client.WatchResource(xdsresource.V3ListenerURL, listenerResourceName, watcher)
	defer cancel()

	// Wait for the ADS stream to be created.
	var adsStream *stream
	select {
	case adsStream = <-adsStreamCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for ADS stream to be created")
	}

	// Configure the listener resource on the management server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerResourceName, routeConfigurationName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Ensure that there is a read on the stream.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for Recv() to be called on the ADS stream")
	}

	// Wait for the update to reach the watcher and unblock it.
	select {
	case <-watcher.updateCh:
		onDone := <-watcher.doneNotifierCh
		onDone()
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for update to reach watcher 1")
	}

	// Ensure that there is a read on the stream.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for Recv() to be called on the ADS stream")
	}

	// Remove the listener resource on the management server.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Wait for the resource not found callback to be invoked.
	select {
	case <-watcher.resourceErrCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for resource not found callback to be invoked on the watcher")
	}

	// Wait for a short duration and ensure that there is no read on the stream.
	select {
	case <-adsStream.recvCh:
		t.Fatal("Recv() called on the ADS stream before all watchers have processed the previous update")
	case <-time.After(defaultTestShortTimeout):
	}

	// Unblock the watcher.
	onDone := <-watcher.doneNotifierCh
	onDone()

	// Ensure that there is a read on the stream.
	select {
	case <-adsStream.recvCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for Recv() to be called on the ADS stream")
	}
}
