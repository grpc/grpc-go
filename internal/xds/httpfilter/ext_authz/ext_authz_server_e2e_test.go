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

package extauthz_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	xdscreds "google.golang.org/grpc/credentials/xds"
	estats "google.golang.org/grpc/experimental/stats"
	grpcinternal "google.golang.org/grpc/internal"
	teststats "google.golang.org/grpc/internal/testutils/stats"

	mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3extauthzfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3authpb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// acceptNotifyingListener wraps a listener and notifies users when a server
// calls the Listener.Accept() method. This can be used to ensure that the
// server is ready before requests are sent to it.
type acceptNotifyingListener struct {
	net.Listener
	serverReady grpcsync.Event
}

func (l *acceptNotifyingListener) Accept() (net.Conn, error) {
	l.serverReady.Fire()
	return l.Listener.Accept()
}

// hostPortFromListener extracts the host string and uint32 port from the
// listener's address.
func hostPortFromListener(lis net.Listener) (string, uint32, error) {
	host, p, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		return "", 0, fmt.Errorf("net.SplitHostPort(%s) failed: %v", lis.Addr().String(), err)
	}
	port, err := strconv.ParseInt(p, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("strconv.ParseInt(%s, 10, 32) failed: %v", p, err)
	}
	return host, uint32(port), nil
}

// buildServerListener creates an inbound xDS Listener resource configured with
// the specified host, port, HTTP filters, and virtual hosts.
func buildServerListener(t *testing.T, host string, port uint32, httpFilters []*v3httppb.HttpFilter, vhs []*v3routepb.VirtualHost) *v3listenerpb.Listener {
	t.Helper()

	return &v3listenerpb.Listener{
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
						Name: "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
								HttpFilters: httpFilters,
								RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
									RouteConfig: &v3routepb.RouteConfiguration{
										Name:         "routeName",
										VirtualHosts: vhs,
									},
								},
							}),
						},
					},
				},
			},
		},
	}
}

// setupXDSListenerAndClient starts an xDS management server and a gRPC server,
// configures the management server with an inbound listener containing the
// specified httpFilters, dials the server using xDS, waits for the server to
// enter SERVING mode, and returns a ClientConn.
func setupXDSListenerAndClient(t *testing.T, stub *stubserver.StubServer, httpFilters ...*v3httppb.HttpFilter) *grpc.ClientConn {
	return setupXDSListenerAndClientWithServerOptions(t, stub, nil, httpFilters...)
}

func setupXDSListenerAndClientWithServerOptions(t *testing.T, stub *stubserver.StubServer, serverOpts []grpc.ServerOption, httpFilters ...*v3httppb.HttpFilter) *grpc.ClientConn {
	t.Helper()

	mgmtServer, nodeID, bootstrapContents, xdsResolver := setup.ManagementServerAndResolver(t)

	servingCh := make(chan struct{})
	servingModeOpt := xds.ServingModeCallback(func(_ net.Addr, args xds.ServingModeChangeArgs) {
		if args.Mode == connectivity.ServingModeServing {
			select {
			case <-servingCh:
			default:
				close(servingCh)
			}
		}
	})

	// Configure xDS credentials to be used on the server-side.
	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatalf("xdscreds.NewServerCredentials failed: %v", err)
	}

	sOpts := append([]grpc.ServerOption{
		grpc.Creds(creds),
		servingModeOpt,
		xds.BootstrapContentsForTesting(bootstrapContents),
	}, serverOpts...)

	if stub.S, err = xds.NewGRPCServer(sOpts...); err != nil {
		t.Fatalf("Failed to create xDS enabled server: %v", err)
	}

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	readyLis := &acceptNotifyingListener{
		Listener:    lis,
		serverReady: *grpcsync.NewEvent(),
	}
	stub.Listener = readyLis
	stubserver.StartTestService(t, stub)
	t.Cleanup(stub.S.Stop)

	// Wait for the server to start running.
	select {
	case <-readyLis.serverReady.Done():
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for server listener to accept")
	}

	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("hostPortFromListener failed: %v", err)
	}

	filters := make([]*v3httppb.HttpFilter, 0, len(httpFilters)+1)
	filters = append(filters, httpFilters...)
	filters = append(filters, e2e.HTTPFilter("router", &v3routerpb.Router{}))

	vhs := []*v3routepb.VirtualHost{
		{
			Domains: []string{"*"},
			Routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
				},
			},
		},
	}

	const serviceName = "service-name"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})

	inboundLis := buildServerListener(t, host, port, filters, vhs)
	resources.Listeners = append(resources.Listeners, inboundLis)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("mgmtServer.Update() failed: %v", err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatalf("xdscreds.NewClientCredentials failed: %v", err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	select {
	case <-servingCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for server to enter SERVING mode")
	}

	return cc
}

// Test verifies the scenarios where external authorization is not enabled on the server.
func (s) TestServerExtAuthz_FilterNotEnabled(t *testing.T) {
	tests := []struct {
		name          string
		denyAtDisable bool
		statusOnError int32
		wantCode      codes.Code
	}{
		{
			name:          "DenyAtDisable_False",
			denyAtDisable: false,
			wantCode:      codes.OK,
		},
		{
			name:          "DenyAtDisable_True_StatusCodeOnError_Default",
			denyAtDisable: true,
			statusOnError: 0, // Use default status code (403 Forbidden).
			wantCode:      codes.PermissionDenied,
		},
		{
			name:          "DenyAtDisable_True_StatusCodeOnError_Unauthorized",
			denyAtDisable: true,
			statusOnError: http.StatusUnauthorized, // Unauthorized (401) translates to codes.Unauthenticated.
			wantCode:      codes.Unauthenticated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, _, _ := startTestServiceBackend(t)
			var checkCalled atomic.Bool
			authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				checkCalled.Store(true)
				return &v3authpb.CheckResponse{Status: &statuspb.Status{Code: int32(codes.OK)}}, nil
			})
			defer stopAuth()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FilterEnabled: &v3corepb.RuntimeFractionalPercent{
					DefaultValue: &v3typepb.FractionalPercent{
						Numerator:   0,
						Denominator: v3typepb.FractionalPercent_HUNDRED,
					},
				},
				DenyAtDisable: &v3corepb.RuntimeFeatureFlag{
					DefaultValue: wrapperspb.Bool(tt.denyAtDisable),
				},
			}
			if tt.statusOnError != 0 {
				extAuthzCfg.StatusOnError = &v3typepb.HttpStatus{
					Code: v3typepb.StatusCode(tt.statusOnError),
				}
			}
			cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			makeUnaryRPC(ctx, t, cc, tt.wantCode, nil, nil, nil)
			makeStreamingRPC(ctx, t, cc, tt.wantCode, nil, nil, nil)
		})
	}
}

