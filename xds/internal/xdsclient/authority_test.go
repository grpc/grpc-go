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
	"strings"
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
	"google.golang.org/grpc/xds/internal/httpfilter/router"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
)

type delegatingDummyWatcher struct {
	watcher listenerWatcher
}

func (d *delegatingDummyWatcher) OnUpdate(data xdsresource.ResourceData) {
	l := data.(*xdsresource.ListenerResourceData)
	d.watcher.OnUpdate(l)
}

func (d *delegatingDummyWatcher) OnError(err error) {
	d.watcher.OnError(err)
}

func (d *delegatingDummyWatcher) OnResourceDoesNotExist() {
	d.watcher.OnResourceDoesNotExist()
}

var (
	rType      = internal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)
	rtRegistry = newResourceTypeRegistry()
	_          = router.TypeURL
)

func init() {
	// Simulating maybeRegister for rType.
	rtRegistry.types[rType.TypeURL()] = rType
}

func setupAuthorityWithMgmtServer(ctx context.Context, t *testing.T, opts e2e.ManagementServerOptions, nodeID string) (*authority, *e2e.ManagementServer) {
	t.Helper()
	mgmtServer, err := e2e.StartManagementServer(opts)
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}

	a, err := newAuthority(authorityArgs{
		serverCfg: &bootstrap.ServerConfig{
			ServerURI:    mgmtServer.Address,
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			CredsType:    "insecure",
			TransportAPI: version.TransportV3,
			NodeProto:    &v3corepb.Node{Id: nodeID},
		},
		bootstrapCfg:       nil,
		serializer:         newCallbackSerializer(ctx),
		resourceTypeGetter: rtRegistry.get,
		watchExpiryTimeout: defaultTestWatchExpiryTimeout,
		logger:             nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, mgmtServer
}

func (s) TestResourceStateTransitionWhenWatchResourceIsInvoked(t *testing.T) {
	// This tests the scenario when WatchResource is called, the wState transitions to
	// watchStateStarted and also the timer is not initiated.

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	cs := newCallbackSerializer(ctx)
	defer cancel()

	nodeID := uuid.New().String()
	a, err := newAuthority(authorityArgs{
		serverCfg: &bootstrap.ServerConfig{
			ServerURI:    "mgmtServer'sAddress",
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			CredsType:    "insecure",
			TransportAPI: version.TransportV3,
			NodeProto:    &v3corepb.Node{Id: nodeID},
		},
		bootstrapCfg:       nil,
		serializer:         cs,
		resourceTypeGetter: rtRegistry.get,
		watchExpiryTimeout: defaultTestWatchExpiryTimeout,
		logger:             nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()

	resourceName := "xdsclient-test-lds-resource"
	watcher := listenerWatcher{resourceName: resourceName, cb: func(xdsresource.ListenerUpdate, error) {}}
	dWatcher := &delegatingDummyWatcher{watcher: watcher}

	cancelResource := a.watchResource(rType, resourceName, dWatcher)
	defer cancelResource()

	a.resourcesMu.Lock()
	if a.resources[rType][resourceName].wTimer != nil {
		t.Fatal("watch resource timer has started. Want: wTimer==nil.")
	}
	if wState := a.resources[rType][resourceName].wState; wState != watchStateStarted {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateStarted)
	}
	a.resourcesMu.Unlock()

}

func (s) TestResourceStateTransitionsFromRequestedToReceivedOnUpdate(t *testing.T) {
	// This tests the scenario where there is a watch on a resource and the mgmt
	// server responds with an update. In this case, the wState should transition
	// from watchStateRequested to watchStateReceived

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	onStreamRequestDoneCh := make(chan struct{})

	mgmtServerOpts := e2e.ManagementServerOptions{
		OnStreamRequest: func(i int64, request *v3discoverypb.DiscoveryRequest) error {
			// This is to avoid calling close() on a closed ch.
			select {
			case <-onStreamRequestDoneCh:
				return nil
			default:
			}
			close(onStreamRequestDoneCh)
			return nil
		},
	}
	nodeID := uuid.New().String()
	a, mgmtServer := setupAuthorityWithMgmtServer(ctx, t, mgmtServerOpts, nodeID)
	defer a.close()
	defer mgmtServer.Stop()

	resourceName := "xdsclient-test-lds-resource"
	watcherUpdateCh := make(chan struct{})
	watcher := listenerWatcher{resourceName: resourceName, cb: func(update xdsresource.ListenerUpdate, err error) {
		close(watcherUpdateCh)
	}}
	dWatcher := &delegatingDummyWatcher{watcher: watcher}

	cancelResource := a.watchResource(rType, resourceName, dWatcher)
	defer cancelResource()

	// Wait for mgmt server to recv request.
	<-onStreamRequestDoneCh

	a.resourcesMu.Lock()
	if a.resources[rType][resourceName].wTimer == nil {
		t.Fatal("watch resource timer is nil. Want: wTimer to be started.")
	}
	if wState := a.resources[rType][resourceName].wState; wState != watchStateRequested {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateRequested)
	}
	a.resourcesMu.Unlock()

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(resourceName, "new-rds-resource")},
		SkipValidation: true,
	}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Wait for authority to receive the update.
	<-watcherUpdateCh
	a.resourcesMu.Lock()
	if wState := a.resources[rType][resourceName].wState; wState != watchStateReceived {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateReceived)
	}
	a.resourcesMu.Unlock()
}

