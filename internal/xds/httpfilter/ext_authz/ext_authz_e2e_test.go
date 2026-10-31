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
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	estats "google.golang.org/grpc/experimental/stats"
	grpcinternal "google.golang.org/grpc/internal"
	iresolver "google.golang.org/grpc/internal/resolver"
	teststats "google.golang.org/grpc/internal/testutils/stats"
	iextauthz "google.golang.org/grpc/internal/xds/httpfilter/ext_authz/internal"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
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

	// insecureCredsTypeURL identifies the insecure channel credentials
	// plugin in a GrpcService proto (gRFC A102).
	insecureCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials"
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
// hooks, starts a test external authorization server, and registers cleanup to
// stop it. It takes checkFunc to handle Check RPCs and returns the server's
// listener address.
func startTestAuthServer(t *testing.T, checkFunc func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)) string {
	t.Helper()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtAuthzEnabled, true)
	iextauthz.RegisterForTesting()

	t.Cleanup(func() {
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

	return lis.Addr().String()
}

// extAuthzHTTPFilter creates an ext_authz HTTPFilter protobuf message
// configured with the test authorization server address.
func extAuthzHTTPFilter(authServerAddr string, extAuthzConfig *v3extauthzfilterpb.ExtAuthz) *v3httppb.HttpFilter {
	extAuthzConfig.Services = &v3extauthzfilterpb.ExtAuthz_GrpcService{
		GrpcService: &corepb.GrpcService{
			TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
				GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
					TargetUri:                authServerAddr,
					ChannelCredentialsPlugin: []*anypb.Any{{TypeUrl: insecureCredsTypeURL}},
				},
			},
			Timeout:         extAuthzConfig.GetGrpcService().GetTimeout(),
			InitialMetadata: extAuthzConfig.GetGrpcService().GetInitialMetadata(),
		},
	}
	return e2e.HTTPFilter("com.google.grpc.ext_authz", extAuthzConfig)
}

// startTestServiceBackend creates a test service backend stub that implements
// a unary and streaming RPC. If respHeaders is non-nil, both RPC handlers send
// it to the client as response headers.
//
// It returns the newly created test service backend and a couple of channels
// on which it writes incoming metadata received by the unary and streaming
// rpc handlers respectively, for the test to inspect.
func startTestServiceBackend(t *testing.T, respHeaders metadata.MD) (*stubserver.StubServer, chan metadata.MD, chan metadata.MD) {
	t.Helper()

	gotUnaryMD := make(chan metadata.MD, 1)
	gotStreamingMD := make(chan metadata.MD, 1)
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			select {
			case gotUnaryMD <- md:
			case <-ctx.Done():
				t.Error("Timeout when attempting to write incoming metadata to channel")
			}

			if respHeaders != nil {
				if err := grpc.SendHeader(ctx, respHeaders); err != nil {
					return nil, err
				}
			}
			return &testpb.Empty{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			ctx := stream.Context()
			md, _ := metadata.FromIncomingContext(ctx)
			select {
			case gotStreamingMD <- md:
			case <-ctx.Done():
				t.Error("Timeout when attempting to write incoming metadata to channel")
			}

			if respHeaders != nil {
				if err := stream.SendHeader(respHeaders); err != nil {
					return err
				}
			}
			for {
				_, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return nil
				}
				if err != nil {
					return err
				}
			}
		},
	}
	stubserver.StartTestService(t, ss)
	t.Cleanup(ss.Stop)
	return ss, gotUnaryMD, gotStreamingMD
}

// makeUnaryRPC is a helper function to make a unary RPC and verify the
// returned status code, response headers and trailers, and the request headers
// received by the server handler (when headersCh is non-nil). It returns the
// error returned by the RPC, which is nil if the RPC is skipped.
func makeUnaryRPC(ctx context.Context, t *testing.T, cc *grpc.ClientConn, wantStatus codes.Code, wantRespHeaders, wantTrailers metadata.MD, headersCh chan metadata.MD, wantHeaders metadata.MD) error {
	t.Helper()
	var rpcErr error
	t.Run("EmptyCall", func(t *testing.T) {
		// TODO: Remove these skips once the fix for headers and trailers
		// requested via call options (grpc.Header and grpc.Trailer) lands.
		if len(wantTrailers) > 0 {
			t.Skip("Unary RPC trailer/header passing using call options is currently not supported for denied streams")
		}
		if len(wantRespHeaders) > 0 {
			t.Skip("Unary RPC response header mutation with CallOption is currently not supported")
		}
		client := testgrpc.NewTestServiceClient(cc)
		var gotRespHeaders, gotTrailers metadata.MD
		_, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Header(&gotRespHeaders), grpc.Trailer(&gotTrailers))
		rpcErr = err
		if status.Code(err) != wantStatus {
			t.Fatalf("EmptyCall RPC returned status code: %s, want: %s (error: %v)", status.Code(err), wantStatus, err)
		}

		if err := compareMetadata(gotRespHeaders, wantRespHeaders); err != nil {
			t.Fatalf("Unexpected header metadata received by the client: %v", err)
		}

		if err := compareMetadata(gotTrailers, wantTrailers); err != nil {
			t.Fatalf("Unexpected trailer metadata received by the client: %v", err)
		}

		// Compare request headers only for RPCs with an OK status code.
		if status.Code(err) == codes.OK && headersCh != nil {
			select {
			case gotMD := <-headersCh:
				if err := compareMetadata(gotMD, wantHeaders); err != nil {
					t.Fatalf("Unexpected header metadata received by the server handler: %v", err)
				}
			case <-ctx.Done():
				t.Fatalf("Timeout when attempting to read incoming metadata received by the server handler")
			}
		}
	})
	return rpcErr
}

