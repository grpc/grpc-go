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

package extproc_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	stats "google.golang.org/grpc/internal/testutils/stats"
	"google.golang.org/grpc/internal/testutils/xds/e2e"

	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	_ "google.golang.org/grpc/internal/xds/httpfilter/extproc"
	iextproc "google.golang.org/grpc/internal/xds/httpfilter/extproc/internal"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3procfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3procservicepb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

func parseGRPCServiceConfigForTesting(gs *v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
	if gs == nil {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("nil GrpcService")
	}
	gg := gs.GetGoogleGrpc()
	if gg == nil {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("only GoogleGrpc is supported in GrpcService")
	}
	target := gg.GetTargetUri()
	if target == "" {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("empty target_uri in GoogleGrpc")
	}
	return xdsresource.GRPCServiceConfig{
		TargetURI: target,
	}, nil
}

func createExtProcChannelForTesting(cfg xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
	cc, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return cc, cc.Close, nil
}

type mockProcessorServer struct {
	v3procservicepb.UnimplementedExternalProcessorServer
	processFunc func(v3procservicepb.ExternalProcessor_ProcessServer) error
}

func (s *mockProcessorServer) Process(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
	if s.processFunc != nil {
		return s.processFunc(stream)
	}
	return nil
}

// startMockProcessor configures extproc environment variables and function
// hooks, starts a mock external processor server, and registers cleanup.
func startMockProcessor(t *testing.T, processFunc func(v3procservicepb.ExternalProcessor_ProcessServer) error, opts ...grpc.ServerOption) string {
	t.Helper()

	origParse := iextproc.ParseGRPCServiceConfig
	origCreate := iextproc.CreateExtProcChannel
	iextproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	iextproc.CreateExtProcChannel = createExtProcChannelForTesting

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	iextproc.RegisterForTesting()

	t.Cleanup(func() {
		iextproc.ParseGRPCServiceConfig = origParse
		iextproc.CreateExtProcChannel = origCreate
		iextproc.UnregisterForTesting()
	})

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer(opts...)
	mockProc := &mockProcessorServer{processFunc: processFunc}
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)

	t.Cleanup(extprocServer.Stop)

	return lis.Addr().String()
}

// setupTestClient configures the management server with xDS resources that
// include the external processor filter, and creates a new gRPC client.
func setupTestClient(t *testing.T, extProcAddr string, extProcConfig *v3procfilterpb.ExternalProcessor, serverAddr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	t.Helper()
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	const serviceName = "test-service"

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

	extProcConfig.GrpcService = &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri: extProcAddr,
			},
		},
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", extProcConfig)},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		return nil, err
	}

	dialOpts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver)}, opts...)
	cc, err := grpc.NewClient("xds:///"+serviceName, dialOpts...)
	return cc, err
}

