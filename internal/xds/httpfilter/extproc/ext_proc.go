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

// Package extproc implements the Envoy external processing HTTP filter.
package extproc

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/optional"
	resolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/httpfilter"
	iextproc "google.golang.org/grpc/internal/xds/httpfilter/extproc/internal"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	v3procfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	v3procservicegrpc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	v3procservicepb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

func init() {
	if envconfig.XDSClientExtProcEnabled {
		httpfilter.Register(builder{})
	}
	iextproc.RegisterForTesting = func() {
		httpfilter.Register(builder{})
	}
	iextproc.UnregisterForTesting = func() {
		for _, typeURL := range builder.TypeURLs(builder{}) {
			httpfilter.UnregisterForTesting(typeURL)
		}
	}
}

var metadataFromOutgoingContextRaw = internal.FromOutgoingContextRaw.(func(context.Context) (metadata.MD, [][]string, bool))

const defaultDeferredCloseTimeout = 5 * time.Second

type builder struct{}

func (builder) TypeURLs() []string {
	return []string{
		"type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor",
		"type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExtProcPerRoute",
	}
}

// validateBodyProcessingMode ensures that the body processing mode is either
// NONE or GRPC. Also ensures that if response body mode is GRPC then response
// trailer mode must be SEND.
func validateBodyProcessingMode(mode *v3procfilterpb.ProcessingMode) error {
	if m := mode.GetRequestBodyMode(); m != v3procfilterpb.ProcessingMode_NONE && m != v3procfilterpb.ProcessingMode_GRPC {
		return fmt.Errorf("extproc: invalid request body mode %v: want %q or %q", m, "NONE", "GRPC")
	}
	if m := mode.GetResponseBodyMode(); m != v3procfilterpb.ProcessingMode_NONE && m != v3procfilterpb.ProcessingMode_GRPC {
		return fmt.Errorf("extproc: invalid response body mode %v: want %q or %q", m, "NONE", "GRPC")
	}
	if mode.GetResponseBodyMode() == v3procfilterpb.ProcessingMode_GRPC && mode.GetResponseTrailerMode() != v3procfilterpb.ProcessingMode_SEND {
		return fmt.Errorf("extproc: invalid response trailer mode %v: must be %q when response body mode is %q", mode.GetResponseTrailerMode(), "SEND", "GRPC")
	}
	return nil
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extproc: error parsing config %v: unknown type %T, want *anypb.Any", cfg, cfg)
	}
	msg := new(v3procfilterpb.ExternalProcessor)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extproc: failed to unmarshal config %v: %v", cfg, err)
	}
	if msg.GetProcessingMode() == nil {
		return nil, fmt.Errorf("extproc: missing processing_mode in config %v", cfg)
	}
	if err := validateBodyProcessingMode(msg.GetProcessingMode()); err != nil {
		return nil, err
	}

	if msg.GetGrpcService() == nil {
		return nil, fmt.Errorf("extproc: empty grpc_service provided in config %v", cfg)
	}
	server, err := iextproc.ParseGRPCServiceConfig(msg.GetGrpcService())
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to parse grpc_service %v", err)
	}

	mutationRules, err := httpfilter.HeaderMutationRulesFromProto(msg.GetMutationRules())
	if err != nil {
		return nil, err
	}

	var allowedHeaders, disallowedHeaders []matcher.StringMatcher
	if allowed := msg.GetForwardRules().GetAllowedHeaders(); allowed != nil {
		allowedHeaders, err = httpfilter.ConvertStringMatchers(allowed.GetPatterns())
		if err != nil {
			return nil, err
		}
	}

	if disallowed := msg.GetForwardRules().GetDisallowedHeaders(); disallowed != nil {
		disallowedHeaders, err = httpfilter.ConvertStringMatchers(disallowed.GetPatterns())
		if err != nil {
			return nil, err
		}
	}

	deferredCloseTimeout := defaultDeferredCloseTimeout
	if msg.GetDeferredCloseTimeout() != nil {
		deferredCloseTimeout = msg.GetDeferredCloseTimeout().AsDuration()
	}

	return baseConfig{
		processingModes:          processingModesFromProto(msg.GetProcessingMode()),
		requestAttributes:        msg.GetRequestAttributes(),
		responseAttributes:       msg.GetResponseAttributes(),
		disableImmediateResponse: msg.GetDisableImmediateResponse(),
		observabilityMode:        msg.GetObservabilityMode(),
		failureModeAllow:         msg.GetFailureModeAllow(),
		server:                   server,
		mutationRules:            mutationRules,
		allowedHeaders:           allowedHeaders,
		disallowedHeaders:        disallowedHeaders,
		deferredCloseTimeout:     deferredCloseTimeout,
	}, nil
}

func (builder) ParseFilterConfigOverride(ov proto.Message) (httpfilter.FilterConfig, error) {
	m, ok := ov.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extproc: error parsing override %v: unknown type %T, want *anypb.Any", ov, ov)
	}
	msg := new(v3procfilterpb.ExtProcPerRoute)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extproc: failed to unmarshal override %v: %v", ov, err)
	}
	override := msg.GetOverrides()

	var processingModesOpt optional.Optional[processingModes]
	if pm := override.GetProcessingMode(); pm != nil {
		if err := validateBodyProcessingMode(pm); err != nil {
			return nil, err
		}
		processingModesOpt = optional.New(processingModesFromProto(pm))
	}

	var serverOpt optional.Optional[xdsresource.GRPCServiceConfig]
	if override.GetGrpcService() != nil {
		server, err := iextproc.ParseGRPCServiceConfig(override.GetGrpcService())
		if err != nil {
			return nil, fmt.Errorf("extproc: failed to parse grpc_service: %v", err)
		}
		serverOpt = optional.New(server)
	}

	var failureModeAllowOpt optional.Optional[bool]
	if override.GetFailureModeAllow() != nil {
		failureModeAllowOpt = optional.New(override.GetFailureModeAllow().GetValue())
	}

	return overrideConfig{
		server:             serverOpt,
		processingModes:    processingModesOpt,
		failureModeAllow:   failureModeAllowOpt,
		requestAttributes:  override.GetRequestAttributes(),
		responseAttributes: override.GetResponseAttributes(),
	}, nil
}

