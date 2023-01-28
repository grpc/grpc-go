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
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal"
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

func (s) TestWatchResourceBaseCase(t *testing.T) {
	// This test simulates a simple scenario where authority is called
	// to watch a single listener resource.
	// This test verifies the resource watch timer behavior and watch states.

	// Start a management server.
	onStreamRequestDoneCh := make(chan struct{})
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{
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
	})
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	defer mgmtServer.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	cs := newCallbackSerializer(ctx)
	defer cancel()

	rtRegistry := newResourceTypeRegistry()

	nodeID := uuid.New().String()
	a, err := newAuthority(authorityArgs{
		serverCfg: &bootstrap.ServerConfig{
			ServerURI:    mgmtServer.Address,
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
	watcherUpdateCh := make(chan struct{})
	watcher := listenerWatcher{resourceName: resourceName, cb: func(update xdsresource.ListenerUpdate, err error) {
		close(watcherUpdateCh)
	}}
	dWatcher := &delegatingDummyWatcher{watcher: watcher}

	rType := internal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)

	// Simulating maybeRegister for rType.
	rtRegistry.types[rType.TypeURL()] = rType

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

	// Wait for mgmt server to recv request.
	<-onStreamRequestDoneCh

	a.resourcesMu.Lock()
	if a.resources[rType][resourceName].wTimer == nil {
		t.Fatal("watch resource timer is nil. Want: wTimer to be started.")
	}
	if wState := a.resources[rType][resourceName].wState; wState != watchStateRespRequested {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateRespRequested)
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
	if wState := a.resources[rType][resourceName].wState; wState != watchStateRespReceived {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateRespReceived)
	}
	a.resourcesMu.Unlock()
}

func (s) TestWatchResourceTimerStopsOnError(t *testing.T) {
	// This test verifies that in the case where the ads stream stops the timer for
	// the resource also stops and the state is ready to be restarted later.

	// Start a management server.
	onStreamRequestDoneCh := make(chan struct{})
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{
		OnStreamRequest: func(i int64, request *v3discoverypb.DiscoveryRequest) error {
			t.Log("Stream Request")
			select {
			case <-onStreamRequestDoneCh:
				return nil
			default:
			}
			close(onStreamRequestDoneCh)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	cs := newCallbackSerializer(ctx)
	defer cancel()

	rtRegistry := newResourceTypeRegistry()

	nodeID := uuid.New().String()
	a, err := newAuthority(authorityArgs{
		serverCfg: &bootstrap.ServerConfig{
			ServerURI:    mgmtServer.Address,
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
	watcherErrCh := make(chan error)
	watcher := listenerWatcher{resourceName: resourceName, cb: func(update xdsresource.ListenerUpdate, err error) {
		watcherErrCh <- err
	}}
	dWatcher := &delegatingDummyWatcher{watcher: watcher}

	rType := internal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)

	// Simulating maybeRegister for rType.
	rtRegistry.types[rType.TypeURL()] = rType

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

	// Wait for mgmt server to recv request.
	<-onStreamRequestDoneCh

	a.resourcesMu.Lock()
	if a.resources[rType][resourceName].wTimer == nil {
		t.Fatal("watch resource timer is nil. Want: wTimer to be started.")
	}
	if wState := a.resources[rType][resourceName].wState; wState != watchStateRespRequested {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateRespRequested)
	}
	a.resourcesMu.Unlock()

	mgmtServer.Stop()

	// Wait for authority to receive the update.
	if err := <-watcherErrCh; err == nil {
		t.Fatalf("got err == nil. Want: %v", watchStateStarted)
	}
	a.resourcesMu.Lock()
	if wState := a.resources[rType][resourceName].wState; wState != watchStateStarted {
		t.Fatalf("watch resource state in: %v. Want: %v", wState, watchStateStarted)
	}
	a.resourcesMu.Unlock()
}
