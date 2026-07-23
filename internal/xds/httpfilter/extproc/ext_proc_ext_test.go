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
	"google.golang.org/grpc/internal/grpcsync"
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
	"google.golang.org/protobuf/types/known/durationpb"

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

	reqBodyC1  = "c1"
	reqBodyC2  = "c2"
	respBodyS1 = "s1"
	respBodyS2 = "s2"
)

func parseGRPCServiceConfigForTesting(gs *v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
	if gs == nil {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("expected non-nil GrpcService")
	}
	gg := gs.GetGoogleGrpc()
	if gg == nil {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("expected non-nil GoogleGrpc")
	}
	target := gg.GetTargetUri()
	if target == "" {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("empty target_uri in GoogleGrpc")
	}
	return xdsresource.GRPCServiceConfig{
		TargetURI: target,
		Timeout:   gs.GetTimeout().AsDuration(),
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
	return requestBodyResponseWithEOS(body, false)
}

func requestBodyResponseWithEOS(body []byte, endOfStream bool) *v3procservicepb.ProcessingResponse {
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

type testExtProcServer struct {
	v3procservicepb.UnimplementedExternalProcessorServer
	processFunc func(v3procservicepb.ExternalProcessor_ProcessServer) error
}

func (s *testExtProcServer) Process(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
	if s.processFunc != nil {
		return s.processFunc(stream)
	}
	return nil
}

// startTestExtProcessor configures extproc environment variables and function
// hooks, starts a test external processor server, and registers cleanup. It
// takes processFunc to handle Process RPC streams and returns the server's
// listener address and a function to stop the server.
func startTestExtProcessor(t *testing.T, processFunc func(v3procservicepb.ExternalProcessor_ProcessServer) error) (string, func()) {
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
	extprocServer := grpc.NewServer()
	mockProc := &testExtProcServer{processFunc: processFunc}
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

	var timeout *durationpb.Duration
	if extProcConfig.GrpcService != nil {
		timeout = extProcConfig.GrpcService.GetTimeout()
	}
	extProcConfig.GrpcService = &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri: extProcAddr,
			},
		},
		Timeout: timeout,
	}

	hcm.HttpFilters = append([]*v3httppb.HttpFilter{e2e.HTTPFilter("extproc", extProcConfig)}, hcm.HttpFilters...)
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
// correctly routes headers and bodies to the processor, the processor returns
// instructions to apply mutations, and the client successfully completes a
// Unary RPC.
func (s) TestAllSendUnary(t *testing.T) {
	const (
		reqHeaderMutated    = "request-mutated"
		reqHeaderToRemove   = "request-to-remove"
		respHeaderMutated   = "response-mutated"
		respHeaderToRemove  = "response-to-remove"
		respTrailerMutated  = "trailer-mutated"
		respTrailerToRemove = "trailer-to-remove"
		reqBody             = "hello-request"
		reqBodyMutated      = "hello-request-mutated"
		respBody            = "hello-response"
		respBodyMutated     = "hello-response-mutated"
	)
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
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
				// Respond to request headers to mutate them by adding "request-mutated:
				// true" and removing "request-to-remove".
				resp = requestHeadersResponse(map[string]string{reqHeaderMutated: "true"}, []string{reqHeaderToRemove})
			case req.GetRequestBody() != nil:
				// For the request body, if this is an end-of-stream message, echo EOS;
				// otherwise verify and mutate the body.
				body := req.GetRequestBody().GetBody()
				if req.GetRequestBody().GetEndOfStreamWithoutMessage() || req.GetRequestBody().GetEndOfStream() {
					resp = requestBodyResponseWithEOS(body, true)
					break
				}
				reqMsg := &testpb.SimpleRequest{}
				if err := proto.Unmarshal(body, reqMsg); err != nil {
					return fmt.Errorf("failed to unmarshal request body: %v", err)
				}
				if bodyStr := string(reqMsg.GetPayload().GetBody()); bodyStr != reqBody {
					return fmt.Errorf("unexpected request body on processor: %q, want: %q", bodyStr, reqBody)
				}
				mutatedProto := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte(reqBodyMutated)}}
				mutatedBytes, err := proto.Marshal(mutatedProto)
				if err != nil {
					return fmt.Errorf("failed to marshal mutated request: %v", err)
				}
				resp = requestBodyResponse(mutatedBytes)
			case req.GetResponseHeaders() != nil:
				// Respond to response headers to mutate them by adding
				// "response-mutated: true" and removing "response-to-remove".
				resp = responseHeadersResponse(map[string]string{respHeaderMutated: "true"}, []string{respHeaderToRemove})
			case req.GetResponseBody() != nil:
				// For the response body, verify the payload, and mutate the body.
				respMsg := &testpb.SimpleResponse{}
				if err := proto.Unmarshal(req.GetResponseBody().GetBody(), respMsg); err != nil {
					return fmt.Errorf("failed to unmarshal response body: %v", err)
				}
				if body := string(respMsg.GetPayload().GetBody()); body != respBody {
					return fmt.Errorf("unexpected response body on processor: %q, want: %q", body, respBody)
				}
				mutatedProto := &testpb.SimpleResponse{Payload: &testpb.Payload{Body: []byte(respBodyMutated)}}
				mutatedBytes, err := proto.Marshal(mutatedProto)
				if err != nil {
					return fmt.Errorf("failed to marshal mutated response: %v", err)
				}
				resp = responseBodyResponse(mutatedBytes)
			case req.GetResponseTrailers() != nil:
				// Respond to response trailers to mutate them by adding
				// "trailer-mutated: true" and removing "trailer-to-remove".
				resp = responseTrailersResponse(map[string]string{respTrailerMutated: "true"}, []string{respTrailerToRemove})
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	// Start a test stub service (backend) that verifies the mutated request
	// metadata and body from the client/filter, and sends headers, trailers,
	// and a response body back.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, fmt.Errorf("server did not receive incoming metadata")
			}
			if vals := md.Get(reqHeaderMutated); len(vals) == 0 || vals[0] != "true" {
				return nil, fmt.Errorf("missing or invalid request-mutated header: %v", vals)
			}
			if vals := md.Get(reqHeaderToRemove); len(vals) > 0 {
				return nil, fmt.Errorf("request-to-remove header was not removed: %v", vals)
			}
			if body := string(in.GetPayload().GetBody()); body != reqBodyMutated {
				return nil, fmt.Errorf("unexpected request body: %q, want: %q", body, reqBodyMutated)
			}

			if err := grpc.SendHeader(ctx, metadata.Pairs(respHeaderToRemove, "true")); err != nil {
				return nil, err
			}
			if err := grpc.SetTrailer(ctx, metadata.Pairs(respTrailerToRemove, "true")); err != nil {
				return nil, err
			}

			return &testpb.SimpleResponse{Payload: &testpb.Payload{Body: []byte(respBody)}}, nil
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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)

	// Send a Unary RPC from the client with "hello-request" and header
	// "request-to-remove", and verify that the final received response body is
	// "hello-response-mutated".
	reqMsg := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte(reqBody)}}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(reqHeaderToRemove, "true"))

	resp, err := client.UnaryCall(ctx, reqMsg)
	if err != nil {
		t.Fatalf("UnaryCall() failed: %v", err)
	}
	if body := string(resp.GetPayload().GetBody()); body != respBodyMutated {
		t.Fatalf("UnaryCall() returned payload: %s, want: %s", body, respBodyMutated)
	}
}