func (builder) IsTerminal() bool {
	return false
}

func (builder) BuildClientFilter(httpfilter.ClientFilterOptions) httpfilter.ClientFilter {
	return clientFilter{}
}

var _ httpfilter.ClientFilterBuilder = builder{}

type clientFilter struct{}

func (clientFilter) Close() {}

func (clientFilter) BuildClientInterceptor(base, override httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
	b, ok := base.(baseConfig)
	if !ok {
		return nil, fmt.Errorf("extproc: incorrect config type provided (%T): %v", base, base)
	}

	var ov overrideConfig
	if override != nil {
		ov, ok = override.(overrideConfig)
		if !ok {
			return nil, fmt.Errorf("extproc: incorrect override config type provided (%T): %v", override, override)
		}
	}

	config := newInterceptorConfig(b, ov)

	// Create a channel to the external processor server.
	cc, cancel, err := iextproc.CreateExtProcChannel(config.server)
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to create channel to the external processor server %q: %v", config.server.TargetURI, err)
	}
	return &clientInterceptor{
		config:      config,
		procClient:  v3procservicegrpc.NewExternalProcessorClient(cc),
		closeClient: cancel,
	}, nil
}

type clientInterceptor struct {
	config      baseConfig
	procClient  v3procservicegrpc.ExternalProcessorClient
	closeClient func() error
}

func (i *clientInterceptor) Close() {
	i.closeClient()
}

func (i *clientInterceptor) NewStream(ctx context.Context, ri resolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	// Create a cancelable context to cancel the dataplane stream and close any
	// goroutines in case of error.
	cancelCtx, cancel := context.WithCancel(ctx)
	cs := &clientStream{
		config:                   i.config,
		procStreamFailed:         grpcsync.NewEvent(),
		procStreamBypass:         grpcsync.NewEvent(),
		mutatedReqBuffer:         buffer.NewUnbounded[*v3procservicepb.StreamedBodyResponse](),
		mutatedRespBuffer:        buffer.NewUnbounded[*v3procservicepb.StreamedBodyResponse](),
		responseHeadersReady:     grpcsync.NewEvent(),
		responseTrailerReady:     grpcsync.NewEvent(),
		dataplaneSetup:           make(chan struct{}),
		ctx:                      cancelCtx,
		cancel:                   cancel,
		procSendCh:               make(chan *v3procservicepb.ProcessingRequest),
		requestForwardLoopDoneCh: make(chan struct{}),
		procRecvLoopDone:         grpcsync.NewEvent(),
	}

	// Create a new context for the RPC to the external processor server. This
	// context has a deadline of the timeout specified in the config, if present.
	// It also contains the initial metadata specified in the config. Create a new
	// child context for the external processor stream to be able to cancel it
	// independently.
	procCtx, cancel := context.WithCancel(cs.ctx)
	if i.config.server.Timeout != 0 {
		procCtx, cancel = context.WithTimeout(cs.ctx, i.config.server.Timeout)
	}
	cs.procCancel = cancel
	procCtx = metadata.NewOutgoingContext(procCtx, i.config.server.InitialMetadata)

	// In the ClientStream API, outgoing headers are sent immediately when the
	// stream is created. Because we need to mutate these headers before they go
	// over the wire, we must establish the ext_proc stream now rather than
	// deferring it.
	var err error
	cs.procStream, err = i.procClient.Process(procCtx)
	if err != nil {
		return cs.handleProcStreamInitError(fmt.Errorf("failed to create a stream to external processor server: %v", err), newStream, opts)
	}

	// Build request attributes upfront to capture the original un-mutated headers
	// and RPC info, and avoid adding protobuf allocation overhead to the critical
	// RPC data path.
	outgoingMD, added, _ := metadataFromOutgoingContextRaw(ctx)
	cs.reqAttrs = constructRequestAttributes(ri, outgoingMD, added, i.config.requestAttributes)

	// When request header processing is "Send", defer creating the dataplane
	// stream until the external processor responds, because gRPC transmits
	// outgoing headers immediately upon stream creation and we must apply any
	// header mutations first. If header processing is skipped, create the
	// dataplane stream immediately.
	if i.config.processingModes.requestHeaderMode == modeSend {
		headerReq := cs.newProcessingRequest(true)
		headerReq.Request = &v3procservicepb.ProcessingRequest_RequestHeaders{
			RequestHeaders: &v3procservicepb.HttpHeaders{
				Headers: httpfilter.ConstructHeaderMap(outgoingMD, added, i.config.allowedHeaders, i.config.disallowedHeaders),
			},
		}
		if err = cs.procStream.Send(headerReq); err != nil {
			if err == io.EOF {
				_, err = cs.procStream.Recv()
			}
			return cs.handleProcStreamInitError(fmt.Errorf("failed to send client headers to external processor server: %v", err), newStream, opts)
		}
	} else {
		if err = cs.createDataplaneStream(cs.ctx, newStream, opts); err != nil {
			return nil, err
		}
	}

	// Start a background loop for sending messages to the external processor
	// server. Use a single goroutine to ensure no concurrent sends.
	go cs.sendToProcServerLoop()

	// Start a background loop to receive messages from the external processor
	// server and send them on the dataplane stream in either direction.
	go cs.recvFromProcServerLoop(newStream, opts)

	return cs, nil
}