// TestObservabilityAllSendUnary tests the scenario where the ExtProc filter is
// configured with all processing modes set to SEND/GRPC and ObservabilityMode
// is true. Verifies that the client correctly forwards headers and bodies to
// the processor with ObservabilityMode=true, does not expect any responses
// back, and successfully completes the Unary RPC.
func (s) TestObservabilityAllSendUnary(t *testing.T) {
	procDone := make(chan struct{})
	errCh := make(chan error, 1)
	extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		defer close(procDone)
		gotEvents := make(map[string]bool)
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("stream.Recv() failed: %v", err)
			}
			if !req.ObservabilityMode {
				return fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			}

			switch {
			case req.GetRequestHeaders() != nil:
				gotEvents["RequestHeaders"] = true
			case req.GetRequestBody() != nil:
				body := req.GetRequestBody()
				if body.GetEndOfStreamWithoutMessage() {
					gotEvents["RequestBodyEOF"] = true
				} else {
					gotEvents["RequestBodyMessage"] = true
					reqMsg := &testpb.SimpleRequest{}
					if err := proto.Unmarshal(body.GetBody(), reqMsg); err != nil {
						err = fmt.Errorf("failed to unmarshal body: %v", err)
						errCh <- err
						return err
					} else if string(reqMsg.GetPayload().GetBody()) != "hello-extproc-echo" {
						err = fmt.Errorf("expected body 'hello-extproc-echo', got %s", reqMsg.GetPayload().GetBody())
						errCh <- err
						return err
					}
				}
			case req.GetResponseHeaders() != nil:
				gotEvents["ResponseHeaders"] = true
			case req.GetResponseBody() != nil:
				gotEvents["ResponseBody"] = true
				if len(req.GetResponseBody().GetBody()) == 0 {
					err := fmt.Errorf("len(req.GetResponseBody().GetBody()) = %d, want > 0", len(req.GetResponseBody().GetBody()))
					errCh <- err
					return err
				}
			case req.GetResponseTrailers() != nil:
				gotEvents["ResponseTrailers"] = true
			}
		}
		expected := []string{
			"RequestHeaders",
			"RequestBodyMessage",
			"RequestBodyEOF",
			"ResponseHeaders",
			"ResponseBody",
			"ResponseTrailers",
		}
		for _, k := range expected {
			if !gotEvents[k] {
				err := fmt.Errorf("proc server did not receive %q", k)
				errCh <- err
				return err
			}
		}
		return nil
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			// Just echo the request payload back as the response.
			return &testpb.SimpleResponse{
				Payload: in.GetPayload(),
			}, nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
	}, stub.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Make the Unary call and verify it succeeds and returns the echoed payload.
	reqMsg := &testpb.SimpleRequest{
		Payload: &testpb.Payload{
			Body: []byte("hello-extproc-echo"),
		},
	}
	resp, err := client.UnaryCall(ctx, reqMsg)
	if err != nil {
		t.Fatalf("UnaryCall() failed: %v", err)
	}
	if string(resp.GetPayload().GetBody()) != "hello-extproc-echo" {
		t.Fatalf("UnaryCall() returned payload: %s, want: %s", resp.GetPayload().GetBody(), "hello-extproc-echo")
	}

	select {
	case <-procDone:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for processor to finish receiving messages")
	}

	// Check for any errors reported by the external processor server handler.
	select {
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(defaultTestShortTimeout):
	}
}

// TestObservabilitySkipProcessingModes verifies that when processing modes are
// set to NONE, the ExtProc filter in observability mode suppresses sending those
// respective request messages to the external processor.
func (s) TestObservabilitySkipProcessingModes(t *testing.T) {
	procDone := make(chan struct{})
	errCh := make(chan error, 1)
	extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		defer close(procDone)
		gotEvents := make(map[string]bool)
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				err = fmt.Errorf("stream.Recv() failed: %v", err)
				errCh <- err
				return err
			}
			switch {
			case req.GetRequestHeaders() != nil:
				gotEvents["RequestHeaders"] = true
			case req.GetRequestBody() != nil:
				gotEvents["RequestBody"] = true
			case req.GetResponseHeaders() != nil:
				gotEvents["ResponseHeaders"] = true
			case req.GetResponseTrailers() != nil:
				gotEvents["ResponseTrailers"] = true
			}
		}

		if !gotEvents["RequestHeaders"] {
			err := fmt.Errorf("External proccessor server did not receive RequestHeaders")
			errCh <- err
			return err
		}
		if gotEvents["RequestBody"] || gotEvents["ResponseHeaders"] || gotEvents["ResponseTrailers"] {
			err := fmt.Errorf("External proccessor server received unexpected skipped messages: %+v", gotEvents)
			errCh <- err
			return err
		}
		return nil
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_NONE,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SKIP,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_NONE,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SKIP,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
	}, stub.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	reqMsg := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("hello-skip")}}
	resp, err := client.UnaryCall(ctx, reqMsg)
	if err != nil {
		t.Fatalf("UnaryCall() failed: %v", err)
	}
	if string(resp.GetPayload().GetBody()) != "hello-skip" {
		t.Fatalf("UnaryCall() returned payload: %s, want: %s", resp.GetPayload().GetBody(), "hello-skip")
	}

	select {
	case <-procDone:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timed out waiting for processor to finish receiving messages")
	}

	// Check for any errors reported by the external processor server handler.
	select {
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(defaultTestShortTimeout):
	}
}

