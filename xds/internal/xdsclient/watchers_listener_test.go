/*
 *
 * Copyright 2020 gRPC authors.
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

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"
)

// TestLDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name
// - an update is received after cancel()
func (s) TestLDSWatch(t *testing.T) {
	testWatch(t, xdsresource.ListenerResource, xdsresource.ListenerUpdate{RouteConfigName: testRDSName}, testLDSName)
}

// TestLDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestLDSTwoWatchSameResourceName(t *testing.T) {
	testTwoWatchSameResourceName(t, xdsresource.ListenerResource, xdsresource.ListenerUpdate{RouteConfigName: testRDSName}, testLDSName)
}

// TestLDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestLDSThreeWatchDifferentResourceName(t *testing.T) {
	testThreeWatchDifferentResourceName(t, xdsresource.ListenerResource,
		xdsresource.ListenerUpdate{RouteConfigName: testRDSName + "1"}, testLDSName+"1",
		xdsresource.ListenerUpdate{RouteConfigName: testRDSName + "2"}, testLDSName+"2",
	)
}

// TestLDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestLDSWatchAfterCache(t *testing.T) {
	testWatchAfterCache(t, xdsresource.ListenerResource, xdsresource.ListenerUpdate{RouteConfigName: testRDSName}, testLDSName)
}

// TestLDSResourceRemoved covers the cases:
// - an update is received after a watch()
// - another update is received, with one resource removed
//   - this should trigger callback with resource removed error
// - one more update without the removed resource
//   - the callback (above) shouldn't receive any update
func (s) TestLDSResourceRemoved(t *testing.T) {
	testResourceRemoved(t, xdsresource.ListenerResource,
		xdsresource.ListenerUpdate{RouteConfigName: testRDSName + "1"}, testLDSName+"1",
		xdsresource.ListenerUpdate{RouteConfigName: testRDSName + "2"}, testLDSName+"2",
	)
}

// TestListenerWatchNACKError covers the case that an update is NACK'ed, and the
// watcher should also receive the error.
func (s) TestListenerWatchNACKError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, ctrlCh := testClientSetup(t, false)
	ldsUpdateCh, _ := newWatch(t, client, xdsresource.ListenerResource, testLDSName)
	_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, xdsresource.ListenerResource, testLDSName)

	wantError := fmt.Errorf("testing error")
	updateHandler.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Err: wantError}}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh, xdsresource.ListenerUpdate{}, wantError); err != nil {
		t.Fatal(err)
	}
}

// TestListenerWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalida resources should receive an error.
func (s) TestListenerWatchPartialValid(t *testing.T) {
	testWatchPartialValid(t, xdsresource.ListenerResource, xdsresource.ListenerUpdate{RouteConfigName: testRDSName}, testLDSName)
}

// TestListenerWatch_RedundantUpdateSupression tests scenarios where an update
// with an unmodified resource is suppressed, and modified resource is not.
func (s) TestListenerWatch_RedundantUpdateSupression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, ctrlCh := testClientSetup(t, false)
	ldsUpdateCh, _ := newWatch(t, client, xdsresource.ListenerResource, testLDSName)
	_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, xdsresource.ListenerResource, testLDSName)

	basicListener := testutils.MarshalAny(&v3listenerpb.Listener{
		Name: testLDSName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
					Rds: &v3httppb.Rds{RouteConfigName: "route-config-name"},
				},
			}),
		},
	})
	listenerWithFilter1 := testutils.MarshalAny(&v3listenerpb.Listener{
		Name: testLDSName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
					Rds: &v3httppb.Rds{RouteConfigName: "route-config-name"},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					{
						Name: "customFilter1",
						ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: &anypb.Any{
							TypeUrl: "custom.filter",
							Value:   []byte{1, 2, 3},
						}},
					},
				},
			}),
		},
	})
	listenerWithFilter2 := testutils.MarshalAny(&v3listenerpb.Listener{
		Name: testLDSName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
					Rds: &v3httppb.Rds{RouteConfigName: "route-config-name"},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					{
						Name: "customFilter2",
						ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: &anypb.Any{
							TypeUrl: "custom.filter",
							Value:   []byte{1, 2, 3},
						}},
					},
				},
			}),
		},
	})

	tests := []struct {
		update       xdsresource.ListenerUpdate
		wantCallback bool
	}{
		{
			// First update. Callback should be invoked.
			update:       xdsresource.ListenerUpdate{Raw: basicListener},
			wantCallback: true,
		},
		{
			// Same update as previous. Callback should be skipped.
			update:       xdsresource.ListenerUpdate{Raw: basicListener},
			wantCallback: false,
		},
		{
			// New update. Callback should be invoked.
			update:       xdsresource.ListenerUpdate{Raw: listenerWithFilter1},
			wantCallback: true,
		},
		{
			// Same update as previous. Callback should be skipped.
			update:       xdsresource.ListenerUpdate{Raw: listenerWithFilter1},
			wantCallback: false,
		},
		{
			// New update. Callback should be invoked.
			update:       xdsresource.ListenerUpdate{Raw: listenerWithFilter2},
			wantCallback: true,
		},
		{
			// Same update as previous. Callback should be skipped.
			update:       xdsresource.ListenerUpdate{Raw: listenerWithFilter2},
			wantCallback: false,
		},
	}
	for _, test := range tests {
		updateHandler.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Update: test.update}}, xdsresource.UpdateMetadata{})
		if test.wantCallback {
			if err := verifyListenerUpdate(ctx, ldsUpdateCh, test.update, nil); err != nil {
				t.Fatal(err)
			}
		} else {
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := ldsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected ListenerUpdate: %v, want receiving from channel timeout", u)
			}
		}
	}
}
