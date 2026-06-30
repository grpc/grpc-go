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
	"sync"
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
// NONE or GRPC.
func validateBodyProcessingMode(mode *v3procfilterpb.ProcessingMode) error {
	if m := mode.GetRequestBodyMode(); m != v3procfilterpb.ProcessingMode_NONE && m != v3procfilterpb.ProcessingMode_GRPC {
		return fmt.Errorf("extproc: invalid request body mode %v: want %q or %q", m, "NONE", "GRPC")
	}
	if m := mode.GetResponseBodyMode(); m != v3procfilterpb.ProcessingMode_NONE && m != v3procfilterpb.ProcessingMode_GRPC {
		return fmt.Errorf("extproc: invalid response body mode %v: want %q or %q", m, "NONE", "GRPC")
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

func (builder) BuildClientFilter() httpfilter.ClientFilter {
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
	// Create a cancellable context to cancel the dataplane stream incase of
	// error.
	cancelCtx, cancel := context.WithCancel(ctx)
	cs := &clientStream{
		config:                   i.config,
		procStreamFailed:         grpcsync.NewEvent(),
		mutatedReqBuffer:         buffer.NewUnbounded(),
		mutatedRespBuffer:        buffer.NewUnbounded(),
		responseHeaderModified:   grpcsync.NewEvent(),
		responseTrailerModified:  grpcsync.NewEvent(),
		dataplaneSetup:           make(chan struct{}),
		ctx:                      cancelCtx,
		cancel:                   cancel,
		procSendCh:               make(chan *v3procservicepb.ProcessingRequest),
		procBypassCh:             make(chan struct{}),
		requestForwardLoopDoneCh: make(chan struct{}),
		procRecvLoopDone:         grpcsync.NewEvent(),
	}

	// Create a new context for the RPC to the external processor server. This
	// context has a deadline of the timeout specified in the config, if present.
	// It also contains the initial metadata specified in the config. Create a new
	// child context for the external processor stream to be able to cancel it
	// independently.
	procCtx, cancel := context.WithCancel(ctx)
	if i.config.server.Timeout != 0 {
		procCtx, cancel = context.WithTimeout(ctx, i.config.server.Timeout)
	}
	cs.procCancel = cancel
	procCtx = metadata.NewOutgoingContext(procCtx, i.config.server.InitialMetadata)

	// In the ClientStream API, outgoing headers are sent immediately when the
	// stream is created. Because we need to mutate these headers before they go
	// over the wire, we must establish the ext_proc stream now rather than
	// deferring it.
	procStream, err := i.procClient.Process(procCtx)
	if err != nil {
		return cs.handleInitError(fmt.Errorf("failed to create a stream to external processor server: %v", err), newStream, opts)
	}
	cs.procStream = procStream

	// Construct request attributes for the RPC to the external processor server.
	outgoingMD, added, _ := metadataFromOutgoingContextRaw(ctx)
	cs.reqAttrs = constructRequestAttributes(ri, outgoingMD, added, i.config.requestAttributes)

	// If the request header processing mode is set to "Send", forward the headers
	// to the ext proc server. The dataplane stream will be created upon receiving
	// the response. Otherwise, create the dataplane stream immediately.
	if i.config.processingModes.requestHeaderMode == modeSend {
		headerReq := cs.newProcessingRequest(true)
		headerReq.Request = &v3procservicepb.ProcessingRequest_RequestHeaders{
			RequestHeaders: &v3procservicepb.HttpHeaders{
				Headers: httpfilter.ConstructHeaderMap(outgoingMD, added, i.config.allowedHeaders, i.config.disallowedHeaders),
			},
		}
		if err = procStream.Send(headerReq); err != nil {
			return cs.handleInitError(fmt.Errorf("failed to send client headers to external processor server: %v", err), newStream, opts)
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
	procCancel func()
	config     baseConfig

	dataplaneStream  grpc.ClientStream                               // underlying gRPC stream to the backend
	procStream       v3procservicepb.ExternalProcessor_ProcessClient // bidirectional stream to the external processor
	procStreamFailed *grpcsync.Event                                 // fired when external processor stream has closed and RPC should be failed
	procStreamBypass atomic.Bool                                     // set to true when the external processor stream should be bypassed
	procStreamClosed atomic.Bool                                     // ensures the stream closure logic is executed exactly once
	procStreamErr    atomic.Value                                    // holds the terminal error causing the external processor stream to fail

	reqAttrs             map[string]*structpb.Struct             // attributes to be sent to proc server with client message
	protocolConfigSent   atomic.Bool                             // tracks whether the protocol configuration has been sent to the processor
	reqAttrsSent         atomic.Bool                             // tracks whether the first client message has been sent to ensure request attributes are sent only once
	ignoreFailureMode    atomic.Bool                             // tracks whether the failureModeAllow setting should be ignored (e.g. after sending body messages)
	discardRequests      atomic.Bool                             // set when ext_proc server signals end_of_stream to stop client sends
	mutatedReqBuffer     *buffer.Unbounded                       // buffers mutated request body messages from the ext_proc server
	reqForwardingStarted bool                                    // tracks whether request forwarding loop to the dataplane has started
	procSendCh           chan *v3procservicepb.ProcessingRequest // serializes writes to the external processor stream to ensure thread-safety

	responseHeader          metadata.MD       // stores headers received from the dataplane stream
	responseHeaderOnce      sync.Once         // ensures response headers are forwarded to the external processor only once
	responseHeaderModified  *grpcsync.Event   // signals that response headers are ready for client
	responseTrailers        metadata.MD       // stores trailers received from the dataplane stream
	responseTrailerOnce     sync.Once         // ensures response trailers are forwarded to the external processor only once
	responseTrailerModified *grpcsync.Event   // signals that response trailers are ready for client
	trailerSent             atomic.Bool       // tracks whether response trailers have been dispatched
	trailersOnly            bool              // tracks whether the backend response is trailers-only
	mutatedRespBuffer       *buffer.Unbounded // buffers mutated response body messages from the ext_proc server
	responseDrained         atomic.Bool       // indicates consumer loop has fully processed buffered items
	dataplaneSetup          chan struct{}     // closed once dataplaneStream creation attempt is complete
	dataplaneCreationErr    error             // stores the error from the dataplane stream creation attempt
	responseRecvStarted     bool              // tracks whether response message reading has started

	procBypassCh             chan struct{}   // closed upon receiving a drain request or bypass signal
	requestForwardLoopDoneCh chan struct{}   // closed when request forwarding loop finishes draining
	bypassTriggered          atomic.Bool     // guards against multiple closures of procBypassCh
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

// Header returns the header metadata received from the server if there is any.
// It blocks if the metadata is not ready to read.
func (cs *clientStream) Header() (metadata.MD, error) {
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		return nil, err
	}
	// Wait for the response headers to be modified.
	select {
	case <-cs.responseHeaderModified.Done():
	case <-cs.procStreamFailed.Done():
		return nil, cs.procStreamErr.Load().(error)
	case <-cs.ctx.Done():
		// Return an Internal status with error (rather than context.Canceled) if
		// the dataplane stream was dropped due to an external processor failure.
		if val := cs.procStreamErr.Load(); val != nil {
			return nil, val.(error)
		}
		return nil, status.FromContextError(cs.ctx.Err()).Err()
	}
	return cs.responseHeader, nil
}

// Trailer returns the trailer metadata from the server, if there is any. It
// returns nil or empty map if trailer is not received yet. It is not blocking.
func (cs *clientStream) Trailer() metadata.MD {
	// If trailers are already modified and ready, return them immediately.
	select {
	case <-cs.responseTrailerModified.Done():
		return cs.responseTrailers
	default:
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
	// If the backend stream has no trailers, return nil to keep the existing
	// behavior.
	if len(s.Trailer()) == 0 {
		return nil
	}

	cs.initiateResponseTrailerProcessing()
	select {
	case <-cs.responseTrailerModified.Done():
	case <-cs.procStreamFailed.Done():
		return nil
	case <-cs.ctx.Done():
		return nil
	}
	return cs.responseTrailers
}

func (cs *clientStream) CloseSend() error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}

	procBypass := cs.procStreamBypass.Load()

	// If the stream is closed and we had started sending data from the processor
	// server to the dataplane server, wait for the buffer to finish before
	// calling CloseSend.
	if procBypass && cs.reqForwardingStarted {
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return err
		}
	}

	// If the stream is not started and the processor stream is closed or the mode
	// is skip, send directly on the dataplane stream.
	if procBypass {
		return cs.dataplaneStream.CloseSend()
	}
	if cs.config.processingModes.requestBodyMode == modeSkip {
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		return s.CloseSend()
	}

	// If external processor stream is active, send to the processor server as
	// request message with `EndOfStreamWithoutMessage` set.
	req := cs.newProcessingRequest(true)
	req.Request = &v3procservicepb.ProcessingRequest_RequestBody{
		RequestBody: &v3procservicepb.HttpBody{
			EndOfStream:               true,
			EndOfStreamWithoutMessage: true,
		},
	}

	// Ignoring the failureMode as we will be sending the request to proc server.
	cs.ignoreFailureMode.CompareAndSwap(false, true)

	select {
	case cs.procSendCh <- req:
		return nil
	case <-cs.procBypassCh:
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return err
		}
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		return s.CloseSend()
	case <-cs.procStreamFailed.Done():
		return cs.procStreamErr.Load().(error)
	case <-cs.ctx.Done():
		// Return an Internal status with error (rather than context error) if the
		// dataplane stream was dropped due to an external processor failure.
		if val := cs.procStreamErr.Load(); val != nil {
			return val.(error)
		}
		return status.FromContextError(cs.ctx.Err()).Err()
	}
}

func (cs *clientStream) Context() context.Context {
	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return cs.ctx
	}
	return s.Context()
}

