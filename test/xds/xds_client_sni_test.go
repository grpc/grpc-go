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
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// validSNI matches the wildcard SAN (*.test.example.com) in the test server
// certificate.
const validSNI = "x.test.example.com"

// Tests the SNI and SAN validation logic by verifying that RPCs succeed when
// AutoSNISANValidation is enabled and the SNI matches a server certificate DNS
// SAN. Also verifies that RPCs fail with an 'Unavailable' status if the SNI is
// present but does not match any DNS SAN in the certificate.
func (s) TestClientSideXDS_SNISANValidation(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSNIEnabled, true)

	// Spin up an xDS management server.
	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	// Create test backends for two clusters
	// backend1 configured with TLS creds, represents cluster1 (valid SNI)
	// backend2 configured with TLS creds, represents cluster2 (invalid SNI)
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)
	server1 := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server1.Stop()

	server2 := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server2.Stop()

	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName1 = "cluster1-" + serviceName
	const clusterName2 = "cluster2-" + serviceName
	const endpointName1 = "endpoint1-" + serviceName
	const endpointName2 = "endpoint2-" + serviceName

	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)}

	// Route configuration:
	// - "/grpc.testing.TestService/EmptyCall" --> cluster1 (valid SNI)
	// - "/grpc.testing.TestService/UnaryCall" --> cluster2 (invalid SNI)
	routes := []*v3routepb.RouteConfiguration{{
		Name: routeConfigName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{serviceName},
			Routes: []*v3routepb.Route{
				{
					Match:  &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName1}}},
				},
				{
					Match:  &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/UnaryCall"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName2}}},
				},
			},
		}},
	}}

	// Configure cluster1 with valid SNI and AutoSniSanValidation set to true.
	cluster1 := e2e.DefaultCluster(clusterName1, endpointName1, e2e.SecurityLevelMTLS)
	cluster1.TransportSocket = &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
				Sni:                  validSNI,
				AutoSniSanValidation: true,
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
						ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: e2e.ClientSideCertProviderInstance},
					},
					TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: e2e.ClientSideCertProviderInstance},
				},
			}),
		},
	}

	// Configure cluster2 with invalid SNI and AutoSniSanValidation set to true.
	cluster2 := e2e.DefaultCluster(clusterName2, endpointName2, e2e.SecurityLevelMTLS)
	cluster2.TransportSocket = &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
				Sni:                  "wrong.sni.domain",
				AutoSniSanValidation: true,
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
						ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
							InstanceName:    e2e.ClientSideCertProviderInstance,
							CertificateName: "root",
						},
					},
					TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
						InstanceName:    e2e.ClientSideCertProviderInstance,
						CertificateName: "identity",
					},
				},
			}),
		},
	}

	clusters := []*v3clusterpb.Cluster{cluster1, cluster2}
	endpoints := []*v3endpointpb.ClusterLoadAssignment{
		e2e.DefaultEndpoint(endpointName1, "localhost", []uint32{testutils.ParsePort(t, server1.Address)}),
		e2e.DefaultEndpoint(endpointName2, "localhost", []uint32{testutils.ParsePort(t, server2.Address)}),
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  clusters,
		Endpoints: endpoints,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	// RPC to cluster1 should succeed because auto_sni_san_validation is true
	// and sni matches server cert SAN.
	peerInfo := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peerInfo)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if got, want := peerInfo.Addr.String(), server1.Address; got != want {
		t.Fatalf("EmptyCall() routed to %q, want to be routed to: %q", got, want)
	}

	// RPC to cluster2 should fail because even though auto_sni_san_validation
	// is true, sni doesn't match server cert SAN.
	const wantErr = "do not match the SNI"
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("UnaryCall() failed: %v, wantCode: %s, wantErr: %s", err, codes.Unavailable, wantErr)
	}
}

