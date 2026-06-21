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
	"google.golang.org/grpc/internal/xds/httpfilter/extproc"
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

// TestAllSendUnary tests the scenario where the ExtProc filter is configured
// with all processing modes set to SEND/GRPC and ObservabilityMode is true.
// Verifies that the client correctly forwards headers and bodies to the processor
// with ObservabilityMode=true, does not expect any responses back, and successfully
// completes the Unary RPC.
func (s) TestAllSendUnary(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Start the echo ExtProc server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer()
	receivedRequests := make(chan *v3procservicepb.ProcessingRequest, 10)
	mockProc := &mockProcessorServer{
		processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
			for {
				req, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				receivedRequests <- req
			}
		},
	}
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)
	defer extprocServer.Stop()

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

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
				RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
				ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
				ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
				ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

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

	// Verify that the external processor received all the expected events
	// and they all had ObservabilityMode set to true.
	expectedRequestsCount := 6
	var reqs []*v3procservicepb.ProcessingRequest
	for i := 0; i < expectedRequestsCount; i++ {
		select {
		case req := <-receivedRequests:
			reqs = append(reqs, req)
		case <-time.After(defaultTestTimeout):
			t.Fatalf("Timed out waiting for requests. Received %d requests, expected %d", len(reqs), expectedRequestsCount)
		}
	}

	counts := make(map[string]int)
	for idx, req := range reqs {
		if !req.ObservabilityMode {
			t.Errorf("Request %d: ObservabilityMode = false, want true", idx)
		}

		switch {
		case req.GetRequestHeaders() != nil:
			counts["RequestHeaders"]++
		case req.GetRequestBody() != nil:
			body := req.GetRequestBody()
			if body.GetEndOfStreamWithoutMessage() {
				counts["RequestBodyEOF"]++
			} else {
				counts["RequestBodyMessage"]++
				reqMsg := &testpb.SimpleRequest{}
				if err := proto.Unmarshal(body.GetBody(), reqMsg); err != nil {
					t.Errorf("Request %d: failed to unmarshal body: %v", idx, err)
				} else if string(reqMsg.GetPayload().GetBody()) != "hello-extproc-echo" {
					t.Errorf("Request %d: expected body 'hello-extproc-echo', got %s", idx, reqMsg.GetPayload().GetBody())
				}
			}
		case req.GetResponseHeaders() != nil:
			counts["ResponseHeaders"]++
		case req.GetResponseBody() != nil:
			counts["ResponseBody"]++
			if len(req.GetResponseBody().GetBody()) == 0 {
				t.Errorf("Request %d: expected non-empty response body", idx)
			}
		case req.GetResponseTrailers() != nil:
			counts["ResponseTrailers"]++
		}
	}

	expected := map[string]int{
		"RequestHeaders":     1,
		"RequestBodyMessage": 1,
		"RequestBodyEOF":     1,
		"ResponseHeaders":    1,
		"ResponseBody":       1,
		"ResponseTrailers":   1,
	}

	for k, want := range expected {
		if got := counts[k]; got != want {
			t.Errorf("got %d %s, want %d", got, k, want)
		}
	}
}