// Test verifies the scenarios where the external authorization RPC times out
// because the configured server timeout is very low.
func (s) TestServerExtAuthz_AuthzRPC_Timeout(t *testing.T) {
	tests := []struct {
		name                      string
		failureModeAllow          bool
		failureModeAllowHeaderAdd bool
		statusOnError             int32
		wantCode                  codes.Code
		wantHeaders               metadata.MD
	}{
		{
			name:             "FailureModeAllow_False_StatusCodeOnError_Default",
			failureModeAllow: false,
			statusOnError:    0, // Use default status code (403 Forbidden -> codes.PermissionDenied).
			wantCode:         codes.PermissionDenied,
		},
		{
			name:             "FailureModeAllow_False_StatusCodeOnError_Unauthorized",
			failureModeAllow: false,
			statusOnError:    http.StatusUnauthorized, // Unauthorized (401) translates to codes.Unauthenticated.
			wantCode:         codes.Unauthenticated,
		},
		{
			name:             "FailureModeAllow_True",
			failureModeAllow: true,
			wantCode:         codes.OK,
			wantHeaders:      metadata.Pairs(":authority", "service-name"),
		},
		{
			name:                      "FailureModeAllow_True_HeaderAdd",
			failureModeAllow:          true,
			failureModeAllowHeaderAdd: true,
			wantCode:                  codes.OK,
			wantHeaders:               metadata.Pairs(":authority", "service-name", "x-envoy-auth-failure-mode-allowed", "true"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t)
			authAddr, stopAuth := startTestAuthServer(t, func(ctx context.Context, _ *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				// Wait until context is done to simulate a timeout on the ext_authz RPC.
				<-ctx.Done()
				return nil, ctx.Err()
			})
			defer stopAuth()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
					GrpcService: &v3corepb.GrpcService{
						Timeout: durationpb.New(defaultTestShortTimeout),
					},
				},
				FailureModeAllow:          tt.failureModeAllow,
				FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
				FilterEnabled: &v3corepb.RuntimeFractionalPercent{
					DefaultValue: &v3typepb.FractionalPercent{
						Numerator:   100,
						Denominator: v3typepb.FractionalPercent_HUNDRED,
					},
				},
			}
			if tt.statusOnError != 0 {
				extAuthzCfg.StatusOnError = &v3typepb.HttpStatus{
					Code: v3typepb.StatusCode(tt.statusOnError),
				}
			}

			cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			makeUnaryRPC(ctx, t, cc, tt.wantCode, nil, gotUnaryMD, tt.wantHeaders)
			makeStreamingRPC(ctx, t, cc, tt.wantCode, nil, gotStreamingMD, tt.wantHeaders)
		})
	}
}

// Test verifies scenarios where the external authorization RPC fails
// (returns an error).
func (s) TestServerExtAuthz_AuthzRPC_Failure(t *testing.T) {
	tests := []struct {
		name                      string
		failureModeAllow          bool
		failureModeAllowHeaderAdd bool
		statusOnError             int32
		wantCode                  codes.Code
		wantHeaders               metadata.MD
	}{
		{
			name:             "FailureModeAllow_False_StatusCodeOnError_Default",
			failureModeAllow: false,
			statusOnError:    0, // Use default status code (403 Forbidden -> codes.PermissionDenied).
			wantCode:         codes.PermissionDenied,
		},
		{
			name:             "FailureModeAllow_False_StatusCodeOnError_Unauthorized",
			failureModeAllow: false,
			statusOnError:    http.StatusUnauthorized, // Unauthorized (401) translates to codes.Unauthenticated.
			wantCode:         codes.Unauthenticated,
		},
		{
			name:             "FailureModeAllow_True",
			failureModeAllow: true,
			wantCode:         codes.OK,
			wantHeaders:      metadata.Pairs(":authority", "service-name"),
		},
		{
			name:                      "FailureModeAllow_True_HeaderAdd",
			failureModeAllow:          true,
			failureModeAllowHeaderAdd: true,
			wantCode:                  codes.OK,
			wantHeaders:               metadata.Pairs(":authority", "service-name", "x-envoy-auth-failure-mode-allowed", "true"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t)
			authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return nil, status.Error(codes.Internal, "internal server error")
			})
			defer stopAuth()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FailureModeAllow:          tt.failureModeAllow,
				FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
				FilterEnabled: &v3corepb.RuntimeFractionalPercent{
					DefaultValue: &v3typepb.FractionalPercent{
						Numerator:   100,
						Denominator: v3typepb.FractionalPercent_HUNDRED,
					},
				},
			}
			if tt.statusOnError != 0 {
				extAuthzCfg.StatusOnError = &v3typepb.HttpStatus{
					Code: v3typepb.StatusCode(tt.statusOnError),
				}
			}

			cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			makeUnaryRPC(ctx, t, cc, tt.wantCode, nil, gotUnaryMD, tt.wantHeaders)
			makeStreamingRPC(ctx, t, cc, tt.wantCode, nil, gotStreamingMD, tt.wantHeaders)
		})
	}
}