// TestStreamingModifications tests the scenario where the ExtProc filter is
// configured with SEND/GRPC processing modes for a bidirectional streaming RPC.
// Verifies that the client correctly routes headers and bodies to the
// processor, the server receives the mutated requests, and the client receives
// the correctly mutated responses back, even when the processor changes the
// response count.
func (s) TestStreamingModifications(t *testing.T) {
	const (
		reqHeaderModified   = "req-header-modified"
		respHeaderModified  = "resp-header-modified"
		respTrailerModified = "resp-trailer-modified"
		reqBodyC3           = "c3"
		reqBodyC4           = "c4"
	)
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			var resps []*v3procservicepb.ProcessingResponse
			switch {
			case req.GetRequestHeaders() != nil:
				// Return a response instructing the filter to mutate request headers by
				// adding "req-header-modified: true".
				resps = append(resps, requestHeadersResponse(map[string]string{reqHeaderModified: "true"}, nil))
			case req.GetRequestBody() != nil:
				body := req.GetRequestBody()
				// For the request body, if this is an end-of-stream message, echo EOS;
				// otherwise mutate the request body by adding `_req_body_modified` to
				// the body.
				if body.GetEndOfStreamWithoutMessage() || body.GetEndOfStream() {
					resps = append(resps, requestBodyResponseWithEOS(body.GetBody(), true))
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
					resps = append(resps, requestBodyResponse(mutated))
				}
			case req.GetResponseHeaders() != nil:
				// Return a response instructing the filter to mutate response headers
				// by adding "resp-header-modified: true".
				resps = append(resps, responseHeadersResponse(map[string]string{respHeaderModified: "true"}, nil))
			case req.GetResponseBody() != nil:
				// For the response body, return 2 body responses, each with a different
				// appended string, to simulate the server sending 2 messages for the
				// client's 1 message.
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
				resps = append(resps, responseBodyResponse(mutated1))

				respMsg.Payload.Body = append(origBody, []byte("_resp_body_modified_2")...)
				mutated2, err := proto.Marshal(respMsg)
				if err != nil {
					return err
				}
				resps = append(resps, responseBodyResponse(mutated2))
			case req.GetResponseTrailers() != nil:
				// Return a response instructing the filter to mutate response trailers
				// by adding "resp-trailer-modified: true".
				resps = append(resps, responseTrailersResponse(map[string]string{respTrailerModified: "true"}, nil))
			}
			for _, r := range resps {
				if err := stream.Send(r); err != nil {
					return err
				}
			}
		}
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Verify if the data plane server receives the mutated headers.
			md, ok := metadata.FromIncomingContext(stream.Context())
			if !ok {
				return fmt.Errorf("server did not receive incoming metadata")
			}
			hdr := md.Get(reqHeaderModified)
			if len(hdr) != 1 || hdr[0] != "true" {
				return fmt.Errorf("missing or invalid req-header-modified: %v", hdr)
			}

			// Explicitly send response headers to client.
			if err := stream.SendHeader(metadata.Pairs("resp-header-from-server", "present")); err != nil {
				return err
			}

			var msgCount int
			for {
				in, err := stream.Recv()
				// Check the message count once we receive io.EOF.
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				msgCount++
				wantBody := fmt.Sprintf("c%d_req_body_modified", msgCount)
				if string(in.GetPayload().GetBody()) != wantBody {
					return fmt.Errorf("server received unexpected message body: %s, want: %s", string(in.GetPayload().GetBody()), wantBody)
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
		t.Fatalf("Failed to dial: %v", err)
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
		[]byte(reqBodyC1),
		[]byte(reqBodyC2),
		[]byte(reqBodyC3),
		[]byte(reqBodyC4),
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

		// Verify we receive exactly 2 messages for each client request because the
		// processor server responds with 2 messages for every server message.
		for i := 1; i <= 2; i++ {
			resp, err := stream.Recv()
			if err != nil {
				t.Fatalf("stream.Recv() failed: %v", err)
			}
			wantBody := fmt.Sprintf("%s_req_body_modified_s1_resp_body_modified_%d", string(msg), i)
			if string(resp.GetPayload().GetBody()) != wantBody {
				t.Fatalf("stream.Recv() returned payload: %s, want: %s", resp.GetPayload().GetBody(), wantBody)
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
	gotHdr := headerMetadata.Get(respHeaderModified)
	if len(gotHdr) != 1 || gotHdr[0] != "true" {
		t.Fatalf("Client received mutated-resp-header = %v, want [true]", gotHdr)
	}

	// Verify mutated response trailers..
	trailerMetadata := stream.Trailer()
	gotTrailers := trailerMetadata.Get(respTrailerModified)
	if len(gotTrailers) != 1 || gotTrailers[0] != "true" {
		t.Fatalf("Client received resp-trailer-modified = %v, want [true]", gotTrailers)
	}
}

// TestProtocolConfigInFirstMessage tests the scenario where multiple processing
// requests are sent over an active stream. Verifies that the first
// ProcessingRequest sent to the processor server contains a valid
// ProtocolConfig populated with the current ProcessingMode, and subsequent
// requests do not.
func (s) TestProtocolConfigInFirstMessage(t *testing.T) {
	var receivedCall atomic.Bool
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			// For the first message, verify that the protocol config is present and
			// matches the configured processing modes.
			if receivedCall.CompareAndSwap(false, true) {
				pConfig := req.GetProtocolConfig()
				if pConfig == nil {
					return fmt.Errorf("proc server got nil in first request, want ProtocolConfig")
				}
				wantConfig := &v3procservicepb.ProtocolConfiguration{
					RequestBodyMode:  v3procfilterpb.ProcessingMode_GRPC,
					ResponseBodyMode: v3procfilterpb.ProcessingMode_NONE,
				}
				if !proto.Equal(pConfig, wantConfig) {
					return fmt.Errorf("proc server got protocol config %v, want %v", pConfig, wantConfig)
				}
			} else {
				// For all subsequent messages, verify that the protocol config is not
				// populated.
				if req.GetProtocolConfig() != nil {
					return fmt.Errorf("subsequent request unexpectedly had ProtocolConfig populated")
				}
			}

			var resp *v3procservicepb.ProcessingResponse
			switch {
			case req.GetRequestHeaders() != nil:
				// Send a response for request header with no mutations.
				resp = requestHeadersResponse(nil, nil)
			case req.GetRequestBody() != nil:
				body := req.GetRequestBody()
				if body.GetEndOfStreamWithoutMessage() || body.GetEndOfStream() {
					resp = requestBodyResponseWithEOS(body.GetBody(), true)
					break
				}
				// Echo the request body back as is.
				resp = requestBodyResponse(body.GetBody())
			case req.GetResponseHeaders() != nil:
				// Send a response for response header with no mutations.
				resp = responseHeadersResponse(nil, nil)
			case req.GetResponseBody() != nil:
				return fmt.Errorf("unexpectedly received ResponseBody in proc server because response body mode was set to skip")
			case req.GetResponseTrailers() != nil:
				// Send a response for response trailers with no mutations.
				resp = responseTrailersResponse(nil, nil)
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	// Backend stub service echoes request payload.
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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	// Send UnaryCall and verify processor received at least one request.
	reqMsg := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("hello-extproc")}}
	if _, err := client.UnaryCall(ctx, reqMsg); err != nil {
		t.Fatalf("UnaryCall() failed: %v", err)
	}

	if !receivedCall.Load() {
		t.Fatal("No requests received by the mock processor")
	}
}

// TestWaitForDataplane tests the scenario where an outbound RPC is initiated
// before the external processor confirms header mutations. Verifies that
// outbound events do not reach the backend until the processor responds to the
// request headers.
func (s) TestWaitForDataplane(t *testing.T) {
	unblockHeaders := make(chan struct{})

	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
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
				// For the request body, echo the payload back while preserving the
				// end-of-stream flag.
				body := req.GetRequestBody()
				if body.GetEndOfStreamWithoutMessage() || body.GetEndOfStream() {
					resp = requestBodyResponseWithEOS(body.GetBody(), body.EndOfStream)
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

	// Backend stub records when handler is invoked and counts received messages.
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
		t.Fatalf("Failed to dial: %v", err)
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
	const respHeaderModified = "resp-header-modified"

	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if req.GetResponseHeaders() == nil {
			return fmt.Errorf("proc server got %v, want ResponseHeaders message", req)
		}
		// When the trailers-only response headers arrive, verify that
		// end-of-stream is set to true and return a response instructing the
		// filter to add "resp-header-modified: true".
		respHeaders := req.GetResponseHeaders()
		if !respHeaders.GetEndOfStream() {
			return fmt.Errorf("proc server got EndOfStream=false, want true for Trailers-Only response headers")
		}
		resp := responseHeadersResponse(map[string]string{respHeaderModified: "true"}, nil)
		return stream.Send(resp)
	})
	// Backend stub service immediately returns an Aborted error to trigger
	// Trailers-Only.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Wait for client request message before failing.
			if _, err := stream.Recv(); err != nil {
				return err
			}
			// Return abort error immediately to trigger Trailers-Only.
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
		t.Fatalf("Failed to dial: %v", err)
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
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	// Recv should return Aborted (trailers-only)
	_, err = stream.Recv()
	if status.Code(err) != codes.Aborted {
		t.Fatalf("stream.Recv() returned error: %v, want Aborted", err)
	}

	// Verify that calling Header() on a trailers-only stream returns nil, nil to
	// be consistent with non-extproc streams.
	headerMetadata, err := stream.Header()
	if err != nil || headerMetadata != nil {
		t.Fatalf("stream.Header() = (%v, %v), want (nil, nil)", headerMetadata, err)
	}

	// Verify mutated response trailers.
	trailerMetadata := stream.Trailer()
	gotTrailers := trailerMetadata.Get(respHeaderModified)
	if len(gotTrailers) != 1 || gotTrailers[0] != "true" {
		t.Fatalf("Client received resp-header-modified = %v, want [true]", gotTrailers)
	}
}

// TestDraining tests the scenario where the processor server signals
// RequestDrain: true. Verifies that the filter correctly drains any pending
// messages and then transitions to bypass mode, causing all subsequent client
// messages and server responses to bypass the processor.
func (s) TestDraining(t *testing.T) {
	const reqBodyC1Mutated = "c1_mutated"
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive the first client message c1.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		body := req.GetRequestBody()
		if body == nil {
			return fmt.Errorf("proc server got %v, want RequestBody", req)
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
		return fmt.Errorf("proc server got %v from Recv after RequestDrain, want io.EOF", err)
	})

	// Start a test stub service.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Receive first client message. Verify that it is the mutated message.
			in1, err := stream.Recv()
			if err != nil {
				return err
			}
			if got, want := string(in1.GetPayload().GetBody()), reqBodyC1Mutated; got != want {
				return status.Errorf(codes.FailedPrecondition, "got body %q, want %q", got, want)
			}

			// Send the server message s1. This should bypass the processor as we have
			// set RequestDrain: true.
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte(respBodyS1)},
			}); err != nil {
				return err
			}

			// Receive the second client message c2 and verify that it is not mutated.
			in2, err := stream.Recv()
			if err != nil {
				return err
			}
			if got, want := string(in2.GetPayload().GetBody()), reqBodyC2; got != want {
				return status.Errorf(codes.FailedPrecondition, "got body %q, want %q", got, want)
			}

			// Send the second server message s2. This should bypass the processor as
			// we have set RequestDrain: true.
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte(respBodyS2)},
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
		t.Fatalf("Failed to dial: %v", err)
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
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send(c1) failed: %v", err)
	}

	// Receive server response s1 and verify that it is not mutated.
	resp1, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(s1) failed: %v", err)
	}
	if got, want := string(resp1.GetPayload().GetBody()), respBodyS1; got != want {
		t.Fatalf("Got response %q, want %q", got, want)
	}

	// Send second request message c2.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC2)}}); err != nil {
		t.Fatalf("stream.Send(c2) failed: %v", err)
	}

	// Receive second server response s2.
	resp2, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(s2) failed: %v", err)
	}
	if got, want := string(resp2.GetPayload().GetBody()), respBodyS2; got != want {
		t.Fatalf("Got response %q, want %q", got, want)
	}
}