// clientStream implements resolver.ClientStream to coordinate bidirectional
// message exchanges between the application client, the external processor, and
// the backend dataplane.
type clientStream struct {
	// ctx is stored to allow blocking ClientStream interface methods (which do
	// not accept context parameters) to respect context cancellation/timeout and
	// retrieve ctx.Err() for returning the correct gRPC status error. This
	// context is directly derived from the dataplane RPC's context.
	ctx        context.Context
	cancel     context.CancelFunc
	procCancel context.CancelFunc
	config     baseConfig

	dataplaneStream  grpc.ClientStream                                 // underlying gRPC stream to the backend
	procStream       v3procservicegrpc.ExternalProcessor_ProcessClient // bidirectional stream to the external processor
	procStreamFailed *grpcsync.Event                                   // fired when external processor stream has closed and RPC should be failed
	procStreamBypass *grpcsync.Event                                   // fired when the external processor stream should be bypassed or drained
	procStreamClosed atomic.Bool                                       // ensures the stream closure logic is executed exactly once
	procStreamErr    atomic.Value                                      // holds the terminal error causing the external processor stream to fail

	reqAttrs             map[string]*structpb.Struct                              // attributes to be sent to proc server with client message
	reqAttrsSent         atomic.Bool                                              // tracks whether the first client message has been sent to ensure request attributes are sent only once
	protocolConfigSent   atomic.Bool                                              // tracks whether the protocol configuration has been sent to the processor
	ignoreFailureMode    atomic.Bool                                              // tracks whether the failureModeAllow setting should be ignored (e.g. after sending body messages)
	discardRequests      atomic.Bool                                              // set when ext_proc server signals end_of_stream to stop client sends
	mutatedReqBuffer     *buffer.Unbounded[*v3procservicepb.StreamedBodyResponse] // buffers mutated request body messages from the ext_proc server
	reqForwardingStarted bool                                                     // tracks whether request forwarding loop to the dataplane has started
	procSendCh           chan *v3procservicepb.ProcessingRequest                  // serializes writes to the external processor stream to ensure thread-safety

	responseHeader        metadata.MD                                              // stores headers received from the dataplane stream
	responseHeaderOnce    atomic.Bool                                              // ensures response headers are forwarded to the external processor only once
	responseHeadersReady  *grpcsync.Event                                          // signals that response headers are ready for client
	responseHeaderSent    atomic.Bool                                              // tracks whether response headers have been dispatched to external processor
	responseTrailers      metadata.MD                                              // stores trailers received from the dataplane stream
	responseTrailerOnce   atomic.Bool                                              // ensures response trailers are forwarded to the external processor only once
	responseTrailerReady  *grpcsync.Event                                          // signals that response trailers are ready for client
	trailerSent           atomic.Bool                                              // tracks whether response trailers have been dispatched
	trailersOnly          bool                                                     // tracks whether the backend response is trailers-only
	trailerErr            atomic.Value                                             // holds any terminal status returned upon processing response trailers (e.g. ImmediateResponse)
	mutatedRespBuffer     *buffer.Unbounded[*v3procservicepb.StreamedBodyResponse] // buffers mutated response body messages from the ext_proc server
	responseDrained       atomic.Bool                                              // tracks whether all buffered response body messages from the ext_proc server have been drained
	dataplaneSetup        chan struct{}                                            // closed once dataplaneStream creation attempt is complete
	dataplaneCreationErr  error                                                    // stores the error from the dataplane stream creation attempt
	respForwardingStarted bool                                                     // tracks whether response forwarding loop to the external processor has started

	requestForwardLoopDoneCh chan struct{}   // closed when request forwarding loop finishes draining
	procRecvLoopDone         *grpcsync.Event // fires when external processor stream receive loop finishes
}

// newProcessingRequest creates a new ProcessingRequest with ObservabilityMode,
// ProtocolConfig, Attributes fields initialized.
func (cs *clientStream) newProcessingRequest(isClientMessage bool) *v3procservicepb.ProcessingRequest {
	req := &v3procservicepb.ProcessingRequest{
		ObservabilityMode: cs.config.observabilityMode,
	}

	if cs.protocolConfigSent.CompareAndSwap(false, true) {
		req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
			RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
			ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
		}
	}

	if isClientMessage && cs.reqAttrsSent.CompareAndSwap(false, true) {
		req.Attributes = cs.reqAttrs
	}
	return req
}

// Header returns the response headers received from the backend, potentially
// modified by the external processor if configured. It blocks until header
// processing is complete, or returns nil if the response is trailers-only.
func (cs *clientStream) Header() (metadata.MD, error) {
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		return nil, err
	}
	// Wait for the response headers to be modified.
	select {
	case <-cs.responseHeadersReady.Done():
		if cs.trailersOnly {
			return nil, nil
		}
		return cs.responseHeader, nil
	case <-cs.procStreamFailed.Done():
		return nil, cs.streamError()
	case <-cs.ctx.Done():
		return nil, cs.streamError()
	}
}

// Trailer returns the trailer metadata received from the server, potentially
// modified by the external processor if configured. It returns nil or empty map
// immediately if trailers are not yet received from the backend. If backend
// trailers are available, it blocks until any configured trailer processing by
// the external processor completes.
func (cs *clientStream) Trailer() metadata.MD {
	// If trailers are already modified and ready, return them immediately.
	if cs.responseTrailerReady.HasFired() {
		return cs.responseTrailers
	}
	// Checking if backend stream has trailers ready now.
	s, err := cs.waitForDataplaneStream(cs.ctx)
	// Since Trailer is not supposed to be a blocking function and should be
	// called after CloseRecv or after Recv() returns an error, it should have the
	// dataplane stream created. If it does not, then Trailer has been called
	// prematurely, which means we need to return nil and not block here.
	if err != nil {
		return nil
	}
	// If no backend trailers are ready yet, return nil to preserve standard
	// non-blocking behaviour.
	if len(s.Trailer()) == 0 {
		return nil
	}

	cs.initiateResponseTrailerProcessing()
	select {
	case <-cs.responseTrailerReady.Done():
		return cs.responseTrailers
	case <-cs.procStreamFailed.Done():
		return nil
	case <-cs.ctx.Done():
		return nil
	}
}

func (cs *clientStream) CloseSend() error {
	s, err := cs.bypassProcStreamForClientMsg()
	if err != nil {
		return err
	}
	if s != nil {
		return s.CloseSend()
	}
	// If external processor stream is active, client CLoseSend is sent to the
	// processor server as request message with `EndOfStreamWithoutMessage` set.
	req := cs.newProcessingRequest(true)
	req.Request = &v3procservicepb.ProcessingRequest_RequestBody{
		RequestBody: &v3procservicepb.HttpBody{
			EndOfStream:               true,
			EndOfStreamWithoutMessage: true,
		},
	}

	s, err = cs.sendClientReqToProcServer(req)
	if s != nil {
		return s.CloseSend()
	}
	return err
}

func (cs *clientStream) Context() context.Context {
	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return cs.ctx
	}
	return s.Context()
}

