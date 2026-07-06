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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/httpfilter/extproc/internal"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3procfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3procservicepb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
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

func requestHeadersResponse(setHeaders map[string]string, removeHeaders []string) *v3procservicepb.ProcessingResponse {
	var setOptions []*v3corepb.HeaderValueOption
	for k, v := range setHeaders {
		setOptions = append(setOptions, &v3corepb.HeaderValueOption{
			Header: &v3corepb.HeaderValue{
				Key:   k,
				Value: v,
			},
			AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	mutation := &v3procservicepb.HeaderMutation{
		SetHeaders:    setOptions,
		RemoveHeaders: removeHeaders,
	}

	return &v3procservicepb.ProcessingResponse{
		Response: &v3procservicepb.ProcessingResponse_RequestHeaders{
			RequestHeaders: &v3procservicepb.HeadersResponse{
				Response: &v3procservicepb.CommonResponse{
					Status:         v3procservicepb.CommonResponse_CONTINUE,
					HeaderMutation: mutation,
				},
			},
		},
	}
}

func responseHeadersResponse(setHeaders map[string]string, removeHeaders []string) *v3procservicepb.ProcessingResponse {
	var setOptions []*v3corepb.HeaderValueOption
	for k, v := range setHeaders {
		setOptions = append(setOptions, &v3corepb.HeaderValueOption{
			Header: &v3corepb.HeaderValue{
				Key:   k,
				Value: v,
			},
			AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	mutation := &v3procservicepb.HeaderMutation{
		SetHeaders:    setOptions,
		RemoveHeaders: removeHeaders,
	}

	return &v3procservicepb.ProcessingResponse{
		Response: &v3procservicepb.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &v3procservicepb.HeadersResponse{
				Response: &v3procservicepb.CommonResponse{
					Status:         v3procservicepb.CommonResponse_CONTINUE,
					HeaderMutation: mutation,
				},
			},
		},
	}
}

func requestBodyResponse(body []byte) *v3procservicepb.ProcessingResponse {
	return requestBodyResponseWithEOF(body, false)
}

func requestBodyResponseWithEOF(body []byte, endOfStream bool) *v3procservicepb.ProcessingResponse {
	streamedResp := &v3procservicepb.StreamedBodyResponse{
		Body:        body,
		EndOfStream: endOfStream,
	}

	if body == nil && endOfStream {
		streamedResp.EndOfStreamWithoutMessage = true
	}

	return &v3procservicepb.ProcessingResponse{
		Response: &v3procservicepb.ProcessingResponse_RequestBody{
			RequestBody: &v3procservicepb.BodyResponse{
				Response: &v3procservicepb.CommonResponse{
					Status: v3procservicepb.CommonResponse_CONTINUE,
					BodyMutation: &v3procservicepb.BodyMutation{
						Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
							StreamedResponse: streamedResp,
						},
					},
				},
			},
		},
	}
}

func responseBodyResponse(body []byte) *v3procservicepb.ProcessingResponse {
	return &v3procservicepb.ProcessingResponse{
		Response: &v3procservicepb.ProcessingResponse_ResponseBody{
			ResponseBody: &v3procservicepb.BodyResponse{
				Response: &v3procservicepb.CommonResponse{
					Status: v3procservicepb.CommonResponse_CONTINUE,
					BodyMutation: &v3procservicepb.BodyMutation{
						Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
							StreamedResponse: &v3procservicepb.StreamedBodyResponse{
								Body: body,
							},
						},
					},
				},
			},
		},
	}
}

func responseTrailersResponse(setHeaders map[string]string, removeHeaders []string) *v3procservicepb.ProcessingResponse {
	var setOptions []*v3corepb.HeaderValueOption
	for k, v := range setHeaders {
		setOptions = append(setOptions, &v3corepb.HeaderValueOption{
			Header: &v3corepb.HeaderValue{
				Key:   k,
				Value: v,
			},
		})
	}
	mutation := &v3procservicepb.HeaderMutation{
		SetHeaders:    setOptions,
		RemoveHeaders: removeHeaders,
	}

	return &v3procservicepb.ProcessingResponse{
		Response: &v3procservicepb.ProcessingResponse_ResponseTrailers{
			ResponseTrailers: &v3procservicepb.TrailersResponse{
				HeaderMutation: mutation,
			},
		},
	}
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
func startMockProcessor(t *testing.T, processFunc func(v3procservicepb.ExternalProcessor_ProcessServer) error, opts ...grpc.ServerOption) (string, func()) {
	t.Helper()

	origParse := internal.ParseGRPCServiceConfig
	origCreate := internal.CreateExtProcChannel
	internal.ParseGRPCServiceConfig = parseGRPCServiceConfigForTesting
	internal.CreateExtProcChannel = createExtProcChannelForTesting

	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	internal.RegisterForTesting()

	t.Cleanup(func() {
		internal.ParseGRPCServiceConfig = origParse
		internal.CreateExtProcChannel = origCreate
		internal.UnregisterForTesting()
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

	return lis.Addr().String(), extprocServer.Stop
}

// setupTestClient configures the management server with xDS resources that
// include the external processor filter, and creates a new gRPC client.
func setupTestClient(t *testing.T, extProcAddr string, extProcConfig *v3procfilterpb.ExternalProcessor, serverAddr string) (*grpc.ClientConn, error) {
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

	cc, err := grpc.NewClient("xds:///"+serviceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	return cc, err
}

// TestAllSendUnary tests the scenario where the ExtProc filter is configured
// with all processing modes set to SEND/GRPC. Verifies that the client
// correctly routes headers and bodies to the processor, the processor echoes
// the mutations back, and the client successfully completes a Unary RPC.
func (s) TestAllSendUnary(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			var resp *v3procservicepb.ProcessingResponse
			switch {
			case req.GetRequestHeaders() != nil:
				resp = requestHeadersResponse(map[string]string{"request-mutated": "true"}, []string{"request-to-remove"})
			case req.GetRequestBody() != nil:
				body := req.GetRequestBody().GetBody()
				if req.GetRequestBody().GetEndOfStreamWithoutMessage() || req.GetRequestBody().GetEndOfStream() {
					resp = requestBodyResponseWithEOF(body, true)
					break
				}
				reqMsg := &testpb.SimpleRequest{}
				if err := proto.Unmarshal(body, reqMsg); err != nil {
					return fmt.Errorf("failed to unmarshal request body: %v", err)
				}
				if bodyStr := string(reqMsg.GetPayload().GetBody()); bodyStr != "hello-request" {
					return fmt.Errorf("unexpected request body on processor: %q, want: %q", bodyStr, "hello-request")
				}
				mutatedProto := &testpb.SimpleRequest{
					Payload: &testpb.Payload{
						Body: []byte("hello-request-mutated"),
					},
				}
				mutatedBytes, err := proto.Marshal(mutatedProto)
				if err != nil {
					return fmt.Errorf("failed to marshal mutated request: %v", err)
				}
				resp = requestBodyResponse(mutatedBytes)
			case req.GetResponseHeaders() != nil:
				resp = responseHeadersResponse(map[string]string{"response-mutated": "true"}, []string{"response-to-remove"})
			case req.GetResponseBody() != nil:
				respMsg := &testpb.SimpleResponse{}
				if err := proto.Unmarshal(req.GetResponseBody().GetBody(), respMsg); err != nil {
					return fmt.Errorf("failed to unmarshal response body: %v", err)
				}
				if body := string(respMsg.GetPayload().GetBody()); body != "hello-response" {
					return fmt.Errorf("unexpected response body on processor: %q, want: %q", body, "hello-response")
				}
				mutatedProto := &testpb.SimpleResponse{
					Payload: &testpb.Payload{
						Body: []byte("hello-response-mutated"),
					},
				}
				mutatedBytes, err := proto.Marshal(mutatedProto)
				if err != nil {
					return fmt.Errorf("failed to marshal mutated response: %v", err)
				}
				resp = responseBodyResponse(mutatedBytes)
			case req.GetResponseTrailers() != nil:
				resp = responseTrailersResponse(map[string]string{"trailer-mutated": "true"}, []string{"trailer-to-remove"})
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				t.Error("no incoming metadata")
				return nil, fmt.Errorf("no incoming metadata")
			}
			if vals := md.Get("request-mutated"); len(vals) == 0 || vals[0] != "true" {
				t.Errorf("missing or invalid request-mutated header: %v", vals)
			}
			if vals := md.Get("request-to-remove"); len(vals) > 0 {
				t.Errorf("request-to-remove header was not removed: %v", vals)
			}
			if body := string(in.GetPayload().GetBody()); body != "hello-request-mutated" {
				t.Errorf("unexpected request body: %q, want: %q", body, "hello-request-mutated")
			}

			if err := grpc.SendHeader(ctx, metadata.Pairs("response-to-remove", "yes")); err != nil {
				return nil, err
			}
			if err := grpc.SetTrailer(ctx, metadata.Pairs("trailer-to-remove", "yes")); err != nil {
				return nil, err
			}

			return &testpb.SimpleResponse{
				Payload: &testpb.Payload{
					Body: []byte("hello-response"),
				},
			}, nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
	}, stub.Address)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)

	reqMsg := &testpb.SimpleRequest{
		Payload: &testpb.Payload{
			Body: []byte("hello-request"),
		},
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("request-to-remove", "yes"))

	resp, err := client.UnaryCall(ctx, reqMsg)
	if err != nil {
		t.Fatalf("UnaryCall() failed: %v", err)
	}

	if body := string(resp.GetPayload().GetBody()); body != "hello-response-mutated" {
		t.Fatalf("UnaryCall() returned payload: %s, want: %s", body, "hello-response-mutated")
	}
}

// TestStreamingModifications tests the scenario where the ExtProc filter is
// configured with SEND/GRPC processing modes for a bidirectional streaming RPC.
// Verifies that the client correctly routes headers and bodies to the
// processor, the server receives the mutated requests, and the client receives
// the correctly mutated responses back, even when the processor changes the
// response count.
func (s) TestStreamingModifications(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			var resp *v3procservicepb.ProcessingResponse
			switch {
			case req.GetRequestHeaders() != nil:
				resp = requestHeadersResponse(map[string]string{"req-header-modified": "true"}, nil)
			case req.GetRequestBody() != nil:
				body := req.GetRequestBody()
				// If the request has EndOfStream or EndOfStreamWithoutMessage, it
				// indicated CloseSend from client. Send EndOfStream to indicate no
				// more client responses.
				if body.GetEndOfStreamWithoutMessage() || body.GetEndOfStream() {
					resp = requestBodyResponseWithEOF(body.GetBody(), true)
				} else {
					reqMsg := &testpb.StreamingOutputCallRequest{}
					if err := proto.Unmarshal(body.GetBody(), reqMsg); err != nil {
						return err
					}
					reqMsg.Payload.Body = append(reqMsg.Payload.Body, []byte("_req_body_modified")...)
					mutated, err := proto.Marshal(reqMsg)
					if err != nil {
						return err
					}
					resp = requestBodyResponse(mutated)
				}
			case req.GetResponseHeaders() != nil:
				resp = responseHeadersResponse(map[string]string{"resp-header-modified": "true"}, nil)
			case req.GetResponseBody() != nil:
				body := req.GetResponseBody()
				respMsg := &testpb.StreamingOutputCallResponse{}
				if err := proto.Unmarshal(body.GetBody(), respMsg); err != nil {
					return err
				}
				origBody := respMsg.Payload.Body
				// Send 2 messages for 1 server message received.
				respMsg.Payload.Body = append(origBody, []byte("_resp_body_modified_1")...)
				mutated1, err := proto.Marshal(respMsg)
				if err != nil {
					return err
				}
				resp1 := responseBodyResponse(mutated1)
				if err := stream.Send(resp1); err != nil {
					return err
				}

				respMsg.Payload.Body = append(origBody, []byte("_resp_body_modified_2")...)
				mutated2, err := proto.Marshal(respMsg)
				if err != nil {
					return err
				}
				resp = responseBodyResponse(mutated2)
			case req.GetResponseTrailers() != nil:
				resp = responseTrailersResponse(map[string]string{"resp-trailer-modified": "true"}, nil)
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Verify if the dataplane server receives the mutated headers.
			md, ok := metadata.FromIncomingContext(stream.Context())
			if !ok {
				t.Error("missing incoming metadata")
				return fmt.Errorf("missing incoming metadata")
			}
			hdr := md.Get("req-header-modified")
			if len(hdr) != 1 || hdr[0] != "true" {
				t.Errorf("missing or invalid req-header-modified: %v", hdr)
			}

			// Explicitly send response headers to client
			if err := stream.SendHeader(metadata.Pairs("resp-header-from-server", "present")); err != nil {
				return err
			}

			var msgCount int
			for {
				in, err := stream.Recv()
				// Check the message count once we receive io.EOF
				if err == io.EOF {
					if msgCount != 4 {
						t.Errorf("server received %d messages, want 4", msgCount)
					}
					return nil
				}
				if err != nil {
					return err
				}
				msgCount++
				expectedBody := fmt.Sprintf("c%d_req_body_modified", msgCount)
				if string(in.GetPayload().GetBody()) != expectedBody {
					t.Errorf("server received unexpected message body: %s, want: %s", string(in.GetPayload().GetBody()), expectedBody)
				}
				// Send one response message for each client request.
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: append(in.GetPayload().GetBody(), []byte("_s1")...)},
				}); err != nil {
					return err
				}
			}
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
	}, stub.Address)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)

	// Make the Streaming call and verify it succeeds and correctly returns the
	// modified payloads.
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	messages := [][]byte{
		[]byte("c1"),
		[]byte("c2"),
		[]byte("c3"),
		[]byte("c4"),
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

		// Verify we receive exactly 2 messages for each client request because proc
		// server response with 2 messages for every server messages.
		for i := 1; i <= 2; i++ {
			resp, err := stream.Recv()
			if err != nil {
				t.Fatalf("stream.Recv() failed: %v", err)
			}
			expectedBody := fmt.Sprintf("%s_req_body_modified_s1_resp_body_modified_%d", string(msg), i)
			if string(resp.GetPayload().GetBody()) != expectedBody {
				t.Fatalf("stream.Recv() returned payload: %s, want: %s", resp.GetPayload().GetBody(), expectedBody)
			}
		}
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}

	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() returned error: %v, want EOF", err)
	}

	// Verify mutated response headers, even though Header() is called after
	// Recv().
	headerMetadata, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}
	gotHdr := headerMetadata.Get("resp-header-modified")
	if len(gotHdr) != 1 || gotHdr[0] != "true" {
		t.Fatalf("client received mutated-resp-header = %v, want [true]", gotHdr)
	}

	// Verify mutated response trailers
	trailerMetadata := stream.Trailer()
	gotTrailers := trailerMetadata.Get("resp-trailer-modified")
	if len(gotTrailers) != 1 || gotTrailers[0] != "true" {
		t.Fatalf("client received resp-trailer-modified = %v, want [true]", gotTrailers)
	}
}