// TestImmediateResponse tests filter behavior on receiving an ImmediateResponse
// under different configurations.
func (s) TestImmediateResponse(t *testing.T) {
	const simulatedImmediateResp = "simulated immediate response"
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
			wantMsgContains:          simulatedImmediateResp,
		},
		{
			name:                     "EnabledWithFailureModeAllow",
			disableImmediateResponse: false,
			failureModeAllow:         true,
			wantCode:                 codes.Aborted,
			wantMsgContains:          simulatedImmediateResp,
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
			lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("proc server got %v, want request headers", req)
				}
				// When the request headers arrive, respond immediately with an aborted
				// status and simulated details.
				resp := &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_ImmediateResponse{
						ImmediateResponse: &v3procservicepb.ImmediateResponse{
							GrpcStatus: &v3procservicepb.GrpcStatus{
								Status: uint32(codes.Aborted),
							},
							Details: simulatedImmediateResp,
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
				t.Fatalf("Failed to dial: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client := testgrpc.NewTestServiceClient(cc)
			_, err = client.EmptyCall(ctx, &testpb.Empty{})
			if tc.wantCode == codes.OK {
				if err != nil {
					t.Fatalf("EmptyCall() failed unexpectedly %v", err)
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
			wantMsgContains: "abrupt stream failure",
		},
		{
			name: "UnexpectedHeaderStatus",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("proc server got %v, want request headers", req)
				}
				// When the request headers arrive, respond with an invalid status of
				// continue and replace.
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
					return fmt.Errorf("proc server got %v, want request headers", req)
				}
				// When the request headers arrive, respond with an unexpected request
				// body message having the compressed flag set.
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
			lisAddr, _ := startTestExtProcessor(t, tc.processFunc)

			stub := stubserver.StartTestService(t, nil)
			defer stub.Stop()

			cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
				},
				FailureModeAllow: false,
			}, stub.Address)
			if err != nil {
				t.Fatalf("Failed to dial: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client := testgrpc.NewTestServiceClient(cc)
			// Verify EmptyCall fails with status Internal and expected error message.
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
			name: "GracefulStreamClosure",
			processFunc: func(v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Immediately close processor stream with io.EOF without replying.
				return nil
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
					return fmt.Errorf("proc server got %v, want request headers", req)
				}
				// When the request headers arrive, respond with an invalid status of
				// continue and replace.
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
					return fmt.Errorf("proc server got %v, want request headers", req)
				}
				// When the request headers arrive, respond with an unexpected request
				// body message having the compressed flag set.
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
			lisAddr, _ := startTestExtProcessor(t, tc.processFunc)

			stub := stubserver.StartTestService(t, nil)
			defer stub.Stop()

			cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
				},
				FailureModeAllow: true,
			}, stub.Address)
			if err != nil {
				t.Fatalf("Failed to dial: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client := testgrpc.NewTestServiceClient(cc)
			// Verify EmptyCall succeeds when failure_mode_allow is true.
			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Fatalf("EmptyCall() failed unexpectedly when failure_mode_allow is true: %v", err)
			}
		})
	}
}

