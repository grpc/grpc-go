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
 *
 */

package xdsclient_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/pretty"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

	v3adminpb "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
)

func makeGenericXdsConfig(typeURL, name, version string, status v3adminpb.ClientResourceStatus, config *anypb.Any, failure *v3adminpb.UpdateFailureState) *v3statuspb.ClientConfig_GenericXdsConfig {
	return &v3statuspb.ClientConfig_GenericXdsConfig{
		TypeUrl:      typeURL,
		Name:         name,
		VersionInfo:  version,
		ClientStatus: status,
		XdsConfig:    config,
		ErrorState:   failure,
	}
}

func checkResourceDump(ctx context.Context, want *v3statuspb.ClientStatusResponse, client *xdsclient.XDSClient) error {
	var cmpOpts = cmp.Options{
		protocmp.Transform(),
		protocmp.IgnoreFields((*v3statuspb.ClientConfig_GenericXdsConfig)(nil), "last_updated"),
		protocmp.IgnoreFields((*v3statuspb.ClientConfig)(nil), "client_scope"),
		protocmp.IgnoreFields((*v3adminpb.UpdateFailureState)(nil), "last_update_attempt", "details"),
	}

	var lastErr error
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		b, err := client.DumpResources()
		if err != nil {
			lastErr = err
			continue
		}
		got := &v3statuspb.ClientStatusResponse{}
		if err := proto.Unmarshal(b, got); err != nil {
			lastErr = err
			continue
		}
		// Sort the client configs based on the `client_scope` field.
		slices.SortFunc(got.GetConfig(), func(a, b *v3statuspb.ClientConfig) int {
			return strings.Compare(a.ClientScope, b.ClientScope)
		})
		// Sort the resource configs based on the type_url and name fields.
		for _, cc := range got.GetConfig() {
			slices.SortFunc(cc.GetGenericXdsConfigs(), func(a, b *v3statuspb.ClientConfig_GenericXdsConfig) int {
				if strings.Compare(a.TypeUrl, b.TypeUrl) == 0 {
					return strings.Compare(a.Name, b.Name)
				}
				return strings.Compare(a.TypeUrl, b.TypeUrl)
			})
		}
		diff := cmp.Diff(want, got, cmpOpts)
		if diff == "" {
			return nil
		}
		lastErr = fmt.Errorf("received unexpected resource dump, diff (-got, +want):\n%s, got: %s\n want:%s", diff, pretty.ToJSON(got), pretty.ToJSON(want))
	}
	return fmt.Errorf("timeout when waiting for resource dump to reach expected state: %v", lastErr)
}

// Tests the scenario where there are multiple xDS clients talking to the same
// management server, and requesting the same set of resources. Verifies that
// under all circumstances, both xDS clients receive the same configuration from
// the server.
func (s) TestDumpResources_ManyToOne(t *testing.T) {
	// Initialize the xDS resources to be used in this test.
	ldsTargets := []string{"lds.target.good:0000", "lds.target.good:1111"}
	rdsTargets := []string{"route-config-0", "route-config-1"}
	listeners := make([]*v3listenerpb.Listener, len(ldsTargets))
	listenerAnys := make([]*anypb.Any, len(ldsTargets))
	for i := range ldsTargets {
		listeners[i] = e2e.DefaultClientListener(ldsTargets[i], rdsTargets[i])
		listenerAnys[i] = testutils.MarshalAny(t, listeners[i])
	}

	// Spin up an xDS management server on a local port.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp resource names used in this test do not specify an
		// authority. These will end up looking up an entry with the
		// empty key in the authorities map. Having an entry with an
		// empty key and empty configuration, results in these
		// resources also using the top-level configuration.
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create two xDS clients with the above config.
	client1, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client1.Close()
	client2, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client2.Close()

	// Dump resources and expect empty configs.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	wantNode := &v3corepb.Node{
		Id:                   nodeID,
		UserAgentName:        "user-agent",
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "0.0.0.0"},
		ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw"},
	}
	wantResp := &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node: wantNode,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client1); err != nil {
		t.Fatal(err)
	}
	if err := checkResourceDump(ctx, wantResp, client2); err != nil {
		t.Fatal(err)
	}

	// Register watches, dump resources and expect configs in requested state.
	for _, xdsC := range []*xdsclient.XDSClient{client1, client2} {
		for _, target := range ldsTargets {
			xdsC.WatchResource(xdsresource.V3ListenerURL, target, noopListenerWatcher{})
		}
	}
	wantConfigs := []*v3statuspb.ClientConfig_GenericXdsConfig{
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[0], "", v3adminpb.ClientResourceStatus_REQUESTED, nil, nil),
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[1], "", v3adminpb.ClientResourceStatus_REQUESTED, nil, nil),
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client1); err != nil {
		t.Fatal(err)
	}
	if err := checkResourceDump(ctx, wantResp, client2); err != nil {
		t.Fatal(err)
	}

	// Configure the resources on the management server.
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      listeners,
		SkipValidation: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Dump resources and expect ACK configs.
	wantConfigs = []*v3statuspb.ClientConfig_GenericXdsConfig{
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[0], "1", v3adminpb.ClientResourceStatus_ACKED, listenerAnys[0], nil),
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[1], "1", v3adminpb.ClientResourceStatus_ACKED, listenerAnys[1], nil),
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client1); err != nil {
		t.Fatal(err)
	}
	if err := checkResourceDump(ctx, wantResp, client2); err != nil {
		t.Fatal(err)
	}

	// Update the first resource of each type in the management server to a
	// value which is expected to be NACK'ed by the xDS client.
	listeners[0] = func() *v3listenerpb.Listener {
		hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{})
		return &v3listenerpb.Listener{
			Name:        ldsTargets[0],
			ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		}
	}()
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      listeners,
		SkipValidation: true,
	}); err != nil {
		t.Fatal(err)
	}

	wantConfigs = []*v3statuspb.ClientConfig_GenericXdsConfig{
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[0], "1", v3adminpb.ClientResourceStatus_NACKED, listenerAnys[0], &v3adminpb.UpdateFailureState{VersionInfo: "2"}),
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[1], "2", v3adminpb.ClientResourceStatus_ACKED, listenerAnys[1], nil),
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client1); err != nil {
		t.Fatal(err)
	}
	if err := checkResourceDump(ctx, wantResp, client2); err != nil {
		t.Fatal(err)
	}
}

