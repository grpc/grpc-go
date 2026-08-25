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
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	teststats "google.golang.org/grpc/internal/testutils/stats"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	iextauthz "google.golang.org/grpc/internal/xds/httpfilter/ext_authz/internal"

	mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3extauthzfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3authgrpc "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	v3authpb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// testExtAuthzServer is a test implementation of the Envoy Authorization
// service that delegates Check calls to a test-only hook.
type testExtAuthzServer struct {
	v3authgrpc.UnimplementedAuthorizationServer
	checkFunc func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)
}

// Check handles incoming Check RPCs by delegating to checkFunc if configured.
func (s *testExtAuthzServer) Check(ctx context.Context, req *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
	if s.checkFunc != nil {
		return s.checkFunc(ctx, req)
	}
	return nil, nil
}

// startTestAuthServer configures ext_authz environment variables and function
// hooks, starts a test external authorization server, and registers cleanup.
// It takes checkFunc to handle Check RPCs and returns the server's listener
// address and a function to stop the server.
func startTestAuthServer(t *testing.T, checkFunc func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)) (string, func()) {
	t.Helper()

	origCreate := iextauthz.CreateExtAuthzChannel
	origParse := iextauthz.ParseGRPCServiceConfig
	iextauthz.ParseGRPCServiceConfig = iextauthz.ParseGRPCServiceConfigForTesting
	iextauthz.CreateExtAuthzChannel = func(cfg xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		conn, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return conn, conn.Close, nil
	}

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtAuthzEnabled, true)
	iextauthz.RegisterForTesting()

	t.Cleanup(func() {
		iextauthz.CreateExtAuthzChannel = origCreate
		iextauthz.ParseGRPCServiceConfig = origParse
		iextauthz.UnregisterForTesting()
	})

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	authServer := &testExtAuthzServer{checkFunc: checkFunc}
	gs := grpc.NewServer()
	v3authgrpc.RegisterAuthorizationServer(gs, authServer)
	go gs.Serve(lis)

	t.Cleanup(gs.Stop)

	return lis.Addr().String(), gs.Stop
}

// setupTestClient configures the management server with xDS resources that
// include the ext_authz filter, and creates a new gRPC client.
func setupTestClient(t *testing.T, authServerAddr string, extAuthzConfig *v3extauthzfilterpb.ExtAuthz, serverAddr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	t.Helper()
	mgmtServer, nodeID, _, resolverBuilder := setup.ManagementServerAndResolver(t)

	extAuthzConfig.Services = &v3extauthzfilterpb.ExtAuthz_GrpcService{
		GrpcService: &corepb.GrpcService{
			TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
				GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
					TargetUri: authServerAddr,
				},
			},
			Timeout:         extAuthzConfig.GetGrpcService().GetTimeout(),
			InitialMetadata: extAuthzConfig.GetGrpcService().GetInitialMetadata(),
		},
	}

	const serviceName = "service-name"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, serverAddr),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err := apiListener.UnmarshalTo(hcm); err != nil {
		return nil, err
	}
	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("com.google.grpc.ext_authz", extAuthzConfig),
	}, hcm.HttpFilters...)
	resources.Listeners[0].ApiListener.ApiListener = testutils.MarshalAny(t, hcm)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		return nil, err
	}

	dopts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder)}, opts...)
	cc, err := grpc.NewClient("xds:///"+serviceName, dopts...)
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	return cc, nil
}

// compareMetadata removes metadata entries that are not pertinent to tests in
// this package before comparing them. This ensures that the expected metadata
// defined in tests will be shorter.
func compareMetadata(got, want metadata.MD) error {
	if got.Get("content-type") != nil {
		got.Delete("content-type")
	}
	if got.Get("user-agent") != nil {
		got.Delete("user-agent")
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		return fmt.Errorf("diff in metadata (-want +got):\n%s", diff)
	}
	return nil
}

// Test verifies the scenarios where external authorization is not enabled.
func (s) TestExtAuthz_FilterNotEnabled(t *testing.T) {
	// Start a backend server.
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

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
			var checkCalled atomic.Bool
			authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				checkCalled.Store(true)
				return &v3authpb.CheckResponse{Status: &statuspb.Status{Code: int32(codes.OK)}}, nil
			})
			defer stopAuth()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FilterEnabled: &corepb.RuntimeFractionalPercent{
					DefaultValue: &v3typepb.FractionalPercent{
						Numerator:   0,
						Denominator: v3typepb.FractionalPercent_HUNDRED,
					},
				},
				DenyAtDisable: &corepb.RuntimeFeatureFlag{
					DefaultValue: wrapperspb.Bool(tt.denyAtDisable),
				},
			}
			if tt.statusOnError != 0 {
				extAuthzCfg.StatusOnError = &v3typepb.HttpStatus{
					Code: v3typepb.StatusCode(tt.statusOnError),
				}
			}

			cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
			if err != nil {
				t.Fatalf("setupTestClient() failed: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != tt.wantCode {
				t.Fatalf("EmptyCall() failed with status code = %v, want %v (error: %v)", status.Code(err), tt.wantCode, err)
			}
		})
	}
}