func (cs *clientStream) RecvMsg(m any) error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}
	// Initiate response header processing because external processor requires the
	// events to be sent in the correct order, i.e. response header before
	// response message. And if Header() has not already been called, send the
	// response headers to external processor server first.
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		cs.failProcStream(err)
		return err
	}

	// If all the responses from external processor server has been sent or if the
	// external processor is bypassed, or if the response body mode is skip, then
	// receive directly from the dataplane stream.
	if cs.responseDrained.Load() || (cs.procStreamBypass.Load() && !cs.responseRecvStarted) || cs.config.processingModes.responseBodyMode == modeSkip {
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		if err := s.RecvMsg(m); err != nil {
			// If RecvMsg returns error, fail the RPC incase external processor stream
			// has failed. Otherwise process the trailers.
			if val := cs.procStreamErr.Load(); val != nil {
				return val.(error)
			}
			cs.initiateResponseTrailerProcessing()
			return err
		}
		return nil
	}

	msg, ok := m.(proto.Message)
	if !ok {
		return fmt.Errorf("extproc: response message does not implement proto.Message")
	}

	// Start the background receiving loop on the first RecvMsg call to capture
	// the type of message to be received.
	if !cs.responseRecvStarted {
		cs.responseRecvStarted = true
		go cs.responseForwardingToProcServerLoop(msg.ProtoReflect().Type())
	}

	// Pull response messages from mutatedRespBuffer.
	select {
	case item, ok := <-cs.mutatedRespBuffer.Get():
		cs.mutatedRespBuffer.Load()
		if !ok {
			// Closed channel implies that all messages from the external processor
			// have been received. Start receiving directly from dataplane stream.
			cs.responseDrained.Store(true)
			if val := cs.procStreamErr.Load(); val != nil {
				return val.(error)
			}
			s, err := cs.waitForDataplaneStream(cs.ctx)
			if err != nil {
				return err
			}
			if err := s.RecvMsg(m); err != nil {
				cs.initiateResponseTrailerProcessing()
				return err
			}
			return nil
		}
		streamedResp, ok := item.(*v3procservicepb.StreamedBodyResponse)
		if !ok {
			cs.failProcStream(fmt.Errorf("unexpected response type in responseBuffer: %T", item))
			return fmt.Errorf("extproc: unexpected response type in responseBuffer: %T", item)
		}
		if err := proto.Unmarshal(streamedResp.GetBody(), msg); err != nil {
			return err
		}
		return nil

	case <-cs.ctx.Done():
		// Return an Internal status and error (rather than context error) if the
		// dataplane stream was dropped due to an external processor failure.
		if cs.procStreamFailed.HasFired() {
			return cs.procStreamErr.Load().(error)
		}
		return status.FromContextError(cs.ctx.Err()).Err()
	case <-cs.procStreamFailed.Done():
		return cs.procStreamErr.Load().(error)
	}
}

