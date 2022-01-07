/*
 *
 * Copyright 2021 gRPC authors.
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

package xdsclient

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/testing/protocmp"
)

var (
	serverConfigs = []*bootstrap.ServerConfig{
		{
			ServerURI:    testXDSServer + "0",
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			CredsType:    "creds-0",
			TransportAPI: version.TransportV2,
			NodeProto:    xdstestutils.EmptyNodeProtoV2,
		},
		{
			ServerURI:    testXDSServer + "1",
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			CredsType:    "creds-1",
			TransportAPI: version.TransportV3,
			NodeProto:    xdstestutils.EmptyNodeProtoV3,
		},
		{
			ServerURI:    testXDSServer + "2",
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			CredsType:    "creds-2",
			TransportAPI: version.TransportV2,
			NodeProto:    xdstestutils.EmptyNodeProtoV2,
		},
	}

	serverConfigCmpOptions = cmp.Options{
		cmpopts.IgnoreFields(bootstrap.ServerConfig{}, "Creds"),
		protocmp.Transform(),
	}
)

// watchAndFetchNewController starts a CDS watch on the client for the given
// resourceName, and tries to receive a new controller from the ctrlCh.
//
// It returns false if there's no controller in the ctrlCh.
func watchAndFetchNewController(t *testing.T, client *clientImpl, resourceName string, ctrlCh *testutils.Channel) (*testController, bool, func()) {
	updateCh := testutils.NewChannel()
	cancelWatch := client.WatchCluster(resourceName, func(update xdsresource.ClusterUpdate, err error) {
		updateCh.Send(xdsresource.ClusterUpdateErrTuple{Update: update, Err: err})
	})

	// Clear the item in the watch channel, otherwise the next watch will block.
	authority := xdsresource.ParseName(resourceName).Authority
	var config *bootstrap.ServerConfig
	if authority == "" {
		config = client.config.XDSServer
	} else {
		authConfig, ok := client.config.Authorities[authority]
		if !ok {
			t.Fatalf("failed to find authority %q", authority)
		}
		config = authConfig.XDSServer
	}
	a := client.authorities[config.String()]
	if a == nil {
		t.Fatalf("authority for %q is not created", authority)
	}
	ctrlTemp := a.controller.(*testController)
	// Clear the channel so the next watch on this controller can proceed.
	ctrlTemp.addWatches[xdsresource.ClusterResource].ReceiveOrFail()

	cancelWatchRet := func() {
		cancelWatch()
		ctrlTemp.removeWatches[xdsresource.ClusterResource].ReceiveOrFail()
	}

	// Try to receive a new controller.
	c, ok := ctrlCh.ReceiveOrFail()
	if !ok {
		return nil, false, cancelWatchRet
	}
	ctrl := c.(*testController)
	return ctrl, true, cancelWatchRet
}

// TestAuthorityDefaultAuthority covers that a watch for an old style resource
// name (one without authority) builds a controller using the top level server
// config.
func (s) TestAuthorityDefaultAuthority(t *testing.T) {
	overrideFedEnvVar(t)
	ctrlCh := overrideNewController(t)

	client, err := newWithConfig(&bootstrap.Config{
		XDSServer:   serverConfigs[0],
		Authorities: map[string]*bootstrap.Authority{testAuthority: {XDSServer: serverConfigs[1]}},
	}, defaultWatchExpiryTimeout, defaultIdleAuthorityDeleteTimeout)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(client.Close)

	ctrl, ok, _ := watchAndFetchNewController(t, client, testCDSName, ctrlCh)
	if !ok {
		t.Fatalf("want a new controller to be built, got none")
	}
	// Want the default server config.
	wantConfig := serverConfigs[0]
	if diff := cmp.Diff(ctrl.config, wantConfig, serverConfigCmpOptions); diff != "" {
		t.Fatalf("controller is built with unexpected config, diff (-got +want): %v", diff)
	}
}

// TestAuthorityNoneDefaultAuthority covers that a watch with a new style
// resource name creates a controller with the corresponding server config.
func (s) TestAuthorityNoneDefaultAuthority(t *testing.T) {
	overrideFedEnvVar(t)
	ctrlCh := overrideNewController(t)

	client, err := newWithConfig(&bootstrap.Config{
		XDSServer:   serverConfigs[0],
		Authorities: map[string]*bootstrap.Authority{testAuthority: {XDSServer: serverConfigs[1]}},
	}, defaultWatchExpiryTimeout, defaultIdleAuthorityDeleteTimeout)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(client.Close)

	resourceName := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority, testCDSName, nil)
	ctrl, ok, _ := watchAndFetchNewController(t, client, resourceName, ctrlCh)
	if !ok {
		t.Fatalf("want a new controller to be built, got none")
	}
	// Want the server config for this authority.
	wantConfig := serverConfigs[1]
	if diff := cmp.Diff(ctrl.config, wantConfig, serverConfigCmpOptions); diff != "" {
		t.Fatalf("controller is built with unexpected config, diff (-got +want): %v", diff)
	}
}

// TestAuthorityShare covers that
// - watch with the same authority name doesn't create new authority
// - watch with different authority name but same authority config doesn't
//   create new authority
func (s) TestAuthorityShare(t *testing.T) {
	overrideFedEnvVar(t)
	ctrlCh := overrideNewController(t)

	client, err := newWithConfig(&bootstrap.Config{
		XDSServer: serverConfigs[0],
		Authorities: map[string]*bootstrap.Authority{
			testAuthority:  {XDSServer: serverConfigs[1]},
			testAuthority2: {XDSServer: serverConfigs[1]}, // Another authority name, but with the same config.
		},
	}, defaultWatchExpiryTimeout, defaultIdleAuthorityDeleteTimeout)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(client.Close)

	resourceName := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority, testCDSName, nil)
	ctrl1, ok1, _ := watchAndFetchNewController(t, client, resourceName, ctrlCh)
	if !ok1 {
		t.Fatalf("want a new controller to be built, got none")
	}
	// Want the server config for this authority.
	wantConfig := serverConfigs[1]
	if diff := cmp.Diff(ctrl1.config, wantConfig, serverConfigCmpOptions); diff != "" {
		t.Fatalf("controller is built with unexpected config, diff (-got +want): %v", diff)
	}

	// Call the watch with the same authority name. This shouldn't create a new
	// controller.
	resourceNameSameAuthority := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority, testCDSName+"1", nil)
	ctrl2, ok2, _ := watchAndFetchNewController(t, client, resourceNameSameAuthority, ctrlCh)
	if ok2 {
		t.Fatalf("an unexpected controller is built with config: %v", ctrl2.config)
	}

	// Call the watch with a different authority name, but the same server
	// config. This shouldn't create a new controller.
	resourceNameSameConfig := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority2, testCDSName+"1", nil)
	if ctrl, ok, _ := watchAndFetchNewController(t, client, resourceNameSameConfig, ctrlCh); ok {
		t.Fatalf("an unexpected controller is built with config: %v", ctrl.config)
	}
}

// TestAuthorityIdle covers that
// - authorities are put in a timeout cache when the last watch is canceled
// - idle authorities are not immediately closed. They will be closed after a
//   timeout.
func (s) TestAuthorityIdleTimeout(t *testing.T) {
	overrideFedEnvVar(t)
	ctrlCh := overrideNewController(t)

	const idleTimeout = 50 * time.Millisecond

	client, err := newWithConfig(&bootstrap.Config{
		XDSServer: serverConfigs[0],
		Authorities: map[string]*bootstrap.Authority{
			testAuthority: {XDSServer: serverConfigs[1]},
		},
	}, defaultWatchExpiryTimeout, idleTimeout)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(client.Close)

	resourceName := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority, testCDSName, nil)
	ctrl1, ok1, cancelWatch1 := watchAndFetchNewController(t, client, resourceName, ctrlCh)
	if !ok1 {
		t.Fatalf("want a new controller to be built, got none")
	}

	var cancelWatch2 func()
	// Call the watch with the same authority name. This shouldn't create a new
	// controller.
	resourceNameSameAuthority := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority, testCDSName+"1", nil)
	ctrl2, ok2, cancelWatch2 := watchAndFetchNewController(t, client, resourceNameSameAuthority, ctrlCh)
	if ok2 {
		t.Fatalf("an unexpected controller is built with config: %v", ctrl2.config)
	}

	cancelWatch1()
	if ctrl1.done.HasFired() {
		t.Fatalf("controller is closed immediately when the watch is canceled, wanted to be put in the idle cache")
	}

	// Cancel the second watch, should put controller in the idle cache.
	cancelWatch2()
	if ctrl1.done.HasFired() {
		t.Fatalf("controller is closed when the second watch is closed")
	}

	time.Sleep(idleTimeout * 2)
	if !ctrl1.done.HasFired() {
		t.Fatalf("controller is not closed after idle timeout")
	}
}

// TestAuthorityClientClose covers that the authorities in use and in idle cache
// are all closed when the client is closed.
func (s) TestAuthorityClientClose(t *testing.T) {
	overrideFedEnvVar(t)
	ctrlCh := overrideNewController(t)

	client, err := newWithConfig(&bootstrap.Config{
		XDSServer: serverConfigs[0],
		Authorities: map[string]*bootstrap.Authority{
			testAuthority: {XDSServer: serverConfigs[1]},
		},
	}, defaultWatchExpiryTimeout, defaultIdleAuthorityDeleteTimeout)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(client.Close)

	resourceName := testCDSName
	ctrl1, ok1, cancelWatch1 := watchAndFetchNewController(t, client, resourceName, ctrlCh)
	if !ok1 {
		t.Fatalf("want a new controller to be built, got none")
	}

	resourceNameWithAuthority := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority, testCDSName, nil)
	ctrl2, ok2, _ := watchAndFetchNewController(t, client, resourceNameWithAuthority, ctrlCh)
	if !ok2 {
		t.Fatalf("want a new controller to be built, got none")
	}

	cancelWatch1()
	if ctrl1.done.HasFired() {
		t.Fatalf("controller is closed immediately when the watch is canceled, wanted to be put in the idle cache")
	}

	// Close the client while watch2 is not canceled. ctrl1 is in the idle
	// cache, ctrl2 is in use. Both should be closed.
	client.Close()

	if !ctrl1.done.HasFired() {
		t.Fatalf("controller in idle cache is not closed after client is closed")
	}
	if !ctrl2.done.HasFired() {
		t.Fatalf("controller in use is not closed after client is closed")
	}
}

// TestAuthorityRevive covers that the authorities in the idle cache is revived
// when a new watch is started on this authority.
func (s) TestAuthorityRevive(t *testing.T) {
	overrideFedEnvVar(t)
	ctrlCh := overrideNewController(t)

	const idleTimeout = 50 * time.Millisecond

	client, err := newWithConfig(&bootstrap.Config{
		XDSServer: serverConfigs[0],
		Authorities: map[string]*bootstrap.Authority{
			testAuthority: {XDSServer: serverConfigs[1]},
		},
	}, defaultWatchExpiryTimeout, idleTimeout)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(client.Close)

	// Start a watch on the authority, and cancel it. This puts the authority in
	// the idle cache.
	resourceName := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority, testCDSName, nil)
	ctrl1, ok1, cancelWatch1 := watchAndFetchNewController(t, client, resourceName, ctrlCh)
	if !ok1 {
		t.Fatalf("want a new controller to be built, got none")
	}
	cancelWatch1()

	// Start another watch on this authority, it should retrieve the authority
	// from the cache, instead of creating a new one.
	resourceNameWithAuthority := xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority, testCDSName+"1", nil)
	ctrl2, ok2, _ := watchAndFetchNewController(t, client, resourceNameWithAuthority, ctrlCh)
	if ok2 {
		t.Fatalf("an unexpected controller is built with config: %v", ctrl2.config)
	}

	// Wait for double the idle timeout, the controller shouldn't be closed,
	// since it was revived.
	time.Sleep(idleTimeout * 2)
	if ctrl1.done.HasFired() {
		t.Fatalf("controller that was revived is closed after timeout, want not closed")
	}
}