// makeStreamingRPC is a helper function to make a streaming RPC and verify the
// returned status code, response headers and trailers, and the request headers
// received by the server handler (when headersCh is non-nil). It returns the
// error returned by the RPC.
func makeStreamingRPC(ctx context.Context, t *testing.T, cc *grpc.ClientConn, wantStatus codes.Code, wantRespHeaders, wantTrailers metadata.MD, headersCh chan metadata.MD, wantHeaders metadata.MD) error {
	t.Helper()
	var rpcErr error
	t.Run("FullDuplexCall", func(t *testing.T) {
		client := testgrpc.NewTestServiceClient(cc)
		var gotTrailers metadata.MD
		stream, err := client.FullDuplexCall(ctx, grpc.Trailer(&gotTrailers))
		if err != nil {
			rpcErr = err
			if status.Code(err) != wantStatus {
				t.Fatalf("FullDuplexCall RPC failed with status code = %v, want %v (error: %v)", status.Code(err), wantStatus, err)
			}
			return
		}

		// Send one empty message and close the sending side. Ignore the errors
		// from the send and close, and rely on errors returned from recv.
		stream.Send(&testpb.StreamingOutputCallRequest{})
		stream.CloseSend()
		gotRespHeaders, err := stream.Header()
		if err != nil {
			t.Fatalf("stream.Header() failed: %v", err)
		}
		_, err = stream.Recv()
		if errors.Is(err, io.EOF) {
			err = status.Error(codes.OK, "")
		}
		rpcErr = err
		if status.Code(err) != wantStatus {
			t.Fatalf("FullDuplexCall RPC returned error: %v, want: %s", err, wantStatus)
		}

		if err := compareMetadata(gotRespHeaders, wantRespHeaders); err != nil {
			t.Fatalf("Unexpected header metadata received by the client: %v", err)
		}

		if err := compareMetadata(stream.Trailer(), wantTrailers); err != nil {
			t.Fatalf("Unexpected trailer metadata received by the client: %v", err)
		}

		// Compare request headers only for RPCs with an OK status code.
		if status.Code(err) == codes.OK && headersCh != nil {
			select {
			case gotMD := <-headersCh:
				if err := compareMetadata(gotMD, wantHeaders); err != nil {
					t.Fatalf("Unexpected header metadata received by the server handler: %v", err)
				}
			case <-ctx.Done():
				t.Fatalf("Timeout when attempting to read incoming metadata received by the server handler")
			}
		}
	})
	return rpcErr
}

// setupTestClient configures the management server with xDS resources that
// include the ext_authz filter, and creates a new gRPC client that is closed
// when the test completes. It fails the test on any error.
func setupTestClient(t *testing.T, authServerAddr string, extAuthzConfig *v3extauthzfilterpb.ExtAuthz, serverAddr string, opts ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()
	return setupTestClientWithFilters(t, []*v3httppb.HttpFilter{extAuthzHTTPFilter(authServerAddr, extAuthzConfig)}, serverAddr, opts...)
}

// setupTestClientWithFilters is like setupTestClient, but configures the given
// HTTP filters, in order, ahead of the router filter.
func setupTestClientWithFilters(t *testing.T, filters []*v3httppb.HttpFilter, serverAddr string, opts ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()
	mgmtServer, nodeID, _, resolverBuilder := setup.ManagementServerAndResolver(t)

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
		t.Fatalf("Failed to unmarshal apiListener: %v", err)
	}
	hcm.HttpFilters = slices.Concat(filters, hcm.HttpFilters)
	resources.Listeners[0].ApiListener.ApiListener = testutils.MarshalAny(t, hcm)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v", err)
	}

	dopts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder)}, opts...)
	cc, err := grpc.NewClient("xds:///"+serviceName, dopts...)
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	return cc
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
			backend, _, _ := startTestServiceBackend(t, nil)
			var checkCount atomic.Int32
			authAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				checkCount.Add(1)
				return &v3authpb.CheckResponse{Status: &statuspb.Status{Code: int32(codes.OK)}}, nil
			})

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

			cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			makeUnaryRPC(ctx, t, cc, tt.wantCode, nil, nil, nil, nil)
			makeStreamingRPC(ctx, t, cc, tt.wantCode, nil, nil, nil, nil)

			if got := checkCount.Load(); got != 0 {
				t.Fatalf("External authorization server received %d Check RPCs when the filter is not enabled, want 0", got)
			}
		})
	}
}

// Test verifies the scenarios where the call to the external authorization
// server fails, either because the server returns an error or because the
// call exceeds the configured timeout. If failure_mode_allow is false, the
// data plane RPC fails with the status derived from status_on_error.
// Otherwise, the data plane RPC is allowed, and the
// x-envoy-auth-failure-mode-allowed header is added if
// failure_mode_allow_header_add is true.
func (s) TestExtAuthz_AuthzRPC_Failure(t *testing.T) {
	authzFailures := []struct {
		name               string
		checkFunc          func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)
		grpcServiceTimeout *durationpb.Duration
	}{
		{
			name: "AuthzServer_Error",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return nil, status.Error(codes.Internal, "internal server error")
			},
		},
		{
			name: "AuthzServer_Timeout",
			checkFunc: func(ctx context.Context, _ *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				// Wait until context is done to simulate a timeout on the ext_authz RPC.
				<-ctx.Done()
				return nil, ctx.Err()
			},
			grpcServiceTimeout: durationpb.New(defaultTestShortTimeout),
		},
	}
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
	for _, failure := range authzFailures {
		t.Run(failure.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, nil)
					authAddr := startTestAuthServer(t, failure.checkFunc)

					extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
						Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
							GrpcService: &corepb.GrpcService{Timeout: failure.grpcServiceTimeout},
						},
						FailureModeAllow:          tt.failureModeAllow,
						FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
					}
					if tt.statusOnError != 0 {
						extAuthzCfg.StatusOnError = &v3typepb.HttpStatus{
							Code: v3typepb.StatusCode(tt.statusOnError),
						}
					}

					cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

					ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
					defer cancel()

					makeUnaryRPC(ctx, t, cc, tt.wantCode, nil, nil, gotUnaryMD, tt.wantHeaders)
					makeStreamingRPC(ctx, t, cc, tt.wantCode, nil, nil, gotStreamingMD, tt.wantHeaders)
				})
			}
		})
	}
}