// TestGracefulStreamClosureDeny verifies that when the external processor
// stream ends gracefully during the request header phase while
// failure_mode_allow is false, the filter unblocks via bypass and the RPC
// cleanly continues and succeeds.
func (s) TestGracefulStreamClosureDeny(t *testing.T) {
	lisAddr, _ := startTestExtProcessor(t, func(v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Immediately close the processor stream completely gracefully without
		// replying. Verifies that when the client receives io.EOF right during
		// initial header processing, bypass is triggered and the client RPC
		// continues regardless of failure_mode_allow.
		return nil
	})

	stub := stubserver.StartTestService(t, nil)
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
		},
		FailureModeAllow: false,
	}, stub.Address)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	// Verify that even with failure_mode_allow: false, graceful stream closure
	// allows EmptyCall to succeed.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed with error %v right when failure_mode_allow is false after graceful stream closure, expected clean continuation", err)
	}
}

// TestStreamFailureBodyPhaseAllow tests the scenario where the external
// processor stream fails abruptly during the request body phase while
// failure_mode_allow is true. Verifies that the RPC fails since
// failure_mode_allow is not respected once body has been sent to the external
// processor.
func (s) TestStreamFailureBodyPhaseAllow(t *testing.T) {
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive request headers and respond with no mutations.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("proc server got %v, want request headers", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// Receive request message and echo back.
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		bodyBytes := req.GetRequestBody().GetBody()
		reqMsg := new(testpb.StreamingOutputCallRequest)
		if err := proto.Unmarshal(bodyBytes, reqMsg); err != nil {
			return fmt.Errorf("failed to unmarshal request body: %v", err)
		}
		if got, want := string(reqMsg.GetPayload().GetBody()), reqBodyC1; got != want {
			return fmt.Errorf("got body %q, want %q", got, want)
		}
		resp = requestBodyResponse(bodyBytes)
		if err := stream.Send(resp); err != nil {
			return err
		}
		// Immediately after replying to body "c1", fail abruptly with Unavailable
		// error.
		return status.Error(codes.Unavailable, "abrupt stream failure in body phase")
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Receive c1.
			in1, err := stream.Recv()
			if err != nil {
				return err
			}
			if got, want := string(in1.GetPayload().GetBody()), reqBodyC1; got != want {
				return status.Errorf(codes.FailedPrecondition, "got body %q, want %q", got, want)
			}

			// Send s1.
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte(respBodyS1)},
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
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
			ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
		FailureModeAllow: true,
	}, stub.Address)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
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
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
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
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive request headers and respond with no mutations.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("proc server got %v, want request headers", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}
		// Immediately fail abruptly with Unavailable error after sending header
		// response.
		return status.Error(codes.Unavailable, "abrupt stream failure after headers")
	})

	// Backend stub simply echoes payload it receives.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
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

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:   v3procfilterpb.ProcessingMode_NONE,
			ResponseBodyMode:  v3procfilterpb.ProcessingMode_NONE,
		},
		FailureModeAllow: true,
	}, stub.Address)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
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
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send(c1) failed: %v", err)
	}

	// Receive echo of c1.
	resp1, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(c1) failed: %v", err)
	}
	if got, want := string(resp1.GetPayload().GetBody()), reqBodyC1; got != want {
		t.Fatalf("Got response %q, want %q", got, want)
	}

	// Send c2.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC2)}}); err != nil {
		t.Fatalf("stream.Send(c2) failed: %v", err)
	}

	// Receive echo of c2.
	resp2, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(c2) failed: %v", err)
	}
	if got, want := string(resp2.GetPayload().GetBody()), reqBodyC2; got != want {
		t.Fatalf("Got response %q, want %q", got, want)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}

	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() returned error: %v, want io.EOF", err)
	}
}