func (cs *clientStream) RecvMsg(m any) error {
	// Initiate response header processing because external processor requires the
	// events to be sent in the correct order, i.e. response header before
	// response message. And if Header() has not already been called, send the
	// response headers to external processor server first.
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		return err
	}

	// If all the responses from external processor server have been drained or if
	// the external processor is bypassed, or if the response body mode is skip,
	// then receive directly from the dataplane stream.
	if cs.responseDrained.Load() || (cs.procStreamBypass.HasFired() && !cs.respForwardingStarted) || cs.config.processingModes.responseBodyMode == modeSkip {
		return cs.recvFromDataplane(m)
	}

	msg, ok := m.(proto.Message)
	if !ok {
		return fmt.Errorf("extproc: response message does not implement proto.Message")
	}

	// Start the background receiving loop on the first RecvMsg call to capture
	// the type of message to be received.
	if !cs.respForwardingStarted {
		cs.respForwardingStarted = true
		go cs.responseForwardingToProcServerLoop(msg.ProtoReflect().Type())
	}

	// Pull response messages from mutatedRespBuffer.
	select {
	case streamedResp, ok := <-cs.mutatedRespBuffer.Get():
		cs.mutatedRespBuffer.Load()
		if !ok {
			// Closed channel implies that all messages from the external processor
			// have been received. Start receiving directly from dataplane stream.
			cs.responseDrained.Store(true)
			return cs.recvFromDataplane(m)
		}
		return proto.Unmarshal(streamedResp.GetBody(), msg)
	case <-cs.ctx.Done():
		return cs.streamError()
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	}
}

func (cs *clientStream) SendMsg(m any) error {
	s, err := cs.bypassProcStreamForClientMsg()
	if err != nil {
		return err
	}
	if s != nil {
		return s.SendMsg(m)
	}

	msg, ok := m.(proto.Message)
	if !ok {
		return status.Errorf(codes.Internal, "extproc: message does not implement proto.Message")
	}

	// Start request forwarding loop on the first send because we need the message
	// type to send the data to the dataplane server.
	if !cs.reqForwardingStarted {
		cs.reqForwardingStarted = true
		go cs.requestForwardingToDataplaneLoop(msg.ProtoReflect().Type())
	}

	// If the ExtProc server has already signaled end_of_stream, discard any
	// subsequent client messages.
	if cs.discardRequests.Load() {
		return nil
	}

	bodyBytes, err := proto.Marshal(msg)
	if err != nil {
		return status.Errorf(codes.Internal, "extproc: failed to marshal message: %v", err)
	}
	req := cs.newProcessingRequest(true)
	req.Request = &v3procservicepb.ProcessingRequest_RequestBody{
		RequestBody: &v3procservicepb.HttpBody{
			Body: bodyBytes,
		},
	}
	s, err = cs.sendClientReqToProcServer(req)
	if s != nil {
		return s.SendMsg(m)
	}
	return err
}

// recvFromDataplane receives directly from the dataplane stream. If the
// dataplane stream returns an error, it initiates and waits for response
// trailers.
func (cs *clientStream) recvFromDataplane(m any) error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}
	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return err
	}
	if err := s.RecvMsg(m); err != nil {
		// If RecvMsg returns error, fail the RPC in case external processor stream
		// has failed. Otherwise process the trailers.
		if cs.procStreamFailed.HasFired() {
			return cs.procStreamErr.Load().(error)
		}
		cs.initiateResponseTrailerProcessing()
		// Wait for trailer processing to be done to make sure we are able to return
		// correct error and fail RPC if the trailer processing fails. This needs to
		// be done because Trailer() does not return an error. TODO: Verify if the
		// wait is acceptable.
		return cs.waitForTrailerProcessing(err)
	}
	return nil
}

// sendClientReqToProcServer attempts to send the given request for client to
// server msg to the external processor. If the processor stream is bypassed
// while waiting, it blocks until all queued messages are forwarded, and returns
// the dataplane stream so the caller can send the message directly to the
// dataplane stream instead.
func (cs *clientStream) sendClientReqToProcServer(req *v3procservicepb.ProcessingRequest) (grpc.ClientStream, error) {
	// Ignoring the failureMode as we will be sending the request to proc server.
	if !cs.ignoreFailureMode.Load() {
		cs.ignoreFailureMode.Store(true)
	}

	select {
	case cs.procSendCh <- req:
		return nil, nil
	case <-cs.procStreamBypass.Done():
		if cs.reqForwardingStarted {
			// When the drain is triggered, wait for all the queued requests to be sent
			// to dataplane server before forwarding current request directly to
			// dataplane server.
			if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
				return nil, err
			}
		}
		return cs.waitForDataplaneStream(cs.ctx)
	case <-cs.procStreamFailed.Done():
		return nil, cs.streamError()
	case <-cs.ctx.Done():
		return nil, cs.streamError()
	}
}

// bypassProcStreamForClientMsg checks if the external processor should be
// bypassed for the current request message or CloseSend. If so, it returns the
// dataplane stream to be used directly. It blocks until all client messages
// received from the proc server are forwarded to ensure correct sequence. It
// also blocks to wait for stream creation if necessary. If the message needs to
// be sent to the external processor server, it returns (nil, nil).
func (cs *clientStream) bypassProcStreamForClientMsg() (grpc.ClientStream, error) {
	if cs.procStreamFailed.HasFired() {
		return nil, cs.procStreamErr.Load().(error)
	}

	bypass := cs.procStreamBypass.HasFired()
	if bypass && cs.reqForwardingStarted {
		// We started forwarding messages to the proc server and then it
		// initiated a drain. We must wait until all messages received from
		// the proc server are sent on the dataplane stream, before
		// unblocking the application's Send() or CloseSend(), to ensure
		// correct message sequencing.
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return nil, err
		}
	}

	if bypass || cs.config.processingModes.requestBodyMode == modeSkip {
		return cs.waitForDataplaneStream(cs.ctx)
	}

	return nil, nil // Do not bypass.
}