// Test verifies cases where the external authorization server denies a data
// plane RPC.
func (s) TestExtAuthz_DeniedResponse(t *testing.T) {
	tests := []struct {
		name                       string
		checkFunc                  func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error)
		decoderHeaderMutationRules *mutationpb.HeaderMutationRules
		failureModeAllow           bool
		failureModeAllowHeaderAdd  bool
		wantStatus                 codes.Code
		wantErrorContains          string
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
			wantStatus:        codes.PermissionDenied,
			wantErrorContains: "custom denial reason",
		},
		{
			name: "WithDeniedResponse_Headers",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "custom-denied-header", Value: "custom-val"}},
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
							Headers: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
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
							Headers: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
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
							Headers: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
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
			backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, nil)
			authAddr := startTestAuthServer(t, tt.checkFunc)

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				DecoderHeaderMutationRules: tt.decoderHeaderMutationRules,
				FailureModeAllow:           tt.failureModeAllow,
				FailureModeAllowHeaderAdd:  tt.failureModeAllowHeaderAdd,
			}

			cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			err := makeUnaryRPC(ctx, t, cc, tt.wantStatus, nil, tt.wantTrailer, gotUnaryMD, tt.wantHeaders)
			if tt.wantErrorContains != "" && !strings.Contains(err.Error(), tt.wantErrorContains) {
				t.Errorf("EmptyCall RPC returned status message %q, want it to contain %q", err.Error(), tt.wantErrorContains)
			}
			err = makeStreamingRPC(ctx, t, cc, tt.wantStatus, nil, tt.wantTrailer, gotStreamingMD, tt.wantHeaders)
			if tt.wantErrorContains != "" && !strings.Contains(err.Error(), tt.wantErrorContains) {
				t.Errorf("FullDuplexCall RPC returned status message %q, want it to contain %q", err.Error(), tt.wantErrorContains)
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
	authAddr := startTestAuthServer(t, func(ctx context.Context, _ *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
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

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, nil)

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
			GrpcService: &corepb.GrpcService{
				InitialMetadata: []*corepb.HeaderValue{
					{Key: "key1", Value: "value1"},
				},
			},
		},
	}

	cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("test-key", "test-value"))
	wantHeaders := metadata.Pairs(":authority", "service-name", "test-key", "test-value")

	makeUnaryRPC(outgoingCtx, t, cc, codes.OK, nil, nil, gotUnaryMD, wantHeaders)
	makeStreamingRPC(outgoingCtx, t, cc, codes.OK, nil, nil, gotStreamingMD, wantHeaders)
}

// Tests verifies the case where the ext_authz server allows the data plane RPC
// and specifies headers to be added and removed from the data plane RPC.
// Verifies that the RPC is sent to the backend with the expected headers and
// that it eventually succeeds.
func (s) TestExtAuthz_Allowed_WithHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
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

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, nil)

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(false), // Disallowed header mutations are silently ignored.
		},
	}

	cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))
	wantHeaders := metadata.Pairs(":authority", "service-name", "k1", "v1", "k2-bin", "\x00\x01\x02\x03")

	makeUnaryRPC(outgoingCtx, t, cc, codes.OK, nil, nil, gotUnaryMD, wantHeaders)
	makeStreamingRPC(outgoingCtx, t, cc, codes.OK, nil, nil, gotStreamingMD, wantHeaders)
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies headers to be added and removed from the data plane RPC. One
// of the specified header mutations is not allowed by the configuration.
// Verifies that the RPC is failed with error code PermissionDenied.
func (s) TestExtAuthz_Allowed_WithHeaders_MutationFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start a test ext_authz server that allows the data plane RPC.
	authAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
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

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, nil)

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
			DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
			DisallowIsError:    wrapperspb.Bool(true), // Disallowed header mutations result in RPC failures.
		},
	}

	cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("k-test-header-to-be-removed", "true"))

	makeUnaryRPC(outgoingCtx, t, cc, codes.PermissionDenied, nil, nil, gotUnaryMD, nil)
	makeStreamingRPC(outgoingCtx, t, cc, codes.PermissionDenied, nil, nil, gotStreamingMD, nil)
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
		responseHeadersToAdd      []*corepb.HeaderValueOption
		outgoingHeaders           metadata.MD
		wantHeaders               metadata.MD
	}{
		{
			name:                      "OnlyHeaderAddFails_HeaderAdd_False",
			failureModeAllowHeaderAdd: false,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"k-test-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("k-test-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name", "k-test-header-to-be-removed", "true"),
		},
		{
			name:                      "OnlyHeaderAddFails_HeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"k-test-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("k-test-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name", "k-test-header-to-be-removed", "true", "x-envoy-auth-failure-mode-allowed", "true"),
		},
		{
			name:                      "OnlyHeaderRemoveFails_HeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              allowedHeaders,
			headersToRemove:           []string{"a2-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("a2-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name", "a2-header-to-be-removed", "true", "x-envoy-auth-failure-mode-allowed", "true"),
		},
		{
			name:                      "BothMutationsFail_HeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              append(allowedHeaders, disallowedHeader),
			headersToRemove:           []string{"a2-header-to-be-removed"},
			outgoingHeaders:           metadata.Pairs("a2-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name", "a2-header-to-be-removed", "true", "x-envoy-auth-failure-mode-allowed", "true"),
		},
		{
			name:                      "OnlyResponseHeaderAddFails_HeaderAdd_True",
			failureModeAllowHeaderAdd: true,
			headersToAdd:              allowedHeaders,
			headersToRemove:           []string{"k-test-header-to-be-removed"},
			responseHeadersToAdd:      []*corepb.HeaderValueOption{disallowedHeader},
			outgoingHeaders:           metadata.Pairs("k-test-header-to-be-removed", "true"),
			wantHeaders:               metadata.Pairs(":authority", "service-name", "k-test-header-to-be-removed", "true", "x-envoy-auth-failure-mode-allowed", "true"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			// Start a test ext_authz server that allows the data plane RPC.
			authAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				st := &statuspb.Status{Code: int32(codes.OK)}
				return &v3authpb.CheckResponse{
					Status: st,
					HttpResponse: &v3authpb.CheckResponse_OkResponse{
						OkResponse: &v3authpb.OkHttpResponse{
							Headers:              tt.headersToAdd,
							HeadersToRemove:      tt.headersToRemove,
							ResponseHeadersToAdd: tt.responseHeadersToAdd,
						},
					},
				}, nil
			})

			backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, nil)

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FailureModeAllow:          true,
				FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
				DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
					AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
					DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a[0-9].*"},
					DisallowIsError:    wrapperspb.Bool(true), // Disallowed header mutations result in RPC failures.
				},
			}

			cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

			outgoingCtx := metadata.NewOutgoingContext(ctx, tt.outgoingHeaders)
			makeUnaryRPC(outgoingCtx, t, cc, codes.OK, nil, nil, gotUnaryMD, tt.wantHeaders)
			makeStreamingRPC(outgoingCtx, t, cc, codes.OK, nil, nil, gotStreamingMD, tt.wantHeaders)
		})
	}
}