// Test verifies cases where the external authorization server denies a
// data plane RPC on the server.
func (s) TestServerExtAuthz_DeniedResponse(t *testing.T) {
	tests := []struct {
		name                       string
		checkFunc                  func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)
		decoderHeaderMutationRules *mutationpb.HeaderMutationRules
		failureModeAllow           bool
		failureModeAllowHeaderAdd  bool
		wantStatus                 codes.Code
		wantTrailer                metadata.MD
		wantHeaders                metadata.MD
	}{
		{
			name: "NoDeniedResponse",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
				}, nil
			},
			wantStatus: codes.PermissionDenied,
		},
		{
			name: "WithDeniedResponse",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							// Unauthorized (401) translates to codes.Unauthenticated.
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Unauthorized},
						},
					},
				}, nil
			},
			wantStatus: codes.Unauthenticated,
		},
		{
			name: "WithDeniedResponse_NoStatusCode",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							// Status is nil, should default to codes.PermissionDenied.
						},
					},
				}, nil
			},
			wantStatus: codes.PermissionDenied,
		},
		{
			name: "WithDeniedResponse_CustomMessage",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{
						Code:    int32(codes.PermissionDenied),
						Message: "custom denial reason",
					},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
						},
					},
				}, nil
			},
			wantStatus: codes.PermissionDenied,
		},
		{
			name: "WithDeniedResponse_Headers",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "custom-denied-header", Value: "custom-val"}},
							},
						},
					},
				}, nil
			},
			wantStatus:  codes.PermissionDenied,
			wantTrailer: metadata.Pairs("custom-denied-header", "custom-val"),
		},
		{
			name: "WithDeniedResponse_HeaderMutationFails_FailureModeAllow_False",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^disallowed-header$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			wantStatus: codes.PermissionDenied,
		},
		{
			name: "WithDeniedResponse_HeaderMutationFails_FailureModeAllow_True_NoHeaderAdd",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^disallowed-header$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			failureModeAllow:          true,
			failureModeAllowHeaderAdd: false,
			wantStatus:                codes.OK,
			wantHeaders:               metadata.Pairs(":authority", "service-name"),
		},
		{
			name: "WithDeniedResponse_HeaderMutationFails_FailureModeAllow_True_HeaderAdd",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^disallowed-header$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			failureModeAllow:          true,
			failureModeAllowHeaderAdd: true,
			wantStatus:                codes.OK,
			wantHeaders:               metadata.Pairs(":authority", "service-name", "x-envoy-auth-failure-mode-allowed", "true"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t)
			authAddr, stopAuth := startTestAuthServer(t, tt.checkFunc)
			defer stopAuth()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FilterEnabled: &v3corepb.RuntimeFractionalPercent{
					DefaultValue: &v3typepb.FractionalPercent{
						Numerator:   100,
						Denominator: v3typepb.FractionalPercent_HUNDRED,
					},
				},
				DecoderHeaderMutationRules: tt.decoderHeaderMutationRules,
				FailureModeAllow:           tt.failureModeAllow,
				FailureModeAllowHeaderAdd:  tt.failureModeAllowHeaderAdd,
			}

			cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			makeUnaryRPC(ctx, t, cc, tt.wantStatus, tt.wantTrailer, gotUnaryMD, tt.wantHeaders)
			makeStreamingRPC(ctx, t, cc, tt.wantStatus, tt.wantTrailer, gotStreamingMD, tt.wantHeaders)
		})
	}
}

// Tests verifies the cases where the ext_authz server allows the data plane
// RPC, but does not specify any headers or response headers to add. Verifies
// that the RPC is sent to the backend without modification and that it
// eventually succeeds.
func (s) TestServerExtAuthz_Allowed_NoHTTPResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	initialMetadata := metadata.Pairs("key1", "value1")
	authAddr, stopAuth := startTestAuthServer(t, func(ctx context.Context, _ *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Errorf(codes.Internal, "metadata not found in incoming context")
		}
		if len(md["key1"]) == 0 || md["key1"][0] != "value1" {
			return nil, status.Errorf(codes.Internal, "initial metadata not found in CheckRequest, got: %v, want: %v", md, initialMetadata)
		}
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{Status: st}, nil
	})
	defer stopAuth()

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t)

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
			GrpcService: &v3corepb.GrpcService{
				InitialMetadata: []*v3corepb.HeaderValue{
					{Key: "key1", Value: "value1"},
				},
			},
		},
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("test-key", "test-value"))
	wantHeaders := metadata.Pairs(":authority", "service-name", "test-key", "test-value")

	makeUnaryRPC(outgoingCtx, t, cc, codes.OK, nil, gotUnaryMD, wantHeaders)
	makeStreamingRPC(outgoingCtx, t, cc, codes.OK, nil, gotStreamingMD, wantHeaders)
}