func (cs *clientStream) SendMsg(m any) error {
	if cs.procStreamFailed.HasFired() {
		return cs.procStreamErr.Load().(error)
	}
	procBypass := cs.procStreamBypass.Load()

	// If the stream is closed and we started sending messages to the dataplane,
	// it means the drain has been triggered, and we need to wait for the forward
	// loop to finish before sending any more messages.
	if procBypass && cs.reqForwardingStarted {
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return err
		}
	}
	if procBypass {
		// If proc bypass is enabled, send the message directly to the dataplane
		// stream.
		return cs.dataplaneStream.SendMsg(m)
	}
	if cs.config.processingModes.requestBodyMode == modeSkip {
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
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

	cs.ignoreFailureMode.CompareAndSwap(false, true)

	select {
	case cs.procSendCh <- req:
		return nil
	case <-cs.procBypassCh:
		// When the drain is triggered, wait for all the queued request to be sent
		// to dataplane server before forwarding current request directly to
		// dataplane server.
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return err
		}
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		return s.SendMsg(m)
	case <-cs.procStreamFailed.Done():
		return cs.procStreamErr.Load().(error)
	case <-cs.ctx.Done():
		return status.FromContextError(cs.ctx.Err()).Err()
	}
}

// responseForwardingToProcServerLoop continuously receives raw response
// messages from the underlying dataplane stream, marshals them, and forwards
// them to the external processor server via procSendCh.
func (cs *clientStream) responseForwardingToProcServerLoop(msgType protoreflect.MessageType) {
	defer func() {
		// Once the external processor server receive loop finishes, close the buffer to
		// signal end of processed responses.
		cs.waitChannel(cs.procRecvLoopDone.Done())
		cs.mutatedRespBuffer.Close()
	}()

	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}

	for {
		// If the processor stream has closed or the receive loop finishes, we can stop
		// receiving messages in the background and Recv should now directly be
		// called from the cs.RecvMsg() function.
		if cs.procStreamBypass.Load() || cs.procRecvLoopDone.HasFired() {
			return
		}

		newMsg := msgType.New().Interface()
		if err := s.RecvMsg(newMsg); err != nil {
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

		cs.ignoreFailureMode.CompareAndSwap(false, true)

		select {
		case cs.procSendCh <- req:
		case <-cs.procBypassCh:
			// If drain is triggered, wait for external processor server receive loop
			// to finish before pushing this message on the mutatedRespBuffer
			// to ensure correct order of messages.
			<-cs.procRecvLoopDone.Done()
			resp := &v3procservicepb.StreamedBodyResponse{
				Body: bodyBytes,
			}
			cs.mutatedRespBuffer.Put(resp)
			return
		case <-cs.procStreamFailed.Done():
			return
		}
	}
}