// TestStreamFailureTrailerPhaseAllow tests the scenario where the external
// processor stream fails abruptly during the response trailer phase while
// failure_mode_allow is true and body mode are not send. Verifies that calling
// Trailer() does not hang and instead returns the unmutated trailers directly
// from the data plane stream.
func (s) TestStreamFailureTrailerPhaseAllow(t *testing.T) {
	const (
		testTrailerKey = "test-trailer"
		originalVal    = "original"
	)
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive request headers and respond with no mutations.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("proc server got %v, want request headers", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// When the response trailers request arrives, fail abruptly by returning an
		// unavailable error.
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		if req.GetResponseTrailers() == nil {
			return fmt.Errorf("proc server got %v, want response trailers", req)
		}
		return status.Error(codes.Unavailable, "abrupt stream failure in trailer phase")
	})

	// Backend stub sets trailer "test-trailer: original" and echoes payload.
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			in, err := stream.Recv()
			if err != nil {
				return err
			}
			stream.SetTrailer(metadata.Pairs(testTrailerKey, originalVal))
			return stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: in.GetPayload(),
			})
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:     v3procfilterpb.ProcessingMode_NONE,
			ResponseBodyMode:    v3procfilterpb.ProcessingMode_NONE,
			ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
		},
		FailureModeAllow: true,
	}, stub.Address)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send(c1) failed: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv(c1) failed: %v", err)
	}
	if got, want := string(resp.GetPayload().GetBody()), reqBodyC1; got != want {
		t.Fatalf("Got response %q, want %q", got, want)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}

	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() returned error: %v, want io.EOF", err)
	}

	// Calling Trailer() should not hang and must return the unmutated trailer.
	got := stream.Trailer()
	if vals := got.Get(testTrailerKey); len(vals) != 1 || vals[0] != originalVal {
		t.Fatalf("Trailer() returned %v, want test-trailer containing 'original'", got)
	}
}

// TestStreamFailureResponseHeaderPhaseAllow tests the scenario where the
// external processor stream fails abruptly during the response header phase
// while failure_mode_allow is true. Verifies that calling Header() does not
// hang and instead returns the unmutated response headers amd messages from the
// data plane stream.
func (s) TestStreamFailureResponseHeaderPhaseAllow(t *testing.T) {
	const (
		testHeaderKey = "test-header"
		originalVal   = "original"
	)
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive request headers and respond with no mutations.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("proc server got %v, want request headers", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// When the response headers request arrives, fail abruptly by returning an
		// unavailable error.
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		if req.GetResponseHeaders() == nil {
			return fmt.Errorf("proc server got %v, want response headers", req)
		}
		return status.Error(codes.Unavailable, "abrupt stream failure in response header phase")
	})

	// Backend stub sends response header "test-header: original".
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			stream.SendHeader(metadata.Pairs("test-header", "original"))
			return stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte("s1")},
			})
		},
	})
	defer stub.Stop()

	cc, err := setupTestClient(t, lisAddr, &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:    v3procfilterpb.ProcessingMode_NONE,
			ResponseBodyMode:   v3procfilterpb.ProcessingMode_NONE,
			ResponseHeaderMode: v3procfilterpb.ProcessingMode_SEND,
		},
		FailureModeAllow: true,
	}, stub.Address)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send(c1) failed: %v", err)
	}

	// Calling Header() should return the unmutated response header.
	got, err := stream.Header()
	if err != nil {
		t.Fatalf("Header() failed: %v", err)
	}
	if vals := got.Get(testHeaderKey); len(vals) != 1 || vals[0] != originalVal {
		t.Fatalf("Header() returned %v, want test-header containing 'original'", got)
	}
	m, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}
	if got, want := string(m.GetPayload().GetBody()), "s1"; got != want {
		t.Fatalf("Got response %q, want %q", got, want)
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
				// Receive request headers and respond with no mutations.
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("proc server got %v, want request headers", req)
				}
				resp := requestHeadersResponse(nil, nil)
				if err := stream.Send(resp); err != nil {
					return err
				}

				// When the request body message arrives, fail abruptly by returning an
				// unavailable error.
				if _, err = stream.Recv(); err != nil {
					return err
				}
				return status.Error(codes.Unavailable, "abrupt stream failure in body phase")
			},
			wantMsgContains: "abrupt stream failure in body phase",
		},
		{
			name: "GrpcMessageCompressedUnsupported",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Receive request headers and respond with no mutations.
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("proc server got %v, want request headers", req)
				}
				resp := requestHeadersResponse(nil, nil)
				if err := stream.Send(resp); err != nil {
					return err
				}

				// For the request body, respond with the unsupported compressed message
				// flag set to true.
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
			lisAddr, _ := startTestExtProcessor(t, tc.processFunc)

			// Backend stub handler simply calls Recv() and returns any error
			// encountered.
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
				t.Fatalf("Failed to dial: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client := testgrpc.NewTestServiceClient(cc)
			stream, err := client.FullDuplexCall(ctx)
			if err != nil {
				t.Fatalf("FullDuplexCall() failed: %v", err)
			}

			if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
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

// TestUnaryFailureBodyPhaseDeny tests the scenario where the external processor
// stream fails abruptly during the request body phase of a Unary RPC while
// failure_mode_allow is false. Verifies that the Unary RPC fails with status
// code Internal.
func (s) TestUnaryFailureBodyPhaseDeny(t *testing.T) {
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive request headers and respond with no mutations.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("proc server got %v, want request headers", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// When the request body message arrives, fail abruptly by returning an
		// unavailable error.
		if _, err = stream.Recv(); err != nil {
			return err
		}

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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	reqMsg := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}
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
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive request headers and return with no mutations.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("proc server got %v, want request headers", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// When the first request body message arrives, return a response with
		// request drain set to true.
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

		// Continually receive any remaining in-flight body requests until EOF is
		// reached and echo them back.
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if req.GetRequestBody() != nil {
				resp := requestBodyResponse(req.GetRequestBody().GetBody())
				if err := stream.Send(resp); err != nil {
					return err
				}
			}
			if req.GetResponseBody() != nil {
				resp := responseBodyResponse(req.GetRequestBody().GetBody())
				if err := stream.Send(resp); err != nil {
					return err
				}
			}
		}
	})

	const numMessages = 50
	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			for i := 1; i <= numMessages; i++ {
				in, err := stream.Recv()
				if err != nil {
					return fmt.Errorf("backend Recv(%d) failed: %v", i, err)
				}
				want := fmt.Sprintf("c%d", i)
				if got := string(in.GetPayload().GetBody()); got != want {
					return status.Errorf(codes.FailedPrecondition, "got body %q, want %q", got, want)
				}

				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: fmt.Appendf(nil, "s%d", i)},
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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	for i := 1; i <= numMessages; i++ {
		if err := stream.Send(&testpb.StreamingOutputCallRequest{
			Payload: &testpb.Payload{Body: fmt.Appendf(nil, "c%d", i)},
		}); err != nil {
			t.Fatalf("Client Send(%d) failed: %v", i, err)
		}

		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("Client Recv(%d) failed: %v", i, err)
		}
		want := fmt.Sprintf("s%d", i)
		if got := string(resp.GetPayload().GetBody()); got != want {
			t.Fatalf("Client got response %q, want %q", got, want)
		}
	}
}