// TestProtocolConfigInFirstMessage tests the scenario where multiple processing
// requests are sent over an active stream. Verifies that the first
// ProcessingRequest sent to the processor server contains a valid
// ProtocolConfig populated with the current ProcessingMode, and subsequent
// requests do not.
func (s) TestProtocolConfigInFirstMessage(t *testing.T) {
	var receivedCall atomic.Bool
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		var callCount int
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			receivedCall.Store(true)
			callCount++
			if callCount == 1 {
				pConfig := req.GetProtocolConfig()
				if pConfig == nil {
					return fmt.Errorf("Expected ProtocolConfig in first request, got nil")
				} else if pConfig.GetRequestBodyMode() != v3procfilterpb.ProcessingMode_GRPC {
					return fmt.Errorf("RequestBodyMode = %v, want GRPC", pConfig.GetRequestBodyMode())
				}
			} else {
				if req.GetProtocolConfig() != nil {
					return fmt.Errorf("Request %d unexpectedly had ProtocolConfig populated", callCount)
				}
			}

			var resp *v3procservicepb.ProcessingResponse
			switch {
			case req.GetRequestHeaders() != nil:
				resp = requestHeadersResponse(nil, nil)
			case req.GetRequestBody() != nil:
				resp = requestBodyResponse(req.GetRequestBody().GetBody())
			case req.GetResponseHeaders() != nil:
				resp = responseHeadersResponse(nil, nil)
			case req.GetResponseBody() != nil:
				return fmt.Errorf("Unexpectedly received ResponseBody in proc server because response body mode was set to skip")
			case req.GetResponseTrailers() != nil:
				resp = responseTrailersResponse(nil, nil)
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_NONE,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
	}, stub.Address)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	reqMsg := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("hello-extproc")}}
	if _, err := client.UnaryCall(ctx, reqMsg); err != nil {
		t.Fatalf("UnaryCall() failed: %v", err)
	}

	if !receivedCall.Load() {
		t.Fatal("no requests received by the mock processor")
	}
}