// TestAllSendStreaming tests the scenario where the ExtProc filter is
// configured with all processing modes set to SEND/GRPC and ObservabilityMode
// is true, for a bidirectional streaming RPC. Verifies that the client
// correctly forwards headers and bodies to the processor with
// ObservabilityMode=true, does not expect any responses back, and successfully
// completes the streaming RPC.
func (s) TestAllSendStreaming(t *testing.T) {
	procDone := make(chan struct{})
	errCh := make(chan error, 1)
	extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		defer close(procDone)

		req, err := stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if !req.ObservabilityMode {
			err := fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			errCh <- err
			return err
		}
		if req.GetRequestHeaders() == nil {
			err := fmt.Errorf("received unexpected message of type %T, want RequestHeaders", req.Request)
			errCh <- err
			return err
		}

		// RequestBody c1
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if !req.ObservabilityMode {
			err := fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			errCh <- err
			return err
		}
		if req.GetRequestBody() == nil {
			err := fmt.Errorf("received unexpected message of type %T, want RequestBody", req.Request)
			errCh <- err
			return err
		} else {
			reqMsg := &testpb.StreamingOutputCallRequest{}
			if err := proto.Unmarshal(req.GetRequestBody().GetBody(), reqMsg); err != nil {
				errCh <- fmt.Errorf("proto.Unmarshal returned unexpected error %v", err)
				return err
			} else if string(reqMsg.GetPayload().GetBody()) != "c1" {
				err := fmt.Errorf("reqMsg.GetPayload().GetBody() = %q, want %q", reqMsg.GetPayload().GetBody(), "c1")
				errCh <- err
				return err
			}
		}

		// ResponseHeaders
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if !req.ObservabilityMode {
			err := fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			errCh <- err
			return err
		}
		if req.GetResponseHeaders() == nil {
			err := fmt.Errorf("received unexpected message of type %T, want ResponseHeaders", req.Request)
			errCh <- err
			return err
		}

		// ResponseBody c1
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if !req.ObservabilityMode {
			err := fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			errCh <- err
			return err
		}
		if req.GetResponseBody() == nil {
			err := fmt.Errorf("received unexpected message of type %T, want ResponseBody", req.Request)
			errCh <- err
			return err
		} else if len(req.GetResponseBody().GetBody()) == 0 {
			err := fmt.Errorf("len(req.GetResponseBody().GetBody()) = 0, want > 0")
			errCh <- err
			return err
		}

		// RequestBody c2
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if !req.ObservabilityMode {
			err := fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			errCh <- err
			return err
		}
		if req.GetRequestBody() == nil {
			err := fmt.Errorf("received unexpected message of type %T, want RequestBody", req.Request)
			errCh <- err
			return err
		} else {
			reqMsg := &testpb.StreamingOutputCallRequest{}
			if err := proto.Unmarshal(req.GetRequestBody().GetBody(), reqMsg); err != nil {
				errCh <- fmt.Errorf("proto.Unmarshal returned unexpected error %v", err)
				return err
			} else if string(reqMsg.GetPayload().GetBody()) != "c2" {
				err := fmt.Errorf("reqMsg.GetPayload().GetBody() = %q, want %q", reqMsg.GetPayload().GetBody(), "c2")
				errCh <- err
				return err
			}
		}

		// ResponseBody c2
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if !req.ObservabilityMode {
			err := fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			errCh <- err
			return err
		}
		if req.GetResponseBody() == nil {
			err := fmt.Errorf("received unexpected message of type %T, want ResponseBody", req.Request)
			errCh <- err
			return err
		} else if len(req.GetResponseBody().GetBody()) == 0 {
			err := fmt.Errorf("len(req.GetResponseBody().GetBody()) = 0, want > 0")
			errCh <- err
			return err
		}

		// RequestBody EOF
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if !req.ObservabilityMode {
			err := fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			errCh <- err
			return err
		}
		if req.GetRequestBody() == nil {
			err := fmt.Errorf("received unexpected message of type %T, want RequestBody", req.Request)
			errCh <- err
			return err
		} else if !req.GetRequestBody().GetEndOfStreamWithoutMessage() {
			err := fmt.Errorf("req.GetRequestBody().GetEndOfStreamWithoutMessage() = %v, want true", req.GetRequestBody().GetEndOfStreamWithoutMessage())
			errCh <- err
			return err
		}

		// ResponseTrailers
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if !req.ObservabilityMode {
			err := fmt.Errorf("req.ObservabilityMode = %v, want true", req.ObservabilityMode)
			errCh <- err
			return err
		}
		if req.GetResponseTrailers() == nil {
			err := fmt.Errorf("received unexpected message of type %T, want ResponseTrailers", req.Request)
			errCh <- err
			return err
		}

		return nil
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Read client headers
			_, ok := metadata.FromIncomingContext(stream.Context())
			if !ok {
				return status.Error(codes.InvalidArgument, "missing incoming metadata")
			}
			// Send response headers
			if err := stream.SendHeader(metadata.Pairs("x-resp-header-from-server", "present")); err != nil {
				return err
			}
			for {
				req, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: req.GetPayload(),
				}); err != nil {
					return err
				}
			}
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
	}, stub.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	messages := [][]byte{
		[]byte("c1"),
		[]byte("c2"),
	}

	for _, msg := range messages {
		reqMsg := &testpb.StreamingOutputCallRequest{
			Payload: &testpb.Payload{
				Body: msg,
			},
		}
		if err := stream.Send(reqMsg); err != nil {
			t.Fatalf("stream.Send(%q) = %v, want nil", string(msg), err)
		}

		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream.Recv() failed: %v", err)
		}
		if string(resp.GetPayload().GetBody()) != string(msg) {
			t.Fatalf("stream.Recv() returned payload: %s, want: %s", resp.GetPayload().GetBody(), string(msg))
		}
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() = %v, want nil", err)
	}

	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() returned error: %v, want EOF", err)
	}

	// Verify that the external processor finished processing all requests.
	select {
	case <-procDone:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timed out waiting for mock processor server assertions")
	}
	// Check for any errors reported by the external processor server handler.
	select {
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(defaultTestShortTimeout):
	}
}