// Tests verifies the case where the ext_authz server allows the data plane RPC
// and specifies headers to be added and removed from the data plane RPC.
// Verifies that the RPC is sent to the backend with the expected headers and
// that it eventually succeeds.
func (s) TestServerExtAuthz_Allowed_WithHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t)

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(false), // Disallowed header mutations are silently ignored.
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	wantHeaders := metadata.Pairs(":authority", "service-name", "k1", "v1", "k2-bin", "\x00\x01\x02\x03")

	makeUnaryRPC(outgoingCtx, t, cc, codes.OK, nil, gotUnaryMD, wantHeaders)
	makeStreamingRPC(outgoingCtx, t, cc, codes.OK, nil, gotStreamingMD, wantHeaders)
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies headers to be added and removed from the data plane RPC. One
// of the specified header mutations is not allowed by the configuration.
// Verifies that the RPC is failed with error code PermissionDenied.
func (s) TestServerExtAuthz_Allowed_WithHeaders_MutationFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t)

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		StatusOnError: &v3typepb.HttpStatus{
			Code: v3typepb.StatusCode_Unauthorized, // 401 Unauthorized
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(true), // Disallowed header mutations result in RPC failures.
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))

	makeUnaryRPC(outgoingCtx, t, cc, codes.Unauthenticated, nil, gotUnaryMD, nil)
	makeStreamingRPC(outgoingCtx, t, cc, codes.Unauthenticated, nil, gotStreamingMD, nil)
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies headers to be added and removed from the data plane RPC. One
// of the specified header mutations is not allowed by the configuration, but
// failure_mode_allow is set to true. Verifies that the header mutation error
// is ignored, no partial header mutations are applied, and that the data plane
// RPC succeeds with unmutated headers.
func (s) TestServerExtAuthz_AllowedWithHeaders_MutationFails_FailureModeAllow(t *testing.T) {
	allowedHeaders := []*v3corepb.HeaderValueOption{
		{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}},
		{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}},
	}
	disallowedHeader := &v3corepb.HeaderValueOption{
		Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"},
	}

	tests := []struct {
		name                      string
		failureModeAllowHeaderAdd bool
		headersToAdd              []*v3corepb.HeaderValueOption
		headersToRemove           []string
		outgoingHeaders           metadata.MD
		wantHeaders               func(serviceName string) metadata.MD
	}{
		{
			name:                      "OnlyHeaderAddFails_FailureModeHeaderAdd_False",
			failureModeAllowHeaderAdd: false,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"k-test-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("k-test-header-to-be-removed", "true"),
			wantHeaders: func(serviceName string) metadata.MD {
				return metadata.Pairs(":authority", serviceName)
			},
		},
		{
			name:                      "OnlyHeaderAddFails_FailureModeHeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"k-test-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("k-test-header-to-be-removed", "true"),
			wantHeaders: func(serviceName string) metadata.MD {
				return metadata.Pairs(":authority", serviceName, "x-envoy-auth-failure-mode-allowed", "true")
			},
		},
		{
			name:                      "OnlyHeaderRemoveFails_FailureModeHeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              allowedHeaders,
			headersToRemove:           []string{"a2-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("a2-header-to-be-removed", "true"),
			wantHeaders: func(serviceName string) metadata.MD {
				return metadata.Pairs(":authority", serviceName, "k1", "v1", "k2-bin", "\x00\x01\x02\x03", "a2-header-to-be-removed", "true", "x-envoy-auth-failure-mode-allowed", "true")
			},
		},
		{
			name:                      "BothMutationsFail_FailureModeHeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"a2-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("a2-header-to-be-removed", "true"),
			wantHeaders: func(serviceName string) metadata.MD {
				return metadata.Pairs(":authority", serviceName, "a2-header-to-be-removed", "true", "x-envoy-auth-failure-mode-allowed", "true")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			// Start a test ext_authz server that allows the data plane RPC.
			authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				st := &statuspb.Status{Code: int32(codes.OK)}
				return &v3authpb.CheckResponse{
					Status: st,
					HttpResponse: &v3authpb.CheckResponse_OkResponse{
						OkResponse: &v3authpb.OkHttpResponse{
							Headers:         tt.headersToAdd,
							HeadersToRemove: tt.headersToRemove,
						},
					},
				}, nil
			})
			defer stopAuth()

			backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t)

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FailureModeAllow:          true,
				FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
				FilterEnabled: &v3corepb.RuntimeFractionalPercent{
					DefaultValue: &v3typepb.FractionalPercent{
						Numerator:   100,
						Denominator: v3typepb.FractionalPercent_HUNDRED,
					},
				},
				DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
					AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
					DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a[0-9].*"},
					DisallowIsError:    wrapperspb.Bool(true), // Disallowed header mutations result in RPC failures.
				},
			}

			cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
			outgoingCtx := metadata.NewOutgoingContext(ctx, tt.outgoingHeaders)
			wantHeaders := tt.wantHeaders("service-name")

			makeUnaryRPC(outgoingCtx, t, cc, codes.OK, nil, gotUnaryMD, wantHeaders)
			makeStreamingRPC(outgoingCtx, t, cc, codes.OK, nil, gotStreamingMD, wantHeaders)
		})
	}
}

// Test verifies the case where the ext_authz server allows a unary data plane
// RPC and specifies response headers to be added and removed from the data
// plane RPC. Verifies that the unary data plane RPC succeeds with expected
// response headers.
func (s) TestServerExtAuthz_Allowed_WithResponseHeadersMutations_UnaryRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	// Start a test backend that verifies request headers and sends response headers.
	respHeaders := metadata.Pairs("test-trailer-key", "test-trailer-value", "k1", "test-trailer-v1")
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			gotReqMD, _ := metadata.FromIncomingContext(ctx)
			wantReqHeaders := metadata.Pairs(":authority", "service-name")
			if err := compareMetadata(gotReqMD, wantReqHeaders); err != nil {
				return nil, status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			if err := grpc.SendHeader(ctx, respHeaders); err != nil {
				return nil, err
			}
			return &testpb.Empty{}, nil
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(false), // Disallowed header mutations are silently ignored.
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))

	wantRespHeaders := metadata.Pairs(
		"test-trailer-key", "test-trailer-value",
		"k1", "v1",
		"k1", "test-trailer-v1",
		"k2-bin", "\x00\x01\x02\x03",
	)

	client := testgrpc.NewTestServiceClient(cc)
	var gotHeaders metadata.MD
	if _, err := client.EmptyCall(outgoingCtx, &testpb.Empty{}, grpc.Header(&gotHeaders)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if err := compareMetadata(gotHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Unexpected headers metadata received by the client: %v", err)
	}
}

// Test verifies the case where the ext_authz server allows a streaming data
// plane RPC and specifies response headers to be added and removed from the
// data plane RPC. Verifies that the streaming data plane RPC succeeds with
// expected response headers.
func (s) TestServerExtAuthz_Allowed_WithResponseHeadersMutations_StreamingRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	// Start a test backend that verifies request headers and sends response headers.
	respHeaders := metadata.Pairs("test-trailer-key", "test-trailer-value", "k1", "test-trailer-v1")
	backend := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			gotReqMD, _ := metadata.FromIncomingContext(stream.Context())
			wantReqHeaders := metadata.Pairs(":authority", "service-name")
			if err := compareMetadata(gotReqMD, wantReqHeaders); err != nil {
				return status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			return stream.SendHeader(respHeaders)
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(false), // Disallowed header mutations are silently ignored.
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))

	wantRespHeaders := metadata.Pairs(
		"test-trailer-key", "test-trailer-value",
		"k1", "v1",
		"k1", "test-trailer-v1",
		"k2-bin", "\x00\x01\x02\x03",
	)

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(outgoingCtx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}
	gotHeaders, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}
	if err := compareMetadata(gotHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Unexpected headers metadata received by the client: %v", err)
	}
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies response headers to add, but the backend sends a Trailers-Only
// response (no response headers). Verifies that response headers are empty/nil
// and response header mutations are safely ignored without error.
func (s) TestServerExtAuthz_Allowed_TrailersOnlyResponse_HeaderMutationSkipped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}},
					},
				},
			},
		}, nil
	})
	defer stopAuth()

	// Backend returns without sending initial headers (trailers-only response).
	backend := &stubserver.StubServer{
		FullDuplexCallF: func(testgrpc.TestService_FullDuplexCallServer) error {
			return nil
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	gotHeaders, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}
	if gotHeaders != nil {
		t.Fatalf("stream.Header() = %v, want nil for trailers-only response", gotHeaders)
	}
}