// TestWaitForDataplane tests the scenario where an outbound RPC is initiated
// before the external processor confirms header mutations. Verifies that
// outbound events do not reach the backend until the processor responds to the
// request headers.
func (s) TestWaitForDataplane(t *testing.T) {
	unblockHeaders := make(chan struct{})

	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			var resp *v3procservicepb.ProcessingResponse
			switch {
			case req.GetRequestHeaders() != nil:
				<-unblockHeaders
				resp = requestHeadersResponse(nil, nil)
			case req.GetRequestBody() != nil:
				body := req.GetRequestBody()
				if body.GetEndOfStreamWithoutMessage() || body.GetEndOfStream() {
					resp = requestBodyResponseWithEOF(body.GetBody(), body.EndOfStream)
				} else {
					resp = requestBodyResponse(body.GetBody())
				}
			default:
				resp = &v3procservicepb.ProcessingResponse{}
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	backendCalledCh := make(chan struct{})
	var backendMessagesReceived int32
	backendCloseSendReceived := make(chan struct{})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			close(backendCalledCh)
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					close(backendCloseSendReceived)
					return nil
				}
				if err != nil {
					return err
				}
				atomic.AddInt32(&backendMessagesReceived, 1)
			}
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:   v3procfilterpb.ProcessingMode_GRPC,
		},
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

	// Send message and call CloseSend. Since header response is blocked,
	// these should not reach the backend.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("msg")}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend failed: %v", err)
	}

	// Verify backend does not receive any calls or messages.
	select {
	case <-backendCalledCh:
		t.Fatal("Backend was called prematurely before headers were processed")
	case <-time.After(defaultTestShortTimeout):
	}

	// Unblock the processor's headers response.
	close(unblockHeaders)

	// Verify backend eventually receives everything.
	select {
	case <-backendCalledCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for backend to be called")
	}

	select {
	case <-backendCloseSendReceived:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for backend to receive CloseSend")
	}

	if got := atomic.LoadInt32(&backendMessagesReceived); got != 1 {
		t.Fatalf("Backend received %d messages, want 1", got)
	}
}

// TestTrailersOnly tests the scenario where the backend sends an immediate
// Trailers-Only response. Verifies that this is correctly delivered to the
// processor as a ResponseHeader message with end_of_stream set to true.
func (s) TestTrailersOnly(t *testing.T) {
	receivedHeadersCh := make(chan *v3procservicepb.ProcessingRequest, 1)
	errCh := make(chan error, 1)

	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
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
			resp := responseHeadersResponse(map[string]string{"resp-header-modified": "true"}, nil)
			return stream.Send(resp)
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

	// Verify mutated response trailers
	trailerMetadata := stream.Trailer()
	gotTrailers := trailerMetadata.Get("resp-header-modified")
	if len(gotTrailers) != 1 || gotTrailers[0] != "true" {
		t.Fatalf("client received resp-header-modified = %v, want [true]", gotTrailers)
	}
}

// TestDraining tests the scenario where the processor server signals
// RequestDrain: true. Verifies that the filter correctly drains any pending
// messages and then transitions to bypass mode, causing all subsequent client
// messages and server responses to bypass the processor.
func (s) TestDraining(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive the first client message c1.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		body := req.GetRequestBody()
		if body == nil {
			return fmt.Errorf("expected RequestBody, got %v", req)
		}
		reqMsg := &testpb.StreamingOutputCallRequest{}
		if err := proto.Unmarshal(body.GetBody(), reqMsg); err != nil {
			return err
		}
		// Mutate the client message.
		reqMsg.Payload.Body = append(reqMsg.Payload.Body, []byte("_mutated")...)
		mutatedBytes, err := proto.Marshal(reqMsg)
		if err != nil {
			return err
		}

		// Respond to the client message with RequestDrain: true and the mutated
		// body.
		resp := &v3procservicepb.ProcessingResponse{
			RequestDrain: true,
			Response: &v3procservicepb.ProcessingResponse_RequestBody{
				RequestBody: &v3procservicepb.BodyResponse{
					Response: &v3procservicepb.CommonResponse{
						Status: v3procservicepb.CommonResponse_CONTINUE,
						BodyMutation: &v3procservicepb.BodyMutation{
							Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
								StreamedResponse: &v3procservicepb.StreamedBodyResponse{
									Body: mutatedBytes,
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

		// Since write side is closed by client filter upon drain, Recv should get
		// EOF.
		_, err = stream.Recv()
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("expected Recv to return io.EOF after RequestDrain, got %v", err)
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Receive first client message. Verify that it is the mutated message.
			in1, err := stream.Recv()
			if err != nil {
				return err
			}
			if got, want := string(in1.GetPayload().GetBody()), "c1_mutated"; got != want {
				return status.Errorf(codes.FailedPrecondition, "expected body %q, got %q", want, got)
			}

			// Send the server message s1. This should bypass the processor as we have
			// set RequestDrain: true.
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte("s1")},
			}); err != nil {
				return err
			}

			// Receive the second client message c2 and verify that it is not mutated.
			in2, err := stream.Recv()
			if err != nil {
				return err
			}
			if got, want := string(in2.GetPayload().GetBody()), "c2"; got != want {
				return status.Errorf(codes.FailedPrecondition, "expected body %q, got %q", want, got)
			}

			// Send the second server message s2. This should bypass the processor as
			// we have set RequestDrain: true.
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte("s2")},
			}); err != nil {
				return err
			}
			return nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SKIP,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SKIP,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
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

	// Send first request message c1.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c1")}}); err != nil {
		t.Fatalf("stream.Send(c1) failed: %v", err)
	}

	// Receive server response s1 and verify that it is not mutated.
	resp1, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(s1) failed: %v", err)
	}
	if got, want := string(resp1.GetPayload().GetBody()), "s1"; got != want {
		t.Fatalf("got response %q, want %q", got, want)
	}

	// Send second request message c2.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c2")}}); err != nil {
		t.Fatalf("stream.Send(c2) failed: %v", err)
	}

	// Receive second server response s2.
	resp2, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(s2) failed: %v", err)
	}
	if got, want := string(resp2.GetPayload().GetBody()), "s2"; got != want {
		t.Fatalf("got response %q, want %q", got, want)
	}
}

