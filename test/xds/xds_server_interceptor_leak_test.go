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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/xds"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Tests that interceptor instances are properly closed when a new RDS
// configuration is received for the same route configuration name.
func (s) TestServerSideXDS_InterceptorLeak_RDSUpdate(t *testing.T) {
	// Register a custom httpFilter builder for the test.
	var filtersCreated, filtersDestroyed, interceptorsCreated, interceptorsDestroyed atomic.Int32
	pathCh := make(chan string, 1)
	testFilterTypeURL := t.Name()
	fb := &trackingHTTPFilterBuilder{
		filtersCreated:        &filtersCreated,
		filtersDestroyed:      &filtersDestroyed,
		interceptorsCreated:   &interceptorsCreated,
		interceptorsDestroyed: &interceptorsDestroyed,
		typeURL:               testFilterTypeURL,
		pathCh:                pathCh,
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

	// Add xDS resources for the client.
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	const serviceName = "my-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})

	// Add xDS resources for the server.
	vhs := []*v3routepb.VirtualHost{{
		Domains: []string{"*"},
		Routes: []*v3routepb.Route{{
			Match: &v3routepb.RouteMatch{
				PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
			},
			Action: &v3routepb.Route_NonForwardingAction{},
		}},
	}}
	const routeConfigName = "routeName"
	rds1 := &v3routepb.RouteConfiguration{
		Name:         routeConfigName,
		VirtualHosts: vhs,
	}
	networkFilters := []*v3listenerpb.Filter{{
		Name: "hcm",
		ConfigType: &v3listenerpb.Filter_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				HttpFilters: []*v3httppb.HttpFilter{
					newHTTPFilter(t, "tracker", testFilterTypeURL, "path1"),
					e2e.HTTPFilter("router", &v3routerpb.Router{}),
				},
				RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
					Rds: &v3httppb.Rds{
						ConfigSource: &v3corepb.ConfigSource{
							ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
						},
						RouteConfigName: routeConfigName,
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
		FilterChains: []*v3listenerpb.FilterChain{
			{
				Name: "v4-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{{
						AddressPrefix: "0.0.0.0",
						PrefixLen:     &wrapperspb.UInt32Value{Value: uint32(0)},
					}},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{{
						AddressPrefix: "0.0.0.0",
						PrefixLen:     &wrapperspb.UInt32Value{Value: uint32(0)},
					}},
				},
				Filters: networkFilters,
			},
			{
				Name: "v6-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{{
						AddressPrefix: "::",
						PrefixLen:     &wrapperspb.UInt32Value{Value: uint32(0)},
					}},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{{
						AddressPrefix: "::",
						PrefixLen:     &wrapperspb.UInt32Value{Value: uint32(0)},
					}},
				},
				Filters: networkFilters,
			},
		},
	}
	resources.Listeners = append(resources.Listeners, inboundLis)
	resources.Routes = append(resources.Routes, rds1)

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

	// Make an RPC and verify that one filter and two interceptors are created
	// (one per filter chain).
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	select {
	case cfg := <-pathCh:
		if got, want := cfg, "path1"; got != want {
			t.Fatalf("Unexpected config sent to filter, got: %q, want: %q", got, want)
		}
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for interceptor to be invoked")
	}
	if got, want := filtersCreated.Load(), int32(1); got != want {
		t.Fatalf("Created %d filter instances, want: %d", got, want)
	}
	if got, want := interceptorsCreated.Load(), int32(2); got != want {
		t.Fatalf("Created %d interceptor instances, want: %d", got, want)
	}

	// Update the RDS configuration with a new route for the same route configuration
	// name. This should result in a new interceptor instance being created for
	// the new route.
	rds2 := &v3routepb.RouteConfiguration{
		Name: routeConfigName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{"*"},
			Routes: []*v3routepb.Route{
				{
					Name: "EmptyCall",
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
				},
				{
					Name: "Wildcard",
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
				},
			},
		}},
	}
	resources.Routes = []*v3routepb.RouteConfiguration{resources.Routes[0], rds2}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the updated config to be applied. Since the new configuration has
	// two routes, we expect two interceptor instances to be created for each
	// filter chain, for a total of 6 interceptor instances. We also wait for
	// the old interceptors (one per filter chain, two total) to be destroyed,
	// since destruction happens after the new interceptors are created within
	// the same RDS update callback. Breaking only on interceptorsCreated==6
	// would race with the subsequent interceptorsDestroyed increment.
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("EmptyCall() failed: %v", err)
		}
		select {
		case <-pathCh:
		default:
		}
		if interceptorsCreated.Load() == 6 && interceptorsDestroyed.Load() == 2 {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("Timeout when waiting for updated config to be applied: %v", ctx.Err())
	}

	// Verify the filter instance is retained, while the interceptor instances
	// are replaced with the updated config.
	if got, want := filtersCreated.Load(), int32(1); got != want {
		t.Fatalf("Created %d filter instances, want: %d", got, want)
	}
	if got, want := interceptorsCreated.Load(), int32(6); got != want {
		t.Fatalf("Created %d interceptor instances, want: %d", got, want)
	}
	if got, want := interceptorsDestroyed.Load(), int32(2); got != want {
		t.Fatalf("Destroyed %d interceptor instances, want: %d", got, want)
	}

	stopServer()
	if got, want := filtersDestroyed.Load(), int32(1); got != want {
		t.Fatalf("Destroyed %d filter instances, want: %d", got, want)
	}
	if got, want := interceptorsDestroyed.Load(), int32(6); got != want {
		t.Fatalf("Destroyed %d interceptor instances, want: %d", got, want)
	}
}