// responseForwardingToProcServerLoop continuously receives response messages
// from the underlying dataplane stream, marshals them, and forwards them to the
// external processor server via procSendCh.
func (cs *clientStream) responseForwardingToProcServerLoop(msgType protoreflect.MessageType) {
	defer func() {
		// Once the external processor server receive loop finishes, close the
		// buffer to signal end of processed responses.
		cs.waitChannel(cs.procRecvLoopDone.Done())
		cs.mutatedRespBuffer.Close()
	}()

	dataplaneStream, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}

	for {
		// If the processor stream has closed or the receive loop finishes, we can
		// stop receiving messages in the background and Recv should now directly be
		// called from the cs.RecvMsg() function.
		if cs.procStreamBypass.HasFired() || cs.procRecvLoopDone.HasFired() {
			return
		}

		newMsg := msgType.New().Interface()
		if err := dataplaneStream.RecvMsg(newMsg); err != nil {
			cs.initiateResponseTrailerProcessing()
			return
		}

		bodyBytes, err := proto.Marshal(newMsg)
		if err != nil {
			cs.failProcStream(err)
			return
		}

		req := cs.newProcessingRequest(false)
		req.Request = &v3procservicepb.ProcessingRequest_ResponseBody{
			ResponseBody: &v3procservicepb.HttpBody{
				Body: bodyBytes,
			},
		}

		if !cs.ignoreFailureMode.Load() {
			cs.ignoreFailureMode.Store(true)
		}

		select {
		case cs.procSendCh <- req:
		case <-cs.procStreamBypass.Done():
			// If drain is triggered, wait for external processor server receive loop
			// to finish before pushing this message on the mutatedRespBuffer
			// to ensure correct order of messages.
			if err := cs.waitChannel(cs.procRecvLoopDone.Done()); err != nil {
				return
			}
			resp := &v3procservicepb.StreamedBodyResponse{Body: bodyBytes}
			cs.mutatedRespBuffer.Put(resp)
			return
		case <-cs.procStreamFailed.Done():
			return
		case <-cs.ctx.Done():
			return
		}
	}
}

// requestForwardingToDataplaneLoop continuously consumes mutated request body
// from mutatedReqBuffer (populated by the external processor server),
// unmarshals them, and forwards the final messages to the backend server.
func (cs *clientStream) requestForwardingToDataplaneLoop(msgType protoreflect.MessageType) {
	defer close(cs.requestForwardLoopDoneCh)
	dataplaneStream, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}
	for {
		select {
		case streamedResp, ok := <-cs.mutatedReqBuffer.Get():
			if !ok {
				return
			}
			cs.mutatedReqBuffer.Load()

			// As per gRFC A93, ignore `end_of_stream_without_message` if
			// `end_of_stream` is false.
			if streamedResp.GetEndOfStream() && streamedResp.GetEndOfStreamWithoutMessage() {
				dataplaneStream.CloseSend()
				return
			}

			newMsg := msgType.New().Interface()
			if err := proto.Unmarshal(streamedResp.GetBody(), newMsg); err != nil {
				cs.failProcStream(err)
				return
			}
			if err := dataplaneStream.SendMsg(newMsg); err != nil {
				cs.cancelStream(err)
				return
			}

			if streamedResp.GetEndOfStream() {
				dataplaneStream.CloseSend()
				return
			}
		case <-cs.ctx.Done():
			return
		case <-cs.procStreamFailed.Done():
			return
		}
	}
}

// recvFromProcServerLoop continuously receives messages from the external
// processor server and redirects them according to the response type.
func (cs *clientStream) recvFromProcServerLoop(newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) {
	defer func() {
		cs.procRecvLoopDone.Fire()
		// Close mutatedReqBuffer to indicate completion of receiving
		// the mutated requests. Do not close mutatedResponseBuffer because we
		// might push the message that has been read when drain is triggered.
		cs.mutatedReqBuffer.Close()
	}()

	// If request header mode is send, the first response should be the mutation
	// for request header. Create the dataplane stream using the mutated header.
	if cs.config.processingModes.requestHeaderMode == modeSend {
		if !cs.processInitialHeaders(cs.ctx, newStream, opts) {
			return
		}
	}

	// If header mode is not send or if we receive the header and create the
	// dataplane stream, start receiving from the external processor server.
	for {
		resp, err := cs.procStream.Recv()
		if err != nil {
			cs.failProcStream(err)
			return
		}
		if resp.GetRequestDrain() {
			// Trigger the drain but continue receiving the drained messages until we
			// get io.EOF.
			cs.triggerBypass()
		}

		if resp.GetImmediateResponse() != nil {
			cs.handleImmediateResponse(resp.GetImmediateResponse(), newStream, opts)
			return
		}

		switch {
		case resp.GetRequestBody() != nil:
			if cs.config.processingModes.requestBodyMode == modeSkip {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent request body when request body processing is disabled"))
				return
			}

			streamedResp, ok := cs.validateBodyResponse(resp.GetRequestBody())
			if !ok {
				return
			}
			if streamedResp.GetEndOfStream() {
				cs.discardRequests.Store(true)
			}
			cs.mutatedReqBuffer.Put(streamedResp)

		case resp.GetResponseBody() != nil:
			if cs.config.processingModes.responseBodyMode == modeSkip {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent response body when response body processing is disabled"))
				return
			}

			// If response headers have been sent and mutated response headers have
			// not been received before receiving the response body message, fail the
			// RPC.
			if cs.config.processingModes.responseHeaderMode == modeSend && !cs.responseHeadersReady.HasFired() {
				cs.failProcStream(fmt.Errorf("external processor sent response body before sending response headers"))
				return
			}

			// If mutated response trailers have been received before receiving the
			// response body message, fail the RPC.
			if cs.config.processingModes.responseTrailerMode == modeSend && cs.responseTrailerReady.HasFired() {
				cs.failProcStream(fmt.Errorf("external processor sent response body after response trailers were already processed"))
				return
			}

			streamedResp, ok := cs.validateBodyResponse(resp.GetResponseBody())
			if !ok {
				return
			}
			if streamedResp.GetEndOfStream() {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly set end of stream in response body mutation"))
				return
			}
			cs.mutatedRespBuffer.Put(streamedResp)

		case resp.GetResponseHeaders() != nil:
			if cs.config.processingModes.responseHeaderMode == modeSkip {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent response headers when response header processing is disabled"))
				return
			}
			if !cs.responseHeaderSent.Load() {
				cs.failProcStream(fmt.Errorf("external processor sent response headers before response headers were sent to it"))
				return
			}
			if cs.responseHeadersReady.HasFired() {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent duplicate response headers after response headers were already processed"))
				return
			}

			header := resp.GetResponseHeaders()
			// Check if the status in the header response is CONTINUE; if not, fail
			// the stream.
			if status := header.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
				cs.failProcStream(fmt.Errorf("external processor returned unexpected status %v for response headers, expected %v", status, v3procservicepb.CommonResponse_CONTINUE))
				return
			}
			if err = cs.applyMutations(header.GetResponse().GetHeaderMutation(), cs.responseHeader); err != nil {
				cs.failProcStream(err)
				return
			}
			// Signal that the response header is modified and ready to be sent to the
			// client, so that if there is any buffered response body, it can be sent
			// after the header.
			cs.responseHeadersReady.Fire()

		case resp.GetResponseTrailers() != nil:
			if cs.config.processingModes.responseTrailerMode == modeSkip {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent response trailers when response trailer processing is disabled"))
				return
			}
			if !cs.trailerSent.Load() {
				cs.failProcStream(fmt.Errorf("external processor sent response trailers before response trailers were sent to it"))
				return
			}
			if cs.responseTrailerReady.HasFired() {
				cs.failProcStream(fmt.Errorf("external processor unexpectedly sent duplicate response trailers after response trailers were already processed"))
				return
			}
			trailer := resp.GetResponseTrailers()
			if err = cs.applyMutations(trailer.GetHeaderMutation(), cs.responseTrailers); err != nil {
				cs.failProcStream(err)
				return
			}
			// Signal that the response trailer is modified and ready to be sent to
			// the client.
			cs.responseTrailerReady.Fire()
		}
	}
}