func (s) TestResourceStateTransitionsFromRequestedToStartedOnError(t *testing.T) {
	// This tests the scenario where the ADS stream breaks after watchResource call.
	// In this case the timer for the resource also stops and the state is ready
	// to be restarted later.

	onStreamRequestDoneCh := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	mgmtServerOpts := e2e.ManagementServerOptions{
		OnStreamRequest: func(i int64, request *v3discoverypb.DiscoveryRequest) error {
			// This is to avoid calling close() on a closed ch.
			select {
			case <-onStreamRequestDoneCh:
				return nil
			default:
			}
			close(onStreamRequestDoneCh)
			return nil
		},
	}
	nodeID := uuid.New().String()
	a, mgmtServer := setupAuthorityWithMgmtServer(ctx, t, mgmtServerOpts, nodeID)
	defer a.close()

	resourceName := "xdsclient-test-lds-resource"
	watcherErrCh := make(chan error)
	watcher := listenerWatcher{resourceName: resourceName, cb: func(update xdsresource.ListenerUpdate, err error) {
		watcherErrCh <- err
	}}
	dWatcher := &delegatingDummyWatcher{watcher: watcher}

	cancelResource := a.watchResource(rType, resourceName, dWatcher)
	defer cancelResource()

	// Wait for mgmt server to recv request.
	<-onStreamRequestDoneCh

	a.resourcesMu.Lock()
	if a.resources[rType][resourceName].wTimer == nil {
		t.Fatal("watch resource timer is nil. Want: wTimer to be started.")
	}
	if wState := a.resources[rType][resourceName].wState; wState != watchStateRequested {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateRequested)
	}
	a.resourcesMu.Unlock()

	mgmtServer.Stop()

	// Wait for watcher to receive the update.
	if err := <-watcherErrCh; err == nil {
		t.Fatalf("got err == nil. Want: %v", watchStateStarted)
	}
	a.resourcesMu.Lock()
	if wState := a.resources[rType][resourceName].wState; wState != watchStateStarted {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateStarted)
	}
	a.resourcesMu.Unlock()
}

func (s) TestWatchResourceTimerCanRestartOnIgnoredADSRecvError(t *testing.T) {
	// This test verifies that in the case where the ADS stream breaks after successfully
	// receiving a message on the stream. In this case we want to ignore propagating
	// connection error to the watcher. But since the mgmt server stops in this test,
	// the watcher would be updated with a connection error for the subsequent attempt
	// to create an ADS stream.

	onStreamRequestDoneCh := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	mgmtServerOpts := e2e.ManagementServerOptions{
		OnStreamRequest: func(i int64, request *v3discoverypb.DiscoveryRequest) error {
			// This is to avoid calling close() on a closed ch.
			select {
			case <-onStreamRequestDoneCh:
				return nil
			default:
			}
			close(onStreamRequestDoneCh)
			return nil
		},
	}
	nodeID := uuid.New().String()
	a, mgmtServer := setupAuthorityWithMgmtServer(ctx, t, mgmtServerOpts, nodeID)
	defer a.close()

	resourceNameA := "xdsclient-test-lds-resourceA"
	watcherAUpdateCh := make(chan struct{})
	watcherA := listenerWatcher{resourceName: resourceNameA, cb: func(update xdsresource.ListenerUpdate, err error) {
		select {
		case <-watcherAUpdateCh:
			return
		default:
		}
		close(watcherAUpdateCh)
	}}
	dWatcherA := &delegatingDummyWatcher{watcher: watcherA}

	cancelWatchResourceA := a.watchResource(rType, resourceNameA, dWatcherA)
	defer cancelWatchResourceA()

	// Wait for mgmt server to recv request.
	<-onStreamRequestDoneCh

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(resourceNameA, "new-rds-resource")},
		SkipValidation: true,
	}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Verify update for resource A is received and stop the mgmt server.
	<-watcherAUpdateCh

	resourceNameB := "xdsclient-test-lds-resourceB"
	watcherB := listenerWatcher{resourceName: resourceNameB, cb: func(update xdsresource.ListenerUpdate, err error) {
		switch xdsresource.ErrType(err) {
		case xdsresource.ErrTypeStreamFailedAfterRecv:
			// Verify that an Ignored error was not propagated to the watcher.
			t.Fatalf("watch got an unexpected error update; want: ErrTypeStreamFailedAfterRecv error updates should be ignored.")
		case xdsresource.ErrorTypeConnection:
			// Verify that the error was during ADS stream creation.
			if !strings.Contains(err.Error(), "Error while dialing:") {
				t.Fatalf("watch got an unexpected error update; want: error updates only for failed stream creation.")
			}
		}
	}}
	dWatcherB := &delegatingDummyWatcher{watcher: watcherB}

	cancelWatchResourceB := a.watchResource(rType, resourceNameB, dWatcherB)
	defer cancelWatchResourceB()

	mgmtServer.Stop()

	// Since there was already a response on the stream, the timer for resource B should not fire.
	<-time.After(defaultTestWatchExpiryTimeout)

	a.resourcesMu.Lock()
	if wState := a.resources[rType][resourceNameB].wState; wState != watchStateStarted {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateStarted)
	}
	a.resourcesMu.Unlock()
}