// requestForwardingToDataplaneLoop continuously consumes mutated request body
// chunks from mutatedReqBuffer (populated by the external processor server),
// unmarshals them, and forwards the final messages to the backend server.
func (cs *clientStream) requestForwardingToDataplaneLoop(msgType protoreflect.MessageType) {
	defer close(cs.requestForwardLoopDoneCh)
	if _, err := cs.waitForDataplaneStream(cs.ctx); err != nil {
		return
	}
	for {
		item, ok := <-cs.mutatedReqBuffer.Get()
		if !ok {
			if cs.procStreamFailed.HasFired() && !cs.config.failureModeAllow && cs.dataplaneStream != nil {
				cs.dataplaneStream.CloseSend()
			}
			return
		}
		cs.mutatedReqBuffer.Load()
		streamedResp, ok := item.(*v3procservicepb.StreamedBodyResponse)
		if !ok {
			return
		}

		if streamedResp.GetEndOfStream() && streamedResp.GetEndOfStreamWithoutMessage() {
			cs.dataplaneStream.CloseSend()
			return
		}

		newMsg := msgType.New().Interface()
		if err := proto.Unmarshal(streamedResp.GetBody(), newMsg); err != nil {
			cs.failProcStream(err)
			return
		}

		if err := cs.dataplaneStream.SendMsg(newMsg); err != nil {
			cs.failProcStream(err)
			return
		}

		if streamedResp.GetEndOfStream() {
			cs.dataplaneStream.CloseSend()
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
			cs.handleImmediateResponse(resp.GetImmediateResponse())
			return
		}

		switch {
		case resp.GetRequestBody() != nil:
			if cs.config.processingModes.requestBodyMode == modeSkip {
				cs.failProcStream(fmt.Errorf("extproc: external processor unexpectedly sent request body when request body processing is disabled"))
				return
			}

			bodyResp := resp.GetRequestBody()
			if status := bodyResp.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
				cs.failProcStream(fmt.Errorf("external processor returned unexpected status %v for request body, expected %v", status, v3procservicepb.CommonResponse_CONTINUE))
				return
			}
			streamedResp := bodyResp.GetResponse().GetBodyMutation().GetStreamedResponse()
			if streamedResp == nil {
				cs.failProcStream(fmt.Errorf("external processor returned invalid body mutation for request body"))
				return
			}
			if streamedResp.GetEndOfStream() {
				cs.discardRequests.Store(true)
			}
			if streamedResp.GetGrpcMessageCompressed() {
				cs.failProcStream(fmt.Errorf("external processor returned compressed grpc message which is not supported"))
				return
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
			if cs.config.processingModes.responseHeaderMode == modeSend {
				select {
				case <-cs.responseHeaderModified.Done():
				default:
					cs.failProcStream(fmt.Errorf("external processor sent response body before sending response headers"))
					return

				}
			}

			// If mutated response trailers have been received before receiving the
			// response body message, fail the RPC.
			if cs.config.processingModes.responseTrailerMode == modeSend {
				select {
				case <-cs.responseTrailerModified.Done():
					cs.failProcStream(fmt.Errorf("external processor sent response body after response trailers were already processed"))
					return
				default:

				}
			}

			bodyResp := resp.GetResponseBody()
			if status := bodyResp.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
				cs.failProcStream(fmt.Errorf("external processor returned unexpected status %v for response body, expected %v", status, v3procservicepb.CommonResponse_CONTINUE))
				return
			}
			streamedResp := bodyResp.GetResponse().GetBodyMutation().GetStreamedResponse()
			if streamedResp == nil {
				cs.failProcStream(fmt.Errorf("external processor returned invalid body mutation for response body"))
				return
			}
			if streamedResp.GetGrpcMessageCompressed() {
				cs.failProcStream(fmt.Errorf("external processor returned compressed grpc message which is not supported"))
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

			header := resp.GetResponseHeaders()
			// Check if the status in the header response is CONTINUE; if not, fail
			// the stream.
			if status := header.GetResponse().GetStatus(); status != v3procservicepb.CommonResponse_CONTINUE {
				cs.failProcStream(fmt.Errorf("external processor returned unexpected status %v for response headers, expected %v", status, v3procservicepb.CommonResponse_CONTINUE))
				return
			}
			if err = cs.config.mutationRules.ApplyAdditions(header.GetResponse().GetHeaderMutation().GetSetHeaders(), cs.responseHeader); err != nil {
				cs.failProcStream(err)
				return
			}
			if err = cs.config.mutationRules.ApplyRemovals(header.GetResponse().GetHeaderMutation().GetRemoveHeaders(), cs.responseHeader); err != nil {
				cs.failProcStream(err)
				return
			}
			// Signal that the response header is modified and ready to be sent to the
			// client, so that if there is any buffered response body, it can be sent
			// after the header.
			cs.responseHeaderModified.Fire()

		case resp.GetResponseTrailers() != nil:
			if cs.config.processingModes.responseTrailerMode == modeSkip {
				cs.failProcStream(fmt.Errorf("extproc: external processor unexpectedly sent response trailers when response trailer processing is disabled"))
				return
			}
			trailer := resp.GetResponseTrailers()
			if err = cs.config.mutationRules.ApplyAdditions(trailer.GetHeaderMutation().GetSetHeaders(), cs.responseTrailers); err != nil {
				cs.failProcStream(err)
				return
			}
			if err = cs.config.mutationRules.ApplyRemovals(trailer.GetHeaderMutation().GetRemoveHeaders(), cs.responseTrailers); err != nil {
				cs.failProcStream(err)
				return
			}
			// Signal that the response trailer is modified and ready to be sent to
			// the client.
			cs.responseTrailerModified.Fire()
		}
	}
}

