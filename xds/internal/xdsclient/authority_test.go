/*
 *
 * Copyright 2023 gRPC authors.
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
	"fmt"
	"testing"
	"time"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal"
	_ "google.golang.org/grpc/xds/internal/httpfilter/router"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
)

type testResourceWatcher struct {
	updateCh               chan *xdsresource.ResourceData
	errorCh                chan error
	resourceDoesNotExistCh chan struct{}
}

func (w *testResourceWatcher) OnUpdate(data xdsresource.ResourceData) {
	w.updateCh <- &data
}

func (w *testResourceWatcher) OnError(err error) {
	w.errorCh <- err
}

func (w *testResourceWatcher) OnResourceDoesNotExist() {
	w.resourceDoesNotExistCh <- struct{}{}
}

func newTestResourceWatcher() *testResourceWatcher {
	return &testResourceWatcher{
		updateCh:               make(chan *xdsresource.ResourceData),
		errorCh:                make(chan error, 1),
		resourceDoesNotExistCh: make(chan struct{}),
	}
}

var (
	// listenerResourceType gets the resource type from the lookup map in the internal
	// package, which is initialized when the individual resource types are created.
	listenerResourceType = internal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)
	rtRegistry           = newResourceTypeRegistry()
)

func init() {
	// Simulating maybeRegister for listenerResourceType. The getter to this registry
	// is passed to the authority for accessing the resource type.
	rtRegistry.types[listenerResourceType.TypeURL()] = listenerResourceType
}

func mgmtServerOptWithOnRequestDoneCh(done chan struct{}) e2e.ManagementServerOptions {
	return e2e.ManagementServerOptions{
		OnStreamRequest: func(i int64, request *v3discoverypb.DiscoveryRequest) error {
			select {
			case done <- struct{}{}:
			default:
			}
			return nil
		},
	}
}

func setupAuthorityWithMgmtServer(ctx context.Context, t *testing.T, opts e2e.ManagementServerOptions, watchExpiryTimeout time.Duration) (*authority, *e2e.ManagementServer, string) {
	t.Helper()
	nodeID := uuid.New().String()
	mgmtServer, err := e2e.StartManagementServer(opts)
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}

	a, err := newAuthority(authorityArgs{
		serverCfg: &bootstrap.ServerConfig{
			ServerURI: mgmtServer.Address,
			Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
			CredsType: "insecure",
		},
		bootstrapCfg: &bootstrap.Config{
			NodeProto: &v3corepb.Node{Id: nodeID},
		},
		serializer:         newCallbackSerializer(ctx),
		resourceTypeGetter: rtRegistry.get,
		watchExpiryTimeout: watchExpiryTimeout,
		logger:             nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, mgmtServer, nodeID
}

// This tests a resource's watch state when a watch is registered for a resource.
// The test calls the `watchResource` api to register a watch for a resource and
// verifies that the resource's watch state is set to `watchStateStarted`.
func (s) TestResourceStateTransitionWhenWatchResourceIsInvoked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	a, mgmtServer, _ := setupAuthorityWithMgmtServer(ctx, t, e2e.ManagementServerOptions{}, defaultTestTimeout)
	defer a.close()
	defer mgmtServer.Stop()

	resourceName := "xdsclient-test-lds-resource"
	watcher := newTestResourceWatcher()
	cancelResource := a.watchResource(listenerResourceType, resourceName, watcher)
	defer cancelResource()

	if err := compareWStateAndWTimer(a, listenerResourceType, resourceName, watchStateStarted); err != nil {
		t.Fatal(err)
	}
}

// This tests the resource's watch state transition when there is a response for
// the resource from the management server. The test calls the `watchResource`
// api to register a watch for a resource, sends an update from the mgmt server,
// and verifies that the resource's watch state transitions from
// `watchStateRequested` to `watchStateReceived`.
func (s) TestResourceStateTransitionsFromRequestedToReceivedOnUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	onStreamRequestDoneCh := make(chan struct{})
	mgmtServerOpts := mgmtServerOptWithOnRequestDoneCh(onStreamRequestDoneCh)
	a, mgmtServer, nodeID := setupAuthorityWithMgmtServer(ctx, t, mgmtServerOpts, defaultTestTimeout)
	defer a.close()
	defer mgmtServer.Stop()

	resourceName := "xdsclient-test-lds-resource"
	watcher := newTestResourceWatcher()
	cancelResource := a.watchResource(listenerResourceType, resourceName, watcher)
	defer cancelResource()

	// Waiting for mgmt server to recv request before verifying state.
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out before mgmt server got the request.")
	case <-onStreamRequestDoneCh:
	}
	if err := compareWStateAndWTimer(a, listenerResourceType, resourceName, watchStateRequested); err != nil {
		t.Fatal(err)
	}

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(resourceName, "new-rds-resource")},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Waiting for authority to receive the update before verifying state.
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out before watcher received the update.")
	case <-watcher.updateCh:
	}

	if err := compareWStateAndWTimer(a, listenerResourceType, resourceName, watchStateReceived); err != nil {
		t.Fatal(err)
	}
}

// This tests the resource's watch state transition when the ADS stream is closed
// by the management server. The test calls the `watchResource` api to register
// a watch for a resource, stops the management server, and verifies the resource's
// watch state transitions from `watchStateRequested` to `watchStateStarted` so
// that the watch can be restarted later.
func (s) TestResourceStateTransitionsFromRequestedToStartedOnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	onStreamRequestDoneCh := make(chan struct{})
	mgmtServerOpts := mgmtServerOptWithOnRequestDoneCh(onStreamRequestDoneCh)
	a, mgmtServer, _ := setupAuthorityWithMgmtServer(ctx, t, mgmtServerOpts, defaultTestTimeout)
	defer a.close()

	resourceName := "xdsclient-test-lds-resource"
	watcher := newTestResourceWatcher()
	cancelResource := a.watchResource(listenerResourceType, resourceName, watcher)
	defer cancelResource()

	// Waiting for mgmt server to recv request.
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out before mgmt server got the request.")
	case <-onStreamRequestDoneCh:
	}
	if err := compareWStateAndWTimer(a, listenerResourceType, resourceName, watchStateRequested); err != nil {
		t.Fatal(err)
	}

	mgmtServer.Stop()

	// Waiting for watcher to receive the update.
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out before verifying error propagation.")
	case err := <-watcher.errorCh:
		if err == nil {
			t.Fatal("got err == nil. Want: err from stream connection reset by peer")
		}
	}
	if err := compareWStateAndWTimer(a, listenerResourceType, resourceName, watchStateStarted); err != nil {
		t.Fatal(err)
	}
}

// This tests the case where ADS stream breaks after a successfully receiving a
// message on the stream. In this case, we do not want the error to be propagated
// to the watchers. verifies that in the case where the ADS stream breaks after successfully
// receiving a message on the stream. In this case we want to ignore propagating
// connection error to the watcher. But since the mgmt server stops in this test,
// the watcher would be updated with a connection error for the subsequent attempt
// to create an ADS stream.
func (s) TestWatchResourceTimerCanRestartOnIgnoredADSRecvError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	onStreamRequestDoneCh := make(chan struct{})
	mgmtServerOpts := mgmtServerOptWithOnRequestDoneCh(onStreamRequestDoneCh)
	a, mgmtServer, nodeID := setupAuthorityWithMgmtServer(ctx, t, mgmtServerOpts, defaultTestWatchExpiryTimeout)
	defer a.close()

	resourceNameA := "xdsclient-test-lds-resourceA"
	watcherA := newTestResourceWatcher()

	cancelWatchResourceA := a.watchResource(listenerResourceType, resourceNameA, watcherA)
	defer cancelWatchResourceA()

	// Wait for mgmt server to recv request.
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out before mgmt server got the request.")
	case <-onStreamRequestDoneCh:
	}

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(resourceNameA, "new-rds-resource")},
		SkipValidation: true,
	}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verifying update for resource A is received.
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out before watcher received the update.")
	case <-watcherA.updateCh:
	}

	resourceNameB := "xdsclient-test-lds-resourceB"
	watcherB := newTestResourceWatcher()
	cancelWatchResourceB := a.watchResource(listenerResourceType, resourceNameB, watcherB)
	defer cancelWatchResourceB()

	// Stopping the management server after watch was created for resource B.
	mgmtServer.Stop()

	// Waiting for watcherB to get an error on err ch.
	var err error
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out before mgmt server got the request.")
	case err = <-watcherB.errorCh:
	}
	// This error should be due to connectivity issue when reconnecting because
	// the mgmt server was already been stopped. Any other error should fail the
	// test.
	switch xdsresource.ErrType(err) {
	case xdsresource.ErrorTypeConnection:
	default:
		// Verify that no error was not propagated to the watcher.
		t.Fatalf("watch got an unexpected error update; want: error propagation should be ignored.")
	}

	// Since there was already a response on the stream, the timer for resource B
	// should not fire.
	<-time.After(defaultTestWatchExpiryTimeout)
	if err := compareWStateAndWTimer(a, listenerResourceType, resourceNameB, watchStateStarted); err != nil {
		t.Fatal(err)
	}

}

func compareWStateAndWTimer(a *authority, rt xdsresource.Type, rn string, wantState watchState) error {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	wState := a.resources[rt][rn].wState

	if wState != wantState {
		return fmt.Errorf("resource watch state in: %v. Want: %v", wState, wantState)
	}

	wTimer := a.resources[rt][rn].wTimer
	switch wState {
	case watchStateRequested:
		if wTimer == nil {
			return fmt.Errorf("got timer that is nil. want: timer that is active")
		}
	case watchStateStarted:
		if wTimer != nil {
			return fmt.Errorf("got timer that is not nil. want: nil")
		}
	default:
		if wTimer.Stop() {
			// This means that the timer was running but could be successfully stopped.
			return fmt.Errorf("got timer that was actively running. want: timer that has already stopped")
		}
	}

	return nil
}