// TestDeferredCloseTimeout verifies that in observability mode, the client
// delays closing the processor stream by the configured DeferredCloseTimeout
// duration after the data plane RPC has completed.
func (s) TestDeferredCloseTimeout(t *testing.T) {
	var procCancelTime time.Time
	var procCancelErr error
	procDone := make(chan struct{})
	extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		defer close(procDone)
		for {
			_, err := stream.Recv()
			if err != nil {
				<-stream.Context().Done()
				procCancelTime = time.Now()
				procCancelErr = stream.Context().Err()
				return nil
			}
		}
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	const delay = 200 * time.Millisecond
	cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(delay),
	}, stub.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Perform the UnaryCall and verify it succeeds.
	_, err = client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("a")}})
	if err != nil {
		t.Fatalf("UnaryCall failed: %v", err)
	}
	clientDoneTime := time.Now()

	// Wait for the mock processor stream to close.
	select {
	case <-procDone:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timed out waiting for mock processor server to close stream")
	}

	// Verify that the context cancellation was delayed by the configured duration.
	gotDelay := procCancelTime.Sub(clientDoneTime)
	if gotDelay < delay-20*time.Millisecond {
		t.Fatalf("Processor stream context canceled after %v, want at least %v", gotDelay, delay)
	}
	if procCancelErr != context.Canceled {
		t.Fatalf("Processor stream context error is %v, want %v", procCancelErr, context.Canceled)
	}
}

