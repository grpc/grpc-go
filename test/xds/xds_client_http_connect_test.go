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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/proxyserver"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3http11proxypb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/http_11_proxy/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
)

func enableXDSHTTPConnect(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSHTTPConnectEnabled, true)
	cleanup, err := xdsresource.RegisterMetadataConverterForTesting(version.V3AddressURL)
	if err != nil {
		t.Fatalf("RegisterMetadataConverterForTesting(%q) failed: %v", version.V3AddressURL, err)
	}
	t.Cleanup(cleanup)
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("Failed to split host and port for address %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Failed to parse port %q: %v", portStr, err)
	}
	return host, port
}

// Test veifies client-side HTTP CONNECT proxying support where the proxy
// address is configured via endpoint metadata. It verifies that gRPC requests
// are successfully routed through the HTTP CONNECT proxy to the correct
// backend server.
func (s) TestXDSHTTPConnect(t *testing.T) {
	enableXDSHTTPConnect(t)

	// Spin up a mock HTTP CONNECT proxy server to capture client connections.
	connectCh := make(chan string, 1)
	pServer := proxyserver.New(t, func(req *http.Request) {
		connectCh <- req.URL.Host
	}, false)

	proxyHost, proxyPort := splitHostPort(t, pServer.Addr)

	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	backendHost, backendPort := splitHostPort(t, server.Address)

	const (
		serviceName     = "my-service-xds-http-connect"
		routeConfigName = "route-my-service-xds-http-connect"
		clusterName     = "cluster-my-service-xds-http-connect"
		endpointsName   = "endpoints-my-service-xds-http-connect"
	)

	cluster := &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig:   &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}}},
			ServiceName: endpointsName,
		},
		LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
		TransportSocket: &v3corepb.TransportSocket{
			Name:       "envoy.transport_sockets.http_11_proxy",
			ConfigType: &v3corepb.TransportSocket_TypedConfig{TypedConfig: testutils.MarshalAny(t, &v3http11proxypb.Http11ProxyUpstreamTransport{})},
		},
	}

	loadAssignment := &v3endpointpb.ClusterLoadAssignment{
		ClusterName: endpointsName,
		Endpoints: []*v3endpointpb.LocalityLbEndpoints{
			{
				Locality:            &v3corepb.Locality{SubZone: "locality-1"},
				LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
				LbEndpoints: []*v3endpointpb.LbEndpoint{
					{
						HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
							Endpoint: &v3endpointpb.Endpoint{
								Address: &v3corepb.Address{
									Address: &v3corepb.Address_SocketAddress{
										SocketAddress: &v3corepb.SocketAddress{
											Address:       backendHost,
											PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(backendPort)},
										},
									},
								},
							},
						},
						Metadata: &v3corepb.Metadata{
							TypedFilterMetadata: map[string]*anypb.Any{
								"envoy.http11_proxy_transport_socket.proxy_address": testutils.MarshalAny(t, &v3corepb.Address{
									Address: &v3corepb.Address_SocketAddress{
										SocketAddress: &v3corepb.SocketAddress{
											Address:       proxyHost,
											PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(proxyPort)},
										},
									},
								}),
							},
						},
						LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
					},
				},
			},
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{loadAssignment},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the request was routed through the proxy to the expected
	// backend.
	select {
	case gotTarget := <-connectCh:
		if gotTarget != server.Address {
			t.Errorf("Unexpected server address from proxy server, got %s want %s", gotTarget, server.Address)
		}
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout while waiting for server address from proxy server")
	}
}