// Test verifies the cases where the ext_authz server allows the data plane RPC
// and specifies response headers to be added to it. Verifies the status of the
// data plane RPCs, the request headers received by the backend, and the
// response headers received by the client.
func (s) TestExtAuthz_Allowed_WithResponseHeaders(t *testing.T) {
	allowedHeader := &corepb.HeaderValueOption{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}}
	allowedBinaryHeader := &corepb.HeaderValueOption{Header: &corepb.HeaderValue{Key: "k2-bin", Value: "v2", RawValue: []byte{0, 1, 2, 3}}}
	disallowedHeader := &corepb.HeaderValueOption{Header: &corepb.HeaderValue{Key: "a1", Value: "v1"}}

	tests := []struct {
		name                      string
		headersToAdd              []*corepb.HeaderValueOption
		headersToRemove           []string
		responseHeadersToAdd      []*corepb.HeaderValueOption
		disallowIsError           bool
		failureModeAllow          bool
		failureModeAllowHeaderAdd bool
		outgoingHeaders           metadata.MD
		backendRespHeaders        metadata.MD
		wantStatus                codes.Code
		wantHeaders               metadata.MD
		wantRespHeaders           metadata.MD
	}{
		{
			// Allowed response headers are appended to the response headers
			// sent by the backend, and the disallowed one is silently ignored.
			name:                 "MutationFails_IsDisallowedHeader_False",
			headersToRemove:      []string{"k-test-header-to-be-removed"},
			responseHeadersToAdd: []*corepb.HeaderValueOption{allowedHeader, allowedBinaryHeader, disallowedHeader},
			disallowIsError:      false, // Disallowed header mutations are silently ignored.
			outgoingHeaders:      metadata.Pairs("k-test-header-to-be-removed", "true"),
			backendRespHeaders:   metadata.Pairs("test-trailer-key", "test-trailer-value", "k1", "test-trailer-v1"),
			wantStatus:           codes.OK,
			wantHeaders:          metadata.Pairs(":authority", "service-name"),
			wantRespHeaders: metadata.Pairs(
				"test-trailer-key", "test-trailer-value",
				"k1", "test-trailer-v1",
				"k1", "v1",
				"k2-bin", "\x00\x01\x02\x03",
			),
		},
		{
			// One of the response header mutations is not allowed by the
			// configuration, and the data plane RPC fails with
			// PermissionDenied.
			name:                 "MutationFails",
			headersToRemove:      []string{"k-test-header-to-be-removed"},
			responseHeadersToAdd: []*corepb.HeaderValueOption{allowedHeader, allowedBinaryHeader, disallowedHeader},
			disallowIsError:      true, // Disallowed header mutations result in RPC failures.
			outgoingHeaders:      metadata.Pairs("k-test-header-to-be-removed", "true"),
			backendRespHeaders:   metadata.Pairs("test-trailer-key", "test-trailer-value"),
			wantStatus:           codes.PermissionDenied,
		},
		{
			// The response header mutation fails validation, but
			// failure_mode_allow is true. The data plane RPC succeeds with the
			// failure mode header added to the request headers, and the
			// invalid response header is not added.
			name:                      "MutationFails_FailureModeAllow",
			responseHeadersToAdd:      []*corepb.HeaderValueOption{disallowedHeader},
			disallowIsError:           true,
			failureModeAllow:          true,
			failureModeAllowHeaderAdd: true,
			backendRespHeaders:        metadata.Pairs("test-header-key", "test-header-value"),
			wantStatus:                codes.OK,
			wantHeaders:               metadata.Pairs(":authority", "service-name", "x-envoy-auth-failure-mode-allowed", "true"),
			wantRespHeaders:           metadata.Pairs("test-header-key", "test-header-value"),
		},
		{
			// Both request and response header mutations are allowed and
			// applied to the data plane RPC.
			name:                 "MutationsSucceed",
			headersToAdd:         []*corepb.HeaderValueOption{allowedHeader},
			headersToRemove:      []string{"k-test-header-to-be-removed"},
			responseHeadersToAdd: []*corepb.HeaderValueOption{allowedBinaryHeader},
			disallowIsError:      true, // Disallowed header mutations result in RPC failures.
			outgoingHeaders:      metadata.Pairs("k-test-header-to-be-removed", "true"),
			backendRespHeaders:   metadata.Pairs("test-trailer-key", "test-trailer-value"),
			wantStatus:           codes.OK,
			wantHeaders:          metadata.Pairs(":authority", "service-name", "k1", "v1"),
			wantRespHeaders: metadata.Pairs(
				"test-trailer-key", "test-trailer-value",
				"k2-bin", "\x00\x01\x02\x03",
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			// Start a test ext_authz server that allows the data plane RPC.
			authAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.OK)},
					HttpResponse: &v3authpb.CheckResponse_OkResponse{
						OkResponse: &v3authpb.OkHttpResponse{
							Headers:              tt.headersToAdd,
							HeadersToRemove:      tt.headersToRemove,
							ResponseHeadersToAdd: tt.responseHeadersToAdd,
						},
					},
				}, nil
			})

			backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, tt.backendRespHeaders)

			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				FailureModeAllow:          tt.failureModeAllow,
				FailureModeAllowHeaderAdd: tt.failureModeAllowHeaderAdd,
				DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
					AllowExpression:    &matcherpb.RegexMatcher{Regex: "^k.*"},
					DisallowExpression: &matcherpb.RegexMatcher{Regex: "^a1$"},
					DisallowIsError:    wrapperspb.Bool(tt.disallowIsError),
				},
			}

			cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

			outgoingCtx := metadata.NewOutgoingContext(ctx, tt.outgoingHeaders)
			makeUnaryRPC(outgoingCtx, t, cc, tt.wantStatus, tt.wantRespHeaders, nil, gotUnaryMD, tt.wantHeaders)
			makeStreamingRPC(outgoingCtx, t, cc, tt.wantStatus, tt.wantRespHeaders, nil, gotStreamingMD, tt.wantHeaders)
		})
	}
}