// Test verifies the scenarios where the external authorization RPC times out
// because the configured server timeout is very low.
func (s) TestExtAuthz_AuthzRPC_Timeout(t *testing.T) {
	tests := []struct {
		name                      string
		failureModeAllow          bool
		failureModeAllowHeaderAdd bool
		statusOnError             int32
		wantCode                  codes.Code
		wantFailureHeader         bool
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
		},
		{
			name:                      "FailureModeAllow_True_HeaderAdd",
			failureModeAllow:          true,
			failureModeAllowHeaderAdd: true,
			wantCode:                  codes.OK,
			wantFailureHeader:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authAddr, stopAuth := startTestAuthServer(t, func(ctx context.Context, _ *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				// Wait until context is done to simulate a timeout on the ext_authz RPC.
				<-ctx.Done()
				return nil, ctx.Err()
			})
			defer stopAuth()

			backend := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					if tt.wantFailureHeader {
						md, ok := metadata.FromIncomingContext(ctx)
						if !ok {
							return nil, status.Error(codes.Internal, "no incoming metadata")
						}
						vals := md.Get("x-envoy-auth-failure-mode-allowed")
						if len(vals) == 0 || vals[0] != "true" {
							return nil, status.Errorf(codes.Internal, "missing or invalid x-envoy-auth-failure-mode-allowed header: %v", vals)
						}
					}
					return &testpb.Empty{}, nil
				},
			}
			stubserver.StartTestService(t, backend)
			defer backend.Stop()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
					GrpcService: &corepb.GrpcService{
						Timeout: durationpb.New(defaultTestShortTimeout),
					},
				},
				FailureModeAllow:          tt.failureModeAllow,
				FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
				FilterEnabled: &corepb.RuntimeFractionalPercent{
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

			cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
			if err != nil {
				t.Fatalf("setupTestClient() failed: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != tt.wantCode {
				t.Fatalf("EmptyCall() failed with status code = %v, want %v (error: %v)", status.Code(err), tt.wantCode, err)
			}
		})
	}
}

// Test verifies scenarios where the external authorization RPC fails
// (returns an error).
func (s) TestExtAuthz_AuthzRPC_Failure(t *testing.T) {
	tests := []struct {
		name                      string
		failureModeAllow          bool
		failureModeAllowHeaderAdd bool
		statusOnError             int32
		wantCode                  codes.Code
		wantFailureHeader         bool
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
		},
		{
			name:                      "FailureModeAllow_True_HeaderAdd",
			failureModeAllow:          true,
			failureModeAllowHeaderAdd: true,
			wantCode:                  codes.OK,
			wantFailureHeader:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return nil, status.Error(codes.Internal, "internal server error")
			})
			defer stopAuth()

			backend := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					if tt.wantFailureHeader {
						md, ok := metadata.FromIncomingContext(ctx)
						if !ok {
							return nil, status.Error(codes.Internal, "no incoming metadata")
						}
						vals := md.Get("x-envoy-auth-failure-mode-allowed")
						if len(vals) == 0 || vals[0] != "true" {
							return nil, status.Errorf(codes.Internal, "missing or invalid x-envoy-auth-failure-mode-allowed header: %v", vals)
						}
					}
					return &testpb.Empty{}, nil
				},
			}
			stubserver.StartTestService(t, backend)
			defer backend.Stop()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FailureModeAllow:          tt.failureModeAllow,
				FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
				FilterEnabled: &corepb.RuntimeFractionalPercent{
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

			cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
			if err != nil {
				t.Fatalf("setupTestClient() failed: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != tt.wantCode {
				t.Fatalf("EmptyCall() failed with status code = %v, want %v (error: %v)", status.Code(err), tt.wantCode, err)
			}
		})
	}
}