// TestAllSendStreaming tests the scenario where the ExtProc filter is
// configured with all processing modes set to SEND/GRPC and ObservabilityMode
// is true, for a bidirectional streaming RPC. Verifies that the client
// correctly forwards headers and bodies to the processor with
// ObservabilityMode=true, does not expect any responses back, and successfully
// completes the streaming RPC.
func (s) TestAllSendStreaming(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Start the echo ExtProc server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer()
	procDone := make(chan struct{})
	mockProc := &mockProcessorServer{
		processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
			defer close(procDone)

			// 1. RequestHeaders
			req, err := stream.Recv()
			if err != nil {
				t.Errorf("1. RequestHeaders: Recv failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				err := fmt.Errorf("1. RequestHeaders: ObservabilityMode = false, want true")
				t.Error(err)
				return err
			}
			if req.GetRequestHeaders() == nil {
				err := fmt.Errorf("1. RequestHeaders: expected RequestHeaders, got %+v", req)
				t.Error(err)
				return err
			}

			// 2. RequestBody "c1"
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("2. RequestBody c1: Recv failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				err := fmt.Errorf("2. RequestBody c1: ObservabilityMode = false, want true")
				t.Error(err)
				return err
			}
			if req.GetRequestBody() == nil {
				err := fmt.Errorf("2. RequestBody c1: expected RequestBody, got %+v", req)
				t.Error(err)
				return err
			} else {
				reqMsg := &testpb.StreamingOutputCallRequest{}
				if err := proto.Unmarshal(req.GetRequestBody().GetBody(), reqMsg); err != nil {
					t.Errorf("2. RequestBody c1: failed to unmarshal: %v", err)
					return err
				} else if string(reqMsg.GetPayload().GetBody()) != "c1" {
					err := fmt.Errorf("2. RequestBody c1: expected body 'c1', got %q", reqMsg.GetPayload().GetBody())
					t.Error(err)
					return err
				}
			}

			// 3. ResponseHeaders
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("3. ResponseHeaders: Recv failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				err := fmt.Errorf("3. ResponseHeaders: ObservabilityMode = false, want true")
				t.Error(err)
				return err
			}
			if req.GetResponseHeaders() == nil {
				err := fmt.Errorf("3. ResponseHeaders: expected ResponseHeaders, got %+v", req)
				t.Error(err)
				return err
			}

			// 4. ResponseBody "c1"
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("4. ResponseBody c1: Recv failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				err := fmt.Errorf("4. ResponseBody c1: ObservabilityMode = false, want true")
				t.Error(err)
				return err
			}
			if req.GetResponseBody() == nil {
				err := fmt.Errorf("4. ResponseBody c1: expected ResponseBody, got %+v", req)
				t.Error(err)
				return err
			} else if len(req.GetResponseBody().GetBody()) == 0 {
				err := fmt.Errorf("4. ResponseBody c1: expected non-empty response body")
				t.Error(err)
				return err
			}

			// 5. RequestBody "c2"
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("5. RequestBody c2: Recv failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				err := fmt.Errorf("5. RequestBody c2: ObservabilityMode = false, want true")
				t.Error(err)
				return err
			}
			if req.GetRequestBody() == nil {
				err := fmt.Errorf("5. RequestBody c2: expected RequestBody, got %+v", req)
				t.Error(err)
				return err
			} else {
				reqMsg := &testpb.StreamingOutputCallRequest{}
				if err := proto.Unmarshal(req.GetRequestBody().GetBody(), reqMsg); err != nil {
					t.Errorf("5. RequestBody c2: failed to unmarshal: %v", err)
					return err
				} else if string(reqMsg.GetPayload().GetBody()) != "c2" {
					err := fmt.Errorf("5. RequestBody c2: expected body 'c2', got %q", reqMsg.GetPayload().GetBody())
					t.Error(err)
					return err
				}
			}

			// 6. ResponseBody "c2"
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("6. ResponseBody c2: Recv failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				err := fmt.Errorf("6. ResponseBody c2: ObservabilityMode = false, want true")
				t.Error(err)
				return err
			}
			if req.GetResponseBody() == nil {
				err := fmt.Errorf("6. ResponseBody c2: expected ResponseBody, got %+v", req)
				t.Error(err)
				return err
			} else if len(req.GetResponseBody().GetBody()) == 0 {
				err := fmt.Errorf("6. ResponseBody c2: expected non-empty response body")
				t.Error(err)
				return err
			}

			// 7. RequestBody EOF
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("7. RequestBody EOF: Recv failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				err := fmt.Errorf("7. RequestBody EOF: ObservabilityMode = false, want true")
				t.Error(err)
				return err
			}
			if req.GetRequestBody() == nil {
				err := fmt.Errorf("7. RequestBody EOF: expected RequestBody, got %+v", req)
				t.Error(err)
				return err
			} else if !req.GetRequestBody().GetEndOfStreamWithoutMessage() {
				err := fmt.Errorf("7. RequestBody EOF: expected EndOfStreamWithoutMessage=true, got %+v", req.GetRequestBody())
				t.Error(err)
				return err
			}

			// 8. ResponseTrailers
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("8. ResponseTrailers: Recv failed: %v", err)
				return err
			}
			if !req.ObservabilityMode {
				err := fmt.Errorf("8. ResponseTrailers: ObservabilityMode = false, want true")
				t.Error(err)
				return err
			}
			if req.GetResponseTrailers() == nil {
				err := fmt.Errorf("8. ResponseTrailers: expected ResponseTrailers, got %+v", req)
				t.Error(err)
				return err
			}

			return nil
		},
	}
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)
	defer extprocServer.Stop()

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
				in, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				// Echo message back to client
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: in.GetPayload().GetBody()},
				}); err != nil {
					return err
				}
			}
		},
	})
	defer stub.Stop()

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
				RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
				ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
				ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
				ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Make the FullDuplexCall and verify it succeeds and returns the echoed payloads.
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
			t.Fatalf("stream.Send(%s) failed: %v", string(msg), err)
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
		t.Fatalf("stream.CloseSend() failed: %v", err)
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
}