// TestImmediateResponse tests filter behavior on receiving an ImmediateResponse
// under different configurations.
func (s) TestImmediateResponse(t *testing.T) {
	tests := []struct {
		name                     string
		disableImmediateResponse bool
		failureModeAllow         bool
		wantCode                 codes.Code
		wantMsgContains          string
	}{
		{
			name:                     "Enabled",
			disableImmediateResponse: false,
			failureModeAllow:         false,
			wantCode:                 codes.Aborted,
			wantMsgContains:          "simulated immediate response",
		},
		{
			name:                     "EnabledWithFailureModeAllow",
			disableImmediateResponse: false,
			failureModeAllow:         true,
			wantCode:                 codes.Aborted,
			wantMsgContains:          "simulated immediate response",
		},
		{
			name:                     "Disabled",
			disableImmediateResponse: true,
			failureModeAllow:         false,
			wantCode:                 codes.Internal,
			wantMsgContains:          "external processor sent an immediate response but immediate responses are disabled in configuration",
		},
		{
			name:                     "DisabledWithFailureModeAllow",
			disableImmediateResponse: true,
			failureModeAllow:         true,
			wantCode:                 codes.OK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("expected request headers, got %v", req)
				}
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_ImmediateResponse{
						ImmediateResponse: &v3procservicepb.ImmediateResponse{
							GrpcStatus: &v3procservicepb.GrpcStatus{
								Status: uint32(codes.Aborted),
							},
							Details: "simulated immediate response",
						},
					},
				}
				return stream.Send(resp)
			})

			stub := stubserver.StartTestService(t, nil)
			defer stub.Stop()

			cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
				},
				DisableImmediateResponse: tc.disableImmediateResponse,
				FailureModeAllow:         tc.failureModeAllow,
			}, stub.Address)
			if err != nil {
				t.Fatalf("failed to dial: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client := testgrpc.NewTestServiceClient(cc)
			_, err = client.EmptyCall(ctx, &testpb.Empty{})

			if tc.wantCode == codes.OK {
				if err != nil {
					t.Fatalf("EmptyCall() failed unexpectedly when failure_mode_allow is true: %v", err)
				}
				return
			}

			if got := status.Code(err); got != tc.wantCode {
				t.Fatalf("EmptyCall() returned status code %v, want %v", got, tc.wantCode)
			}
			if tc.wantMsgContains != "" && !strings.Contains(err.Error(), tc.wantMsgContains) {
				t.Fatalf("EmptyCall() returned error message %q, want it to contain %q", err.Error(), tc.wantMsgContains)
			}
		})
	}
}

// TestStreamFailureHeaderPhaseDeny verifies that if the external processor
// stream fails during the request header phase due to different conditions and
// failure_mode_allow is false, the RPC fails with correct errors.
func (s) TestStreamFailureHeaderPhaseDeny(t *testing.T) {
	tests := []struct {
		name            string
		processFunc     func(v3procservicepb.ExternalProcessor_ProcessServer) error
		wantMsgContains string
	}{
		{
			name: "AbruptStreamFailure",
			processFunc: func(v3procservicepb.ExternalProcessor_ProcessServer) error {
				return status.Error(codes.Unavailable, "abrupt stream failure")
			},
		},
		{
			name: "UnexpectedHeaderStatus",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("expected request headers, got %v", req)
				}
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_RequestHeaders{
						RequestHeaders: &v3procservicepb.HeadersResponse{
							Response: &v3procservicepb.CommonResponse{
								Status: v3procservicepb.CommonResponse_CONTINUE_AND_REPLACE,
							},
						},
					},
				}
				return stream.Send(resp)
			},
			wantMsgContains: "external processor returned unexpected status CONTINUE_AND_REPLACE for request headers, expected CONTINUE",
		},
		{
			name: "UnexpectedCompressedBodyMessage",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("expected request headers, got %v", req)
				}
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_RequestBody{
						RequestBody: &v3procservicepb.BodyResponse{
							Response: &v3procservicepb.CommonResponse{
								Status: v3procservicepb.CommonResponse_CONTINUE,
								BodyMutation: &v3procservicepb.BodyMutation{
									Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
										StreamedResponse: &v3procservicepb.StreamedBodyResponse{
											GrpcMessageCompressed: true,
										},
									},
								},
							},
						},
					},
				}
				return stream.Send(resp)
			},
			wantMsgContains: "external processor returned an unexpected message type *ext_procv3.ProcessingResponse_RequestBody, expected request headers response",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lisAddr, _ := startMockProcessor(t, tc.processFunc)

			stub := stubserver.StartTestService(t, nil)
			defer stub.Stop()

			cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
				},
				FailureModeAllow: false,
			}, stub.Address)
			if err != nil {
				t.Fatalf("failed to dial: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client := testgrpc.NewTestServiceClient(cc)
			_, err = client.EmptyCall(ctx, &testpb.Empty{})
			if got, want := status.Code(err), codes.Internal; got != want {
				t.Fatalf("EmptyCall() returned status code %v, want %v", got, want)
			}
			if tc.wantMsgContains != "" && !strings.Contains(err.Error(), tc.wantMsgContains) {
				t.Fatalf("EmptyCall() returned error message %q, want it to contain %q", err.Error(), tc.wantMsgContains)
			}
		})
	}
}