// Test verifies cases where the external authorization server denies the data
// plane RPC.
func (s) TestExtAuthz_Denied(t *testing.T) {
	tests := []struct {
		name       string
		checkFunc  func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)
		wantStatus codes.Code
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authAddr, stopAuth := startTestAuthServer(t, tt.checkFunc)
			defer stopAuth()

			var backendCalled atomic.Bool
			backend := &stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					backendCalled.Store(true)
					return &testpb.Empty{}, nil
				},
			}
			stubserver.StartTestService(t, backend)
			defer backend.Stop()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FilterEnabled: &corepb.RuntimeFractionalPercent{
					DefaultValue: &v3typepb.FractionalPercent{
						Numerator:   100,
						Denominator: v3typepb.FractionalPercent_HUNDRED,
					},
				},
			}

			cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
			if err != nil {
				t.Fatalf("setupTestClient() failed: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != tt.wantStatus {
				t.Fatalf("EmptyCall() failed with status code = %v, want %v (error: %v)", status.Code(err), tt.wantStatus, err)
			}

			if backendCalled.Load() {
				t.Fatal("Backend was called unexpectedly when RPC was denied")
			}
		})
	}
}

// Tests verifies the cases where the ext_authz server allows the data plane
// RPC, but does not specify any headers or response headers to add. Verifies
// that the RPC is sent to the backend without modification and that it
// eventually succeeds.
func (s) TestExtAuthz_Allowed_NoHTTPResponse(t *testing.T) {
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

	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			gotMD, _ := metadata.FromIncomingContext(ctx)
			wantHeaders := metadata.Pairs(":authority", "service-name", "test-key", "test-value")
			if err := compareMetadata(gotMD, wantHeaders); err != nil {
				return nil, status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
			GrpcService: &corepb.GrpcService{
				InitialMetadata: []*corepb.HeaderValue{
					{Key: "key1", Value: "value1"},
				},
			},
		},
		FilterEnabled: &corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
	}

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("test-key", "test-value"))
	if _, err := client.EmptyCall(outgoingCtx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests verifies the case where the ext_authz server allows the data plane RPC
// and specifies headers to be added and removed from the data plane RPC.
// Verifies that the RPC is sent to the backend with the expected headers and
// that it eventually succeeds.
func (s) TestExtAuthz_Allowed_WithHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			gotMD, _ := metadata.FromIncomingContext(ctx)
			wantHeaders := metadata.Pairs(":authority", "service-name", "k1", "v1", "k2-bin", "\x00\x01\x02\x03")
			if err := compareMetadata(gotMD, wantHeaders); err != nil {
				return nil, status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &corepb.RuntimeFractionalPercent{
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

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	if _, err := client.EmptyCall(outgoingCtx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Test verifes the case where the ext_authz server allows the data plane RPC
// and specifies headers to be added and removed from the data plane RPC. One
// of the specified header mutations is not allowed by the configuration.
// Verifies that the RPC is failed with error code Unknown. Also verifies that
// the backend is not called.
func (s) TestExtAuthz_Allowed_WithHeaders_MutationFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
					},
					HeadersToRemove: []string{"k-test-header-to-be-removed"},
				},
			},
		}, nil
	})
	defer stopAuth()

	var backendCalled atomic.Bool
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			backendCalled.Store(true)
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &corepb.RuntimeFractionalPercent{
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

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	if _, err := client.EmptyCall(outgoingCtx, &testpb.Empty{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("EmptyCall() failed with status code = %v, want %v (error: %v)", status.Code(err), codes.PermissionDenied, err)
	}

	if backendCalled.Load() {
		t.Fatal("Backend was called when it should not have been called")
	}
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies headers to be added and removed from the data plane RPC. One
// of the specified header mutations is not allowed by the configuration, but
// failure_mode_allow is set to true. Verifies that the header mutation error is
// ignored, no partial header mutations are applied, and that the data plane
// RPC succeeds with unmutated headers.
func (s) TestExtAuthz_Allowed_WithHeaders_MutationFails_FailureModeAllow(t *testing.T) {
	allowedHeaders := []*corepb.HeaderValueOption{
		{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}},
		{Header: &corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}},
	}
	disallowedHeader := &corepb.HeaderValueOption{
		Header: &corepb.HeaderValue{Key: "a1", Value: "v1"},
	}

	tests := []struct {
		name                      string
		failureModeAllowHeaderAdd bool
		headersToAdd              []*corepb.HeaderValueOption
		headersToRemove           []string
		outgoingHeaders           metadata.MD
		wantHeaders               metadata.MD
	}{
		{
			name:                      "OnlyHeaderAddFails_HeaderAdd_False",
			failureModeAllowHeaderAdd: false,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"k-test-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("k-test-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name"),
		},
		{
			name:                      "OnlyHeaderAddFails_HeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"k-test-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("k-test-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name", "x-envoy-auth-failure-mode-allowed", "true"),
		},
		{
			name:                      "OnlyHeaderRemoveFails_HeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              allowedHeaders,
			headersToRemove:           []string{"a2-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("a2-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name", "k1", "v1", "k2-bin", "\x00\x01\x02\x03", "a2-header-to-be-removed", "true", "x-envoy-auth-failure-mode-allowed", "true"),
		},
		{
			name:                      "BothMutationsFail_HeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"a2-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("a2-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name", "a2-header-to-be-removed", "true", "x-envoy-auth-failure-mode-allowed", "true"),
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

			backend := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					gotMD, _ := metadata.FromIncomingContext(ctx)
					if err := compareMetadata(gotMD, tt.wantHeaders); err != nil {
						return nil, status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
					}
					return &testpb.Empty{}, nil
				},
			}
			stubserver.StartTestService(t, backend)
			defer backend.Stop()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FailureModeAllow:          true,
				FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
				FilterEnabled: &corepb.RuntimeFractionalPercent{
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

			cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
			if err != nil {
				t.Fatalf("setupTestClient() failed: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)
			outgoingCtx := metadata.NewOutgoingContext(ctx, tt.outgoingHeaders)
			if _, err := client.EmptyCall(outgoingCtx, &testpb.Empty{}); err != nil {
				t.Fatalf("EmptyCall() failed: %v", err)
			}
		})
	}
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies response headers to be added and removed from the data plane
// RPC. Verifies that data plane RPC succeeds with expected response headers.
func (s) TestExtAuthz_Allowed_WithResponseHeadersMutations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
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
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &corepb.RuntimeFractionalPercent{
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

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	stream, err := client.FullDuplexCall(outgoingCtx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	gotHeaders, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}

	wantRespHeaders := metadata.Pairs(
		"test-trailer-key", "test-trailer-value",
		"k1", "test-trailer-v1",
		"k1", "v1",
		"k2-bin", "\x00\x01\x02\x03",
	)
	if err := compareMetadata(gotHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Unexpected headers metadata received by the client: %v", err)
	}
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies response headers to add, but the backend sends a Trailers-Only
// response (no response headers). Verifies that stream.Header() returns
// (nil, nil) and response header mutations are safely ignored without error.
func (s) TestExtAuthz_Allowed_TrailersOnlyResponse_HeaderMutationSkipped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}},
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
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &corepb.RuntimeFractionalPercent{
			DefaultValue: &v3typepb.FractionalPercent{
				Numerator:   100,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		},
	}

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

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

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies response headers to be added and removed from the data plane
// RPC. One of the response header mutations is not allowed by the
// configuration. Verifies that retrieving headers fails.
func (s) TestExtAuthz_Allowed_ResponseHeaderMutationFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}},                                   // Allowed.
						{Header: &corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
						{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}},                                   // Disallowed.
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
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &corepb.RuntimeFractionalPercent{
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

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	if _, err := client.FullDuplexCall(outgoingCtx); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("FullDuplexCall() failed with status code = %v, want %v (error: %v)", status.Code(err), codes.PermissionDenied, err)
	}
}