func (cs *clientStream) validateBodyResponse(bodyResp *v3procservicepb.BodyResponse) (*v3procservicepb.StreamedBodyResponse, bool) {
	if status := bodyResp.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
		cs.failProcStream(fmt.Errorf("external processor returned unexpected status %v for body response, expected %v", status, v3procservicepb.CommonResponse_CONTINUE))
		return nil, false
	}
	streamedResp := bodyResp.GetResponse().GetBodyMutation().GetStreamedResponse()
	if streamedResp == nil {
		cs.failProcStream(fmt.Errorf("external processor returned invalid body mutation in body response"))
		return nil, false
	}
	if streamedResp.GetGrpcMessageCompressed() {
		cs.failProcStream(fmt.Errorf("external processor returned compressed grpc message which is not supported"))
		return nil, false
	}
	return streamedResp, true
}

func (cs *clientStream) applyMutations(mutation *v3procservicepb.HeaderMutation, md metadata.MD) error {
	if mutation == nil {
		return nil
	}
	if err := cs.config.mutationRules.ApplyAdditions(mutation.GetSetHeaders(), md); err != nil {
		return err
	}
	return cs.config.mutationRules.ApplyRemovals(mutation.GetRemoveHeaders(), md)
}

// closeProcSend gracefully half-closes the external processor stream across the
// background send goroutine by pushing a nil sentinel onto procSendCh.
func (cs *clientStream) closeProcSend() {
	select {
	case cs.procSendCh <- nil:
	case <-cs.ctx.Done():
	case <-cs.procStreamFailed.Done():
	case <-cs.procStreamBypass.Done():
	}
}

// sendToProcServer runs as a dedicated background goroutine that serializes all
// outbound messages to the external processor server. It listens on procSendCh
// for messages to forward. It actively monitors stream lifecycle events:
//   - Drain/Bypass: If procStreamBypass fires, it initiates a graceful shutdown
//     by sending a half-close (CloseSend) to the external processor server and
//     returning so that no more messages can be sent to the external processor.
//   - Cancellation/Failure: It aborts immediately if the context is canceled or
//     the underlying stream fails.
func (cs *clientStream) sendToProcServerLoop() {
	for {
		select {
		case req := <-cs.procSendCh:
			if req == nil {
				// A nil request is used as a sentinel message to gracefully half-close
				// the processor stream from this background thread. This ensures that
				// CloseSend is called sequentially after all preceding requests in the
				// channel are successfully sent, avoiding out-of-order data races.
				cs.procStream.CloseSend()
				return
			}
			if err := cs.procStream.Send(req); err != nil {
				if err != io.EOF {
					// For non-EOF client-side send errors, fail the stream immediately.
					cs.failProcStream(err)
				}
				// If Send returned io.EOF, the server closed the stream early. Let
				// the Recv loop retrieve the actual status error from the server and
				// propagate it.
				return
			}
		case <-cs.procStreamBypass.Done():
			cs.procStream.CloseSend()
			return
		case <-cs.procStreamFailed.Done():
			return
		case <-cs.ctx.Done():
			return
		}
	}
}

// streamError returns the appropriate error when a stream operation is
// preempted by a stream failure or context cancellation. It prioritizes the
// external processor stream's terminal error over the context error.
func (cs *clientStream) streamError() error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}
	return status.FromContextError(cs.ctx.Err()).Err()
}

// waitChannel waits for the provided channel to be closed, while also
// respecting context cancellation and stream failures.
func (cs *clientStream) waitChannel(ch <-chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-cs.ctx.Done():
		return cs.streamError()
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	}
}

// createDataplaneStream initializes the underlying gRPC dataplane stream using
// the provided newStream function. If dataplane stream creation fails, the
// top-level context used by the extproc filter is canceled and thereby all
// spawned goroutines terminate.
func (cs *clientStream) createDataplaneStream(ctx context.Context, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) error {
	defer close(cs.dataplaneSetup)
	cs.dataplaneStream, cs.dataplaneCreationErr = newStream(ctx, opts...)
	if cs.dataplaneCreationErr != nil {
		cs.cancel()
	}
	return cs.dataplaneCreationErr
}

// failProcStream handles stream failures, recording errors or bypassing the
// external processor based on failureModeAllow configuration. It returns true
// if the stream is bypassed, and false if the connection is torn down.
func (cs *clientStream) failProcStream(err error) bool {
	if !cs.procStreamClosed.CompareAndSwap(false, true) {
		return cs.procStreamBypass.HasFired()
	}
	cs.procCancel()
	if err != io.EOF && (cs.ignoreFailureMode.Load() || !cs.config.failureModeAllow) {
		cs.procStreamErr.Store(status.Errorf(codes.Internal, "extproc: %v", err))
		cs.procStreamFailed.Fire()
		// Cancel the stream's context to immediately tear down the active dataplane
		// connection and unblock any pending client I/O.
		cs.cancel()
		return false
	}
	cs.triggerBypass()
	return true
}