// Test verifies the case where the ext_authz server allows the data plane RPC
// and specifies response headers to add, but the backend sends a Trailers-Only
// response (no response headers). Verifies that response headers are empty/nil
// and response header mutations are safely ignored without error.
func (s) TestExtAuthz_Allowed_TrailersOnlyResponse_HeaderMutationSkipped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	authAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
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

	// Backend returns without sending initial headers (trailers-only response).
	backend := &stubserver.StubServer{
		FullDuplexCallF: func(testgrpc.TestService_FullDuplexCallServer) error {
			return nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{}

	cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

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

// Test verifies the scenario where allowed_headers and disallowed_headers are
// configured on the filter. Verifies that the CheckRequest sent to the
// authorization server only includes headers matching allowed_headers and
// excludes headers matching disallowed_headers.
func (s) TestExtAuthz_RequestHeaderFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	authAddr := startTestAuthServer(t, func(_ context.Context, req *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
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

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, nil)

	extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
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

	cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address)

	outgoingCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"allow-header-1", "val1",
		"exact-header", "val2",
		"allow-disallowed-header", "val3",
		"random-header", "val4",
	))
	wantHeaders := metadata.Pairs(
		":authority", "service-name",
		"allow-header-1", "val1",
		"exact-header", "val2",
		"allow-disallowed-header", "val3",
		"random-header", "val4",
	)

	makeUnaryRPC(outgoingCtx, t, cc, codes.OK, nil, nil, gotUnaryMD, wantHeaders)
	makeStreamingRPC(outgoingCtx, t, cc, codes.OK, nil, nil, gotStreamingMD, wantHeaders)
}

// Test verifies that client-side metrics are emitted correctly during RPC
// execution across different authorization outcomes.
func (s) TestExtAuthz_ClientMetrics(t *testing.T) {
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
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.OK)},
				}, nil
			},
			wantMetric: "grpc.client_ext_authz.allowed_rpcs",
		},
		{
			name: "Denied_RPCs",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
				}, nil
			},
			wantMetric: "grpc.client_ext_authz.denied_rpcs",
		},
		{
			name: "Failed_RPCs_DeniedHeaderMutationFailed",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
							},
						},
					},
				}, nil
			},
			decoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
				DisallowExpression: &matcherpb.RegexMatcher{Regex: "^disallowed-header$"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			wantMetric:    "grpc.client_ext_authz.failed_rpcs",
			wantNotMetric: "grpc.client_ext_authz.denied_rpcs",
		},
		{
			name: "Failed_RPCs_DeniedHeaderMutationFailed_FailureModeAllow",
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
					HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
						DeniedResponse: &v3authpb.DeniedHttpResponse{
							Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
							Headers: []*corepb.HeaderValueOption{
								{Header: &corepb.HeaderValue{Key: "disallowed-header", Value: "val"}},
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
			wantMetric:       "grpc.client_ext_authz.failed_rpcs",
			wantNotMetric:    "grpc.client_ext_authz.denied_rpcs",
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
			checkFunc: func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				return nil, status.Error(codes.Internal, "internal server error")
			},
			wantMetric: "grpc.client_ext_authz.failed_rpcs",
		},
		{
			name: "Failed_RPCs_HeaderMutationFailed",
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
			for _, rpcType := range []string{"Unary", "Streaming"} {
				t.Run(rpcType, func(t *testing.T) {
					authAddr := startTestAuthServer(t, test.checkFunc)

					backend, _, _ := startTestServiceBackend(t, nil)

					extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
						FilterEnabled:              test.filterEnabled,
						DecoderHeaderMutationRules: test.decoderHeaderMutationRules,
						FailureModeAllow:           test.failureModeAllow,
					}

					tmr := teststats.NewTestMetricsRecorder()
					cc := setupTestClient(t, authAddr, extAuthzCfg, backend.Address, grpc.WithStatsHandler(tmr))

					client := testgrpc.NewTestServiceClient(cc)
					ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
					defer cancel()

					if rpcType == "Unary" {
						client.EmptyCall(ctx, &testpb.Empty{})
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
						LabelKeys: []string{"grpc.target"},
						LabelVals: []string{"xds:///service-name"},
					}
					// Poll until the specific metric is recorded, then assert all fields
					// with cmp.Diff
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
func (s) TestUntrustedServerAllowedGRPCServices(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	const mutatedHeader = "request-mutated"
	authAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		return &v3authpb.CheckResponse{
			Status: &statuspb.Status{Code: int32(codes.OK)},
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					Headers: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: mutatedHeader, Value: "true"}},
					},
				},
			},
		}, nil
	})

	backend, gotUnaryMD, gotStreamingMD := startTestServiceBackend(t, nil)

	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	nodeID := uuid.New().String()
	bc, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers:             fmt.Appendf(nil, `[{"server_uri": "passthrough:///%s", "channel_creds": [{"type": "insecure"}]}]`, managementServer.Address),
		Node:                fmt.Appendf(nil, `{"id": %q}`, nodeID),
		AllowedGRPCServices: fmt.Appendf(nil, `{%q: {"channel_creds": [{"type": "insecure"}]}}`, authAddr),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap contents: %v", err)
	}
	xdsResolver, err := grpcinternal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver: %v", err)
	}

	const serviceName = "service-name"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err := apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatalf("Failed to unmarshal apiListener: %v", err)
	}
	extAuthzConfig := &v3extauthzfilterpb.ExtAuthz{
		Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
			GrpcService: &corepb.GrpcService{
				TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
						TargetUri: authAddr,
					},
				},
			},
		},
		DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
			AllowExpression: &matcherpb.RegexMatcher{Regex: ".*"},
		},
	}
	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("com.google.grpc.ext_authz", extAuthzConfig),
	}, hcm.HttpFilters...)
	resources.Listeners[0].ApiListener.ApiListener = testutils.MarshalAny(t, hcm)

	updateCtx, updateCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer updateCancel()
	if err := managementServer.Update(updateCtx, resources); err != nil {
		t.Fatalf("managementServer.Update() failed: %v", err)
	}

	dopts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver)}
	cc, err := grpc.NewClient("xds:///"+serviceName, dopts...)
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	wantHeaders := metadata.Pairs(":authority", serviceName, mutatedHeader, "true")
	makeUnaryRPC(ctx, t, cc, codes.OK, nil, nil, gotUnaryMD, wantHeaders)
	makeStreamingRPC(ctx, t, cc, codes.OK, nil, nil, gotStreamingMD, wantHeaders)
}