// Tests that when AutoHostSNI is enabled, the endpoint's hostname is used for
// the ServerName even though SNI is specified. It verifies that the TLS
// handshake and subsequent RPC succeed because the resolved SNI i.e. the
// hostname matches the server's certificate SAN.
func (s) TestClientSideXDS_AutoHostSNI(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSNIEnabled, true)

	// Spin up an xDS management server.
	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	// Create test backend
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)
	server := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server.Stop()

	// Configure client side xDS resources on the management server.
	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName = "cluster-" + serviceName
	const endpointName = "endpoint-" + serviceName

	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)}
	routes := []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)}

	// Configure the cluster with AutoHostSni and AutoSniSanValidation set to
	// true with a invalid SNI.
	cluster := e2e.DefaultCluster(clusterName, endpointName, e2e.SecurityLevelMTLS)
	cluster.TransportSocket = &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
				AutoHostSni:          true,
				AutoSniSanValidation: true,
				Sni:                  "wrong.sni.domain",
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
						ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
							InstanceName:    e2e.ClientSideCertProviderInstance,
							CertificateName: "root",
						},
					},
					TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
						InstanceName:    e2e.ClientSideCertProviderInstance,
						CertificateName: "identity",
					},
				},
			}),
		},
	}

	// Endpoints configuring Hostname to the defaultTestCertSAN to verify
	// AutoHostSni usage.
	endpoints := []*v3endpointpb.ClusterLoadAssignment{
		e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: endpointName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{{
				Weight: 1,
				Backends: []e2e.BackendOptions{{
					Ports:    []uint32{testutils.ParsePort(t, server.Address)},
					Hostname: validSNI,
				}},
			}},
		}),
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Endpoints: endpoints,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	// RPC should succeed because auto_host_sni sets SNI from the endpoint
	// hostname and auto_sni_san_validation validates that the SNI matches
	// server cert SAN.
	peerInfo := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peerInfo)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if got, want := peerInfo.Addr.String(), server.Address; got != want {
		t.Fatalf("EmptyCall() routed to %q, want to be routed to: %q", got, want)
	}
}

// Tests the scenario where multiple endpoints are configured with distinct
// hostnames. It verifies that the client uses the specific hostname associated
// with each endpoint during the TLS handshake by simulating an EDS failover and
// confirming that the SNI is dynamically updated to match the active endpoint.
func (s) TestClientSideXDS_AutoHostSNI_MultipleEndpoints(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSNIEnabled, true)

	// Spin up an xDS management server.
	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	// Create test backends
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)
	server1 := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server1.Stop()

	server2 := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server2.Stop()

	// Configure client side xDS resources on the management server.
	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName = "cluster-" + serviceName
	const endpointName = "endpoint-" + serviceName

	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)}
	routes := []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)}

	clusters := []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, endpointName, e2e.SecurityLevelMTLS)}
	clusters[0].TransportSocket = &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
				AutoHostSni:          true,
				AutoSniSanValidation: true,
				Sni:                  "wrong.sni.domain",
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
						ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
							InstanceName:    e2e.ClientSideCertProviderInstance,
							CertificateName: "root",
						},
					},
					TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
						InstanceName:    e2e.ClientSideCertProviderInstance,
						CertificateName: "identity",
					},
				},
			}),
		},
	}

	// Configure only Priority 0 with an invalid hostname. RPC should fail due
	// to SAN mismatch.
	endpoints := []*v3endpointpb.ClusterLoadAssignment{
		e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: endpointName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Name:     "locality-0",
					Weight:   1,
					Priority: 0,
					Backends: []e2e.BackendOptions{{
						Ports:    []uint32{testutils.ParsePort(t, server1.Address)},
						Hostname: "wrong.sni.domain",
					}},
				},
			},
		}),
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  clusters,
		Endpoints: endpoints,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// RPC should fail because hostname for endpoints in Priority 0 is invalid.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatalf("EmptyCall() unexpectedly succeeded with invalid SNI and no failover")
	} else if !strings.Contains(err.Error(), "authentication handshake failed") {
		t.Fatalf("EmptyCall() failed with %v, want error containing 'authentication handshake failed'", err)
	}

	// Update the same EDS resource to add Priority 1 with an endpoint with
	// valid hostname.
	endpoints = []*v3endpointpb.ClusterLoadAssignment{
		e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: endpointName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Name:     "locality-0",
					Weight:   1,
					Priority: 0,
					Backends: []e2e.BackendOptions{{
						Ports:    []uint32{testutils.ParsePort(t, server1.Address)},
						Hostname: "wrong.sni.domain",
					}},
				},
				{
					Name:     "locality-1",
					Weight:   1,
					Priority: 1,
					Backends: []e2e.BackendOptions{{
						Ports:    []uint32{testutils.ParsePort(t, server2.Address)},
						Hostname: validSNI,
					}},
				},
			},
		}),
	}
	resources.Endpoints = endpoints
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// RPC should succeed because it automatically fails over to Priority 1. Id
	// only one hostname would have been used per leaf cluster , this RPC should
	// have failed due to invalid SNI in Priority 0.
	peerInfo := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peerInfo), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed after adding Priority 1: %v", err)
	}
	if got, want := peerInfo.Addr.String(), server2.Address; got != want {
		t.Fatalf("EmptyCall() successes routed to %q, want %q", got, want)
	}
}