// Test verifies the case where the ext_authz server specifies a response
// header mutation that fails validation, but failure_mode_allow is set to
// true. Verifies that the data plane RPC succeeds and the invalid response
// header is not added.
func (s) TestExtAuthz_Allowed_ResponseHeaderMutationFailed_FailureModeAllow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}}, // Disallowed response header.
					},
				},
			},
		}, nil
	})
	defer stopAuth()

	wantIncomingHeaders := metadata.Pairs(":authority", "service-name", "x-envoy-auth-failure-mode-allowed", "true")
	respHeaders := metadata.Pairs("test-header-key", "test-header-value")
	backend := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			gotMD, _ := metadata.FromIncomingContext(stream.Context())
			if err := compareMetadata(gotMD, wantIncomingHeaders); err != nil {
				return status.Errorf(codes.Internal, "Unexpected headers metadata received by the server: %v", err)
			}
			return stream.SendHeader(respHeaders)
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FailureModeAllow:          true,
		FailureModeAllowHeaderAdd: true,
		FilterEnabled: &corepb.RuntimeFractionalPercent{
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

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}
	hdr, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}
	wantRespHeaders := metadata.Pairs("test-header-key", "test-header-value")
	if err := compareMetadata(hdr, wantRespHeaders); err != nil {
		t.Fatalf("Response header mismatch: %v", err)
	}
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies both header and response header mutations that are expected to
// succeed. Verifies that the data plane RPC succeeds.
func (s) TestExtAuthz_Allowed_WithRequestAndResponseHeadersMutations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr, stopAuth := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		st := &statuspb.Status{Code: int32(codes.OK)}
		return &v3authpb.CheckResponse{
			Status: st,
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}}, // Allowed.
					},
					ResponseHeadersToAdd: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}, // Allowed binary header.
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
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &corepb.RuntimeFractionalPercent{
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

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	stream, err := client.FullDuplexCall(outgoingCtx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	gotHeaders, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}

	wantRespHeaders := metadata.Pairs(
		"test-trailer-key", "test-trailer-value",
		"k2-bin", "\x00\x01\x02\x03",
	)
	if err := compareMetadata(gotHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Unexpected headers metadata received by the client: %v", err)
	}
}