// sendToProcServer runs as a dedicated background goroutine that serializes all
// outbound messages to the external processor server. It listens on procSendCh
// for messages to forward. It actively monitors stream lifecycle events:
//   - Drain/Bypass: If procBypassCh is closed, it initiates a graceful shutdown
//     by sending a half-close (CloseSend) to the external processor server and
//     returning so that no more messages can be sent to the external processor.
//   - Cancellation/Failure: It aborts immediately if the context is cancelled or
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
				// If the send failed, do not call failProcStream immediately. The Recv
				// loop will retrieve the actual status error from the server and
				// propagate it.
				return
			}
		case <-cs.procBypassCh:
			cs.procStream.CloseSend()
			return
		case <-cs.procStreamFailed.Done():
			return
		case <-cs.ctx.Done():
			return
		}
	}
}

// waitChannel waits for the provided channel to be closed, while also
// respecting context cancellation and stream failures.
func (cs *clientStream) waitChannel(ch <-chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-cs.ctx.Done():
		return status.FromContextError(cs.ctx.Err()).Err()
	case <-cs.procStreamFailed.Done():
		return cs.procStreamErr.Load().(error)
	}
}

// createDataplaneStream initializes the underlying gRPC dataplane stream using
// the provided newStream function.
func (cs *clientStream) createDataplaneStream(ctx context.Context, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) error {
	defer close(cs.dataplaneSetup)
	var err error
	if cs.dataplaneStream, err = newStream(ctx, opts...); err != nil {
		cs.dataplaneCreationErr = err
		cs.cancel()
		return err
	}
	return nil
}