// TestClientSideXDS_FallbackSANMatchers tests that when AutoSniSanValidation is
// true, and no SNI is provided by the control plane and AutoHostSNI is not set,
// the validation falls back to using the explicit SAN matchers specified in the
// configuration. It verifies that RPCs succeed when the fallback matchers match
// the server certificate SAN and fail when they do not.
func (s) TestClientSideXDS_FallbackSANMatchers(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSNIEnabled, true)

	// Spin up an xDS management server.
	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	// Create test backends
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)
	server1 := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server1.Stop()

	server2 := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server2.Stop()

	// Configure client side xDS resources on the management server.
	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName1 = "cluster1-" + serviceName
	const clusterName2 = "cluster2-" + serviceName
	const endpointName1 = "endpoint1-" + serviceName
	const endpointName2 = "endpoint2-" + serviceName

	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)}

	routes := []*v3routepb.RouteConfiguration{{
		Name: routeConfigName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{serviceName},
			Routes: []*v3routepb.Route{
				{
					Match:  &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName1}}},
				},
				{
					Match:  &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/UnaryCall"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName2}}},
				},
			},
		}},
	}}

	// Configure cluster1 with AutoSniSanValidation set to true and no SNI
	// provided for the handshake. The validation falls back to using the explicit
	// SAN matchers specified in the configuration which matches the server1's
	// certificate SAN.
	cluster1 := e2e.DefaultCluster(clusterName1, endpointName1, e2e.SecurityLevelMTLS)
	cluster1.TransportSocket = &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
				AutoSniSanValidation: true,
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &v3tlspb.CertificateValidationContext{
								MatchSubjectAltNames: []*v3matcherpb.StringMatcher{
									{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "*.test.example.com"}},
								},
								CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
									InstanceName:    e2e.ClientSideCertProviderInstance,
									CertificateName: "root",
								},
							},
						},
					},
					TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
						InstanceName:    e2e.ClientSideCertProviderInstance,
						CertificateName: "identity",
					},
				},
			}),
		},
	}

	// Configure cluster2 with AutoSniSanValidation set to true and no SNI
	// provided for the handshake. The validation falls back to using the explicit
	// SAN matchers specified in the configuration which does not match the server2's
	// certificate SAN.
	cluster2 := e2e.DefaultCluster(clusterName2, endpointName2, e2e.SecurityLevelMTLS)
	cluster2.TransportSocket = &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
				AutoSniSanValidation: true,
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &v3tlspb.CertificateValidationContext{
								MatchSubjectAltNames: []*v3matcherpb.StringMatcher{{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "wrong.san.domain"}}},
								CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
									InstanceName:    e2e.ClientSideCertProviderInstance,
									CertificateName: "root",
								},
							},
						},
					},
					TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
						InstanceName:    e2e.ClientSideCertProviderInstance,
						CertificateName: "identity",
					},
				},
			}),
		},
	}

	clusters := []*v3clusterpb.Cluster{cluster1, cluster2}
	endpoints := []*v3endpointpb.ClusterLoadAssignment{
		e2e.DefaultEndpoint(endpointName1, "localhost", []uint32{testutils.ParsePort(t, server1.Address)}),
		e2e.DefaultEndpoint(endpointName2, "localhost", []uint32{testutils.ParsePort(t, server2.Address)}),
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  clusters,
		Endpoints: endpoints,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// RPC to cluster1 should succeed because fallback SAN mathchers are used
	// and they match server cert SAN.
	peerInfo := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peerInfo)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if got, want := peerInfo.Addr.String(), server1.Address; got != want {
		t.Fatalf("EmptyCall() routed to %q, want to be routed to: %q", got, want)
	}

	// RPC to cluster2 should fail because fallback SAN matchers are used
	// but they don't match server cert SAN.
	const wantErr = "do not match any of the accepted SANs"
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("UnaryCall() failed: %v, wantCode: %s, wantErr: %s", err, codes.Unavailable, wantErr)
	}
}

