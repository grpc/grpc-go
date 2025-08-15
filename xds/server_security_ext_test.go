/*
 *
 * Copyright 2025 gRPC authors.
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
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/xds"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// Tests the case where the bootstrap configuration contains no certificate
// providers, and xDS credentials with an insecure fallback is specified at
// server creation time. The management server is configured to return a
// server-side xDS Listener resource with no security configuration. The test
// verifies that a gRPC client configured with insecure credentials is able to
// make RPCs to the backend. This ensures that the insecure fallback
// credentials are getting used on the server.
func (s) TestServer_Security_NoCertProvidersInBootstrap_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Create a listener on a local port to act as the xDS enabled gRPC server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to listen to local port: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}

	// Configure the managegement server with a listener and route configuration
	// resource for the above xDS enabled gRPC server.
	const routeConfigName = "routeName"
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")},
		Routes:         []*v3routepb.RouteConfiguration{e2e.RouteConfigNonForwardingAction(routeConfigName)},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Start an xDS-enabled gRPC server with the above bootstrap configuration
	// and configure xDS credentials to be used on the server-side.
	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("Serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
	})
	createStubServer(t, lis, grpc.Creds(creds), modeChangeOpt, xds.ClientPoolForTesting(pool))

	// Create a client that uses insecure creds and verify RPCs.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests the case where the bootstrap configuration contains no certificate
// providers, and xDS credentials with an insecure fallback is specified at
// server creation time. The management server is configured to return a
// server-side xDS Listener resource with mTLS security configuration. The xDS
// client is expected to NACK this resource because the certificate provider
// instance name specified in the Listener resource will not be present in the
// bootstrap file. The test verifies that server creation does not fail and that
// if the xDS-enabled gRPC server receives resource error causing mode change,
// it does not enter "serving" mode.
func (s) TestServer_Security_NoCertificateProvidersInBootstrap_Failure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Spin up an xDS management server that pushes on a channel when it
	// receives a NACK for an LDS response.
	nackCh := make(chan struct{}, 1)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() != "type.googleapis.com/envoy.config.listener.v3.Listener" {
				return nil
			}
			if req.GetErrorDetail() == nil {
				return nil
			}
			select {
			case nackCh <- struct{}{}:
			default:
			}
			return nil
		},
		AllowResourceSubset: true,
	})

	// Create bootstrap configuration with no certificate providers.
	nodeID := uuid.New().String()
	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		}]`, mgmtServer.Address)),
		Node:                               []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap configuration: %v", err)
	}

	// Create a listener on a local port to act as the xDS enabled gRPC server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to listen to local port: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	// Start an xDS-enabled gRPC server with the above bootstrap configuration
	// and configure xDS credentials to be used on the server-side.
	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeHandler := newServingModeChangeHandler(t)
	modeChangeOpt := xds.ServingModeCallback(modeChangeHandler.modeChangeCallback)
	createStubServer(t, lis, grpc.Creds(creds), modeChangeOpt, xds.ClientPoolForTesting(pool))

	// Create an inbound xDS listener resource for the server side that contains
	// mTLS security configuration. Since the received certificate provider
	// instance name would be missing in the bootstrap configuration, this
	// resource is expected to NACKed by the xDS client.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListener(host, port, e2e.SecurityLevelMTLS, "routeName")},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the NACK from the xDS client.
	select {
	case <-nackCh:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for an NACK from the xDS client for the LDS response")
	}

	// Wait a short duration and ensure that if the server receive mode change
	// it does not enter "serving" mode.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case stateCh := <-modeChangeHandler.modeCh:
		if stateCh == connectivity.ServingModeServing {
			t.Fatal("Server entered serving mode before the route config was received")
		}
	}

	// Create a client that uses insecure creds and verify that RPCs don't
	// succeed.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	waitForFailedRPCWithStatus(ctx, t, cc, codes.Unavailable, "", "")
}

// Tests the case where the bootstrap configuration contains one certificate
// provider, and xDS credentials with an insecure fallback is specified at
// server creation time. Two listeners are configured on the xDS-enabled gRPC
// server. The management server responds with two listener resources:
//  1. contains valid security configuration pointing to the certificate provider
//     instance specified in the bootstrap
//  2. contains invalid security configuration pointing to a non-existent
//     certificate provider instance
//
// The test verifies that an RPC to the first listener succeeds, while the
// second listener receive a resource error which cause the server mode change
// but never moves to "serving" mode.
func (s) TestServer_Security_WithValidAndInvalidSecurityConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Spin up an xDS management server that pushes on a channel when it
	// receives a NACK for an LDS response.
	nackCh := make(chan struct{}, 1)
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() != "type.googleapis.com/envoy.config.listener.v3.Listener" {
				return nil
			}
			if req.GetErrorDetail() == nil {
				return nil
			}
			select {
			case nackCh <- struct{}{}:
			default:
			}
			return nil
		},
		AllowResourceSubset: true,
	})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Create two xDS-enabled gRPC servers using the above bootstrap configs.
	lis1, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis2, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	modeChangeHandler1 := newServingModeChangeHandler(t)
	modeChangeOpt1 := xds.ServingModeCallback(modeChangeHandler1.modeChangeCallback)
	modeChangeHandler2 := newServingModeChangeHandler(t)
	modeChangeOpt2 := xds.ServingModeCallback(modeChangeHandler2.modeChangeCallback)
	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatal(err)
	}
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	createStubServer(t, lis1, grpc.Creds(creds), modeChangeOpt1, xds.ClientPoolForTesting(pool))
	createStubServer(t, lis2, grpc.Creds(creds), modeChangeOpt2, xds.ClientPoolForTesting(pool))

	// Create inbound xDS listener resources for the server side that contains
	// mTLS security configuration.
	// lis1 --> security configuration pointing to a valid cert provider
	// lis2 --> security configuration pointing to a non-existent cert provider
	host1, port1, err := hostPortFromListener(lis1)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	resource1 := e2e.DefaultServerListener(host1, port1, e2e.SecurityLevelMTLS, "routeName")
	host2, port2, err := hostPortFromListener(lis2)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	hcm := &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
			RouteConfig: &v3routepb.RouteConfiguration{
				Name: "routeName",
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{"*"},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{
							PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
						},
						Action: &v3routepb.Route_NonForwardingAction{},
					}}}}},
		},
		HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
	}
	ts := &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
				RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
						InstanceName: "non-existent-certificate-provider",
					},
					ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
						ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
							InstanceName: "non-existent-certificate-provider",
						},
					},
				},
			}),
		},
	}
	resource2 := &v3listenerpb.Listener{
		Name: fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host2, strconv.Itoa(int(port2)))),
		Address: &v3corepb.Address{
			Address: &v3corepb.Address_SocketAddress{
				SocketAddress: &v3corepb.SocketAddress{
					Address: host2,
					PortSpecifier: &v3corepb.SocketAddress_PortValue{
						PortValue: port2,
					},
				},
			},
		},
		FilterChains: []*v3listenerpb.FilterChain{
			{
				Name: "v4-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "0.0.0.0",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "0.0.0.0",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
				},
				Filters: []*v3listenerpb.Filter{
					{
						Name:       "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: testutils.MarshalAny(t, hcm)},
					},
				},
				TransportSocket: ts,
			},
			{
				Name: "v6-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "::",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "::",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
				},
				Filters: []*v3listenerpb.Filter{
					{
						Name:       "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: testutils.MarshalAny(t, hcm)},
					},
				},
				TransportSocket: ts,
			},
		},
	}
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{resource1, resource2},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a client that uses TLS creds and verify RPCs to listener1.
	clientCreds := testutils.CreateClientTLSCredentials(t)
	cc1, err := grpc.NewClient(lis1.Addr().String(), grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc1.Close()

	client1 := testgrpc.NewTestServiceClient(cc1)
	if _, err := client1.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Wait for the NACK from the xDS client.
	select {
	case <-nackCh:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for an NACK from the xDS client for the LDS response")
	}

	// Wait a short duration and ensure that if the server receives mode change
	// it does not enter "serving" mode.
	select {
	case <-time.After(2 * defaultTestShortTimeout):
	case mode := <-modeChangeHandler2.modeCh:
		if mode == connectivity.ServingModeServing {
			t.Fatal("Server changed to serving mode when not expected to")
		}
	}

	// Create a client that uses insecure creds and verify that RPCs don't
	// succeed to listener2.
	cc2, err := grpc.NewClient(lis2.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc2.Close()

	waitForFailedRPCWithStatus(ctx, t, cc2, codes.Unavailable, "", "")
}