// failProcStream handles stream failures, recording errors or bypassing external
// processor based on failureModeAllow configuration.
func (cs *clientStream) failProcStream(err error) {
	if !cs.procStreamClosed.CompareAndSwap(false, true) {
		return
	}
	if cs.procStreamFailed.HasFired() {
		return
	}
	if err != io.EOF && (cs.ignoreFailureMode.Load() || !cs.config.failureModeAllow) {
		cs.procStreamErr.Store(status.Errorf(codes.Internal, "extproc: external processor RPC failed: %v", err))
		cs.procStreamFailed.Fire()
		// Cancel the stream's context to immediately tear down the active dataplane
		// connection and unblock any pending client I/O.
		cs.cancel()
		return
	}
	cs.procStreamBypass.Store(true)
	cs.triggerBypass()
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

// handleInitError handles failures during the initialization of the external
// processor stream in NewStream. It returns a new dataplane stream if failure
// mode allows, else returns the error.
func (cs *clientStream) handleInitError(err error, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) (grpc.ClientStream, error) {
	cs.procCancel()
	if !cs.config.failureModeAllow {
		return nil, status.Errorf(codes.Internal, "extproc: %v", err)
	}
	if err := cs.createDataplaneStream(cs.ctx, newStream, opts); err != nil {
		return nil, err
	}
	cs.procStreamBypass.Store(true)
	return cs, nil
}

// handleHeaderError handles failures that occur duringreceiving or processing
// the initial request headers. If the failure mode allows it, the external
// processor is bypassed and the direct dataplane stream is created.
func (cs *clientStream) handleHeaderError(err error, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts []grpc.CallOption) {
	if err != io.EOF && (cs.ignoreFailureMode.Load() || !cs.config.failureModeAllow) {
		cs.procStreamErr.Store(status.Errorf(codes.Internal, "extproc: %v", err))
		cs.procStreamFailed.Fire()
		cs.cancel()
		return
	}
	// Triggering bypass closes procBypassCh to unblock any background goroutines
	// currently stuck sending or forwarding messages, preventing them from being
	// sent to the external processor.
	cs.triggerBypass()
	if err := cs.createDataplaneStream(cs.ctx, newStream, opts); err != nil {
		return
	}
	// Store procStreamBypass to signal that all subsequent client/server
	// messages should bypass the filter and go directly to the dataplane.
	cs.procStreamBypass.Store(true)
}

// processInitialHeaders waits for the initial header response from the external
// processor server, applies any requested header mutations to the outgoing
// context, and initializes the data plane stream. It returns true on success,
// or false if the stream is aborted due to an error, immediate response, or
// invalid status.
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
		if cs.config.disableImmediateResponse {
			cs.handleHeaderError(fmt.Errorf("external processor sent an immediate response but immediate responses are disabled in configuration"), newStream, opts)
		} else {
			imm := resp.GetImmediateResponse()
			statusCode := codes.Internal
			if imm.GetGrpcStatus() != nil {
				statusCode = codes.Code(imm.GetGrpcStatus().GetStatus())
			}
			cs.cancelStream(status.Error(statusCode, imm.GetDetails()))
		}
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
	if err = cs.config.mutationRules.ApplyAdditions(header.GetResponse().GetHeaderMutation().GetSetHeaders(), outgoingMD); err != nil {
		cs.handleHeaderError(err, newStream, opts)
		return false
	}
	if err = cs.config.mutationRules.ApplyRemovals(header.GetResponse().GetHeaderMutation().GetRemoveHeaders(), outgoingMD); err != nil {
		cs.handleHeaderError(err, newStream, opts)
		return false
	}
	dataplaneCtx := metadata.NewOutgoingContext(ctx, outgoingMD)
	if err = cs.createDataplaneStream(dataplaneCtx, newStream, opts); err != nil {
		return false
	}
	return true
}