// cancelStream immediately terminates the stream with the specified error and
// fires the failure event. It does not respect the failure mode.
func (cs *clientStream) cancelStream(err error) {
	if !cs.procStreamClosed.CompareAndSwap(false, true) {
		return
	}
	cs.procStreamErr.Store(err)
	cs.procStreamFailed.Fire()
	// Cancel the stream's context to immediately tear down the active
	// dataplane connection and unblock any pending client I/O.
	cs.cancel()
}

// handleProcStreamInitError handles failures during the initialization of the
// external processor stream in NewStream. If the failure mode allows it, the
// external processor is bypassed and the direct dataplane stream is created.
func (cs *clientStream) handleProcStreamInitError(err error, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) (grpc.ClientStream, error) {
	if !cs.failProcStream(err) {
		return nil, cs.streamError()
	}
	if err := cs.createDataplaneStream(cs.ctx, newStream, opts); err != nil {
		return nil, err
	}
	return cs.dataplaneStream, nil
}

// handleHeaderError handles failures that occur during receiving or processing
// the initial request headers. If the failure mode allows it, the external
// processor is bypassed and the direct dataplane stream is created.
func (cs *clientStream) handleHeaderError(err error, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) {
	if cs.failProcStream(err) {
		cs.createDataplaneStream(cs.ctx, newStream, opts)
	}
}

// processInitialHeaders waits for the initial header response from the external
// processor server, applies any requested header mutations to the outgoing
// context, and initializes the data plane stream. It returns true if the
// headers were processed successfully and dataplane stream was created. It
// returns false if the proc stream needs to be bypassed, or failed due to an
// error, or aborted early by a processor-initiated immediate response.
func (cs *clientStream) processInitialHeaders(ctx context.Context, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) bool {
	resp, err := cs.procStream.Recv()
	if err != nil {
		cs.handleHeaderError(err, newStream, opts)
		return false
	}
	if resp.GetRequestDrain() {
		cs.triggerBypass()
	}
	if resp.GetImmediateResponse() != nil {
		cs.handleImmediateResponse(resp.GetImmediateResponse(), newStream, opts)
		return false
	}

	header := resp.GetRequestHeaders()
	if header == nil {
		err := fmt.Errorf("external processor returned an unexpected message type %T, expected request headers response", resp.GetResponse())
		cs.handleHeaderError(err, newStream, opts)
		return false
	}
	// Check if status in header response is CONTINUE; if not, fail the stream.
	if status := header.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
		cs.handleHeaderError(fmt.Errorf("external processor returned unexpected status %v for request headers, expected %v", status, v3procservicepb.CommonResponse_CONTINUE), newStream, opts)
		return false
	}
	// Mutate the outgoing headers with additions and removals received from the
	// external processor.
	outgoingMD, _ := metadata.FromOutgoingContext(ctx)
	if outgoingMD == nil {
		outgoingMD = metadata.MD{}
	}
	if err = cs.applyMutations(header.GetResponse().GetHeaderMutation(), outgoingMD); err != nil {
		cs.handleHeaderError(err, newStream, opts)
		return false
	}
	dataplaneCtx := metadata.NewOutgoingContext(ctx, outgoingMD)
	if err = cs.createDataplaneStream(dataplaneCtx, newStream, opts); err != nil {
		return false
	}
	return true
}

func (cs *clientStream) handleImmediateResponse(imm *v3procservicepb.ImmediateResponse, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) {
	if cs.config.disableImmediateResponse {
		err := fmt.Errorf("external processor sent an immediate response but immediate responses are disabled in configuration")
		if cs.dataplaneStream == nil {
			cs.handleHeaderError(err, newStream, opts)
		} else {
			cs.failProcStream(err)
		}
		return
	}

	statusCode := codes.Internal
	if imm.GetGrpcStatus() != nil {
		statusCode = codes.Code(imm.GetGrpcStatus().GetStatus())
		if statusCode > codes.Code(16) {
			statusCode = codes.Unknown
		}
	}
	err := status.Error(statusCode, imm.GetDetails())

	if cs.trailerSent.Load() {
		if mutation := imm.GetHeaders(); mutation != nil {
			cs.applyMutations(mutation, cs.responseTrailers)
		}
		cs.trailerErr.Store(err)
		cs.responseTrailerReady.Fire()
	} else {
		cs.cancelStream(err)
	}
}

func (cs *clientStream) triggerBypass() {
	if cs.procStreamBypass.Fire() {
		cs.responseHeadersReady.Fire()
		cs.responseTrailerReady.Fire()
	}
}

// waitForDataplaneStream waits for the dataplane stream to be created or for
// the context to be done. It also checks if the processor stream has not ended
// abruptly with a non-io.EOF error.
func (cs *clientStream) waitForDataplaneStream(ctx context.Context) (grpc.ClientStream, error) {
	select {
	case <-cs.dataplaneSetup:
		if cs.dataplaneCreationErr != nil {
			return nil, cs.dataplaneCreationErr
		}
		return cs.dataplaneStream, nil
	case <-ctx.Done():
		// Prioritize returning the underlying dataplane creation failure or
		// external processor error over the context cancellation error.
		if cs.dataplaneCreationErr != nil {
			return nil, cs.dataplaneCreationErr
		}
		return nil, cs.streamError()
	case <-cs.procStreamFailed.Done():
		return nil, cs.procStreamErr.Load().(error)
	}
}