// TestObservabilityMutationsIgnored verifies that in observability mode, any
// mutations returned by the external processor server are completely ignored,
// and the data plane RPC proceeds with the original unmodified headers.
func (s) TestObservabilityMutationsIgnored(t *testing.T) {
	procDone := make(chan struct{})
	errCh := make(chan error, 1)
	extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		defer close(procDone)
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				errCh <- fmt.Errorf("stream.Recv() failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				errCh <- fmt.Errorf("ObservabilityMode = false, want true")
			}
			if req.GetRequestHeaders() != nil {
				// Attempt to mutate a request header.
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_RequestHeaders{
						RequestHeaders: &v3procservicepb.HeadersResponse{
							Response: &v3procservicepb.CommonResponse{
								HeaderMutation: &v3procservicepb.HeaderMutation{
									SetHeaders: []*v3corepb.HeaderValueOption{
										{
											Header: &v3corepb.HeaderValue{
												Key:   "x-req-header-modified",
												Value: "true",
											},
										},
									},
								},
							},
						},
					},
				}
				if err := stream.Send(resp); err != nil {
					errCh <- fmt.Errorf("stream.Send() failed: %v", err)
					return err
				}
			}
		}
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			// Verify that the mutated header was NOT received by the server.
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, status.Error(codes.InvalidArgument, "missing incoming metadata")
			}
			if len(md.Get("x-req-header-modified")) > 0 {
				return nil, status.Error(codes.FailedPrecondition, "observability mode failed: mutated header was received by backend")
			}
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
	}, stub.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Perform the UnaryCall and verify it succeeds.
	_, err = client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("a")}})
	if err != nil {
		t.Fatalf("UnaryCall failed: %v", err)
	}

	select {
	case <-procDone:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timed out waiting for processor to finish")
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(defaultTestShortTimeout):
	}
}

// TestObservabilityFailureMode verifies client behavior under various external
// processor stream failure scenarios (deny, allow, and graceful EOF).
func (s) TestObservabilityFailureMode(t *testing.T) {
	tests := []struct {
		name             string
		procErr          error
		failureModeAllow bool
		wantCode         codes.Code
	}{
		{
			name:             "deny_on_processor_error",
			procErr:          status.Error(codes.Internal, "processor stream error"),
			failureModeAllow: false,
			wantCode:         codes.Internal,
		},
		{
			name:             "allow_on_processor_error",
			procErr:          status.Error(codes.Internal, "processor stream error"),
			failureModeAllow: true,
			wantCode:         codes.OK,
		},
		{
			name:             "graceful_eof_succeeds_even_when_deny",
			procErr:          nil,
			failureModeAllow: false,
			wantCode:         codes.OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				if tt.procErr != nil {
					return tt.procErr
				}
				_, err := stream.Recv()
				return err
			})

			stub := stubserver.StartTestService(t, &stubserver.StubServer{
				UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
					return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
				},
			})
			defer stub.Stop()

			cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
				},
				ObservabilityMode:    true,
				FailureModeAllow:     tt.failureModeAllow,
				DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
			}, stub.Address)
			if err != nil {
				t.Fatalf("setupTestClient() failed: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			_, err = client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("a")}})
			if status.Code(err) != tt.wantCode {
				t.Fatalf("UnaryCall() status code = %v, want %v", status.Code(err), tt.wantCode)
			}
		})
	}
}