// Test verifies that the CheckRequest sent to the authorization server contains
// the expected attributes (HTTP method, path, protocol, size, time, headers)
// for both Unary and Streaming RPCs, and that client-side source and
// destination socket addresses are nil per gRFC A92. It also verifies that the
// grpc_service initial_metadata is sent only as Check RPC metadata (and not in
// the header_map), and that data plane headers are sent only in the header_map
// (and not as Check RPC metadata).
func (s) TestExtAuthz_CheckRequestAttributes(t *testing.T) {
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
			var capturedCheckMD atomic.Pointer[metadata.MD]

			authServerAddr := startTestAuthServer(t, func(ctx context.Context, req *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				capturedCheckMD.Store(&md)
				capturedReq.Store(req)
				return &v3authpb.CheckResponse{
					Status: &statuspb.Status{Code: int32(codes.OK)},
					HttpResponse: &v3authpb.CheckResponse_OkResponse{
						OkResponse: &v3authpb.OkHttpResponse{},
					},
				}, nil
			})

			backend, _, _ := startTestServiceBackend(t, nil)

			const initialMDKey, initialMDValue = "initial-md-key", "initial-md-value"
			extAuthzCfg := &v3extauthzfilterpb.ExtAuthz{
				Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
					GrpcService: &corepb.GrpcService{
						InitialMetadata: []*corepb.HeaderValue{
							{Key: initialMDKey, Value: initialMDValue},
						},
					},
				},
			}
			cc := setupTestClient(t, authServerAddr, extAuthzCfg, backend.Address)

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

			// Verify Request HTTP fields.
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
			if reqHTTP.GetSize() != -1 {
				t.Errorf("Size = %v, want -1", reqHTTP.GetSize())
			}
			if attrs.GetRequest().GetTime() == nil {
				t.Error("Request.Time is nil, want non-nil")
			}

			gotCheckHeaders := metadata.MD{}
			for _, h := range reqHTTP.GetHeaderMap().GetHeaders() {
				gotCheckHeaders.Append(h.GetKey(), string(h.GetRawValue()))
			}
			// The grpc_service initial_metadata is metadata for the Check RPC
			// itself and must not leak into the data plane header_map.
			if v := gotCheckHeaders.Get(initialMDKey); len(v) != 0 {
				t.Errorf("CheckRequest header_map contains grpc_service initial_metadata %q: %v", initialMDKey, v)
			}
			wantCheckHeaders := metadata.Pairs(
				"custom-client-header", "client-header-value",
			)
			if err := compareMetadata(gotCheckHeaders, wantCheckHeaders); err != nil {
				t.Errorf("Unexpected headers in CheckRequest: %v", err)
			}

			// Verify the metadata of the Check RPC itself: it must carry the
			// grpc_service initial_metadata, but not the data plane headers
			// (which belong only in the header_map).
			checkMD := *capturedCheckMD.Load()
			if got := checkMD.Get(initialMDKey); len(got) != 1 || got[0] != initialMDValue {
				t.Errorf("Check RPC metadata %q = %v, want [%q]", initialMDKey, got, initialMDValue)
			}
			if got := checkMD.Get("custom-client-header"); len(got) != 0 {
				t.Errorf("Check RPC metadata contains data plane header %q: %v", "custom-client-header", got)
			}

			// Per gRFC A92, client-side ExtAuthz filter runs before
			// subchannel/connection creation, so Source and Destination
			// socket addresses must be nil.
			if src := attrs.GetSource(); src != nil {
				t.Errorf("Source is not nil on client side: %v", src)
			}
			if dst := attrs.GetDestination(); dst != nil {
				t.Errorf("Destination is not nil on client side: %v", dst)
			}
		})
	}
}

// Test verifies that a per-route configuration can disable the ExtAuthz filter
// on a specific route on the client side while other routes remain protected.
func (s) TestExtAuthz_PerRouteOverride(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Authz server denies all requests
	authServerAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		return &v3authpb.CheckResponse{
			Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
			HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
				DeniedResponse: &v3authpb.DeniedHttpResponse{
					Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
				},
			},
		}, nil
	})

	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	backend, _, _ := startTestServiceBackend(t, nil)

	const serviceName = "my-service-ext-authz-route-override"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})

	// Add ExtAuthz filter to HCM on Listener.
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err := apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatalf("Failed to unmarshal apiListener: %v", err)
	}
	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		extAuthzHTTPFilter(authServerAddr, &v3extauthzfilterpb.ExtAuthz{}),
	}, hcm.HttpFilters...)
	resources.Listeners[0].ApiListener.ApiListener = testutils.MarshalAny(t, hcm)

	// Configure Routes on RouteConfiguration.
	clusterName := resources.Clusters[0].Name
	resources.Routes = []*v3routepb.RouteConfiguration{{
		Name: resources.Routes[0].Name,
		VirtualHosts: []*v3routepb.VirtualHost{
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
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
							},
						},
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
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
							},
						},
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
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
							},
						},
					},
					// Route 4: FullDuplexCall without header - protected by ExtAuthz (will be denied)
					{
						Match: &v3routepb.RouteMatch{
							PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/FullDuplexCall"},
						},
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
							},
						},
					},
				},
			},
		},
	}}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	// Protected routes: EmptyCall and FullDuplexCall hit Routes 3 & 4 -> Denied by ExtAuthz
	makeUnaryRPC(ctx, t, cc, codes.PermissionDenied, nil, nil, nil, nil)
	makeStreamingRPC(ctx, t, cc, codes.PermissionDenied, nil, nil, nil, nil)

	// Disabled routes: EmptyCall and FullDuplexCall with header hit Routes 1 & 2 -> Filter disabled on route -> Succeeded
	disabledCtx := metadata.AppendToOutgoingContext(ctx, "x-disable-ext-authz", "true")
	makeUnaryRPC(disabledCtx, t, cc, codes.OK, nil, nil, nil, nil)
	makeStreamingRPC(disabledCtx, t, cc, codes.OK, nil, nil, nil, nil)
}