// Test verifies client-side HTTP CONNECT proxying support where the proxy
// address is configured via metadata at the locality level. It verifies that
// the client successfully fallback to using the locality-level proxy address
// if no endpoint-level metadata is present.
func (s) TestXDSHTTPConnect_LocalityFallback(t *testing.T) {
	enableXDSHTTPConnect(t)

	// Spin up a mock HTTP CONNECT proxy server to capture client connections.
	connectCh := make(chan string, 1)
	pServer := proxyserver.New(t, func(req *http.Request) {
		connectCh <- req.URL.Host
	}, false)

	proxyHost, proxyPort := splitHostPort(t, pServer.Addr)

	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	backendHost, backendPort := splitHostPort(t, server.Address)

	const (
		serviceName     = "my-service-xds-http-connect"
		routeConfigName = "route-my-service-xds-http-connect"
		clusterName     = "cluster-my-service-xds-http-connect"
		endpointsName   = "endpoints-my-service-xds-http-connect"
	)

	cluster := &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig:   &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}}},
			ServiceName: endpointsName,
		},
		LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
		TransportSocket: &v3corepb.TransportSocket{
			Name:       "envoy.transport_sockets.http_11_proxy",
			ConfigType: &v3corepb.TransportSocket_TypedConfig{TypedConfig: testutils.MarshalAny(t, &v3http11proxypb.Http11ProxyUpstreamTransport{})},
		},
	}

	loadAssignment := &v3endpointpb.ClusterLoadAssignment{
		ClusterName: endpointsName,
		Endpoints: []*v3endpointpb.LocalityLbEndpoints{
			{
				Locality:            &v3corepb.Locality{SubZone: "locality-1"},
				LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
				// Locality-level metadata contains the proxy address.
				Metadata: &v3corepb.Metadata{
					TypedFilterMetadata: map[string]*anypb.Any{
						"envoy.http11_proxy_transport_socket.proxy_address": testutils.MarshalAny(t, &v3corepb.Address{
							Address: &v3corepb.Address_SocketAddress{
								SocketAddress: &v3corepb.SocketAddress{
									Address:       proxyHost,
									PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(proxyPort)},
								},
							},
						}),
					},
				},
				LbEndpoints: []*v3endpointpb.LbEndpoint{
					{
						HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
							Endpoint: &v3endpointpb.Endpoint{
								Address: &v3corepb.Address{
									Address: &v3corepb.Address_SocketAddress{
										SocketAddress: &v3corepb.SocketAddress{
											Address:       backendHost,
											PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(backendPort)},
										},
									},
								},
							},
						},
						LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
					},
				},
			},
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{loadAssignment},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the request was routed through the proxy to the expected
	// backend.
	select {
	case gotTarget := <-connectCh:
		if gotTarget != server.Address {
			t.Errorf("Unexpected server address from proxy server, got %s want %s", gotTarget, server.Address)
		}
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout while waiting for server address from proxy server")
	}
}