// Test verifies the case where the ext_authz server allows a unary data plane
// RPC and specifies response headers to be added and removed from the data
// plane RPC. One of the response header mutations is not allowed by the
// configuration. Verifies that the server rejects the unary RPC with
// PermissionDenied.
func (s) TestServerExtAuthz_Allowed_ResponseHeaderMutationFailed_UnaryRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	// Start a test backend that sends response headers.
	respHeaders := metadata.Pairs("test-trailer-key", "test-trailer-value")
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			if err := grpc.SendHeader(ctx, respHeaders); err != nil {
				return nil, err
			}
			return &testpb.Empty{}, nil
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(true), // Disallowed header mutations result in RPC failures.
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(outgoingCtx, &testpb.Empty{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("EmptyCall() status = %v, want %v (error: %v)", status.Code(err), codes.PermissionDenied, err)
	}
}

// Test verifies the case where the ext_authz server allows a streaming data
// plane RPC and specifies response headers to be added and removed from the
// data plane RPC. One of the response header mutations is not allowed by the
// configuration. Verifies that the server rejects the streaming RPC with
// PermissionDenied.
func (s) TestServerExtAuthz_Allowed_ResponseHeaderMutationFailed_StreamingRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	// Start a test backend that sends response headers.
	respHeaders := metadata.Pairs("test-trailer-key", "test-trailer-value")
	backend := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			return stream.SendHeader(respHeaders)
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(true), // Disallowed header mutations result in RPC failures.
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(outgoingCtx)
	if err != nil {
		t.Fatalf("Failed to start streaming RPC: %v", err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("FullDuplexCall stream.Recv() failed with status code = %v, want %v (error: %v)", status.Code(err), codes.PermissionDenied, err)
	}
}

// Test verifies the case where the ext_authz server specifies a response
// header mutation for a unary data plane RPC that fails validation, but
// failure_mode_allow is set to true. Verifies that the unary data plane RPC
// succeeds and the invalid response header is not added.
func (s) TestServerExtAuthz_Allowed_ResponseHeaderMutationFailed_FailureModeAllow_UnaryRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}}, // Disallowed response header.
					},
				},
			},
		}, nil
	})
	defer stopAuth()

	respHeaders := metadata.Pairs("test-header-key", "test-header-value")
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			gotMD, _ := metadata.FromIncomingContext(ctx)
			wantIncomingHeaders := metadata.Pairs(":authority", "service-name", "x-envoy-auth-failure-mode-allowed", "true")
			if err := compareMetadata(gotMD, wantIncomingHeaders); err != nil {
				return nil, status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			if err := grpc.SendHeader(ctx, respHeaders); err != nil {
				return nil, err
			}
			return &testpb.Empty{}, nil
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FailureModeAllow:          true,
		FailureModeAllowHeaderAdd: true,
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(true),
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	wantRespHeaders := metadata.Pairs("test-header-key", "test-header-value")

	client := testgrpc.NewTestServiceClient(cc)
	var gotHeaders metadata.MD
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Header(&gotHeaders)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if err := compareMetadata(gotHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Response header mismatch: %v", err)
	}
}

// Test verifies the case where the ext_authz server specifies a response
// header mutation for a streaming data plane RPC that fails validation, but
// failure_mode_allow is set to true. Verifies that the streaming data plane
// RPC succeeds and the invalid response header is not added.
func (s) TestServerExtAuthz_Allowed_ResponseHeaderMutationFailed_FailureModeAllow_StreamingRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}}, // Disallowed response header.
					},
				},
			},
		}, nil
	})
	defer stopAuth()

	respHeaders := metadata.Pairs("test-header-key", "test-header-value")
	backend := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			gotMD, _ := metadata.FromIncomingContext(stream.Context())
			wantIncomingHeaders := metadata.Pairs(":authority", "service-name", "x-envoy-auth-failure-mode-allowed", "true")
			if err := compareMetadata(gotMD, wantIncomingHeaders); err != nil {
				return status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			return stream.SendHeader(respHeaders)
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FailureModeAllow:          true,
		FailureModeAllowHeaderAdd: true,
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(true),
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	wantRespHeaders := metadata.Pairs("test-header-key", "test-header-value")

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}
	hdr, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}
	if err := compareMetadata(hdr, wantRespHeaders); err != nil {
		t.Fatalf("Response header mismatch: %v", err)
	}
}

// Test verifies the case where the ext_authz server allows a unary data plane
// RPC and specifies both header and response header mutations that are
// expected to succeed. Verifies that the unary data plane RPC succeeds.
func (s) TestServerExtAuthz_Allowed_WithRequestAndResponseHeadersMutations_UnaryRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}}, // Allowed.
					},
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	// Start a test backend that verifies request headers and sends response headers.
	respHeaders := metadata.Pairs("test-trailer-key", "test-trailer-value")
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			gotReqMD, _ := metadata.FromIncomingContext(ctx)
			wantReqHeaders := metadata.Pairs(":authority", "service-name", "k1", "v1")
			if err := compareMetadata(gotReqMD, wantReqHeaders); err != nil {
				return nil, status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			if err := grpc.SendHeader(ctx, respHeaders); err != nil {
				return nil, err
			}
			return &testpb.Empty{}, nil
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(true), // Disallowed header mutations result in RPC failures.
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	wantRespHeaders := metadata.Pairs(
		"test-trailer-key", "test-trailer-value",
		"k2-bin", "\x00\x01\x02\x03",
	)

	client := testgrpc.NewTestServiceClient(cc)
	var gotHeaders metadata.MD
	if _, err := client.EmptyCall(outgoingCtx, &testpb.Empty{}, grpc.Header(&gotHeaders)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if err := compareMetadata(gotHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Unexpected headers metadata received by the client: %v", err)
	}
}

// Test verifies the case where the ext_authz server allows a streaming data
// plane RPC and specifies both header and response header mutations that are
// expected to succeed. Verifies that the streaming data plane RPC succeeds.
func (s) TestServerExtAuthz_Allowed_WithRequestAndResponseHeadersMutations_StreamingRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k1", Value: "v1"}}, // Allowed.
					},
					ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	// Start a test backend that verifies request headers and sends response headers.
	respHeaders := metadata.Pairs("test-trailer-key", "test-trailer-value")
	backend := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			gotReqMD, _ := metadata.FromIncomingContext(stream.Context())
			wantReqHeaders := metadata.Pairs(":authority", "service-name", "k1", "v1")
			if err := compareMetadata(gotReqMD, wantReqHeaders); err != nil {
				return status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			return stream.SendHeader(respHeaders)
		},
	}

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(true), // Disallowed header mutations result in RPC failures.
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	wantRespHeaders := metadata.Pairs(
		"test-trailer-key", "test-trailer-value",
		"k2-bin", "\x00\x01\x02\x03",
	)

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(outgoingCtx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}
	gotHeaders, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}
	if err := compareMetadata(gotHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Unexpected headers metadata received by the client: %v", err)
	}
}