// TestObservabilityRequestAttributesLifecycle verifies that request attributes
// are sent only in the first client message (RequestHeaders) and are not
// populated in subsequent client messages (RequestBody) or any response
// messages (ResponseHeaders, ResponseBody).
func (s) TestObservabilityRequestAttributesLifecycle(t *testing.T) {
	procDone := make(chan struct{})
	errCh := make(chan error, 1)
	extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		defer close(procDone)

		// RequestHeaders (First Client Message)
		req, err := stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if req.GetRequestHeaders() == nil {
			errCh <- fmt.Errorf("received unexpected message of type %T, want RequestHeaders", req.Request)
			return err
		}
		if req.GetAttributes() == nil {
			errCh <- fmt.Errorf("req.GetAttributes() = nil, want non-nil Attributes on first client message")
			return err
		}

		// RequestBody (Second Client Message)
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if req.GetRequestBody() == nil {
			errCh <- fmt.Errorf("received unexpected message of type %T, want RequestBody", req.Request)
			return err
		}
		if req.GetAttributes() != nil {
			errCh <- fmt.Errorf("req.GetAttributes() = %+v, want nil Attributes on subsequent client message", req.GetAttributes())
			return err
		}

		// ResponseHeaders (First Response Message)
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if req.GetResponseHeaders() == nil {
			errCh <- fmt.Errorf("received unexpected message of type %T, want ResponseHeaders", req.Request)
			return err
		}
		if req.GetAttributes() != nil {
			errCh <- fmt.Errorf("req.GetAttributes() = %+v, want nil Attributes on response message", req.GetAttributes())
			return err
		}

		// ResponseBody (Second Response Message)
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if req.GetResponseBody() == nil {
			errCh <- fmt.Errorf("received unexpected message of type %T, want ResponseBody", req.Request)
			return err
		}
		if req.GetAttributes() != nil {
			errCh <- fmt.Errorf("req.GetAttributes() = %+v, want nil Attributes on response message", req.GetAttributes())
			return err
		}

		// RequestBody EOF (Third Client Message)
		req, err = stream.Recv()
		if err != nil {
			errCh <- fmt.Errorf("stream.Recv returned unexpected error %v", err)
			return err
		}
		if req.GetRequestBody() == nil {
			errCh <- fmt.Errorf("received unexpected message of type %T, want RequestBody", req.Request)
			return err
		} else if !req.GetRequestBody().GetEndOfStreamWithoutMessage() {
			errCh <- fmt.Errorf("req.GetRequestBody().GetEndOfStreamWithoutMessage() = %v, want true", req.GetRequestBody().GetEndOfStreamWithoutMessage())
			return err
		}
		if req.GetAttributes() != nil {
			errCh <- fmt.Errorf("req.GetAttributes() = %+v, want nil Attributes on subsequent client message", req.GetAttributes())
			return err
		}

		return nil
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			if _, ok := metadata.FromIncomingContext(stream.Context()); !ok {
				return status.Error(codes.InvalidArgument, "missing incoming metadata")
			}
			if err := stream.SendHeader(metadata.Pairs("x-resp-header-from-server", "present")); err != nil {
				return err
			}
			for {
				in, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: in.GetPayload().GetBody()},
				}); err != nil {
					return err
				}
			}
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
		RequestAttributes: []string{"request.path"},
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
	}, stub.Address)
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Make the FullDuplexCall and verify it succeeds and returns the echoed payloads.
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	reqMsg := &testpb.StreamingOutputCallRequest{
		Payload: &testpb.Payload{Body: []byte("c1")},
	}
	if err := stream.Send(reqMsg); err != nil {
		t.Fatalf("stream.Send failed: %v", err)
	}

	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv failed: %v", err)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend failed: %v", err)
	}

	// Verify that the external processor finished processing all requests and assertions passed.
	select {
	case <-procDone:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timed out waiting for mock processor server assertions")
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(defaultTestShortTimeout):
	}
}