// Test verifies the scenario where allowed_headers and disallowed_headers are
// configured on the filter. Verifies that the CheckRequest sent to the
// authorization server only includes headers matching allowed_headers and
// excludes headers matching disallowed_headers.
func (s) TestExtAuthz_RequestHeaderFiltering(t *testing.T) {
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

	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		FilterEnabled: &corepb.RuntimeFractionalPercent{
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

	cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"allow-header-1", "val1",
		"exact-header", "val2",
		"allow-disallowed-header", "val3",
		"random-header", "val4",
	))

	if _, err := client.EmptyCall(outgoingCtx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Test verifies that client-side metrics are emitted correctly during RPC
// execution across different authorization outcomes.
func (s) TestExtAuthz_ClientMetrics(t *testing.T) {
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	tests := []struct {
		name                       string
		checkFunc                  func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)
		filterEnabled              *corepb.RuntimeFractionalPercent
		decoderHeaderMutationRules *mutationpb.HeaderMutationRules
		failureModeAllow           bool
		wantMetric                 string
		wantNotMetric              string
	}{
		{
			name: "Allowed_RPCs",
			filterEnabled: &corepb.RuntimeFractionalPercent{
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
			wantMetric: "grpc.client_ext_authz.allowed_rpcs",
		},
		{
			name: "Denied_RPCs",
			filterEnabled: &corepb.RuntimeFractionalPercent{
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
			wantMetric: "grpc.client_ext_authz.denied_rpcs",
		},
		{
			name: "FilterDisabled_RPCs",
			filterEnabled: &corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   0,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			wantMetric: "grpc.client_ext_authz.filter_disabled_rpcs",
		},
		{
			name: "Failed_RPCs",
			filterEnabled: &corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   100,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return nil, status.Error(codes.Internal, "internal server error")
			},
			wantMetric: "grpc.client_ext_authz.failed_rpcs",
		},
		{
			name: "Failed_RPCs_HeaderMutationFailed",
			filterEnabled: &corepb.RuntimeFractionalPercent{
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
							Headers: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			wantMetric:    "grpc.client_ext_authz.failed_rpcs",
			wantNotMetric: "grpc.client_ext_authz.allowed_rpcs",
		},
		{
			name: "Failed_RPCs_HeaderMutationFailed_FailureModeAllow",
			filterEnabled: &corepb.RuntimeFractionalPercent{
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
							Headers: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}},
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
			wantMetric:       "grpc.client_ext_authz.failed_rpcs",
			wantNotMetric:    "grpc.client_ext_authz.allowed_rpcs",
		},
		{
			name: "Failed_RPCs_ResponseHeaderMutationFailed",
			filterEnabled: &corepb.RuntimeFractionalPercent{
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
							ResponseHeadersToAdd: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			wantMetric:    "grpc.client_ext_authz.failed_rpcs",
			wantNotMetric: "grpc.client_ext_authz.allowed_rpcs",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authAddr, stop := startTestAuthServer(t, test.checkFunc)
			defer stop()

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FilterEnabled:              test.filterEnabled,
				DecoderHeaderMutationRules: test.decoderHeaderMutationRules,
				FailureModeAllow:           test.failureModeAllow,
			}

			tmr := teststats.NewTestMetricsRecorder()
			cc, err := setupTestClient(t, authAddr, extAuthzCfg, backend.Address, grpc.WithStatsHandler(tmr))
			if err != nil {
				t.Fatalf("setupTestClient() failed: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client.EmptyCall(ctx, &testpb.Empty{})

			wantData := teststats.MetricsData{
				Handle:    estats.DescriptorForMetric(test.wantMetric),
				IntIncr:   1,
				LabelKeys: []string{"grpc.target"},
				LabelVals: []string{"xds:///service-name"},
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
}