// TestStreamFailureHeaderPhaseAllow verifies that if the external processor
// stream fails during the request header phase and failure_mode_allow is true,
// the filter is bypassed and the RPC succeeds.
func (s) TestStreamFailureHeaderPhaseAllow(t *testing.T) {
	tests := []struct {
		name        string
		processFunc func(v3procservicepb.ExternalProcessor_ProcessServer) error
	}{
		{
			name: "AbruptStreamFailure",
			processFunc: func(v3procservicepb.ExternalProcessor_ProcessServer) error {
				return status.Error(codes.Unavailable, "abrupt stream failure")
			},
		},
		{
			name: "UnexpectedHeaderStatus",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("expected request headers, got %v", req)
				}
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_RequestHeaders{
						RequestHeaders: &v3procservicepb.HeadersResponse{
							Response: &v3procservicepb.CommonResponse{
								Status: v3procservicepb.CommonResponse_CONTINUE_AND_REPLACE,
							},
						},
					},
				}
				return stream.Send(resp)
			},
		},
		{
			name: "UnexpectedCompressedBodyMessage",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("expected request headers, got %v", req)
				}
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_RequestBody{
						RequestBody: &v3procservicepb.BodyResponse{
							Response: &v3procservicepb.CommonResponse{
								Status: v3procservicepb.CommonResponse_CONTINUE,
								BodyMutation: &v3procservicepb.BodyMutation{
									Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
										StreamedResponse: &v3procservicepb.StreamedBodyResponse{
											GrpcMessageCompressed: true,
										},
									},
								},
							},
						},
					},
				}
				return stream.Send(resp)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lisAddr, _ := startMockProcessor(t, tc.processFunc)

			stub := stubserver.StartTestService(t, nil)
			defer stub.Stop()

			cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
				},
				FailureModeAllow: true,
			}, stub.Address)
			if err != nil {
				t.Fatalf("failed to dial: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client := testgrpc.NewTestServiceClient(cc)
			_, err = client.EmptyCall(ctx, &testpb.Empty{})
			if err != nil {
				t.Fatalf("EmptyCall() failed unexpectedly when failure_mode_allow is true: %v", err)
			}
		})
	}
}

// TestStreamFailureBodyPhaseAllow tests the scenario where the external
// processor stream fails abruptly during the request body phase while
// failure_mode_allow is true. Verifies that the RPC fails since
// failure_mode_allow is not respected once body has been sent to the external
// processor.
func (s) TestStreamFailureBodyPhaseAllow(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive headers and send back.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("expected request headers, got %v", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// Receive c1
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		bodyBytes := req.GetRequestBody().GetBody()
		reqMsg := new(testpb.StreamingOutputCallRequest)
		if err := proto.Unmarshal(bodyBytes, reqMsg); err != nil {
			return status.Errorf(codes.Internal, "failed to unmarshal request body: %v", err)
		}
		if got, want := string(reqMsg.GetPayload().GetBody()), "c1"; got != want {
			return status.Errorf(codes.FailedPrecondition, "expected body %q, got %q", want, got)
		}
		// Send response to c1 to keep it going
		resp = &v3procservicepb.ProcessingResponse{
			Response: &v3procservicepb.ProcessingResponse_RequestBody{
				RequestBody: &v3procservicepb.BodyResponse{
					Response: &v3procservicepb.CommonResponse{
						Status: v3procservicepb.CommonResponse_CONTINUE,
						BodyMutation: &v3procservicepb.BodyMutation{
							Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
								StreamedResponse: &v3procservicepb.StreamedBodyResponse{
									Body: bodyBytes,
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
		// Fail abruptly with non-EOF error during body phase
		return status.Error(codes.Unavailable, "abrupt stream failure in body phase")
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Receive c1
			in1, err := stream.Recv()
			if err != nil {
				return err
			}
			if got, want := string(in1.GetPayload().GetBody()), "c1"; got != want {
				return status.Errorf(codes.FailedPrecondition, "expected body %q, got %q", want, got)
			}

			// Send s1
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte("s1")},
			}); err != nil {
				return err
			}

			// Try to receive c2 but we should get error as ext_proc stream failed
			// after body messages started, overriding failure_mode_allow=true.
			if _, err = stream.Recv(); err == nil {
				return fmt.Errorf("unexpectedly received client messages when expected rpc to fail")
			}
			return nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode: v3procfilterpb.ProcessingMode_SEND,
		},
		FailureModeAllow: true,
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

	// Send c1.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c1")}}); err != nil {
		t.Fatalf("stream.Send(c1) failed: %v", err)
	}

	// Subsequent Recv/Send must fail with INTERNAL status because ext_proc stream
	// failed after body messages started, overriding failure_mode_allow=true.
	_, err = stream.Recv()
	if got, want := status.Code(err), codes.Internal; got != want {
		t.Fatalf("stream.Recv() returned status code %v, want %v", got, want)
	}
}

// TestStreamFailureBodyModeNoneAllow tests the scenario where the external
// processor stream fails abruptly after initial headers while BodySendMode is
// NONE and failure_mode_allow is true. Verifies that because no body messages
// were sent to ext_proc, the data plane RPC is allowed to continue unharmed.
func (s) TestStreamFailureBodyModeNoneAllow(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive initial headers and continue.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("expected request headers, got %v", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}
		// Fail after sending headers.
		return status.Error(codes.Unavailable, "abrupt stream failure after headers")
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			in1, err := stream.Recv()
			if err != nil {
				return err
			}
			if got, want := string(in1.GetPayload().GetBody()), "c1"; got != want {
				return status.Errorf(codes.FailedPrecondition, "expected body %q, got %q", want, got)
			}
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte("s1")},
			}); err != nil {
				return err
			}
			in2, err := stream.Recv()
			if err != nil {
				return err
			}
			if got, want := string(in2.GetPayload().GetBody()), "c2"; got != want {
				return status.Errorf(codes.FailedPrecondition, "expected body %q, got %q", want, got)
			}
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte("s2")},
			}); err != nil {
				return err
			}
			return nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:   v3procfilterpb.ProcessingMode_NONE,
			ResponseBodyMode:  v3procfilterpb.ProcessingMode_NONE,
		},
		FailureModeAllow: true,
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

	// Send c1.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c1")}}); err != nil {
		t.Fatalf("stream.Send(c1) failed: %v", err)
	}

	// Receive s1.
	resp1, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(s1) failed: %v", err)
	}
	if got, want := string(resp1.GetPayload().GetBody()), "s1"; got != want {
		t.Fatalf("got response %q, want %q", got, want)
	}

	// Send c2.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c2")}}); err != nil {
		t.Fatalf("stream.Send(c2) failed: %v", err)
	}

	// Receive s2.
	resp2, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(s2) failed: %v", err)
	}
	if got, want := string(resp2.GetPayload().GetBody()), "s2"; got != want {
		t.Fatalf("got response %q, want %q", got, want)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}

	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() returned error: %v, want io.EOF", err)
	}
}