// TestObservabilityMetricsUnary tests the client-side ext_proc metrics for a
// Unary RPC. It verifies that all 4 duration metrics are recorded with the
// correct labels and that their values are positive.
func (s) TestObservabilityMetricsUnary(t *testing.T) {
	procDone := make(chan struct{})
	errCh := make(chan error, 1)
	extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		defer close(procDone)
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				errCh <- fmt.Errorf("stream.Recv() failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				errCh <- fmt.Errorf("ObservabilityMode = false, want true")
			}
		}
		return nil
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	const serviceName = "test-service"
	tmr := stats.NewTestMetricsRecorder()
	grpcTarget := "xds:///" + serviceName
	cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
	}, stub.Address, grpc.WithStatsHandler(tmr))
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	reqMsg := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("hello")}}
	if _, err = client.UnaryCall(ctx, reqMsg); err != nil {
		t.Fatalf("UnaryCall() failed: %v", err)
	}

	select {
	case <-procDone:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timed out waiting for processor to finish")
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(defaultTestShortTimeout):
	}

	// Verify values in the map.
	if got, _ := tmr.Metric("grpc.client_ext_proc.client_headers_duration"); got <= 0 {
		t.Fatalf("Unexpected data for metric %v, got: %v, want: > 0", "grpc.client_ext_proc.client_headers_duration", got)
	}
	if got, _ := tmr.Metric("grpc.client_ext_proc.server_headers_duration"); got <= 0 {
		t.Fatalf("Unexpected data for metric %v, got: %v, want: > 0", "grpc.client_ext_proc.server_headers_duration", got)
	}
	if got, _ := tmr.Metric("grpc.client_ext_proc.client_half_close_duration"); got <= 0 {
		t.Fatalf("Unexpected data for metric %v, got: %v, want: > 0", "grpc.client_ext_proc.client_half_close_duration", got)
	}
	if got, _ := tmr.Metric("grpc.client_ext_proc.server_trailers_duration"); got <= 0 {
		t.Fatalf("Unexpected data for metric %v, got: %v, want: > 0", "grpc.client_ext_proc.server_trailers_duration", got)
	}

	// Verify labels for the last metric (server_trailers_duration) from the
	// channel. Since it is recorded last, it should be the one remaining in the
	// channel.
	md, err := tmr.ReadFloat64Histo(ctx)
	if err != nil {
		t.Fatalf("Failed to read last metric from channel: %v", err)
	}
	verifyMetric(t, md, "grpc.client_ext_proc.server_trailers_duration", grpcTarget, "cluster-"+serviceName)
}

// TestObservabilityMetricsStreaming tests the client-side ext_proc metrics for
// a Streaming RPC. It verifies that all 4 duration metrics are recorded
// synchronously step-by-step with the correct labels and that their values are
// positive.
func (s) TestObservabilityMetricsStreaming(t *testing.T) {
	procDone := make(chan struct{})
	errCh := make(chan error, 1)
	extProcAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		defer close(procDone)
		for {
			req, err := stream.Recv()
			if err == io.EOF || status.Code(err) == codes.Canceled {
				break
			}
			if err != nil {
				errCh <- fmt.Errorf("stream.Recv() failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				errCh <- fmt.Errorf("ObservabilityMode = false, want true")
			}
		}
		return nil
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			if err := stream.SendHeader(metadata.Pairs("x-resp-header-from-server", "present")); err != nil {
				return err
			}
			for {
				in, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: in.GetPayload().GetBody()},
				}); err != nil {
					return err
				}
			}
		},
	})
	defer stub.Stop()

	const serviceName = "test-service"
	tmr := stats.NewTestMetricsRecorder()
	grpcTarget := "xds:///" + serviceName
	cc, err := setupTestClient(t, extProcAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
	}, stub.Address, grpc.WithStatsHandler(tmr))
	if err != nil {
		t.Fatalf("setupTestClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	// Verify client_headers_duration (recorded during NewStream).
	md, err := tmr.ReadFloat64Histo(ctx)
	if err != nil {
		t.Fatalf("Failed to read client_headers_duration: %v", err)
	}
	verifyMetric(t, md, "grpc.client_ext_proc.client_headers_duration", grpcTarget, "cluster-"+serviceName)

	// Send one message and receive reply.
	reqMsg := &testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("hello")}}
	if err := stream.Send(reqMsg); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}
	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}

	// Verify server_headers_duration (recorded during first Recv).
	md, err = tmr.ReadFloat64Histo(ctx)
	if err != nil {
		t.Fatalf("Failed to read server_headers_duration: %v", err)
	}
	verifyMetric(t, md, "grpc.client_ext_proc.server_headers_duration", grpcTarget, "cluster-"+serviceName)

	// Close send.
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}

	// Verify client_half_close_duration (recorded during CloseSend).
	md, err = tmr.ReadFloat64Histo(ctx)
	if err != nil {
		t.Fatalf("Failed to read client_half_close_duration: %v", err)
	}
	verifyMetric(t, md, "grpc.client_ext_proc.client_half_close_duration", grpcTarget, "cluster-"+serviceName)

	// Receive EOF.
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() returned error: %v, want EOF", err)
	}

	select {
	case <-procDone:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timed out waiting for processor to finish")
	}

	select {
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(defaultTestShortTimeout):
	}

	// Verify server_trailers_duration (recorded when Recv returns EOF).
	md, err = tmr.ReadFloat64Histo(ctx)
	if err != nil {
		t.Fatalf("Failed to read server_trailers_duration: %v", err)
	}
	verifyMetric(t, md, "grpc.client_ext_proc.server_trailers_duration", grpcTarget, "cluster-"+serviceName)
}

