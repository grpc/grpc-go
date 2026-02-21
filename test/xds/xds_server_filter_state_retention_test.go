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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/httpfilter"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

const filterCfgPathFieldName = "path"

// testFilterCfg is the internal representation of the filter config proto. It
// is returned by filter's config parsing methods.
type testFilterCfg struct {
	httpfilter.FilterConfig
	path string
}

// filterConfigFromProto parses filter config specified as a v3.TypedStruct into
// a testFilterCfg.
func filterConfigFromProto(cfg proto.Message) (httpfilter.FilterConfig, error) {
	ts, ok := cfg.(*v3xdsxdstypepb.TypedStruct)
	if !ok {
		return nil, fmt.Errorf("unsupported filter config type: %T, want %T", cfg, &v3xdsxdstypepb.TypedStruct{})
	}

	if ts.GetValue() == nil {
		return testFilterCfg{}, nil
	}
	ret := testFilterCfg{}
	if v := ts.GetValue().GetFields()[filterCfgPathFieldName]; v != nil {
		ret.path = v.GetStringValue()
	}
	return ret, nil
}

// trackingHTTPFilterBuilder is a test filter that allows counting the number of
// times a filter instance or an interceptor instance is built or closed.
type trackingHTTPFilterBuilder struct {
	httpfilter.Builder
	filtersCreated        *atomic.Int32
	filtersDestroyed      *atomic.Int32
	interceptorsCreated   *atomic.Int32
	interceptorsDestroyed *atomic.Int32
	typeURL               string
	pathCh                chan string
}

func (t *trackingHTTPFilterBuilder) IsTerminal() bool { return false }

func (t *trackingHTTPFilterBuilder) TypeURLs() []string { return []string{t.typeURL} }

func (*trackingHTTPFilterBuilder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfigFromProto(cfg)
}

func (t *trackingHTTPFilterBuilder) BuildServerFilter() (httpfilter.ServerFilter, func()) {
	t.filtersCreated.Add(1)
	return t, func() { t.filtersDestroyed.Add(1) }
}

var _ httpfilter.ServerFilterBuilder = &trackingHTTPFilterBuilder{}

func (t *trackingHTTPFilterBuilder) BuildServerInterceptor(config, _ httpfilter.FilterConfig) (resolver.ServerInterceptor, func(), error) {
	t.interceptorsCreated.Add(1)

	if config == nil {
		return nil, nil, fmt.Errorf("unexpected missing config")
	}
	baseCfg := config.(testFilterCfg)

	interceptor := &trackingInterceptor{
		pathCh:   t.pathCh,
		basePath: baseCfg.path,
	}
	return interceptor, func() { t.interceptorsDestroyed.Add(1) }, nil
}

type trackingInterceptor struct {
	pathCh   chan string
	basePath string
}

func (i *trackingInterceptor) AllowRPC(context.Context) error {
	i.pathCh <- i.basePath
	return nil
}

func newHTTPFilter(t *testing.T, name, typeURL, path string) *v3httppb.HttpFilter {
	return &v3httppb.HttpFilter{
		Name: name,
		ConfigType: &v3httppb.HttpFilter_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
				TypeUrl: typeURL,
				Value: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						filterCfgPathFieldName: {Kind: &structpb.Value_StringValue{StringValue: path}},
					},
				},
			}),
		},
	}
}