// TestStreamFailureBodyPhaseDeny tests various external processor failures
// during the request body phase when failure_mode_allow is false, verifying
// that the RPC fails.
func (s) TestStreamFailureBodyPhaseDeny(t *testing.T) {
	tests := []struct {
		name            string
		processFunc     func(v3procservicepb.ExternalProcessor_ProcessServer) error
		wantMsgContains string
	}{
		{
			name: "AbruptStreamFailure",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Receive headers and send back.
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("expected request headers, got %v", req)
				}
				resp := requestHeadersResponse(nil, nil)
				if err := stream.Send(resp); err != nil {
					return err
				}

				_, err = stream.Recv()
				if err != nil {
					return err
				}
				// Fail abruptly with non-EOF error during body phase
				return status.Error(codes.Unavailable, "abrupt stream failure in body phase")
			},
		},
		{
			name: "GrpcMessageCompressedUnsupported",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Receive headers and send back.
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("expected request headers, got %v", req)
				}
				resp := requestHeadersResponse(nil, nil)
				if err := stream.Send(resp); err != nil {
					return err
				}

				// Receive c1 body. Return GrpcMessageCompressed: true!
				req, err = stream.Recv()
				if err != nil {
					return err
				}
				bodyBytes := req.GetRequestBody().GetBody()
				resp = &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_RequestBody{
						RequestBody: &v3procservicepb.BodyResponse{
							Response: &v3procservicepb.CommonResponse{
								Status: v3procservicepb.CommonResponse_CONTINUE,
								BodyMutation: &v3procservicepb.BodyMutation{
									Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
										StreamedResponse: &v3procservicepb.StreamedBodyResponse{
											Body:                  bodyBytes,
											GrpcMessageCompressed: true,
										},
									},
								},
							},
						},
					},
				}
				return stream.Send(resp)
			},
			wantMsgContains: "external processor returned compressed grpc message which is not supported",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lisAddr, _ := startMockProcessor(t, tc.processFunc)

			stub := stubserver.StartTestService(t, &stubserver.StubServer{
				FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
					_, err := stream.Recv()
					return err
				},
			})
			defer stub.Stop()

			cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
					RequestBodyMode:   v3procfilterpb.ProcessingMode_GRPC,
				},
				FailureModeAllow: false,
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

			if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c1")}}); err != nil {
				t.Fatalf("stream.Send(c1) failed: %v", err)
			}

			_, err = stream.Recv()
			if got, want := status.Code(err), codes.Internal; got != want {
				t.Fatalf("stream.Recv returned error code %v, want %v (error: %v)", got, want, err)
			}
			if tc.wantMsgContains != "" && !strings.Contains(err.Error(), tc.wantMsgContains) {
				t.Fatalf("stream.Recv() returned err %v, want error containing %q", err, tc.wantMsgContains)
			}
		})
	}
}

// TestUnaryFailureBodyPhaseDeny tests the scenario where the external
// processor stream fails abruptly during the request body phase of a Unary RPC
// while failure_mode_allow is false. Verifies that the Unary RPC fails with
// status code Internal.
func (s) TestUnaryFailureBodyPhaseDeny(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive headers and send back.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("expected request headers, got %v", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		_, err = stream.Recv()
		if err != nil {
			return err
		}

		// Fail abruptly with non-EOF error during body phase
		return status.Error(codes.Unavailable, "abrupt stream failure in body phase")
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: in.GetPayload()}, nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:   v3procfilterpb.ProcessingMode_GRPC,
		},
		FailureModeAllow: false,
	}, stub.Address)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	reqMsg := &testpb.SimpleRequest{
		Payload: &testpb.Payload{
			Body: []byte("c1"),
		},
	}
	_, err = client.UnaryCall(ctx, reqMsg)
	if got, want := status.Code(err), codes.Internal; got != want {
		t.Fatalf("UnaryCall() returned status code: %v, want %v", got, want)
	}
}

// TestDrainingUnderLoad tests the scenario where a processor server sends
// RequestDrain: true. Verifies that subsequent client SendMsg and RecvMsg calls
// correctly deliver all in-flight and bypassed payloads directly over the data
// plane without message loss.
func (s) TestDrainingUnderLoad(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// 1. Request headers
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("expected request headers, got %v", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// 2. Request body c1. Respond with RequestDrain: true!
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		bodyBytes := req.GetRequestBody().GetBody()
		resp = &v3procservicepb.ProcessingResponse{
			RequestDrain: true,
			Response: &v3procservicepb.ProcessingResponse_RequestBody{
				RequestBody: &v3procservicepb.BodyResponse{
					Response: &v3procservicepb.CommonResponse{
						Status: v3procservicepb.CommonResponse_CONTINUE,
						BodyMutation: &v3procservicepb.BodyMutation{
							Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
								StreamedResponse: &v3procservicepb.StreamedBodyResponse{
									Body: bodyBytes,
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

		// 3. Loop echoing any remaining in-flight requests until CloseSend (EOF)!
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if req.GetRequestBody() != nil {
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_RequestBody{
						RequestBody: &v3procservicepb.BodyResponse{
							Response: &v3procservicepb.CommonResponse{
								Status: v3procservicepb.CommonResponse_CONTINUE,
								BodyMutation: &v3procservicepb.BodyMutation{
									Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
										StreamedResponse: &v3procservicepb.StreamedBodyResponse{
											Body: req.GetRequestBody().GetBody(),
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
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			for i := 1; i <= 50; i++ {
				in, err := stream.Recv()
				if err != nil {
					return fmt.Errorf("backend Recv(%d) failed: %v", i, err)
				}
				want := fmt.Sprintf("c%d", i)
				if got := string(in.GetPayload().GetBody()); got != want {
					return status.Errorf(codes.FailedPrecondition, "expected body %q, got %q", want, got)
				}

				resp := fmt.Sprintf("s%d", i)
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: []byte(resp)},
				}); err != nil {
					return fmt.Errorf("backend Send(%d) failed: %v", i, err)
				}
			}
			return nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
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

	for i := 1; i <= 50; i++ {
		req := fmt.Sprintf("c%d", i)
		if err := stream.Send(&testpb.StreamingOutputCallRequest{
			Payload: &testpb.Payload{Body: []byte(req)},
		}); err != nil {
			t.Fatalf("client Send(%d) failed: %v", i, err)
		}

		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("client Recv(%d) failed: %v", i, err)
		}
		want := fmt.Sprintf("s%d", i)
		if got := string(resp.GetPayload().GetBody()); got != want {
			t.Fatalf("client expected response %q, got %q", want, got)
		}
	}
}

// TestClientTrailer tests the scenario where client stream trailers are
// inspected both early and post-stream. Verifies that calling Trailer()
// prematurely returns nil, and calling it after the stream has finished
// correctly triggers processor trailer mutation and returns the final mutated
// metadata.
func (s) TestClientTrailer(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// 1. Request headers
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("expected request headers, got %v", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// 2. Response headers
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		if req.GetResponseHeaders() == nil {
			return fmt.Errorf("expected response headers, got %v", req)
		}
		resp = responseHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// 3. Response trailers
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		if req.GetResponseTrailers() == nil {
			return fmt.Errorf("expected response trailers, got %v", req)
		}
		resp = &v3procservicepb.ProcessingResponse{
			Response: &v3procservicepb.ProcessingResponse_ResponseTrailers{
				ResponseTrailers: &v3procservicepb.TrailersResponse{
					HeaderMutation: &v3procservicepb.HeaderMutation{
						SetHeaders: []*v3corepb.HeaderValueOption{
							{
								Header: &v3corepb.HeaderValue{
									Key:   "test-trailer",
									Value: "mutated-val",
								},
							},
						},
					},
				},
			},
		}
		return stream.Send(resp)
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Read c1
			req, err := stream.Recv()
			if err != nil {
				return err
			}
			if string(req.GetPayload().GetBody()) != "c1" {
				return fmt.Errorf("unexpected request: %q", req.GetPayload().GetBody())
			}
			// Send response message s1
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte("s1")},
			}); err != nil {
				return err
			}
			// Read c2
			req, err = stream.Recv()
			if err != nil {
				return err
			}
			if string(req.GetPayload().GetBody()) != "c2" {
				return fmt.Errorf("unexpected request: %q", req.GetPayload().GetBody())
			}
			// Set trailer
			stream.SetTrailer(metadata.Pairs("test-trailer", "original"))
			return nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
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
	// 1. Premature Trailer() call before any Recv should return nil.
	if got := stream.Trailer(); got != nil {
		t.Fatalf("Trailer() prematurely returned non-nil metadata: %v, want nil", got)
	}

	// Send request message c1
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c1")}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	// 2. Trailer() call before CloseSend/Recv returns EOF should return nil.
	if got := stream.Trailer(); got != nil {
		t.Fatalf("Trailer() prematurely returned non-nil metadata: %v, want nil", got)
	}

	// Recv s1 response message
	respMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}
	if got, want := string(respMsg.GetPayload().GetBody()), "s1"; got != want {
		t.Fatalf("got response %q, want %q", got, want)
	}

	// 3. Trailer() call before sending c2 (so server hasn't sent trailers) should return nil.
	if got := stream.Trailer(); got != nil {
		t.Fatalf("Trailer() prematurely returned non-nil metadata: %v, want nil", got)
	}

	// Send request message c2
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c2")}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}

	// Read until EOF
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Recv() failed: %v", err)
		}
	}

	// 3. Calling Trailer() after stream has finished should return the mutated trailers.
	got := stream.Trailer()
	if vals := got.Get("test-trailer"); len(vals) != 2 || vals[1] != "mutated-val" {
		t.Fatalf("Trailer() returned %v, want test-trailer containing mutated-val", got)
	}
}

// TestImmediateResponseTrailers tests the scenario where an immediate response
// is received during the trailers event phase. Verifies that the filter
// correctly sets the terminal status and merges any specified mutation headers
// into the stream trailers.
func (s) TestImmediateResponseTrailers(t *testing.T) {
	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		for {
			req, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			var resp *v3procservicepb.ProcessingResponse
			switch {
			case req.GetRequestHeaders() != nil:
				resp = requestHeadersResponse(nil, nil)
			case req.GetResponseHeaders() != nil:
				resp = responseHeadersResponse(nil, nil)
			case req.GetResponseTrailers() != nil:
				resp = &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_ImmediateResponse{
						ImmediateResponse: &v3procservicepb.ImmediateResponse{
							GrpcStatus: &v3procservicepb.GrpcStatus{
								Status: uint32(codes.PermissionDenied),
							},
							Details: "denied on trailers",
							Headers: &v3procservicepb.HeaderMutation{
								SetHeaders: []*v3corepb.HeaderValueOption{
									{
										Header: &v3corepb.HeaderValue{
											Key:   "test-trailer",
											Value: "mutated-immediate-val",
										},
										AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
									},
								},
							},
						},
					},
				}
			default:
				return fmt.Errorf("unexpected request: %v", req)
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
			if req.GetResponseTrailers() != nil {
				return nil
			}
		}
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Read c1
			_, err := stream.Recv()
			if err != nil {
				return err
			}
			// Send response message s1
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte("s1")},
			}); err != nil {
				return err
			}
			// Set trailer
			stream.SetTrailer(metadata.Pairs("test-trailer", "original"))
			return nil
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
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

	// Send request message c1
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c1")}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	// Recv s1 response message
	respMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}
	if got, want := string(respMsg.GetPayload().GetBody()), "s1"; got != want {
		t.Fatalf("got response %q, want %q", got, want)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Recv() failed: %v", err)
		}
		fmt.Println("received message:", req.GetPayload().GetBody())
	}
	// Trailer() should return the headers included in the ImmediateResponse
	gotTrailer := stream.Trailer()
	if got := gotTrailer.Get("test-trailer"); len(got) != 1 || got[0] != "mutated-immediate-val" {
		t.Fatalf("Trailer() returned %v, want test-trailer containing mutated-immediate-val", gotTrailer)
	}
}