// Test verifies the scenario where allowed_headers and disallowed_headers are
// configured on the server filter. Verifies that the CheckRequest sent to the
// authorization server only includes headers matching allowed_headers and
// excludes headers matching disallowed_headers.
func (s) TestServerExtAuthz_RequestHeaderFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	authAddr, stopAuth := startTestAuthServer(t, func(_ context.Context, req *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		gotCheckMD := metadata.MD{}
		for _, h := range req.GetAttributes().GetRequest().GetHttp().GetHeaderMap().GetHeaders() {
			gotCheckMD.Append(h.GetKey(), string(h.GetRawValue()))
		}
		wantCheckHeaders := metadata.Pairs(
			"allow-header-1", "val1",
			"exact-header", "val2",
		)
		if err := compareMetadata(gotCheckMD, wantCheckHeaders); err != nil {
			return nil, status.Errorf(codes.Internal, "Unexpected headers in CheckRequest: %v", err)
		}
		return &v3authpb.CheckResponse{Status: &statuspb.Status{Code: int32(codes.OK)}}, nil
	})
	defer stopAuth()

	backend, _, _ := startTestServiceBackend(t)

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		AllowedHeaders: &matcherpb.ListStringMatcher{
			Patterns: []*matcherpb.StringMatcher{
				{MatchPattern: &matcherpb.StringMatcher_Prefix{Prefix: "allow-"}},
				{MatchPattern: &matcherpb.StringMatcher_Exact{Exact: "exact-header"}},
			},
		},
		DisallowedHeaders: &matcherpb.ListStringMatcher{
			Patterns: []*matcherpb.StringMatcher{
				{MatchPattern: &matcherpb.StringMatcher_Prefix{Prefix: "allow-disallowed-"}},
			},
		},
	}

	cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authAddr, extAuthzCfg))
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"allow-header-1", "val1",
		"exact-header", "val2",
		"allow-disallowed-header", "val3",
		"random-header", "val4",
	))

	makeUnaryRPC(outgoingCtx, t, cc, codes.OK, nil, nil, nil)
	makeStreamingRPC(outgoingCtx, t, cc, codes.OK, nil, nil, nil)
}

// Test verifies that the CheckRequest received by the external authorization
// server accurately reflects the incoming request method, path, headers,
// client source address, and server destination address.
func (s) TestServerExtAuthz_CheckRequestAttributes(t *testing.T) {
	for _, tc := range []struct {
		rpcType  string
		wantPath string
		makeRPC  func(ctx context.Context, client testgrpc.TestServiceClient) error
	}{
		{
			rpcType:  "Unary",
			wantPath: "/grpc.testing.TestService/EmptyCall",
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient) error {
				_, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true))
				return err
			},
		},
		{
			rpcType:  "Streaming",
			wantPath: "/grpc.testing.TestService/FullDuplexCall",
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient) error {
				stream, err := client.FullDuplexCall(ctx)
				if err != nil {
					return err
				}
				stream.Send(&testpb.StreamingOutputCallRequest{})
				stream.CloseSend()
				_, err = stream.Recv()
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			},
		},
	} {
		t.Run(tc.rpcType, func(t *testing.T) {
			var capturedReq atomic.Pointer[v3authpb.CheckRequest]

			authServerAddr, stopAuth := startTestAuthServer(t, func(_ context.Context, req *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				capturedReq.Store(req)
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.OK)},
					HttpResponse: &v3authpb.CheckResponse_OkResponse{
						OkResponse: &v3authpb.OkHttpResponse{},
					},
				}, nil
			})
			defer stopAuth()

			backend, _, _ := startTestServiceBackend(t)

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{}
			cc := setupXDSListenerAndClient(t, backend, extAuthzHTTPFilter(authServerAddr, extAuthzCfg))

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			callCtx := metadata.AppendToOutgoingContext(ctx, "custom-client-header", "client-header-value")
			client := testgrpc.NewTestServiceClient(cc)
			if err := tc.makeRPC(callCtx, client); err != nil {
				t.Fatalf("%s RPC failed: %v", tc.rpcType, err)
			}

			req := capturedReq.Load()
			if req == nil {
				t.Fatal("ExtAuthz CheckRPC was not invoked")
			}

			attrs := req.GetAttributes()
			if attrs == nil {
				t.Fatal("ExtAuthz CheckRPC attributes is nil")
			}

			// Verify Request HTTP fields
			reqHTTP := attrs.GetRequest().GetHttp()
			if reqHTTP == nil {
				t.Fatal("Request.Http is nil")
			}
			if reqHTTP.GetMethod() != "POST" {
				t.Errorf("Method = %v, want POST", reqHTTP.GetMethod())
			}
			if reqHTTP.GetPath() != tc.wantPath {
				t.Errorf("Path = %v, want %v", reqHTTP.GetPath(), tc.wantPath)
			}
			if reqHTTP.GetProtocol() != "HTTP/2" {
				t.Errorf("Protocol = %v, want HTTP/2", reqHTTP.GetProtocol())
			}

			gotCheckMD := metadata.MD{}
			for _, h := range reqHTTP.GetHeaderMap().GetHeaders() {
				gotCheckMD.Append(h.GetKey(), string(h.GetRawValue()))
			}
			wantCheckHeaders := metadata.Pairs(
				":authority", "service-name",
				"custom-client-header", "client-header-value",
			)
			if err := compareMetadata(gotCheckMD, wantCheckHeaders); err != nil {
				t.Errorf("Unexpected headers in CheckRequest: %v", err)
			}

			// Verify Source and Destination
			if src := attrs.GetSource(); src == nil || src.GetAddress() == nil {
				t.Errorf("Source address is nil: %v", src)
			}
			if dst := attrs.GetDestination(); dst == nil || dst.GetAddress() == nil {
				t.Errorf("Destination address is nil: %v", dst)
			}
		})
	}
}