// TestClientTrailer tests the scenario where client stream trailers are
// inspected both early and post-stream. Verifies that calling Trailer()
// prematurely returns nil, and calling it after the stream has finished
// correctly triggers processor trailer mutation and returns the final mutated
// metadata.
func (s) TestClientTrailer(t *testing.T) {
	const (
		testTrailerKey = "test-trailer"
		originalVal    = "original"
		mutatedVal     = "mutated-val"
	)
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// Receive request header and respond with no mutations.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("proc server got %v, want request headers", req)
		}
		resp := requestHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// Receive response header and respond with no mutations.
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		if req.GetResponseHeaders() == nil {
			return fmt.Errorf("proc server got %v, want response headers", req)
		}
		resp = responseHeadersResponse(nil, nil)
		if err := stream.Send(resp); err != nil {
			return err
		}

		// When the response trailers arrive, return a response instructing the
		// filter to set the trailer "test-trailer: mutated-val".
		req, err = stream.Recv()
		if err != nil {
			return err
		}
		if req.GetResponseTrailers() == nil {
			return fmt.Errorf("proc server got %v, want response trailers", req)
		}
		resp = &v3procservicepb.ProcessingResponse{
			Response: &v3procservicepb.ProcessingResponse_ResponseTrailers{
				ResponseTrailers: &v3procservicepb.TrailersResponse{
					HeaderMutation: &v3procservicepb.HeaderMutation{
						SetHeaders: []*v3corepb.HeaderValueOption{
							{
								Header: &v3corepb.HeaderValue{
									Key:   testTrailerKey,
									Value: mutatedVal,
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
			// Read c1.
			req, err := stream.Recv()
			if err != nil {
				return err
			}
			if string(req.GetPayload().GetBody()) != reqBodyC1 {
				return fmt.Errorf("unexpected request: %q", req.GetPayload().GetBody())
			}
			// Send response message s1.
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte(respBodyS1)},
			}); err != nil {
				return err
			}
			// Read c2.
			req, err = stream.Recv()
			if err != nil {
				return err
			}
			if string(req.GetPayload().GetBody()) != reqBodyC2 {
				return fmt.Errorf("unexpected request: %q", req.GetPayload().GetBody())
			}
			// Set trailer.
			stream.SetTrailer(metadata.Pairs(testTrailerKey, originalVal))
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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}
	// Premature Trailer() call before any Recv should return nil.
	if got := stream.Trailer(); got != nil {
		t.Fatalf("Trailer() prematurely returned non-nil metadata: %v, want nil", got)
	}

	// Send request message c1.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	// Trailer() call before CloseSend/Recv returns EOF should return nil.
	if got := stream.Trailer(); got != nil {
		t.Fatalf("Trailer() prematurely returned non-nil metadata: %v, want nil", got)
	}

	// Recv s1 response message.
	respMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}
	if got, want := string(respMsg.GetPayload().GetBody()), respBodyS1; got != want {
		t.Fatalf("Got response %q, want %q", got, want)
	}

	// Trailer() call before sending c2 (so server hasn't sent trailers) should
	// return nil.
	if got := stream.Trailer(); got != nil {
		t.Fatalf("Trailer() prematurely returned non-nil metadata: %v, want nil", got)
	}

	// Send request message c2.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC2)}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}

	// Read until EOF.
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Recv() failed: %v", err)
		}
	}

	// Calling Trailer() after stream has finished should return the mutated
	// trailers.
	got := stream.Trailer()
	if vals := got.Get(testTrailerKey); len(vals) != 2 || vals[1] != mutatedVal {
		t.Fatalf("Trailer() returned %v, want test-trailer containing mutated-val", got)
	}
}

// TestImmediateResponseTrailers tests the scenario where an immediate response
// is received during the trailers event phase. Verifies that the filter
// correctly sets the terminal status and merges any specified mutation headers
// into the stream trailers.
func (s) TestImmediateResponseTrailers(t *testing.T) {
	const (
		testTrailerKey      = "test-trailer"
		originalVal         = "original"
		immediateMutatedVal = "mutated-immediate-val"
	)
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
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
				// Send a response for request header with no mutations.
				resp = requestHeadersResponse(nil, nil)
			case req.GetResponseHeaders() != nil:
				// Send a response for response header with no mutations.
				resp = responseHeadersResponse(nil, nil)
			case req.GetResponseTrailers() != nil:
				// When the response trailers arrive, return an immediate response with
				// permission denied status and a header mutation instructing the filter
				// to set "test-trailer: mutated-immediate-val".
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
											Key:   testTrailerKey,
											Value: immediateMutatedVal,
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
			// Read c1.
			if _, err := stream.Recv(); err != nil {
				return err
			}
			// Send response message s1.
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: []byte(respBodyS1)},
			}); err != nil {
				return err
			}
			// Set trailer.
			stream.SetTrailer(metadata.Pairs(testTrailerKey, originalVal))
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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	// Send request message c1.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	// Recv s1 response message.
	respMsg, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}
	if got, want := string(respMsg.GetPayload().GetBody()), respBodyS1; got != want {
		t.Fatalf("Got response %q, want %q", got, want)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}
	for {
		_, err := stream.Recv()
		if status.Code(err) == codes.PermissionDenied {
			break
		}
		if err != nil {
			t.Fatalf("stream.Recv() failed: %v", err)
		}
	}
	// Trailer() should return the headers included in the ImmediateResponse.
	gotTrailer := stream.Trailer()
	if got := gotTrailer.Get(testTrailerKey); len(got) != 1 || got[0] != immediateMutatedVal {
		t.Fatalf("Trailer() returned %v, want test-trailer containing mutated-immediate-val", gotTrailer)
	}
}