// TestRequestAttributes tests the scenario where request_attributes are
// configured on the filter. Verifies that all requested attribute fields are
// correctly constructed and transmitted within the processing request sent to
// the external processor.
func (s) TestRequestAttributes(t *testing.T) {
	allAttrs := []string{
		"request.path",
		"request.url_path",
		"request.host",
		"request.scheme",
		"request.method",
		"request.headers",
		"request.referer",
		"request.useragent",
		"request.time",
		"request.id",
		"request.protocol",
		"request.query",
	}

	lisAddr, _ := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		attrs := req.GetAttributes()
		if attrs == nil {
			return fmt.Errorf("expected non-nil attributes in ProcessingRequest")
		}
		reqStruct, ok := attrs["envoy.filters.http.ext_proc"]
		if !ok {
			return fmt.Errorf("missing key 'envoy.filters.http.ext_proc' in attributes map")
		}

		// Verify that set attributes ARE present.
		fields := reqStruct.GetFields()

		// Verify that unset attributes (scheme, time, protocol) are NOT set!
		unsetAttrs := []string{
			"request.scheme",
			"request.time",
			"request.protocol",
		}
		for _, attr := range unsetAttrs {
			if _, exists := fields[attr]; exists {
				return fmt.Errorf("expected unset attribute %q to NOT be set, but found: %v", attr, fields[attr])
			}
		}

		// Verify specific field values
		if got, want := fields["request.method"].GetStringValue(), "POST"; got != want {
			return fmt.Errorf("request.method = %q, want %q", got, want)
		}
		if got, want := fields["request.path"].GetStringValue(), "/grpc.testing.TestService/EmptyCall"; got != want {
			return fmt.Errorf("request.path = %q, want %q", got, want)
		}
		if got, want := fields["request.referer"].GetStringValue(), "http://example.com"; got != want {
			return fmt.Errorf("request.referer = %q, want %q", got, want)
		}
		if got, want := fields["request.useragent"].GetStringValue(), "test-user-agent"; got != want {
			return fmt.Errorf("request.useragent = %q, want %q", got, want)
		}
		if got, want := fields["request.id"].GetStringValue(), "req-12345"; got != want {
			return fmt.Errorf("request.id = %q, want %q", got, want)
		}
		if got, want := fields["request.host"].GetStringValue(), "test-service"; got != want {
			return fmt.Errorf("request.host = %q, want %q", got, want)
		}
		if got, want := fields["request.query"].GetStringValue(), ""; got != want {
			return fmt.Errorf("request.query = %q, want %q", got, want)
		}

		resp := requestHeadersResponse(nil, nil)
		return stream.Send(resp)
	})

	stub := stubserver.StartTestService(t, nil)
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		RequestAttributes: allAttrs,
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseHeaderMode: v3procfilterpb.ProcessingMode_SKIP,
		},
	}, stub.Address)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	callCtx := metadata.AppendToOutgoingContext(ctx, "referer", "http://example.com", "user-agent", "test-user-agent", "x-request-id", "req-12345")
	if _, err := client.EmptyCall(callCtx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// TestResponsePhaseValidationFailureDeny tests various server-side response
// validation failures during the response phase when failure_mode_allow is
// false, verifying that the RPC fails.
func (s) TestResponsePhaseValidationFailureDeny(t *testing.T) {
	tests := []struct {
		name            string
		processFunc     func(v3procservicepb.ExternalProcessor_ProcessServer) error
		wantMsgContains string
	}{
		{
			name: "OutOfOrderResponse",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Receive RequestHeaders and respond correctly
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("expected RequestHeaders, got %v", req)
				}
				if err := stream.Send(requestHeadersResponse(nil, nil)); err != nil {
					return err
				}
				var respHeadersReq, respBodyReq, reqBodyReq *v3procservicepb.ProcessingRequest
				r, err := stream.Recv()
				if err != nil {
					return err
				}
				if r.GetRequestBody() != nil {
					reqBodyReq = r
				}
				// Send request body back.
				if err := stream.Send(requestBodyResponse(reqBodyReq.GetRequestBody().GetBody())); err != nil {
					return err
				}

				// Receive the next requests (ResponseHeaders and ResponseBody).
				for i := 0; i < 2; i++ {
					r, err := stream.Recv()
					if err != nil {
						return err
					}
					switch {
					case r.GetResponseHeaders() != nil:
						respHeadersReq = r
					case r.GetResponseBody() != nil:
						respBodyReq = r
					}
					if respHeadersReq != nil && respBodyReq != nil {
						break
					}
				}
				if respBodyReq == nil {
					return fmt.Errorf("missing ResponseBody request")
				}

				// Respond to ResponseBody FIRST (before responding to ResponseHeaders)
				// This violates response ordering (ResponseHeaders was queued before
				// ResponseBody).
				if err := stream.Send(responseBodyResponse(respBodyReq.GetResponseBody().GetBody())); err != nil {
					return err
				}
				return nil
			},
		},
		{
			name: "ResponseBodyEndOfStream",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Receive RequestHeaders and respond correctly
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("Expected RequestHeaders, got %v", req)
				}
				if err := stream.Send(requestHeadersResponse(nil, nil)); err != nil {
					return err
				}

				reqBodyReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if reqBodyReq.GetRequestBody() == nil {
					return fmt.Errorf("Expected RequestBody, got %v", reqBodyReq)
				}
				// Send request body back.
				if err := stream.Send(requestBodyResponse(reqBodyReq.GetRequestBody().GetBody())); err != nil {
					return err
				}

				// Receive ResponseHeaders first.
				respHeadersReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if respHeadersReq.GetResponseHeaders() == nil {
					return fmt.Errorf("Expected ResponseHeaders, got %v", respHeadersReq)
				}
				if err := stream.Send(responseHeadersResponse(nil, nil)); err != nil {
					return err
				}

				// Receive ResponseBody next.
				respBodyReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if respBodyReq.GetResponseBody() == nil {
					return fmt.Errorf("Expected ResponseBody, got %v", respBodyReq)
				}

				// Respond to ResponseBody but with EndOfStream set to true.
				bodyBytes := respBodyReq.GetResponseBody().GetBody()
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_ResponseBody{
						ResponseBody: &v3procservicepb.BodyResponse{
							Response: &v3procservicepb.CommonResponse{
								Status: v3procservicepb.CommonResponse_CONTINUE,
								BodyMutation: &v3procservicepb.BodyMutation{
									Mutation: &v3procservicepb.BodyMutation_StreamedResponse{
										StreamedResponse: &v3procservicepb.StreamedBodyResponse{
											Body:        bodyBytes,
											EndOfStream: true,
										},
									},
								},
							},
						},
					},
				}
				return stream.Send(resp)
			},
		},
		{
			name: "UnexpectedResponseHeaderStatus",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Receive RequestHeaders and respond correctly
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("Expected RequestHeaders, got %v", req)
				}
				if err := stream.Send(requestHeadersResponse(nil, nil)); err != nil {
					return err
				}

				reqBodyReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if reqBodyReq.GetRequestBody() == nil {
					return fmt.Errorf("Expected RequestBody, got %v", reqBodyReq)
				}
				if err := stream.Send(requestBodyResponse(reqBodyReq.GetRequestBody().GetBody())); err != nil {
					return err
				}

				// Receive ResponseHeaders
				respHeadersReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if respHeadersReq.GetResponseHeaders() == nil {
					return fmt.Errorf("Expected ResponseHeaders, got %v", respHeadersReq)
				}

				// Respond to ResponseHeaders with status: CONTINUE_AND_REPLACE!
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_ResponseHeaders{
						ResponseHeaders: &v3procservicepb.HeadersResponse{
							Response: &v3procservicepb.CommonResponse{
								Status: v3procservicepb.CommonResponse_CONTINUE_AND_REPLACE,
							},
						},
					},
				}
				return stream.Send(resp)
			},
			wantMsgContains: "external processor returned unexpected status CONTINUE_AND_REPLACE for response headers, expected CONTINUE",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lisAddr, _ := startMockProcessor(t, tc.processFunc)

			stub := stubserver.StartTestService(t, &stubserver.StubServer{
				FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
					_, err := stream.Recv()
					if err != nil {
						return err
					}
					// Send response headers and body without waiting for client
					if err := stream.SendHeader(metadata.Pairs("backend-header", "present")); err != nil {
						return err
					}
					if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: &testpb.Payload{Body: []byte("s1")}}); err != nil {
						return err
					}
					return nil
				},
			})
			defer stub.Stop()

			cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
					RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
					ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
					ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
					ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
				},
				FailureModeAllow: false,
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

			if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte("c1")}}); err != nil {
				t.Fatalf("stream.Send(c1) failed: %v", err)
			}

			_, err = stream.Recv()
			if got, want := status.Code(err), codes.Internal; got != want {
				t.Fatalf("stream.Recv() returned status code %v, want %v", got, want)
			}
			if tc.wantMsgContains != "" && !strings.Contains(err.Error(), tc.wantMsgContains) {
				t.Fatalf("stream.Recv() returned err %v, want error containing %q", err, tc.wantMsgContains)
			}
		})
	}
}