// initiateResponseHeaderProcessing receives initial response headers exactly
// once, forwards them to the external processor if configured, or unblocks
// readers immediately.
func (cs *clientStream) initiateResponseHeaderProcessing() error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}
	if !cs.responseHeaderOnce.CompareAndSwap(false, true) {
		return nil
	}
	dataplaneStream, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return err
	}
	header, err := dataplaneStream.Header()
	if err != nil {
		// If the processor stream failed first and canceled cs.ctx, prefer
		// that root error over the secondary dataplaneStream.Header() failure.
		if cs.procStreamFailed.HasFired() {
			return cs.procStreamErr.Load().(error)
		}
		return err
	}

	if header == nil {
		// A trailers-only response returns nil from dataplaneStream.Header().
		header = dataplaneStream.Trailer()
		if len(header) > 0 {
			cs.trailersOnly = true
		}
	}
	cs.responseHeader = header
	if cs.config.processingModes.responseHeaderMode != modeSend || cs.procStreamBypass.HasFired() {
		// If header does not need to be sent to the external processor, unblock
		// the functions waiting on header modifications.
		cs.responseHeadersReady.Fire()
		return nil
	}

	req := cs.newProcessingRequest(false)
	req.Request = &v3procservicepb.ProcessingRequest_ResponseHeaders{ResponseHeaders: &v3procservicepb.HttpHeaders{
		Headers:     httpfilter.ConstructHeaderMap(header, nil, cs.config.allowedHeaders, cs.config.disallowedHeaders),
		EndOfStream: cs.trailersOnly,
	}}
	select {
	case cs.procSendCh <- req:
		cs.responseHeaderSent.Store(true)
		return nil
	case <-cs.procStreamBypass.Done():
		return nil
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	case <-cs.ctx.Done():
		return cs.streamError()
	}
}

func (cs *clientStream) initiateResponseTrailerProcessing() {
	if !cs.responseTrailerOnce.CompareAndSwap(false, true) {
		return
	}
	if cs.trailersOnly {
		// For trailers-only response, the trailers metadata map is processed and
		// mutated during the response header phase. Wait for those mutations to
		// be received and applied.
		select {
		case <-cs.responseHeadersReady.Done():
		case <-cs.procStreamFailed.Done():
		case <-cs.ctx.Done():
		}
		cs.responseTrailers = cs.responseHeader
		cs.responseTrailerReady.Fire()
		// Gracefully half-close the external processor stream for trailers-only
		// responses once header modifications finish.
		cs.closeProcSend()
		return
	}
	cs.responseTrailers = cs.dataplaneStream.Trailer()
	if cs.config.processingModes.responseTrailerMode == modeSend && !cs.procStreamBypass.HasFired() {
		req := cs.newProcessingRequest(false)
		req.Request = &v3procservicepb.ProcessingRequest_ResponseTrailers{ResponseTrailers: &v3procservicepb.HttpTrailers{
			Trailers: httpfilter.ConstructHeaderMap(cs.responseTrailers, nil, cs.config.allowedHeaders, cs.config.disallowedHeaders),
		}}
		select {
		case cs.procSendCh <- req:
			cs.trailerSent.Store(true)
		case <-cs.ctx.Done():
		case <-cs.procStreamFailed.Done():
		case <-cs.procStreamBypass.Done():
		}
	} else {
		cs.responseTrailerReady.Fire()
	}
	// Gracefully half-close the external processor stream after forwarding response
	// trailers to signal that all responses have completely processed.
	cs.closeProcSend()
}

func (cs *clientStream) waitForTrailerProcessing(recvErr error) error {
	select {
	case <-cs.responseTrailerReady.Done():
		if val := cs.trailerErr.Load(); val != nil {
			return val.(error)
		}
		if cs.procStreamFailed.HasFired() {
			return cs.procStreamErr.Load().(error)
		}
		return recvErr
	case <-cs.procStreamBypass.Done():
		return recvErr
	case <-cs.procStreamFailed.Done():
		return cs.streamError()
	case <-cs.ctx.Done():
		return cs.streamError()
	}
}

// convertBodyMode converts the body mode from processingMode to
// v3procfilterpb.ProcessingMode_BodySendMode.
func convertBodyMode(mode processingMode) v3procfilterpb.ProcessingMode_BodySendMode {
	if mode == modeSend {
		return v3procfilterpb.ProcessingMode_GRPC
	}
	return v3procfilterpb.ProcessingMode_NONE
}

func getHeader(md metadata.MD, added [][]string, key string) string {
	var vals []string
	if md != nil {
		vals = append(vals, md.Get(key)...)
	}
	for _, kvs := range added {
		for i := 0; i < len(kvs); i += 2 {
			if strings.EqualFold(kvs[i], key) {
				vals = append(vals, kvs[i+1])
			}
		}
	}
	return strings.Join(vals, ",")
}

// constructRequestAttributes builds a map of request attributes specified in
// the interceptor config.
func constructRequestAttributes(rpcInfo resolver.RPCInfo, md metadata.MD, added [][]string, requestedAttributes []string) map[string]*structpb.Struct {
	if len(requestedAttributes) == 0 {
		return nil
	}

	reqFields := make(map[string]any)
	for _, attr := range requestedAttributes {
		switch attr {
		case "request.path", "request.url_path":
			reqFields[attr] = rpcInfo.Method
		case "request.host":
			reqFields[attr] = rpcInfo.Authority
		case "request.method":
			reqFields[attr] = "POST"
		case "request.headers":
			headers := make(map[string]any)
			for k, values := range md {
				headers[strings.ToLower(k)] = strings.Join(values, ",")
			}
			for _, kvs := range added {
				for i := 0; i < len(kvs); i += 2 {
					k := strings.ToLower(kvs[i])
					v := kvs[i+1]
					if prev, ok := headers[k].(string); ok {
						headers[k] = prev + "," + v
					} else {
						headers[k] = v
					}
				}
			}
			reqFields[attr] = headers
		case "request.referer":
			if val := getHeader(md, added, "referer"); val != "" {
				reqFields[attr] = val
			}
		case "request.useragent":
			if val := getHeader(md, added, "user-agent"); val != "" {
				reqFields[attr] = val
			}
		case "request.id":
			if val := getHeader(md, added, "x-request-id"); val != "" {
				reqFields[attr] = val
			}
		case "request.query":
			reqFields[attr] = ""
		default:
			// Unknown/unsupported attributes are ignored.
		}
	}

	reqStruct := &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	for k, v := range reqFields {
		val, err := structpb.NewValue(v)
		if err != nil {
			// Skip values that cannot be represented in a structpb.Value to
			// match Envoy's behavior of encoding as many attributes as possible.
			continue
		}
		reqStruct.Fields[k] = val
	}

	if len(reqStruct.Fields) == 0 {
		return nil
	}

	return map[string]*structpb.Struct{
		"envoy.filters.http.ext_proc": reqStruct,
	}
}