// Test verifies that when the ExtAuthz filter is disabled at the virtual host
// level, a route level ExtAuthzPerRoute override re-enables the filter on that
// route, even though all fields of ExtAuthzPerRoute are ignored. Routes without
// a route level override inherit the virtual host's disabled override.
func (s) TestExtAuthz_PerRouteOverride_VirtualHostDisablesRouteReEnables(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Authz server denies all requests.
	authServerAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		return &v3authpb.CheckResponse{
			Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
			HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
				DeniedResponse: &v3authpb.DeniedHttpResponse{
					Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
				},
			},
		}, nil
	})

	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	backend, _, _ := startTestServiceBackend(t, nil)

	const serviceName = "my-service-ext-authz-vhost-disable-route-enable"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})

	// Add ExtAuthz filter to HCM on Listener.
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err := apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatalf("Failed to unmarshal apiListener: %v", err)
	}
	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		extAuthzHTTPFilter(authServerAddr, &v3extauthzfilterpb.ExtAuthz{}),
	}, hcm.HttpFilters...)
	resources.Listeners[0].ApiListener.ApiListener = testutils.MarshalAny(t, hcm)

	clusterName := resources.Clusters[0].Name
	resources.Routes = []*v3routepb.RouteConfiguration{{
		Name: resources.Routes[0].Name,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{"*"},
			// ExtAuthz is disabled for the whole virtual host.
			TypedPerFilterConfig: map[string]*anypb.Any{
				"com.google.grpc.ext_authz": testutils.MarshalAny(t, &v3routepb.FilterConfig{Disabled: true}),
			},
			Routes: []*v3routepb.Route{
				// Route 1: RPCs with header - ExtAuthz re-enabled via a
				// FilterConfig wrapper with disabled=false and an (ignored)
				// ExtAuthzPerRoute config.
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
						Headers: []*v3routepb.HeaderMatcher{{
							Name:                 "x-enable-ext-authz",
							HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "true"},
						}},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
						},
					},
					TypedPerFilterConfig: map[string]*anypb.Any{
						"com.google.grpc.ext_authz": testutils.MarshalAny(t, &v3routepb.FilterConfig{
							Disabled: false,
							Config:   testutils.MarshalAny(t, &v3extauthzfilterpb.ExtAuthzPerRoute{}),
						}),
					},
				},
				// Route 2: RPCs without header - no route level override,
				// inherits the virtual host's disabled override.
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
						},
					},
				},
			},
		}},
	}}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	// RPCs without header hit Route 2 -> Filter disabled by virtual host ->
	// Succeeded.
	makeUnaryRPC(ctx, t, cc, codes.OK, nil, nil, nil, nil)
	makeStreamingRPC(ctx, t, cc, codes.OK, nil, nil, nil, nil)

	// RPCs with header hit Route 1 -> Filter re-enabled on route -> Denied by
	// ExtAuthz.
	enabledCtx := metadata.AppendToOutgoingContext(ctx, "x-enable-ext-authz", "true")
	makeUnaryRPC(enabledCtx, t, cc, codes.PermissionDenied, nil, nil, nil, nil)
	makeStreamingRPC(enabledCtx, t, cc, codes.PermissionDenied, nil, nil, nil, nil)
}

// Test verifies that a per-filter config override on a weighted cluster can
// disable the ExtAuthz filter, and can re-enable it when it is disabled at the
// route level, since the weighted cluster override takes precedence over the
// route level override.
func (s) TestExtAuthz_PerRouteOverride_WeightedCluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Authz server denies all requests.
	authServerAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		return &v3authpb.CheckResponse{
			Status: &statuspb.Status{Code: int32(codes.PermissionDenied)},
			HttpResponse: &v3authpb.CheckResponse_DeniedResponse{
				DeniedResponse: &v3authpb.DeniedHttpResponse{
					Status: &v3typepb.HttpStatus{Code: v3typepb.StatusCode_Forbidden},
				},
			},
		}, nil
	})

	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	backend, _, _ := startTestServiceBackend(t, nil)

	const serviceName = "my-service-ext-authz-weighted-cluster-override"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})

	// Add ExtAuthz filter to HCM on Listener.
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err := apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatalf("Failed to unmarshal apiListener: %v", err)
	}
	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		extAuthzHTTPFilter(authServerAddr, &v3extauthzfilterpb.ExtAuthz{}),
	}, hcm.HttpFilters...)
	resources.Listeners[0].ApiListener.ApiListener = testutils.MarshalAny(t, hcm)

	// Each route sends all RPCs to a single weighted cluster, so that the
	// cluster picked for an RPC, and hence the override applied, is
	// deterministic.
	clusterName := resources.Clusters[0].Name
	resources.Routes = []*v3routepb.RouteConfiguration{{
		Name: resources.Routes[0].Name,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{"*"},
			Routes: []*v3routepb.Route{
				// Route 1: RPCs with the x-disable-ext-authz header - ExtAuthz
				// disabled on the weighted cluster.
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
						Headers: []*v3routepb.HeaderMatcher{{
							Name:                 "x-disable-ext-authz",
							HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "true"},
						}},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
								WeightedClusters: &v3routepb.WeightedCluster{
									Clusters: []*v3routepb.WeightedCluster_ClusterWeight{{
										Name:   clusterName,
										Weight: wrapperspb.UInt32(1),
										TypedPerFilterConfig: map[string]*anypb.Any{
											"com.google.grpc.ext_authz": testutils.MarshalAny(t, &v3routepb.FilterConfig{Disabled: true}),
										},
									}},
								},
							},
						},
					},
				},
				// Route 2: RPCs with the x-enable-ext-authz header - ExtAuthz
				// disabled on the route, but re-enabled on the weighted
				// cluster via a FilterConfig wrapper with disabled=false and
				// an (ignored) ExtAuthzPerRoute config.
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
						Headers: []*v3routepb.HeaderMatcher{{
							Name:                 "x-enable-ext-authz",
							HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "true"},
						}},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
								WeightedClusters: &v3routepb.WeightedCluster{
									Clusters: []*v3routepb.WeightedCluster_ClusterWeight{{
										Name:   clusterName,
										Weight: wrapperspb.UInt32(1),
										TypedPerFilterConfig: map[string]*anypb.Any{
											"com.google.grpc.ext_authz": testutils.MarshalAny(t, &v3routepb.FilterConfig{
												Disabled: false,
												Config:   testutils.MarshalAny(t, &v3extauthzfilterpb.ExtAuthzPerRoute{}),
											}),
										},
									}},
								},
							},
						},
					},
					TypedPerFilterConfig: map[string]*anypb.Any{
						"com.google.grpc.ext_authz": testutils.MarshalAny(t, &v3routepb.FilterConfig{Disabled: true}),
					},
				},
			},
		}},
	}}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer cc.Close()

	// RPCs with the x-disable-ext-authz header hit Route 1 -> Filter disabled
	// on the weighted cluster -> Succeeded.
	disabledCtx := metadata.AppendToOutgoingContext(ctx, "x-disable-ext-authz", "true")
	makeUnaryRPC(disabledCtx, t, cc, codes.OK, nil, nil, nil, nil)
	makeStreamingRPC(disabledCtx, t, cc, codes.OK, nil, nil, nil, nil)

	// RPCs with the x-enable-ext-authz header hit Route 2 -> Filter disabled on
	// the route but re-enabled on the weighted cluster -> Denied by ExtAuthz.
	enabledCtx := metadata.AppendToOutgoingContext(ctx, "x-enable-ext-authz", "true")
	makeUnaryRPC(enabledCtx, t, cc, codes.PermissionDenied, nil, nil, nil, nil)
	makeStreamingRPC(enabledCtx, t, cc, codes.PermissionDenied, nil, nil, nil, nil)
}