// TestConcurrency executes a stream where client Recv is driven from the main
// goroutine, while Send, CloseSend, and context cancellation run in separate
// concurrent goroutines.
func (s) TestConcurrency(t *testing.T) {
	lisAddr, stopMockServer := startMockProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			var resp *v3procservicepb.ProcessingResponse
			switch {
			case req.GetRequestHeaders() != nil:
				resp = requestHeadersResponse(nil, nil)
			case req.GetRequestBody() != nil:
				resp = requestBodyResponse(req.GetRequestBody().GetBody())
			default:
				resp = &v3procservicepb.ProcessingResponse{}
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
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

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_NONE,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
	}, stub.Address)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	// Send initial message to establish the stream and headers.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{
		Payload: &testpb.Payload{Body: []byte("c1")},
	}); err != nil {
		t.Fatalf("Send(c1) failed: %v", err)
	}

	// Receive the first response.
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("Recv() failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Send concurrently in a background goroutine.
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			if err := stream.Send(&testpb.StreamingOutputCallRequest{
				Payload: &testpb.Payload{Body: []byte(fmt.Sprintf("body-%d", j))},
			}); err != nil {
				break
			}
			if j == 10 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					stopMockServer()
				}()
			}
		}
		_ = stream.CloseSend()
	}()

	var recvErr error
	for {
		_, err := stream.Recv()
		if err != nil {
			recvErr = err
			break
		}
	}

	if recvErr == io.EOF {
		t.Fatalf("stream completed gracefully (EOF) but expected it to fail due to server shutdown")
	}
	if got, want := status.Code(recvErr), codes.Internal; got != want {
		t.Fatalf("stream failed with status code %v, want %v (error: %v)", got, want, recvErr)
	}
	wg.Wait()
}