// Tests the scenario where there are multiple xDS client talking to different
// management server, and requesting different set of resources.
func (s) TestDumpResources_ManyToMany(t *testing.T) {
	// Initialize the xDS resources to be used in this test:
	// - The first xDS client watches old style resource names, and thereby
	//   requests these resources from the top-level xDS server.
	// - The second xDS client watches new style resource names with a non-empty
	//   authority, and thereby requests these resources from the server
	//   configuration for that authority.
	authority := strings.Join(strings.Split(t.Name(), "/"), "")
	ldsTargets := []string{
		"lds.target.good:0000",
		fmt.Sprintf("xdstp://%s/envoy.config.listener.v3.Listener/lds.targer.good:1111", authority),
	}
	rdsTargets := []string{
		"route-config-0",
		fmt.Sprintf("xdstp://%s/envoy.config.route.v3.RouteConfiguration/route-config-1", authority),
	}
	listeners := make([]*v3listenerpb.Listener, len(ldsTargets))
	listenerAnys := make([]*anypb.Any, len(ldsTargets))
	for i := range ldsTargets {
		listeners[i] = e2e.DefaultClientListener(ldsTargets[i], rdsTargets[i])
		listenerAnys[i] = testutils.MarshalAny(t, listeners[i])
	}

	// Start two management servers.
	mgmtServer1 := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	mgmtServer2 := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// The first of the above management servers will be the top-level xDS
	// server in the bootstrap configuration, and the second will be the xDS
	// server corresponding to the test authority.
	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{}
	listenerType := listenerType
	resourceTypes[xdsresource.V3ListenerURL] = listenerType
	si1 := clients.ServerIdentifier{
		ServerURI:  mgmtServer1.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}
	si2 := clients.ServerIdentifier{
		ServerURI:  mgmtServer2.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si1}},
		Node:             clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp style resource names used in this test use a slash removed
		// version of t.Name as their authority, and the empty config
		// results in the top-level xds server configuration being used for
		// this authority.
		Authorities: map[string]xdsclient.Authority{
			authority: {XDSServers: []xdsclient.ServerConfig{{ServerIdentifier: si2}}},
		},
	}

	// Create two xDS clients with the above config.
	client1, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client1.Close()
	client2, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client2.Close()

	// Check the resource dump before configuring resources on the management server.
	// Dump resources and expect empty configs.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	wantNode := &v3corepb.Node{
		Id:                   nodeID,
		UserAgentName:        "user-agent",
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "0.0.0.0"},
		ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw"},
	}
	wantResp := &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node: wantNode,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client1); err != nil {
		t.Fatal(err)
	}
	if err := checkResourceDump(ctx, wantResp, client2); err != nil {
		t.Fatal(err)
	}

	// Register watches, the first xDS client watches old style resource names,
	// while the second xDS client watches new style resource names.
	client1.WatchResource(xdsresource.V3ListenerURL, ldsTargets[0], noopListenerWatcher{})
	client2.WatchResource(xdsresource.V3ListenerURL, ldsTargets[1], noopListenerWatcher{})

	// Check the resource dump. Both clients should have all resources in
	// REQUESTED state.
	wantConfigs1 := []*v3statuspb.ClientConfig_GenericXdsConfig{
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[0], "", v3adminpb.ClientResourceStatus_REQUESTED, nil, nil),
	}
	wantConfigs2 := []*v3statuspb.ClientConfig_GenericXdsConfig{
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[1], "", v3adminpb.ClientResourceStatus_REQUESTED, nil, nil),
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs1,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client1); err != nil {
		t.Fatal(err)
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs2,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client2); err != nil {
		t.Fatal(err)
	}

	// Configure resources on the first management server.
	if err := mgmtServer1.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      listeners[:1],
		SkipValidation: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Check the resource dump. One client should have resources in ACKED state,
	// while the other should still have resources in REQUESTED state.
	wantConfigs1 = []*v3statuspb.ClientConfig_GenericXdsConfig{
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[0], "1", v3adminpb.ClientResourceStatus_ACKED, listenerAnys[0], nil),
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs1,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client1); err != nil {
		t.Fatal(err)
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs2,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client2); err != nil {
		t.Fatal(err)
	}

	// Configure resources on the second management server.
	if err := mgmtServer2.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      listeners[1:],
		SkipValidation: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Check the resource dump. Both clients should have appropriate resources
	// in REQUESTED state.
	wantConfigs2 = []*v3statuspb.ClientConfig_GenericXdsConfig{
		makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[1], "1", v3adminpb.ClientResourceStatus_ACKED, listenerAnys[1], nil),
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs1,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client1); err != nil {
		t.Fatal(err)
	}
	wantResp = &v3statuspb.ClientStatusResponse{
		Config: []*v3statuspb.ClientConfig{
			{
				Node:              wantNode,
				GenericXdsConfigs: wantConfigs2,
			},
		},
	}
	if err := checkResourceDump(ctx, wantResp, client2); err != nil {
		t.Fatal(err)
	}
}