// TestDeferredCloseTimeout verifies that in observability mode, the client delays
// closing the processor stream by the configured DeferredCloseTimeout duration
// after the data plane RPC has completed.
func (s) TestDeferredCloseTimeout(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Start the mock ExtProc server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer()
	var procEOFTime time.Time
	procDone := make(chan struct{})
	mockProc := &mockProcessorServer{
		processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
			defer close(procDone)
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					procEOFTime = time.Now()
					return nil
				}
				if err != nil {
					return err
				}
			}
		},
	}
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)
	defer extprocServer.Stop()

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	const delay = 200 * time.Millisecond
	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			DeferredCloseTimeout: durationpb.New(delay),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

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

	// Verify that the close delay is close to the configured duration (allowing a small 20ms buffer).
	gotDelay := procEOFTime.Sub(clientDoneTime)
	if gotDelay < delay-20*time.Millisecond {
		t.Errorf("processor stream closed after %v, want at least %v", gotDelay, delay)
	}
}

// TestObservabilityMutationsIgnored verifies that in observability mode, any mutations
// returned by the external processor server are completely ignored, and the data plane
// RPC proceeds with the original unmodified headers.
func (s) TestObservabilityMutationsIgnored(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Start the mock ExtProc server that returns a header mutation.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer()
	mockProc := &mockProcessorServer{
		processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
			for {
				req, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
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
						return err
					}
				}
			}
		},
	}
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)
	defer extprocServer.Stop()

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

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Perform the UnaryCall and verify it succeeds.
	_, err = client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("a")}})
	if err != nil {
		t.Fatalf("UnaryCall failed: %v", err)
	}
}

// TestObservabilityFailureModeDeny verifies that in observability mode, if the
// processor server fails with a non-io.EOF error and FailureModeAllow is false,
// the client data plane RPC fails with an INTERNAL error.
func (s) TestObservabilityFailureModeDeny(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Create a listener and close it immediately to force connection failures.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	lis.Close()

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			FailureModeAllow:     false,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Perform the UnaryCall and verify it fails with an INTERNAL status code.
	_, err = client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("a")}})
	if err == nil {
		t.Fatal("UnaryCall succeeded, want error")
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("UnaryCall returned error code %v, want %v", status.Code(err), codes.Internal)
	}
}

// TestObservabilityFailureModeAllow verifies that in observability mode, if the
// processor server fails with an error but FailureModeAllow is true, the client
// data plane RPC succeeds normally.
func (s) TestObservabilityFailureModeAllow(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Create a listener and close it immediately to force connection failures.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	lis.Close()

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			FailureModeAllow:     true,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Perform the UnaryCall and verify it succeeds.
	_, err = client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("a")}})
	if err != nil {
		t.Fatalf("UnaryCall failed: %v", err)
	}
}

// TestObservabilityFailureModeEOF verifies that in observability mode, if the
// processor server closes the stream with io.EOF (graceful termination), the
// client data plane RPC succeeds normally even if FailureModeAllow is false.
func (s) TestObservabilityFailureModeEOF(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Start the mock ExtProc server that closes stream immediately after headers.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer()
	mockProc := &mockProcessorServer{
		processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
			// Receive headers and then close stream gracefully.
			if _, err := stream.Recv(); err != nil {
				return err
			}
			return nil // Graceful close (io.EOF on client side)
		},
	}
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)
	defer extprocServer.Stop()

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			FailureModeAllow:     false,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Perform the UnaryCall and verify it succeeds.
	_, err = client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("a")}})
	if err != nil {
		t.Fatalf("UnaryCall failed: %v", err)
	}
}