// Tests the filter state retention behavior when filter configs in the existing
// filter chain are updated, but there are no changes to the filter names. In
// this case, the existing filter instance should be retained, while new
// interceptor instances should be created with the updated config.
func (s) TestServerSideXDS_FilterStateRetention_AcrossUpdates_FilterConfigChange(t *testing.T) {
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

	lis, stopServer := setupGRPCServer(t, bootstrapContents)
	defer stopServer()

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

	vhs := []*v3routepb.VirtualHost{{
		Domains: []string{"*"},
		Routes: []*v3routepb.Route{{
			Match: &v3routepb.RouteMatch{
				PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
			},
			Action: &v3routepb.Route_NonForwardingAction{},
		}},
	}}
	networkFilters := []*v3listenerpb.Filter{{
		Name: "filter-1",
		ConfigType: &v3listenerpb.Filter_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				HttpFilters: []*v3httppb.HttpFilter{
					newHTTPFilter(t, "tracker", testFilterTypeURL, "initial-path"),
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

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Setup the management server with client and server-side resources.
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Make an RPC and verify that one filter and two interceptors are created
	// (one per filter chain).
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	select {
	case cfg := <-pathCh:
		if got, want := cfg, "initial-path"; got != want {
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

	// Update the filter config in the listener resource.
	networkFilters = []*v3listenerpb.Filter{{
		Name: "filter-1",
		ConfigType: &v3listenerpb.Filter_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				HttpFilters: []*v3httppb.HttpFilter{
					newHTTPFilter(t, "tracker", testFilterTypeURL, "final-path"),
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
	inboundLis.FilterChains[0].Filters = networkFilters
	inboundLis.FilterChains[1].Filters = networkFilters
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the updated config to be applied on the gRPC server.
WaitForUpdatedConfig:
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("EmptyCall() failed: %v", err)
		}
		select {
		case cfg := <-pathCh:
			if got, want := cfg, "final-path"; got == want {
				break WaitForUpdatedConfig
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for interceptor get updated config")
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
	if got, want := interceptorsCreated.Load(), int32(4); got != want {
		t.Fatalf("Created %d interceptor instances, want: %d", got, want)
	}
	if got, want := interceptorsDestroyed.Load(), int32(2); got != want {
		t.Fatalf("Destroyed %d interceptor instances, want: %d", got, want)
	}

	// Stop the server (which causes the listener to be closed) to trigger
	// cleanup of filters and interceptors, and verify that all instances are
	// cleaned up.
	stopServer()
	if got, want := filtersDestroyed.Load(), int32(1); got != want {
		t.Fatalf("Destroyed %d filter instances, want: %d", got, want)
	}
	if got, want := interceptorsDestroyed.Load(), int32(4); got != want {
		t.Fatalf("Destroyed %d interceptor instances, want: %d", got, want)
	}
}

// Tests the filter state retention behavior when a new filter chain with a
// filter of the existing type but different filter name is added to the
// listener resource. In this case, a filter instance should be created because
// the filter name is part of the key used to identify filter instances.
func (s) TestServerSideXDS_FilterStateRetention_AcrossUpdates_FilterChainsChange(t *testing.T) {
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

	lis, stopServer := setupGRPCServer(t, bootstrapContents)
	defer stopServer()

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

	vhs := []*v3routepb.VirtualHost{{
		Domains: []string{"*"},
		Routes: []*v3routepb.Route{{
			Match: &v3routepb.RouteMatch{
				PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
			},
			Action: &v3routepb.Route_NonForwardingAction{},
		}},
	}}
	networkFilters := []*v3listenerpb.Filter{{
		Name: "hcm",
		ConfigType: &v3listenerpb.Filter_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				HttpFilters: []*v3httppb.HttpFilter{
					newHTTPFilter(t, "tracker", testFilterTypeURL, "initial-path"),
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
	// Setup the management server with client and server-side resources.
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Make an RPC and verify that one filter and one interceptor (for the
	// default filter chain) is created.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	select {
	case cfg := <-pathCh:
		if got, want := cfg, "initial-path"; got != want {
			t.Fatalf("Unexpected config sent to filter, got: %q, want: %q", got, want)
		}
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for interceptor to be invoked")
	}
	if got, want := filtersCreated.Load(), int32(1); got != want {
		t.Fatalf("Created %d filter instances, want: %d", got, want)
	}
	if got, want := interceptorsCreated.Load(), int32(1); got != want {
		t.Fatalf("Created %d interceptor instances, want: %d", got, want)
	}

	// Add a new filter chain to the listener resource that contains HTTP
	// filters with a different filter name. This should result in a new filter
	// instance being created because the filter name is part of the key used to
	// identify filter instances.
	networkFilters = []*v3listenerpb.Filter{{
		Name: "hcm",
		ConfigType: &v3listenerpb.Filter_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				HttpFilters: []*v3httppb.HttpFilter{
					newHTTPFilter(t, "tracker-new", testFilterTypeURL, "final-path"),
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
	inboundLis.FilterChains = []*v3listenerpb.FilterChain{
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
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the updated config to be applied on the gRPC server.
WaitForUpdatedConfig:
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("EmptyCall() failed: %v", err)
		}
		select {
		case cfg := <-pathCh:
			if got, want := cfg, "final-path"; got == want {
				break WaitForUpdatedConfig
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for interceptor get updated config")
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("Timeout when waiting for updated config to be applied: %v", ctx.Err())
	}

	// Verify the a new filter instance is created because of the new filter
	// name. Three new interceptor instances should also be created (one for the
	// default filter chain, and two for the newly added filter chains).
	if got, want := filtersCreated.Load(), int32(2); got != want {
		t.Fatalf("Created %d filter instances, want: %d", got, want)
	}
	if got, want := interceptorsCreated.Load(), int32(4); got != want {
		t.Fatalf("Created %d interceptor instances, want: %d", got, want)
	}
	if got, want := interceptorsDestroyed.Load(), int32(1); got != want {
		t.Fatalf("Destroyed %d interceptor instances, want: %d", got, want)
	}

	// Stop the server (which causes the listener to be closed) to trigger
	// cleanup of filters and interceptors, and verify that all instances are
	// cleaned up.
	stopServer()
	if got, want := filtersDestroyed.Load(), int32(2); got != want {
		t.Fatalf("Destroyed %d filter instances, want: %d", got, want)
	}
	if got, want := interceptorsDestroyed.Load(), int32(4); got != want {
		t.Fatalf("Destroyed %d interceptor instances, want: %d", got, want)
	}
}