// cachedHeaderFilterTypeURL is the type URL of cachedHeaderFilter.
const cachedHeaderFilterTypeURL = "type.googleapis.com/grpc.testing.cachedHeaderFilter"

// cachedHeaderFilter is a test HTTP filter whose streams return the same
// header metadata on every call to Header.
type cachedHeaderFilter struct {
	httpfilter.Builder
}

type cachedHeaderFilterConfig struct {
	httpfilter.FilterConfig
}

func (cachedHeaderFilter) TypeURLs() []string { return []string{cachedHeaderFilterTypeURL} }

func (cachedHeaderFilter) ParseFilterConfig(proto.Message, httpfilter.ParseOptions) (httpfilter.FilterConfig, error) {
	return cachedHeaderFilterConfig{}, nil
}

func (cachedHeaderFilter) IsTerminal() bool { return false }

func (f cachedHeaderFilter) BuildClientFilter(httpfilter.ClientFilterOptions) httpfilter.ClientFilter {
	return f
}

func (f cachedHeaderFilter) BuildClientInterceptor(_, _ httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
	return f, nil
}

func (cachedHeaderFilter) NewStream(ctx context.Context, _ iresolver.RPCInfo, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	cs, err := newStream(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &cachedHeaderStream{ClientStream: cs}, nil
}

func (cachedHeaderFilter) Close() {}

// cachedHeaderStream returns the same header metadata on every call to Header.
type cachedHeaderStream struct {
	grpc.ClientStream
	once   sync.Once
	header metadata.MD
	err    error
}

func (hs *cachedHeaderStream) Header() (metadata.MD, error) {
	hs.once.Do(func() { hs.header, hs.err = hs.ClientStream.Header() })
	return hs.header, hs.err
}

// Test verifies that calling Header multiple times on a data plane stream
// returns the same response headers, with the response headers specified by
// the external authorization server added only once, even when the stream
// wrapped by ext_authz returns the same header metadata on every call to
// Header.
func (s) TestExtAuthz_AllowedRPC_WithResponseHeaders_MultipleHeaderCalls(t *testing.T) {
	httpfilter.Register(cachedHeaderFilter{})
	t.Cleanup(func() { httpfilter.UnregisterForTesting(cachedHeaderFilterTypeURL) })

	authAddr := startTestAuthServer(t, func(context.Context, *v3authpb.CheckRequest) (*v3authpb.CheckResponse, error) {
		return &v3authpb.CheckResponse{
			Status: &statuspb.Status{Code: int32(codes.OK)},
			HttpResponse: &v3authpb.CheckResponse_OkResponse{
				OkResponse: &v3authpb.OkHttpResponse{
					ResponseHeadersToAdd: []*corepb.HeaderValueOption{
						{Header: &corepb.HeaderValue{Key: "k1", Value: "v1"}},
					},
				},
			},
		}, nil
	})
	backend, _, _ := startTestServiceBackend(t, metadata.Pairs("k1", "v0"))

	// Configure the cached header filter after ext_authz, so that the stream
	// wrapped by ext_authz returns the same header metadata on every call.
	cc := setupTestClientWithFilters(t, []*v3httppb.HttpFilter{
		extAuthzHTTPFilter(authAddr, &v3extauthzfilterpb.ExtAuthz{}),
		{
			Name: "cached-header-filter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{
				TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{TypeUrl: cachedHeaderFilterTypeURL}),
			},
		},
	}, backend.Address)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	// Send one empty message and close the sending side. Ignore the errors
	// from the send and close, and rely on errors returned from recv.
	stream.Send(&testpb.StreamingOutputCallRequest{})
	stream.CloseSend()

	wantRespHeaders := metadata.Pairs("k1", "v0", "k1", "v1")
	gotRespHeaders, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}
	if err := compareMetadata(gotRespHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Unexpected header metadata received by the client: %v", err)
	}

	// Call Header again and verify that the same response headers are
	// returned, with the response headers specified by the external
	// authorization server added only once.
	gotRespHeaders, err = stream.Header()
	if err != nil {
		t.Fatalf("Second stream.Header() call failed: %v", err)
	}
	if err := compareMetadata(gotRespHeaders, wantRespHeaders); err != nil {
		t.Fatalf("Unexpected header metadata received by the client on the second stream.Header() call: %v", err)
	}

	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream.Recv() returned error: %v, want: %v", err, io.EOF)
	}
}
