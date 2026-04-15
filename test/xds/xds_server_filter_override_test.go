/*
 *
 * Copyright 2026 gRPC authors.
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

package xds_test

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/httpfilter"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/xds"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

type overrideInfo struct {
	disabled bool
	path     string
}

func makeOverrideConfig(t *testing.T, typeURL string, info *overrideInfo) *anypb.Any {
	if info == nil {
		return nil
	}
	cfg := &v3routepb.FilterConfig{Disabled: info.disabled}
	if info.path != "" {
		cfg.Config = testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
			TypeUrl: typeURL,
			Value: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"path": {Kind: &structpb.Value_StringValue{StringValue: info.path}},
				},
			},
		})
	}
	return testutils.MarshalAny(t, cfg)
}

// TestServerSideXDS_FilterOverride_Disabled verifies that filters are NOT
// created when disabled by either base config or overrides at either
// VirtualHost or Route level.
func (s) TestServerSideXDS_FilterOverride_Disabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	tests := []struct {
		name         string
		baseDisabled bool
		vhost        *overrideInfo
		route        *overrideInfo
	}{
		{
			name:         "BaseDisabled_NoOverrides",
			baseDisabled: true,
		},
		{
			name:         "BaseEnabled_VHostDisables",
			baseDisabled: false,
			vhost:        &overrideInfo{disabled: true},
		},
		{
			name:         "BaseEnabled_VHostEnabled_RouteDisables",
			baseDisabled: false,
			vhost:        &overrideInfo{disabled: false, path: "vhost-value"},
			route:        &overrideInfo{disabled: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testFilterTypeURL := t.Name()
			fb := &trackingHTTPFilterBuilder{
				typeURL:             testFilterTypeURL,
				filtersCreated:      &atomic.Int32{},
				interceptorsCreated: &atomic.Int32{},
			}
			httpfilter.Register(fb)
			defer httpfilter.UnregisterForTesting(fb.typeURL)

			managementServer, nodeID, bootstrapContents, xdsResolver := setup.ManagementServerAndResolver(t)

			// Wait for the server to enter SERVING mode before making RPCs to avoid
			// flakes due to the server closing connections.
			servingCh := make(chan struct{})
			opt := xds.ServingModeCallback(func(_ net.Addr, args xds.ServingModeChangeArgs) {
				if args.Mode == connectivity.ServingModeServing {
					close(servingCh)
				}
			})
			lis, stopServer := setupGRPCServer(t, bootstrapContents, opt)
			defer stopServer()

			host, port, err := hostPortFromListener(lis)
			if err != nil {
				t.Fatalf("Failed to retrieve host and port of server: %v", err)
			}
			const serviceName = "my-service"
			resources := e2e.DefaultClientResources(e2e.ResourceParams{
				DialTarget: serviceName,
				NodeID:     nodeID,
				Host:       host,
				Port:       port,
				SecLevel:   e2e.SecurityLevelNone,
			})

			var vhOverrideConfig map[string]*anypb.Any
			if tc.vhost != nil {
				vhOverrideConfig = map[string]*anypb.Any{"test-filter": makeOverrideConfig(t, testFilterTypeURL, tc.vhost)}
			}
			var routeOverrideConfig map[string]*anypb.Any
			if tc.route != nil {
				routeOverrideConfig = map[string]*anypb.Any{"test-filter": makeOverrideConfig(t, testFilterTypeURL, tc.route)}
			}

			vhs := []*v3routepb.VirtualHost{{
				Domains: []string{"*"},
				Routes: []*v3routepb.Route{{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
					},
					Action:               &v3routepb.Route_NonForwardingAction{},
					TypedPerFilterConfig: routeOverrideConfig,
				}},
				TypedPerFilterConfig: vhOverrideConfig,
			}}

			networkFilters := []*v3listenerpb.Filter{{
				Name: "hcm",
				ConfigType: &v3listenerpb.Filter_TypedConfig{
					TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
						HttpFilters: []*v3httppb.HttpFilter{
							newHTTPFilter(t, "test-filter", testFilterTypeURL, "base-path", tc.baseDisabled),
							e2e.HTTPFilter("router", &v3routerpb.Router{}),
						},
						RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
							RouteConfig: &v3routepb.RouteConfiguration{
								Name:         "routeName",
								VirtualHosts: vhs,
							},
						},
					}),
				},
			}}

			inboundLis := &v3listenerpb.Listener{
				Name: fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port)))),
				Address: &v3corepb.Address{
					Address: &v3corepb.Address_SocketAddress{
						SocketAddress: &v3corepb.SocketAddress{
							Address: host,
							PortSpecifier: &v3corepb.SocketAddress_PortValue{
								PortValue: port,
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: networkFilters},
			}
			resources.Listeners = append(resources.Listeners, inboundLis)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := managementServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}

			cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
			if err != nil {
				t.Fatalf("grpc.NewClient() failed: %v", err)
			}
			defer cc.Close()

			select {
			case <-servingCh:
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for server to enter SERVING mode")
			}

			client := testgrpc.NewTestServiceClient(cc)
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Fatalf("EmptyCall() failed: %v", err)
			}

			// Verify no filters or interceptors were created.
			if got := fb.filtersCreated.Load(); got != 0 {
				t.Fatalf("filtersCreated = %d; want 0", got)
			}
			if got := fb.interceptorsCreated.Load(); got != 0 {
				t.Fatalf("interceptorsCreated = %d; want 0", got)
			}
		})
	}
}

// TestServerSideXDS_FilterOverride_Enabled verifies that filters ARE created
// and receive the correct configuration when enabled by either base config or
// overrides at either VirtualHost or Route level.
func (s) TestServerSideXDS_FilterOverride_Enabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	const basePath = "base-path"

	tests := []struct {
		name         string
		baseDisabled bool
		vhost        *overrideInfo
		route        *overrideInfo
		wantPath     string
	}{
		{
			name:         "BaseEnabled_NoOverrides",
			baseDisabled: false,
			wantPath:     basePath,
		},
		{
			name:         "VHostEnables",
			baseDisabled: true,
			vhost:        &overrideInfo{disabled: false, path: "vhost-value"},
			wantPath:     "vhost-value",
		},
		{
			name:         "RouteEnables",
			baseDisabled: true,
			vhost:        &overrideInfo{disabled: true},
			route:        &overrideInfo{disabled: false, path: "route-value"},
			wantPath:     "route-value",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pathCh := make(chan string, 1)
			testFilterTypeURL := t.Name()
			fb := &trackingHTTPFilterBuilder{
				typeURL:               testFilterTypeURL,
				pathCh:                pathCh,
				filtersCreated:        &atomic.Int32{},
				interceptorsCreated:   &atomic.Int32{},
				filtersDestroyed:      &atomic.Int32{},
				interceptorsDestroyed: &atomic.Int32{},
			}
			httpfilter.Register(fb)
			defer httpfilter.UnregisterForTesting(fb.typeURL)

			managementServer, nodeID, bootstrapContents, xdsResolver := setup.ManagementServerAndResolver(t)

			servingCh := make(chan struct{})
			opt := xds.ServingModeCallback(func(_ net.Addr, args xds.ServingModeChangeArgs) {
				if args.Mode == connectivity.ServingModeServing {
					close(servingCh)
				}
			})
			lis, stopServer := setupGRPCServer(t, bootstrapContents, opt)
			defer stopServer()

			host, port, err := hostPortFromListener(lis)
			if err != nil {
				t.Fatalf("Failed to retrieve host and port of server: %v", err)
			}
			const serviceName = "my-service"
			resources := e2e.DefaultClientResources(e2e.ResourceParams{
				DialTarget: serviceName,
				NodeID:     nodeID,
				Host:       host,
				Port:       port,
				SecLevel:   e2e.SecurityLevelNone,
			})

			var vhOverrideConfig map[string]*anypb.Any
			if tc.vhost != nil {
				vhOverrideConfig = map[string]*anypb.Any{"test-filter": makeOverrideConfig(t, testFilterTypeURL, tc.vhost)}
			}
			var routeOverrideConfig map[string]*anypb.Any
			if tc.route != nil {
				routeOverrideConfig = map[string]*anypb.Any{"test-filter": makeOverrideConfig(t, testFilterTypeURL, tc.route)}
			}

			vhs := []*v3routepb.VirtualHost{{
				Domains: []string{"*"},
				Routes: []*v3routepb.Route{{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
					},
					Action:               &v3routepb.Route_NonForwardingAction{},
					TypedPerFilterConfig: routeOverrideConfig,
				}},
				TypedPerFilterConfig: vhOverrideConfig,
			}}

			networkFilters := []*v3listenerpb.Filter{{
				Name: "hcm",
				ConfigType: &v3listenerpb.Filter_TypedConfig{
					TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
						HttpFilters: []*v3httppb.HttpFilter{
							newHTTPFilter(t, "test-filter", testFilterTypeURL, basePath, tc.baseDisabled),
							e2e.HTTPFilter("router", &v3routerpb.Router{}),
						},
						RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
							RouteConfig: &v3routepb.RouteConfiguration{
								Name:         "routeName",
								VirtualHosts: vhs,
							},
						},
					}),
				},
			}}

			inboundLis := &v3listenerpb.Listener{
				Name: fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port)))),
				Address: &v3corepb.Address{
					Address: &v3corepb.Address_SocketAddress{
						SocketAddress: &v3corepb.SocketAddress{
							Address: host,
							PortSpecifier: &v3corepb.SocketAddress_PortValue{
								PortValue: port,
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: networkFilters},
			}
			resources.Listeners = append(resources.Listeners, inboundLis)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := managementServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}

			cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
			if err != nil {
				t.Fatalf("grpc.NewClient() failed: %v", err)
			}
			defer cc.Close()

			select {
			case <-servingCh:
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for server to enter SERVING mode")
			}

			client := testgrpc.NewTestServiceClient(cc)
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Fatalf("EmptyCall() failed: %v", err)
			}

			// Verify invoked with expected path.
			select {
			case p := <-pathCh:
				if p != tc.wantPath {
					t.Fatalf("Unexpected path received by filter: got %q, want %q", p, tc.wantPath)
				}
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for filter to be invoked")
			}
			if got, want := fb.filtersCreated.Load(), int32(1); got != want {
				t.Fatalf("Created %d filter instances, want: %d", got, want)
			}
			if got, want := fb.interceptorsCreated.Load(), int32(1); got != want {
				t.Fatalf("Created %d interceptor instances, want: %d", got, want)
			}
		})
	}
}