// Tests that when the XDSSNIEnabled environment variable is set to false, SNI
// is not used for validation even if AutoSniSanValidation is true and SNI is
// set. It verifies that the system falls back to using explicit SAN matchers if
// provided, and the TLS handshake succeeds when they match the server
// certificate SAN.
func (s) TestClientSideXDS_SNIEnvVarDisabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSNIEnabled, false)

	// Spin up an xDS management server.
	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	// Create test backends
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)
	server1 := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server1.Stop()

	server2 := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server2.Stop()

	// Configure client side xDS resources on the management server.
	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName1 = "cluster1-" + serviceName
	const clusterName2 = "cluster2-" + serviceName
	const endpointName1 = "endpoint1-" + serviceName
	const endpointName2 = "endpoint2-" + serviceName

	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)}

	routes := []*v3routepb.RouteConfiguration{{
		Name: routeConfigName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{serviceName},
			Routes: []*v3routepb.Route{
				{
					Match:  &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName1}}},
				},
				{
					Match:  &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/UnaryCall"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName2}}},
				},
			},
		}},
	}}

	// cluster1 configuration with AutoSniSanValidation set to true and wrong SNI
	// provided for the handshake. The validation falls back to using the explicit
	// SAN matchers specified in the configuration which matches the server1's
	// certificate SAN.
	cluster1 := e2e.DefaultCluster(clusterName1, endpointName1, e2e.SecurityLevelMTLS)
	cluster1.TransportSocket = &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
				Sni:                  "incorrect.sni",
				AutoSniSanValidation: true,
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &v3tlspb.CertificateValidationContext{
								MatchSubjectAltNames: []*v3matcherpb.StringMatcher{
									{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "*.test.example.com"}},
								},
								CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
									InstanceName:    e2e.ClientSideCertProviderInstance,
									CertificateName: "root",
								},
							},
						},
					},
					TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
						InstanceName:    e2e.ClientSideCertProviderInstance,
						CertificateName: "identity",
					},
				},
			}),
		},
	}

	// Configure cluster2 with AutoSniSanValidation set to true and correct SNI
	// provided for the handshake. The validation falls back to using the
	// explicit SAN matchers specified in the configuration which does not match
	// the server2's certificate SAN.
	cluster2 := e2e.DefaultCluster(clusterName2, endpointName2, e2e.SecurityLevelMTLS)
	cluster2.TransportSocket = &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
				Sni:                  validSNI,
				AutoSniSanValidation: true,
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
						CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
							DefaultValidationContext: &v3tlspb.CertificateValidationContext{
								MatchSubjectAltNames: []*v3matcherpb.StringMatcher{{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "wrong.san.domain"}}},
								CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
									InstanceName:    e2e.ClientSideCertProviderInstance,
									CertificateName: "root",
								},
							},
						},
					},
					TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
						InstanceName:    e2e.ClientSideCertProviderInstance,
						CertificateName: "identity",
					},
				},
			}),
		},
	}

	clusters := []*v3clusterpb.Cluster{cluster1, cluster2}
	endpoints := []*v3endpointpb.ClusterLoadAssignment{
		e2e.DefaultEndpoint(endpointName1, "localhost", []uint32{testutils.ParsePort(t, server1.Address)}),
		e2e.DefaultEndpoint(endpointName2, "localhost", []uint32{testutils.ParsePort(t, server2.Address)}),
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  clusters,
		Endpoints: endpoints,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// RPC to cluster1 should succeed because fallback SAN mathchers are used
	// and they match server cert SAN.
	peerInfo := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peerInfo)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if got, want := peerInfo.Addr.String(), server1.Address; got != want {
		t.Fatalf("EmptyCall() routed to %q, want to be routed to: %q", got, want)
	}

	// RPC to cluster2 should fail because fallback SAN matchers are used but they
	// don't match server cert SAN.
	const wantErr = "do not match any of the accepted SANs"
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("UnaryCall() failed: %v, wantCode: %s, wantErr: %s", err, codes.Unavailable, wantErr)
	}
}

// Tests that when AutoHostSNI is enabled for a Logical DNS cluster, the SNI is
// resolved from the DNSHostName in the cluster configuration. It verifies that
// the TLS handshake succeeds when the DNSHostName matches the server's
// certificate SAN.
func (s) TestClientSideXDS_AutoHostSNI_LogicalDNS(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSNIEnabled, true)

	// Spin up an xDS management server.
	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	// Create test backend
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)
	server := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server.Stop()

	// Replace DNS resolver with a manual resolver.
	dnsR := manual.NewBuilderWithScheme("dns")
	originalDNS := resolver.Get("dns")
	resolver.Register(dnsR)
	t.Cleanup(func() { resolver.Register(originalDNS) })

	dnsR.UpdateState(resolver.State{Endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: server.Address}}}}})

	// Configure client side xDS resources on the management server.
	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName = "cluster-" + serviceName

	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)}
	routes := []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)}

	// Cluster of Type LogicalDNS. with DNSHostName set to match the server's cert
	// SAN.
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		Type:          e2e.ClusterTypeLogicalDNS,
		ClusterName:   clusterName,
		DNSHostName:   validSNI,
		DNSPort:       uint32(testutils.ParsePort(t, server.Address)),
		SecurityLevel: e2e.SecurityLevelMTLS,
	})
	cluster.TransportSocket.ConfigType.(*v3corepb.TransportSocket_TypedConfig).TypedConfig = testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{
		AutoHostSni:          true,
		AutoSniSanValidation: true,
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
	})

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  []*v3clusterpb.Cluster{cluster},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	// RPC should succeed because DNSHostName matches the server's certificate
	// SAN.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}