// TestImmediateResponseBody tests the scenario where an immediate response
// is received mid-stream during message body processing. Verifies that the
// filter correctly terminates the RPC with the specified status and error
// details.
func (s) TestImmediateResponseBody(t *testing.T) {
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
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
				// Send a response for request header with no mutations.
				resp = requestHeadersResponse(nil, nil)
			case req.GetRequestBody() != nil:
				// When the request body arrives, return an immediate response with
				// permission denied status and a header mutation instructing the filter
				// to set "test-trailer: mutated-body-val".
				resp = &v3procservicepb.ProcessingResponse{
					Response: &v3procservicepb.ProcessingResponse_ImmediateResponse{
						ImmediateResponse: &v3procservicepb.ImmediateResponse{
							GrpcStatus: &v3procservicepb.GrpcStatus{
								Status: uint32(codes.PermissionDenied),
							},
							Details: "denied on body",
						},
					},
				}
			default:
				return fmt.Errorf("unexpected request: %v", req)
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// Wait for client request message before returning.
			if _, err := stream.Recv(); err != nil {
				return err
			}
			return nil
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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	// Send request message c1 to trigger body processing.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}

	// Calling Recv() should fail with PermissionDenied and details from the
	// ImmediateResponse.
	_, err = stream.Recv()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("stream.Recv() returned error: %v, want PermissionDenied", err)
	}
	wantErr := "denied on body"
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("stream.Recv() returned error %v, want it to contain %q", err, wantErr)
	}
}

// TestRequestAttributes tests the scenario where request attributes are
// configured on the filter. Verifies that all requested attribute fields are
// correctly constructed and transmitted within the processing request sent to
// the external processor.
func (s) TestRequestAttributes(t *testing.T) {
	const (
		serviceName   = "test-service"
		refererAttr   = "http://example.com"
		userAgentAttr = "test-user-agent"
		reqIDAttr     = "req-12345"
		methodAttr    = "POST"
		pathAttr      = "/grpc.testing.TestService/EmptyCall"
	)
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

	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		// When the request headers arrive, inspect the processing request
		// attributes map.
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		// Verify that the attribute map contains the key
		// "envoy.filters.http.ext_proc" and check its fields.
		attrs := req.GetAttributes()
		if attrs == nil {
			return fmt.Errorf("got nil attributes in ProcessingRequest, want non-nil")
		}
		reqStruct, ok := attrs["envoy.filters.http.ext_proc"]
		if !ok {
			return fmt.Errorf("missing key 'envoy.filters.http.ext_proc' in attributes map")
		}

		fields := reqStruct.GetFields()

		// Verify that unset attributes such as scheme, time, and protocol are not
		// present in the fields.
		unsetAttrs := []string{
			"request.scheme",
			"request.time",
			"request.protocol",
		}
		for _, attr := range unsetAttrs {
			if _, exists := fields[attr]; exists {
				return fmt.Errorf("got attribute %q set to %v, want unset", attr, fields[attr])
			}
		}

		// Verify specific field values.
		if got, want := fields["request.method"].GetStringValue(), methodAttr; got != want {
			return fmt.Errorf("request.method = %q, want %q", got, want)
		}
		if got, want := fields["request.path"].GetStringValue(), pathAttr; got != want {
			return fmt.Errorf("request.path = %q, want %q", got, want)
		}
		if got, want := fields["request.referer"].GetStringValue(), refererAttr; got != want {
			return fmt.Errorf("request.referer = %q, want %q", got, want)
		}
		if got, want := fields["request.useragent"].GetStringValue(), userAgentAttr; got != want {
			return fmt.Errorf("request.useragent = %q, want %q", got, want)
		}
		if got, want := fields["request.id"].GetStringValue(), reqIDAttr; got != want {
			return fmt.Errorf("request.id = %q, want %q", got, want)
		}
		if got, want := fields["request.host"].GetStringValue(), serviceName; got != want {
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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	callCtx := metadata.AppendToOutgoingContext(ctx, "referer", refererAttr, "user-agent", userAgentAttr, "x-request-id", reqIDAttr)
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
				// Receive RequestHeaders and respond correctly.
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("proc server got %v, want RequestHeaders", req)
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
			wantMsgContains: "external processor sent response body before sending response headers",
		},
		{
			name: "ResponseBodyEndOfStream",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Receive RequestHeaders and respond with no mutations.
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("proc server got %v, want RequestHeaders", req)
				}
				if err := stream.Send(requestHeadersResponse(nil, nil)); err != nil {
					return err
				}

				reqBodyReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if reqBodyReq.GetRequestBody() == nil {
					return fmt.Errorf("proc server got %v, want RequestBody", reqBodyReq)
				}
				// Send request body back.
				if err := stream.Send(requestBodyResponse(reqBodyReq.GetRequestBody().GetBody())); err != nil {
					return err
				}

				// Receive and echo the ResponseHeaders.
				respHeadersReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if respHeadersReq.GetResponseHeaders() == nil {
					return fmt.Errorf("proc server got %v, want ResponseHeaders", respHeadersReq)
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
					return fmt.Errorf("proc server got %v, want ResponseBody", respBodyReq)
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
			wantMsgContains: "external processor unexpectedly set end of stream in response body mutation",
		},
		{
			name: "UnexpectedResponseHeaderStatus",
			processFunc: func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
				// Respond to request headers with no mutations.
				req, err := stream.Recv()
				if err != nil {
					return err
				}
				if req.GetRequestHeaders() == nil {
					return fmt.Errorf("proc server got %v, want RequestHeaders", req)
				}
				if err := stream.Send(requestHeadersResponse(nil, nil)); err != nil {
					return err
				}

				// Receive and echo the request body.
				reqBodyReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if reqBodyReq.GetRequestBody() == nil {
					return fmt.Errorf("proc server got %v, want RequestBody", reqBodyReq)
				}
				if err := stream.Send(requestBodyResponse(reqBodyReq.GetRequestBody().GetBody())); err != nil {
					return err
				}

				// Receive ResponseHeaders.
				respHeadersReq, err := stream.Recv()
				if err != nil {
					return err
				}
				if respHeadersReq.GetResponseHeaders() == nil {
					return fmt.Errorf("proc server got %v, want ResponseHeaders", respHeadersReq)
				}

				// Respond to ResponseHeaders with unsupported status
				// CONTINUE_AND_REPLACE.
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
			lisAddr, _ := startTestExtProcessor(t, tc.processFunc)

			// Backend stub sends response header "backend-header: present" and
			// response body "s1".
			stub := stubserver.StartTestService(t, &stubserver.StubServer{
				FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
					if _, err := stream.Recv(); err != nil {
						return err
					}
					// Send response headers and body without waiting for client.
					if err := stream.SendHeader(metadata.Pairs("backend-header", "present")); err != nil {
						return err
					}
					if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: &testpb.Payload{Body: []byte(respBodyS1)}}); err != nil {
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
				t.Fatalf("Failed to dial: %v", err)
			}
			defer cc.Close()
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			client := testgrpc.NewTestServiceClient(cc)
			stream, err := client.FullDuplexCall(ctx)
			if err != nil {
				t.Fatalf("FullDuplexCall() failed: %v", err)
			}

			if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
				t.Fatalf("stream.Send(c1) failed: %v", err)
			}

			// Verify Recv fails with status Internal and error message matching
			// expected validation failure.
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
	lisAddr, stopMockServer := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
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
				// Send a response for request header with no mutations.
				resp = requestHeadersResponse(nil, nil)
			case req.GetRequestBody() != nil:
				// For the request body, echo the payload back unchanged.
				resp = requestBodyResponse(req.GetRequestBody().GetBody())
			default:
				resp = &v3procservicepb.ProcessingResponse{}
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	})

	// Backend stub continually receives messages and echoes payloads back.
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
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("Failed to start stream: %v", err)
	}

	// Send initial message to establish the stream and headers.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{
		Payload: &testpb.Payload{Body: []byte(reqBodyC1)},
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
				Payload: &testpb.Payload{Body: fmt.Appendf(nil, "body-%d", j)},
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
		if _, err := stream.Recv(); err != nil {
			recvErr = err
			break
		}
	}

	if recvErr == io.EOF {
		t.Fatalf("Stream completed gracefully (EOF) but expected it to fail due to server shutdown")
	}
	if got, want := status.Code(recvErr), codes.Internal; got != want {
		t.Fatalf("Stream failed with status code %v, want %v (error: %v)", got, want, recvErr)
	}
	wg.Wait()
}