func (cs *clientStream) handleImmediateResponse(imm *v3procservicepb.ImmediateResponse) {
	if cs.config.disableImmediateResponse {
		cs.failProcStream(fmt.Errorf("extproc: external processor sent an immediate response but immediate responses are disabled in configuration"))
		return
	}

	statusCode := codes.Internal
	if imm.GetGrpcStatus() != nil {
		statusCode = codes.Code(imm.GetGrpcStatus().GetStatus())
	}
	err := status.Error(statusCode, imm.GetDetails())

	if cs.trailerSent.Load() {
		if mutation := imm.GetHeaders(); mutation != nil {
			cs.config.mutationRules.ApplyAdditions(mutation.GetSetHeaders(), cs.responseTrailers)
			cs.config.mutationRules.ApplyRemovals(mutation.GetRemoveHeaders(), cs.responseTrailers)
		}
		cs.procStreamErr.Store(err)
		cs.responseTrailerModified.Fire()
	} else {
		cs.cancelStream(err)
	}
}

func (cs *clientStream) triggerBypass() {
	if cs.bypassTriggered.CompareAndSwap(false, true) {
		close(cs.procBypassCh)
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
		// Return an Internal status and error (rather than context.Canceled) if the
		// dataplane stream was dropped due to an external processor failure or
		// dataplane stream creation error.
		if cs.dataplaneCreationErr != nil {
			return nil, cs.dataplaneCreationErr
		}
		if cs.procStreamFailed.HasFired() {
			return nil, cs.procStreamErr.Load().(error)
		}
		return nil, status.FromContextError(ctx.Err()).Err()
	case <-cs.procStreamFailed.Done():
		return nil, cs.procStreamErr.Load().(error)
	}
}