// Test verifies that a per-route configuration can disable the ExtAuthz filter
// on a specific route while other routes remain protected.
func (s) TestServerExtAuthz_PerRouteOverride(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Authz server denies all requests
	authServerAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		return &v3authpb.CheckResponse{
			Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
			HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
				DeniedResponse: &v3authpb.DeniedHttpResponse{
					Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
				},
			},
		}, nil
	})
	defer stopAuth()

	mgmtServer, nodeID, bootstrapContents, xdsResolver := setup.ManagementServerAndResolver(t)

	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	stub, _, _ := startTestServiceBackend(t)

	servingCh := make(chan struct{})
	servingModeOpt := xds.ServingModeCallback(func(_ net.Addr, args xds.ServingModeChangeArgs) {
		if args.Mode == connectivity.ServingModeServing {
			select {
			case <-servingCh:
			default:
				close(servingCh)
			}
		}
	})

	opts := []grpc.ServerOption{
		grpc.Creds(creds),
		servingModeOpt,
		xds.BootstrapContentsForTesting(bootstrapContents),
	}
	if stub.S, err = xds.NewGRPCServer(opts...); err != nil {
		t.Fatalf("Failed to create xDS enabled server: %v", err)
	}

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	readyLis := &acceptNotifyingListener{
		Listener:    lis,
		serverReady: *grpcsync.NewEvent(),
	}
	stub.Listener = readyLis
	stubserver.StartTestService(t, stub)
	defer stub.S.Stop()

	select {
	case <-readyLis.serverReady.Done():
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for server to start")
	}

	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("hostPortFromListener failed: %v", err)
	}

	const serviceName = "my-service-ext-authz-route-override"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})

	vhs := []*v3routepb.VirtualHost{
		{
			Domains: []string{"*"},
			Routes: []*v3routepb.Route{
				// Route 1: EmptyCall with header - ExtAuthz disabled via TypedPerFilterConfig override
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
						Headers: []*v3routepb.HeaderMatcher{
							{
								Name:                 "x-disable-ext-authz",
								HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "true"},
							},
						},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
					TypedPerFilterConfig: map[string]*anypb.Any{
						"com.google.grpc.ext_authz": testutils.MarshalAny(t, &v3routepb.FilterConfig{
							Disabled: true,
						}),
					},
				},
				// Route 2: FullDuplexCall with header - ExtAuthz disabled via TypedPerFilterConfig override
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/FullDuplexCall"},
						Headers: []*v3routepb.HeaderMatcher{
							{
								Name:                 "x-disable-ext-authz",
								HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "true"},
							},
						},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
					TypedPerFilterConfig: map[string]*anypb.Any{
						"com.google.grpc.ext_authz": testutils.MarshalAny(t, &v3routepb.FilterConfig{
							Disabled: true,
						}),
					},
				},
				// Route 3: EmptyCall without header - protected by ExtAuthz (will be denied)
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
				},
				// Route 4: FullDuplexCall without header - protected by ExtAuthz (will be denied)
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/FullDuplexCall"},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
				},
			},
		},
	}

	inboundLis := buildServerListener(t, host, port, []*v3httppb.HttpFilter{
		extAuthzHTTPFilter(authServerAddr, &v3extauthzfilterpb.ExtAuthz{}),
		e2e.HTTPFilter("router", &v3routerpb.Router{}),
	}, vhs)
	resources.Listeners = append(resources.Listeners, inboundLis)

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	select {
	case <-servingCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for server to enter SERVING mode")
	}

	// Protected routes: EmptyCall and FullDuplexCall hit Routes 3 & 4 -> Denied by ExtAuthz
	makeUnaryRPC(ctx, t, cc, codes.PermissionDenied, nil, nil, nil)
	makeStreamingRPC(ctx, t, cc, codes.PermissionDenied, nil, nil, nil)

	// Disabled routes: EmptyCall and FullDuplexCall with header hit Routes 1 & 2 -> Filter disabled on route -> Succeeded
	disabledCtx := metadata.AppendToOutgoingContext(ctx, "x-disable-ext-authz", "true")
	makeUnaryRPC(disabledCtx, t, cc, codes.OK, nil, nil, nil)
	makeStreamingRPC(disabledCtx, t, cc, codes.OK, nil, nil, nil)
}