// TestStreamTimeoutBodyPhaseAllow tests the scenario where the external
// processor becomes unresponsive mid-stream during message body processing and
// the configured stream timeout is reached. Verifies that even when
// failure_mode_allow is true, the RPC terminates with an Internal error once
// body processing has started.
func (s) TestStreamTimeoutBodyPhaseAllow(t *testing.T) {
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		for {
			req, err := stream.Recv()
			if err != nil {
				return err
			}
			if req.GetRequestHeaders() != nil {
				if err := stream.Send(requestHeadersResponse(nil, nil)); err != nil {
					return err
				}
			} else if req.GetRequestBody() != nil {
				// Block until the stream context is canceled or times out.
				<-stream.Context().Done()
				return stream.Context().Err()
			}
		}
	})

	stub := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			in, err := stream.Recv()
			if err != nil {
				return err
			}
			return stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: in.GetPayload(),
			})
		},
	})
	defer stub.Stop()

	extProcConfig := &v3procfilterpb.ExternalProcessor{
		GrpcService: &v3corepb.GrpcService{
			Timeout: durationpb.New(defaultTestShortTimeout),
		},
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
			RequestBodyMode:   v3procfilterpb.ProcessingMode_GRPC,
		},
		FailureModeAllow: true,
	}
	cc, err := setupTestClient(t, lisAddr, extProcConfig, stub.Address)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}

	if err := stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}}); err != nil {
		t.Fatalf("stream.Send(c1) failed: %v", err)
	}

	_, err = stream.Recv()
	if status.Code(err) != codes.Internal {
		t.Fatalf("stream.Recv() returned error: %v, want Internal", err)
	}
	if !strings.Contains(err.Error(), "DeadlineExceeded") {
		t.Fatalf("stream.Recv() returned error %v, want it to contain 'DeadlineExceeded'", err)
	}
}

// TestDataplaneStreamCreationFailure tests the scenario where the external
// processor handshake (sending and receiving request headers) succeeds, but the
// subsequent attempt to connect to the backend (data plane stream) fails.
// Verifies that the data plane RPC fails and the external processor stream is
// cleanly terminated.
func (s) TestDataplaneStreamCreationFailure(t *testing.T) {
	procStreamClosed := grpcsync.NewEvent()
	lisAddr, _ := startTestExtProcessor(t, func(stream v3procservicepb.ExternalProcessor_ProcessServer) error {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetRequestHeaders() == nil {
			return fmt.Errorf("proc server got %v, want request headers", req)
		}
		if err := stream.Send(requestHeadersResponse(nil, nil)); err != nil {
			return err
		}
		// Subsequent Recv should fail when the data plane stream creation fails and
		// cancels the filter context.
		if _, err = stream.Recv(); err != nil {
			procStreamClosed.Fire()
			return nil
		}
		return fmt.Errorf("got nil error, want processor stream to close upon backend failure")
	})

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	closedBackendAddr := lis.Addr().String()
	lis.Close()

	extProcConfig := &v3procfilterpb.ExternalProcessor{
		ProcessingMode: &v3procfilterpb.ProcessingMode{
			RequestHeaderMode: v3procfilterpb.ProcessingMode_SEND,
		},
		FailureModeAllow: true,
	}
	cc, err := setupTestClient(t, lisAddr, extProcConfig, closedBackendAddr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}
	stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: []byte(reqBodyC1)}})
	_, err = stream.Recv()
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("stream.Recv() returned error: %v, want Unavailable", err)
	}

	select {
	case <-procStreamClosed.Done():
	case <-ctx.Done():
		t.Fatal("Timeout waiting for processor stream to close after backend failure")
	}
}