// TestObservabilityRequestAttributesLifecycle verifies that request attributes are
// sent only in the first client message (RequestHeaders) and are not populated
// in subsequent client messages (RequestBody) or any response messages (ResponseHeaders, ResponseBody).
func (s) TestObservabilityRequestAttributesLifecycle(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	procDone := make(chan struct{})
	mockProc := &mockProcessorServer{
		processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
			defer close(procDone)

			// 1. RequestHeaders (First Client Message)
			req, err := stream.Recv()
			if err != nil {
				t.Errorf("1. RequestHeaders: Recv failed: %v", err)
				return err
			}
			if req.GetRequestHeaders() == nil {
				err := fmt.Errorf("1. RequestHeaders: expected RequestHeaders, got %+v", req)
				t.Error(err)
				return err
			}
			if req.GetAttributes() == nil {
				err := fmt.Errorf("1. RequestHeaders: expected non-nil Attributes on first client message")
				t.Error(err)
				return err
			}

			// 2. RequestBody (Second Client Message)
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("2. RequestBody: Recv failed: %v", err)
				return err
			}
			if req.GetRequestBody() == nil {
				err := fmt.Errorf("2. RequestBody: expected RequestBody, got %+v", req)
				t.Error(err)
				return err
			}
			if req.GetAttributes() != nil {
				err := fmt.Errorf("2. RequestBody: expected nil Attributes on subsequent client message, got %+v", req.GetAttributes())
				t.Error(err)
				return err
			}

			// 3. ResponseHeaders (First Response Message)
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("3. ResponseHeaders: Recv failed: %v", err)
				return err
			}
			if req.GetResponseHeaders() == nil {
				err := fmt.Errorf("3. ResponseHeaders: expected ResponseHeaders, got %+v", req)
				t.Error(err)
				return err
			}
			if req.GetAttributes() != nil {
				err := fmt.Errorf("3. ResponseHeaders: expected nil Attributes on response message, got %+v", req.GetAttributes())
				t.Error(err)
				return err
			}

			// 4. ResponseBody (Second Response Message)
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("4. ResponseBody: Recv failed: %v", err)
				return err
			}
			if req.GetResponseBody() == nil {
				err := fmt.Errorf("4. ResponseBody: expected ResponseBody, got %+v", req)
				t.Error(err)
				return err
			}
			if req.GetAttributes() != nil {
				err := fmt.Errorf("4. ResponseBody: expected nil Attributes on response message, got %+v", req.GetAttributes())
				t.Error(err)
				return err
			}

			// 5. RequestBody EOF (Third Client Message)
			req, err = stream.Recv()
			if err != nil {
				t.Errorf("5. RequestBody EOF: Recv failed: %v", err)
				return err
			}
			if req.GetRequestBody() == nil {
				err := fmt.Errorf("5. RequestBody EOF: expected RequestBody, got %+v", req)
				t.Error(err)
				return err
			} else if !req.GetRequestBody().GetEndOfStreamWithoutMessage() {
				err := fmt.Errorf("5. RequestBody EOF: expected EndOfStreamWithoutMessage=true, got %+v", req.GetRequestBody())
				t.Error(err)
				return err
			}
			if req.GetAttributes() != nil {
				err := fmt.Errorf("5. RequestBody EOF: expected nil Attributes on subsequent client message, got %+v", req.GetAttributes())
				t.Error(err)
				return err
			}

			return nil
		},
	}

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer()
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)
	defer extprocServer.Stop()

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

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			RequestAttributes: []string{"request.path"},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
				RequestBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
				ResponseHeaderMode: v3procfilterpb.ProcessingMode_SEND,
				ResponseBodyMode:   v3procfilterpb.ProcessingMode_GRPC,
			},
			ObservabilityMode:    true,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

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
}