// Test verifies that server-side metrics are emitted correctly during RPC
// execution across different authorization outcomes.
func (s) TestServerExtAuthz_ServerMetrics(t *testing.T) {
	tests := []struct {
		name                       string
		checkFunc                  func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)
		filterEnabled              *v3corepb.RuntimeFractionalPercent
		decoderHeaderMutationRules *mutationpb.HeaderMutationRules
		failureModeAllow           bool
		wantMetric                 string
		wantNotMetric              string
	}{
		{
			name: "Allowed_RPCs",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.OK)},
				}, nil
			},
			wantMetric: "grpc.server_ext_authz.allowed_rpcs",
		},
		{
			name: "Denied_RPCs",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
				}, nil
			},
			wantMetric: "grpc.server_ext_authz.denied_rpcs",
		},
		{
			name: "Failed_RPCs_DeniedHeaderMutationFailed",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^disallowed-header$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			wantMetric:    "grpc.server_ext_authz.failed_rpcs",
			wantNotMetric: "grpc.server_ext_authz.denied_rpcs",
		},
		{
			name: "Failed_RPCs_DeniedHeaderMutationFailed_FailureModeAllow",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^disallowed-header$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			failureModeAllow: true,
			wantMetric:       "grpc.server_ext_authz.failed_rpcs",
			wantNotMetric:    "grpc.server_ext_authz.denied_rpcs",
		},
		{
			name: "FilterDisabled_RPCs",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   0,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			wantMetric: "grpc.server_ext_authz.filter_disabled_rpcs",
		},
		{
			name: "Failed_RPCs",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return nil, status.Error(codes.Internal, "internal server error")
			},
			wantMetric: "grpc.server_ext_authz.failed_rpcs",
		},
		{
			name: "Failed_RPCs_HeaderMutationFailed",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.OK)},
					HttpResponse: &v3authpb.CheckResponse_OkResponse{
						OkResponse: &v3authpb.OkHttpResponse{
							Headers: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			wantMetric:    "grpc.server_ext_authz.failed_rpcs",
			wantNotMetric: "grpc.server_ext_authz.allowed_rpcs",
		},
		{
			name: "Failed_RPCs_HeaderMutationFailed_FailureModeAllow",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.OK)},
					HttpResponse: &v3authpb.CheckResponse_OkResponse{
						OkResponse: &v3authpb.OkHttpResponse{
							Headers: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			failureModeAllow: true,
			wantMetric:       "grpc.server_ext_authz.failed_rpcs",
			wantNotMetric:    "grpc.server_ext_authz.allowed_rpcs",
		},
		{
			name: "Failed_RPCs_ResponseHeaderMutationFailed",
			filterEnabled: &v3corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.OK)},
					HttpResponse: &v3authpb.CheckResponse_OkResponse{
						OkResponse: &v3authpb.OkHttpResponse{
							ResponseHeadersToAdd: []*v3corepb.HeaderValueOption{
								{Header: &v3corepb.HeaderValue{Key: "a1", Value: "v1"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			wantMetric:    "grpc.server_ext_authz.failed_rpcs",
			wantNotMetric: "grpc.server_ext_authz.allowed_rpcs",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, rpcType := range []string{"Unary", "Streaming"} {
				t.Run(rpcType, func(t *testing.T) {
					authAddr, stop := startTestAuthServer(t, test.checkFunc)
					defer stop()

					backend, _, _ := startTestServiceBackend(t)
					extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
						FilterEnabled:              test.filterEnabled,
						DecoderHeaderMutationRules: test.decoderHeaderMutationRules,
						FailureModeAllow:           test.failureModeAllow,
					}

					tmr := teststats.NewTestMetricsRecorder()
					cc := setupXDSListenerAndClientWithServerOptions(t, backend, []grpc.ServerOption{grpc.StatsHandler(tmr)}, extAuthzHTTPFilter(authAddr, extAuthzCfg))
					ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
					defer cancel()

					client := testgrpc.NewTestServiceClient(cc)
					if rpcType == "Unary" {
						client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true))
					} else {
						stream, err := client.FullDuplexCall(ctx)
						if err == nil {
							stream.Send(&testpb.StreamingOutputCallRequest{})
							stream.CloseSend()
							stream.Recv()
						}
					}

					wantData := teststats.MetricsData{
						Handle:    estats.DescriptorForMetric(test.wantMetric),
						IntIncr:   1,
						LabelKeys: nil,
						LabelVals: nil,
					}
					// Poll until the specific metric is recorded, then assert ALL fields with cmp.Diff
					for {
						if got, ok := tmr.MetricsData(test.wantMetric); ok {
							if diff := cmp.Diff(wantData, got); diff != "" {
								t.Fatalf("MetricsData mismatch (-want, +got):\n%s", diff)
							}
							break
						}
						select {
						case <-ctx.Done():
							t.Fatalf("Timed out waiting for metric %q: %v", test.wantMetric, ctx.Err())
						case <-time.After(10 * time.Millisecond):
						}
					}

					if test.wantNotMetric != "" {
						if got, ok := tmr.MetricsData(test.wantNotMetric); ok {
							t.Fatalf("Unexpected metric recorded %q: %v", test.wantNotMetric, got)
						}
					}
				})
			}
		})
	}
}

// Test verifies the ext_authz filter when the xDS server is not configured
// with the trusted_xds_server feature. On this path the credentials in the
// GrpcService proto are ignored, and the filter config is accepted only
// because the external authorization server's target is present in the
// bootstrap allowed_grpc_services map, whose credentials are used for the
// side channel (gRFC A102). Verifies that a data-plane RPC flows through
// the external authorization server end-to-end.
func (s) TestServerExtAuthz_UntrustedServerAllowedGRPCServices(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	const mutatedHeader = "request-mutated"
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		return &v3authpb.CheckResponse{
			Status: &statuspb.Status{Code: int32(codes.OK)},
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*v3corepb.HeaderValueOption{
						{Header: &v3corepb.HeaderValue{Key: mutatedHeader, Value: "true"}},
					},
				},
			},
		}, nil
	})
	defer stopAuth()

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t)

	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	nodeID := uuid.New().String()
	bc, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers:                            fmt.Appendf(nil, `[{"server_uri": "passthrough:///%s", "channel_creds": [{"type": "insecure"}]}]`, managementServer.Address),
		Node:                               fmt.Appendf(nil, `{"id": %q}`, nodeID),
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
		AllowedGRPCServices:                fmt.Appendf(nil, `{%q: {"channel_creds": [{"type": "insecure"}]}}`, authAddr),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap contents: %v", err)
	}
	xdsResolver, err := grpcinternal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver: %v", err)
	}

	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	opts := []grpc.ServerOption{
		grpc.Creds(creds),
		xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
			t.Logf("Serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
		}),
		xds.BootstrapContentsForTesting(bc),
	}
	if backend.S, err = xds.NewGRPCServer(opts...); err != nil {
		t.Fatalf("Failed to create xDS enabled server: %v", err)
	}

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	readyLis := &acceptNotifyingListener{
		Listener:    lis,
		serverReady: *grpcsync.NewEvent(),
	}
	backend.Listener = readyLis
	stubserver.StartTestService(t, backend)
	defer backend.S.Stop()

	select {
	case <-readyLis.serverReady.Done():
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for server to start")
	}

	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("hostPortFromListener failed: %v", err)
	}

	const serviceName = "service-untrusted"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})

	extAuthzConfig := &v3extauthzfilterpb.ExtAuthz{
		Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: authAddr,
					},
				},
			},
		},
		FilterEnabled: &v3corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression: &matcherpb.RegexMatcher{Regex: ".*"},
		},
	}

	vhs := []*v3routepb.VirtualHost{
		{
			Domains: []string{"*"},
			Routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
				},
			},
		},
	}

	inboundLis := buildServerListener(t, host, port, []*v3httppb.HttpFilter{
		e2e.HTTPFilter("com.google.grpc.ext_authz", extAuthzConfig),
		e2e.HTTPFilter("router", &v3routerpb.Router{}),
	}, vhs)
	resources.Listeners = append(resources.Listeners, inboundLis)

	updateCtx, updateCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer updateCancel()
	if err := managementServer.Update(updateCtx, resources); err != nil {
		t.Fatalf("managementServer.Update() failed: %v", err)
	}

	clientCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	dopts := []grpc.DialOption{grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(xdsResolver)}
	cc, err := grpc.NewClient("xds:///"+serviceName, dopts...)
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	wantHeaders := metadata.Pairs(":authority", serviceName, mutatedHeader, "true")
	makeUnaryRPC(ctx, t, cc, codes.OK, nil, gotUnaryMD, wantHeaders)
	makeStreamingRPC(ctx, t, cc, codes.OK, nil, gotStreamingMD, wantHeaders)
}