// TestObservabilityTrailersOnly verifies that when the backend service returns
// a trailers-only response (metadata in trailers without sending headers or body),
// the ExtProc filter sends ResponseHeaders with EndOfStream=true containing the
// trailer metadata.
func (s) TestObservabilityTrailersOnly(t *testing.T) {
	receivedHeadersCh := make(chan *v3procservicepb.ProcessingRequest, 1)
	errCh := make(chan error, 1)

	lisAddr := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if req.GetResponseHeaders() != nil {
			respHeaders := req.GetResponseHeaders()
			if !respHeaders.GetEndOfStream() {
				err := fmt.Errorf("Expected EndOfStream to be true for Trailers-Only response headers")
				errCh <- err
				return err
			}
			receivedHeadersCh <- req
		}
		return nil
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Wait for client request message before failing.
			_, err := stream.Recv()
			if err != nil {
				return err
			}
			// Return abort error immediately to trigger Trailers-Only
			return status.Error(codes.Aborted, "intentional backend failure")
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:  v3procfilterpb.ProcessingMode_SKIP,
			ResponseHeaderMode: v3procfilterpb.ProcessingMode_SEND,
		},
		ObservabilityMode:    true,
		DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
	}, stub.Address)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	// Send request message c1 to trigger the server handler.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c1")}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	// Recv should return Aborted (trailers-only)
	_, err = stream.Recv()
	if status.Code(err) != codes.Aborted {
		t.Fatalf("stream.Recv() returned error: %v, want Aborted", err)
	}

	select {
	case <-receivedHeadersCh:
	case err := <-errCh:
		t.Fatalf("Processing server received unexpected response header: %v", err)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for processing server to receive response headers")
	}

	// Verify that calling Header() on a trailers-only stream returns nil, nil to
	// be consistent with non-extproc streams.
	headerMetadata, err := stream.Header()
	if err != nil || headerMetadata != nil {
		t.Fatalf("stream.Header() = (%v, %v), want (nil, nil)", headerMetadata, err)
	}

	// Verify response trailers were received from backend.
	trailerMetadata := stream.Trailer()
	if trailerMetadata == nil {
		t.Fatalf("stream.Trailer() returned nil, want metadata")
	}
}

// verifyMetric is a helper function that asserts the properties of a recorded
// metric. It verifies the metric name, labels (grpc.target and
// grpc.lb.backend_service), and that the recorded duration is positive.
func verifyMetric(t *testing.T, md stats.MetricsData, expectedName string, expectedTarget string, expectedBackendService string) {
	t.Helper()
	if md.Handle.Name != expectedName {
		t.Fatalf("Got metric %s, want %s", md.Handle.Name, expectedName)
	}
	labels := make(map[string]string)
	for i, k := range md.LabelKeys {
		if i < len(md.LabelVals) {
			labels[k] = md.LabelVals[i]
		}
	}
	if gotTarget := labels["grpc.target"]; gotTarget != expectedTarget {
		t.Fatalf("Metric %s: grpc.target label = %q, want %q", expectedName, gotTarget, expectedTarget)
	}
	if gotBackendService := labels["grpc.lb.backend_service"]; gotBackendService != expectedBackendService {
		t.Fatalf("Metric %s: grpc.lb.backend_service label = %q, want %q", expectedName, gotBackendService, expectedBackendService)
	}
	if md.FloatIncr <= 0 {
		t.Fatalf("Unexpected data for metric %v, got: %v, want: > 0", expectedName, md.FloatIncr)
	}
}