// TestObservabilityMetrics tests the client-side ext_proc metrics for a Unary
// RPC. It verifies that all 4 duration metrics are recorded with the correct
// labels and that their values are positive.
func (s) TestObservabilityMetrics(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Start the mock ExtProc server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer()
	mockProc := &mockProcessorServer{
		processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
			}
		},
	}
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)
	defer extprocServer.Stop()

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
				RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
				ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
				ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	tmr := stats.NewTestMetricsRecorder()
	grpcTarget := "xds:///" + serviceName
	cc, err := grpc.NewClient(grpcTarget, grpc.WithStatsHandler(tmr), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	reqMsg := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("hello")}}
	if _, err = client.UnaryCall(ctx, reqMsg); err != nil {
		t.Fatalf("UnaryCall() failed: %v", err)
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
	if md.Handle.Name != "grpc.client_ext_proc.server_trailers_duration" {
		t.Fatalf("Got metric %s from channel, want grpc.client_ext_proc.server_trailers_duration", md.Handle.Name)
	}
	// Convert parallel slices of label keys and values into a map for easier
	// lookup.
	labels := make(map[string]string)
	for i, k := range md.LabelKeys {
		if i < len(md.LabelVals) {
			labels[k] = md.LabelVals[i]
		}
	}
	if gotTarget := labels["grpc.target"]; gotTarget != grpcTarget {
		t.Fatalf("Metric %s: grpc.target label = %q, want %q", md.Handle.Name, gotTarget, grpcTarget)
	}
	expectedCluster := "cluster-" + serviceName
	if gotBackendService := labels["grpc.lb.backend_service"]; gotBackendService != expectedCluster {
		t.Fatalf("Metric %s: grpc.lb.backend_service label = %q, want %q", md.Handle.Name, gotBackendService, expectedCluster)
	}
}

// TestObservabilityMetricsStreaming tests the client-side ext_proc metrics for
// a Streaming RPC. It verifies that all 4 duration metrics are recorded
// synchronously step-by-step with the correct labels and that their values are
// positive.
func (s) TestObservabilityMetricsStreaming(t *testing.T) {
	origParse := extproc.ParseGRPCServiceConfig
	origCreate := extproc.CreateExtProcChannel
	extproc.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	extproc.CreateExtProcChannel = createExtProcChannelForTesting
	defer func() {
		extproc.ParseGRPCServiceConfig = origParse
		extproc.CreateExtProcChannel = origCreate
	}()

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	extproc.RegisterForTesting()
	defer extproc.UnregisterForTesting()

	// Start the mock ExtProc server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("LocalTCPListener() failed: %v", err)
	}
	extprocServer := grpc.NewServer()
	mockProc := &mockProcessorServer{
		processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
			}
		},
	}
	v3procservicepb.RegisterExternalProcessorServer(extprocServer, mockProc)
	go extprocServer.Serve(lis)
	defer extprocServer.Stop()

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

	// Setup management server and resolver.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "test-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, stub.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	hcm := new(v3httppb.HttpConnectionManager)
	apiListener := resources.Listeners[0].GetApiListener().GetApiListener()
	if err = apiListener.UnmarshalTo(hcm); err != nil {
		t.Fatal(err)
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{
		e2e.HTTPFilter("extproc", &v3procfilterpb.ExternalProcessor{
			GrpcService: &v3corepb.GrpcService{
				TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
						TargetUri: lis.Addr().String(),
					},
				},
			},
			ProcessingMode: &v3procfilterpb.ProcessingMode{
				RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
				RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
				ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
				ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
			},
			ObservabilityMode:    true,
			DeferredCloseTimeout: durationpb.New(defaultTestShortTimeout),
		})},
		hcm.HttpFilters...)
	hcmAny := testutils.MarshalAny(t, hcm)
	resources.Listeners[0].ApiListener.ApiListener = hcmAny
	resources.Listeners[0].FilterChains[0].Filters[0].ConfigType = &v3listenerpb.Filter_TypedConfig{TypedConfig: hcmAny}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	tmr := stats.NewTestMetricsRecorder()
	grpcTarget := "xds:///" + serviceName
	cc, err := grpc.NewClient(grpcTarget, grpc.WithStatsHandler(tmr), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

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

	// Verify server_trailers_duration (recorded when Recv returns EOF).
	md, err = tmr.ReadFloat64Histo(ctx)
	if err != nil {
		t.Fatalf("Failed to read server_trailers_duration: %v", err)
	}
	verifyMetric(t, md, "grpc.client_ext_proc.server_trailers_duration", grpcTarget, "cluster-"+serviceName)
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