// Test verifies client-side HTTP CONNECT proxying support when the client
// uses XDS credentials. It verifies that when a plaintext Http11Proxy wrapper
// is present in CDS, the XDS client successfully falls back to insecure
// credentials and establishes a proxy CONNECT tunnel to the correct backend
// server without security handshaking.
func (s) TestXDSHTTPConnect_WithInsecureXDSCredentials(t *testing.T) {
	enableXDSHTTPConnect(t)

	// Spin up a mock HTTP CONNECT proxy server to capture client connections.
	connectCh := make(chan string, 1)
	pServer := proxyserver.New(t, func(req *http.Request) {
		connectCh <- req.URL.Host
	}, false)

	proxyHost, proxyPort := splitHostPort(t, pServer.Addr)

	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	backendHost, backendPort := splitHostPort(t, server.Address)

	const (
		serviceName     = "my-service-xds-http-connect"
		routeConfigName = "route-my-service-xds-http-connect"
		clusterName     = "cluster-my-service-xds-http-connect"
		endpointsName   = "endpoints-my-service-xds-http-connect"
	)

	cluster := &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig:   &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}}},
			ServiceName: endpointsName,
		},
		LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
		TransportSocket: &v3corepb.TransportSocket{
			Name:       "envoy.transport_sockets.http_11_proxy",
			ConfigType: &v3corepb.TransportSocket_TypedConfig{TypedConfig: testutils.MarshalAny(t, &v3http11proxypb.Http11ProxyUpstreamTransport{})},
		},
	}

	loadAssignment := &v3endpointpb.ClusterLoadAssignment{
		ClusterName: endpointsName,
		Endpoints: []*v3endpointpb.LocalityLbEndpoints{
			{
				Locality:            &v3corepb.Locality{SubZone: "locality-1"},
				LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
				LbEndpoints: []*v3endpointpb.LbEndpoint{
					{
						HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
							Endpoint: &v3endpointpb.Endpoint{
								Address: &v3corepb.Address{
									Address: &v3corepb.Address_SocketAddress{
										SocketAddress: &v3corepb.SocketAddress{
											Address:       backendHost,
											PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(backendPort)},
										},
									},
								},
							},
						},
						Metadata: &v3corepb.Metadata{
							TypedFilterMetadata: map[string]*anypb.Any{
								"envoy.http11_proxy_transport_socket.proxy_address": testutils.MarshalAny(t, &v3corepb.Address{
									Address: &v3corepb.Address_SocketAddress{
										SocketAddress: &v3corepb.SocketAddress{
											Address:       proxyHost,
											PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(proxyPort)},
										},
									},
								}),
							},
						},
						LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
					},
				},
			},
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{loadAssignment},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("Failed to create client XDS credentials: %v", err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(creds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the request was routed through the proxy to the expected
	// backend.
	select {
	case gotTarget := <-connectCh:
		if gotTarget != server.Address {
			t.Errorf("Unexpected server address from proxy server, got %s want %s", gotTarget, server.Address)
		}
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout while waiting for server address from proxy server")
	}
}

// Test verifies client-side HTTP CONNECT proxying support when the cluster
// uses an Http11ProxyUpstreamTransport wrapping an inner UpstreamTlsContext.
// It verifies that a secure TLS session is established with the backend over
// the HTTP CONNECT proxy connection.
func (s) TestXDSHTTPConnect_WithUpstreamTLSContext(t *testing.T) {
	enableXDSHTTPConnect(t)

	// Spin up a mock HTTP CONNECT proxy server to capture client connections.
	connectCh := make(chan string, 1)
	pServer := proxyserver.New(t, func(req *http.Request) {
		connectCh <- req.URL.Host
	}, false)

	proxyHost, proxyPort := splitHostPort(t, pServer.Addr)

	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)
	server := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server.Stop()

	backendHost, backendPort := splitHostPort(t, server.Address)

	const (
		serviceName     = "my-service-xds-http-connect"
		routeConfigName = "route-my-service-xds-http-connect"
		clusterName     = "cluster-my-service-xds-http-connect"
		endpointsName   = "endpoints-my-service-xds-http-connect"
	)

	cluster := &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig:   &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}}},
			ServiceName: endpointsName,
		},
		LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
		TransportSocket: &v3corepb.TransportSocket{
			Name: "envoy.transport_sockets.http_11_proxy",
			ConfigType: &v3corepb.TransportSocket_TypedConfig{
				TypedConfig: testutils.MarshalAny(t, &v3http11proxypb.Http11ProxyUpstreamTransport{
					TransportSocket: &v3corepb.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &v3corepb.TransportSocket_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
								CommonTlsContext: &v3tlspb.CommonTlsContext{
									ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
										ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName: e2e.ClientSideCertProviderInstance,
										},
									},
									TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
										InstanceName: e2e.ClientSideCertProviderInstance,
									},
								},
							}),
						},
					},
				}),
			},
		},
	}

	loadAssignment := &v3endpointpb.ClusterLoadAssignment{
		ClusterName: endpointsName,
		Endpoints: []*v3endpointpb.LocalityLbEndpoints{
			{
				Locality:            &v3corepb.Locality{SubZone: "locality-1"},
				LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
				LbEndpoints: []*v3endpointpb.LbEndpoint{
					{
						HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
							Endpoint: &v3endpointpb.Endpoint{
								Address: &v3corepb.Address{
									Address: &v3corepb.Address_SocketAddress{
										SocketAddress: &v3corepb.SocketAddress{
											Address:       backendHost,
											PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(backendPort)},
										},
									},
								},
							},
						},
						Metadata: &v3corepb.Metadata{
							TypedFilterMetadata: map[string]*anypb.Any{
								"envoy.http11_proxy_transport_socket.proxy_address": testutils.MarshalAny(t, &v3corepb.Address{
									Address: &v3corepb.Address_SocketAddress{
										SocketAddress: &v3corepb.SocketAddress{
											Address:       proxyHost,
											PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(proxyPort)},
										},
									},
								}),
							},
						},
						LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
					},
				},
			},
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{loadAssignment},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("Failed to create client XDS credentials: %v", err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(creds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the request was routed through the proxy to the expected
	// backend.
	select {
	case gotTarget := <-connectCh:
		if gotTarget != server.Address {
			t.Errorf("Unexpected server address from proxy server, got %s  want %s", gotTarget, server.Address)
		}
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout while waiting for server address from proxy server")
	}
}

// Test verifies client-side HTTP CONNECT proxying support when cluster
// specifies an HTTP CONNECT proxy transport socket, but no proxy address
// metadata is supplied. It verifies that the client gracefully fallback
// to dialing the backend directly.
func (s) TestXDSHTTPConnect_NoMetadata(t *testing.T) {
	enableXDSHTTPConnect(t)

	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	const (
		serviceName     = "my-service-xds-http-connect"
		routeConfigName = "route-my-service-xds-http-connect"
		clusterName     = "cluster-my-service-xds-http-connect"
		endpointsName   = "endpoints-my-service-xds-http-connect"
	)

	cluster := &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig:   &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}}},
			ServiceName: endpointsName,
		},
		LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
		TransportSocket: &v3corepb.TransportSocket{
			Name: "envoy.transport_sockets.http_11_proxy",
			ConfigType: &v3corepb.TransportSocket_TypedConfig{
				TypedConfig: testutils.MarshalAny(t, &v3http11proxypb.Http11ProxyUpstreamTransport{})},
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(endpointsName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}