func (cs *clientStream) initiateResponseHeaderProcessing() error {
	var err error
	cs.responseHeaderOnce.Do(func() {
		s, dpErr := cs.waitForDataplaneStream(cs.ctx)
		if dpErr != nil {
			err = dpErr
			return
		}
		header, headerErr := s.Header()
		if headerErr != nil {
			// If the external processor fails first, it cancels the dataplane
			// stream, causing s.Header() to unblock and return a generic connection
			// error (e.g. context canceled). If the processor stream has failed,
			// prefer the root processor error over this secondary transport error.
			if cs.procStreamFailed.HasFired() {
				err = cs.procStreamErr.Load().(error)
			} else {
				err = headerErr
			}
			return
		}
		cs.responseHeader = header
		// A trailers-only response will contain "grpc-status" in the headers or
		// will return nil, nil on s.Header() call.
		if header == nil || len(header.Get("grpc-status")) > 0 {
			cs.trailersOnly = true
		}
		if cs.config.processingModes.responseHeaderMode == modeSend && !cs.procStreamBypass.Load() {
			req := cs.newProcessingRequest(false)
			req.Request = &v3procservicepb.ProcessingRequest_ResponseHeaders{ResponseHeaders: &v3procservicepb.HttpHeaders{
				Headers:     httpfilter.ConstructHeaderMap(header, nil, cs.config.allowedHeaders, cs.config.disallowedHeaders),
				EndOfStream: cs.trailersOnly,
			}}
			select {
			case cs.procSendCh <- req:
			case <-cs.ctx.Done():
				// Return an Internal status and error (rather than context error) if
				// the dataplane stream was dropped due to an external processor
				// failure.
				if cs.procStreamFailed.HasFired() {
					err = cs.procStreamErr.Load().(error)
				} else {
					err = status.FromContextError(cs.ctx.Err()).Err()
				}
			case <-cs.procStreamFailed.Done():
				err = cs.procStreamErr.Load().(error)
			case <-cs.procBypassCh:
				cs.responseHeaderModified.Fire()
			}
		} else {
			// If header does not need to be sent to the external processor, unblock
			// the functions waiting on header modifications.
			cs.responseHeaderModified.Fire()
		}
	})
	return err
}

func (cs *clientStream) initiateResponseTrailerProcessing() {
	cs.responseTrailerOnce.Do(func() {
		s, _ := cs.waitForDataplaneStream(cs.ctx)
		if s == nil {
			return
		}
		cs.responseTrailers = s.Trailer()
		if cs.config.processingModes.responseTrailerMode == modeSend && !cs.procStreamBypass.Load() && !cs.trailersOnly {
			req := cs.newProcessingRequest(false)
			req.Request = &v3procservicepb.ProcessingRequest_ResponseTrailers{ResponseTrailers: &v3procservicepb.HttpTrailers{
				Trailers: httpfilter.ConstructHeaderMap(cs.responseTrailers, nil, cs.config.allowedHeaders, cs.config.disallowedHeaders),
			}}
			select {
			case cs.procSendCh <- req:
				cs.trailerSent.Store(true)
			case <-cs.ctx.Done():
			case <-cs.procStreamFailed.Done():
			case <-cs.procBypassCh:
				cs.responseTrailerModified.Fire()
			}
		} else {
			cs.responseTrailerModified.Fire()
		}
		// Send nil sentinel to procSendCh to gracefully half-close the processor
		// stream. Since this is pushed after trailers are sent, the
		// background loop will serialize the CloseSend() call safely after all
		// pending messages are transmitted.
		select {
		case cs.procSendCh <- nil:
		case <-cs.ctx.Done():
		case <-cs.procStreamFailed.Done():
		case <-cs.procBypassCh:
		}
	})
}

// convertBodyMode converts the body mode from processingMode to
// v3procfilterpb.ProcessingMode_BodySendMode.
func convertBodyMode(mode processingMode) v3procfilterpb.ProcessingMode_BodySendMode {
	switch mode {
	case modeSkip:
		return v3procfilterpb.ProcessingMode_NONE
	case modeSend:
		return v3procfilterpb.ProcessingMode_GRPC
	default:
		return v3procfilterpb.ProcessingMode_NONE
	}
}

func getHeaderRaw(md metadata.MD, added [][]string, key string) string {
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
			if val := getHeaderRaw(md, added, "referer"); val != "" {
				reqFields[attr] = val
			}
		case "request.useragent":
			if val := getHeaderRaw(md, added, "user-agent"); val != "" {
				reqFields[attr] = val
			}
		case "request.id":
			if val := getHeaderRaw(md, added, "x-request-id"); val != "" {
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
