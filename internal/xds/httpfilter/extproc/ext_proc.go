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
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3procfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	v3procservicegrpc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	v3procservicepb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

func init() {
	if envconfig.XDSClientExtProcEnabled {
		httpfilter.Register(builder{})
	}
}

// RegisterForTesting registers the external processor HTTP Filter for testing
// purposes, regardless of the XDSClientExtProcEnabled environment variable.
// This is needed because there is no way to set the XDSClientExtProcEnabled
// environment variable to true in a test before init() in this package is run.
func RegisterForTesting() {
	httpfilter.Register(builder{})
}

// UnregisterForTesting unregisters the external processor HTTP Filter for
// testing purposes. This is needed because there is no way to unregister the
// HTTP Filter after registering it solely for testing purposes using
// RegisterForTesting().
func UnregisterForTesting() {
	for _, typeURL := range builder.TypeURLs(builder{}) {
		httpfilter.UnregisterForTesting(typeURL)
	}
}

var (
	// ParseGRPCServiceConfig parses the gRPC service configuration from the given
	// protobuf message.
	ParseGRPCServiceConfig = func(*v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("extproc: ParseGRPCServiceConfig not implemented")
	}

	// CreateExtProcChannel creates a gRPC client channel to the external
	// processing server.
	CreateExtProcChannel = func(xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		return nil, nil, fmt.Errorf("extproc: dialing external processor server not implemented")
	}
)

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
	server, err := ParseGRPCServiceConfig(msg.GetGrpcService())
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
		server, err := ParseGRPCServiceConfig(override.GetGrpcService())
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
	cc, cancel, err := CreateExtProcChannel(config.server)
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to create channel to the external processor server %q: %v", config.server.TargetURI, err)
	}
	return &clientInterceptor{
		config:      config,
		extClient:   v3procservicegrpc.NewExternalProcessorClient(cc),
		closeClient: cancel,
	}, nil
}

type clientInterceptor struct {
	resolver.ClientInterceptor
	config      baseConfig
	extClient   v3procservicegrpc.ExternalProcessorClient
	closeClient func() error
}

func (i *clientInterceptor) Close() {
	i.closeClient()
}

func (i *clientInterceptor) NewStream(ctx context.Context, ri resolver.RPCInfo, done func(), newStream func(ctx context.Context, done func()) (resolver.ClientStream, error)) (resolver.ClientStream, error) {
	cancelCtx, cancel := context.WithCancel(ctx)
	cs := &clientStream{
		config:                   i.config,
		streamFailed:             grpcsync.NewEvent(),
		mutatedReqBuffer:         buffer.NewUnbounded(),
		mutatedRespBuffer:        buffer.NewUnbounded(),
		responseHeaderModified:   grpcsync.NewEvent(),
		responseTrailerModified:  grpcsync.NewEvent(),
		dataplaneReady:           make(chan struct{}),
		ctx:                      cancelCtx,
		cancel:                   cancel,
		extSendCh:                make(chan *v3procservicepb.ProcessingRequest),
		drainTriggeredCh:         make(chan struct{}),
		requestForwardLoopDoneCh: make(chan struct{}),
		drained:                  grpcsync.NewEvent(),
	}

	// Create a new context for the RPC to the external processor server. This
	// context has a deadline of the timeout specified in the config, if present.
	// It also contains the outgoing context's metadata, merged with the initial
	// metadata specified in the config.
	extProcCtx := ctx
	if i.config.server.Timeout != 0 {
		extProcCtx, cs.extCancel = context.WithTimeout(ctx, i.config.server.Timeout)
	}
	outgoingMD, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		outgoingMD = metadata.MD{}
	}
	extProcCtx = metadata.NewOutgoingContext(extProcCtx, i.config.server.InitialMetadata)

	// In the ClientStream API, outgoing headers are sent immediately when the
	// stream is created. Because we need to mutate these headers before they go
	// over the wire, we must establish the ext_proc stream now rather than
	// deferring it.
	extStream, err := i.extClient.Process(extProcCtx)
	if err != nil {
		return cs.handleInitError(fmt.Errorf("external processor failed to start: %v", err), newStream, done)
	}
	cs.extStream = extStream

	// Construct request attributes for the RPC to the external processor server.
	if cs.reqAttrs, err = constructRequestAttributes(ri, outgoingMD, i.config.requestAttributes); err != nil {
		return cs.handleInitError(fmt.Errorf("failed to construct attributes: %v", err), newStream, done)
	}

	// If the request header processing mode is set to "Send", forward the headers
	// to the ext proc server. The dataplane stream will be created upon receiving
	// the response. Otherwise, create the dataplane stream immediately.
	if i.config.processingModes.requestHeaderMode == modeSend {
		headerReq := v3procservicepb.ProcessingRequest{
			Request: &v3procservicepb.ProcessingRequest_RequestHeaders{
				RequestHeaders: &v3procservicepb.HttpHeaders{
					Headers: httpfilter.ConstructHeaderMap(outgoingMD, i.config.allowedHeaders, i.config.disallowedHeaders),
				},
			},
			ObservabilityMode: i.config.observabilityMode,
			ProtocolConfig: &v3procservicepb.ProtocolConfiguration{
				RequestBodyMode:  convertBodyMode(i.config.processingModes.requestBodyMode),
				ResponseBodyMode: convertBodyMode(i.config.processingModes.responseBodyMode),
			},
			Attributes: cs.reqAttrs,
		}
		if err = extStream.Send(&headerReq); err != nil {
			return cs.handleInitError(fmt.Errorf("failed to send client headers to external processor server: %v", err), newStream, done)
		}
		// Mark that the initial message has been sent to prevent sending
		// ProtocolConfig on subsequent messages.
		cs.initMsgSent.Store(true)
		// Mark that the initial client message has been sent to prevent adding
		// request attributes to any other message.
		cs.initClientMsgSent.Store(true)
	} else {
		if err = cs.createDataplaneStream(newStream, done); err != nil {
			return nil, err
		}
	}

	// Start a background loop for sending messages to the external processor
	// server. Use a single goroutine to ensure no concurrent sends.
	go cs.sendToProcServerLoop(cs.ctx)

	// Start a goroutine to receive messages from the external processor server
	// and send them to the dataplane stream in either direction.
	go cs.recvFromProcServerLoop(cs.ctx, done, newStream)

	return cs, nil
}

// clientStream implements resolver.ClientStream to coordinate bidirectional
// message exchanges between the application client, the external processor, and
// the backend dataplane.
type clientStream struct {
	ctx       context.Context
	cancel    context.CancelFunc
	extCancel func()
	config    baseConfig // parsed configuration for this interceptor

	dataplaneStream resolver.ClientStream                           // underlying gRPC stream to the backend
	extStream       v3procservicepb.ExternalProcessor_ProcessClient // bidirectional stream to the external processor
	streamFailed    *grpcsync.Event                                 // fired when external processor stream has closed and RPC should be failed
	extStreamBypass atomic.Bool                                     // set to true when the external processor stream should be bypassed
	extStreamClosed atomic.Bool                                     // ensures the stream closure logic is executed exactly once
	extStreamErr    atomic.Value                                    // holds the terminal error causing the external processor stream to fail

	reqAttrs             map[string]*structpb.Struct             // attributes to be sent to proc server with client message
	initMsgSent          atomic.Bool                             // tracks whether the first stream message has been sent to ensure ProtocolConfig is sent only once
	initClientMsgSent    atomic.Bool                             // tracks whether the first client message has been sent to ensure request attributes are sent only once
	initBodyMsgSent      atomic.Bool                             // tracks whether first body message is sent to lock out FailureModeAllow
	discardRequests      atomic.Bool                             // set when ext_proc server signals end_of_stream to stop client sends
	mutatedReqBuffer     *buffer.Unbounded                       // buffers mutated request body messages from the ext_proc server
	reqForwardingStarted bool                                    // tracks whether request forwarding loop to the dataplane has started
	extSendCh            chan *v3procservicepb.ProcessingRequest // serializes writes to the external processor stream to ensure thread-safety

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
	dataplaneReady          chan struct{}     // closed once dataplaneStream is fully constructed
	responseRecvStarted     bool              // tracks whether response message reading has started

	drainTriggeredCh         chan struct{}   // closed upon receiving a drain request or bypass signal
	requestForwardLoopDoneCh chan struct{}   // closed when request forwarding loop finishes draining
	drainTriggered           atomic.Bool     // guards against multiple closures of drainTriggeredCh
	drained                  *grpcsync.Event // fires when external processor stream is completely drained
}

// Header returns the header metadata received from the server if there
// is any. It blocks if the metadata is not ready to read.
func (cs *clientStream) Header() (metadata.MD, error) {
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		return nil, err
	}
	select {
	case <-cs.responseHeaderModified.Done():
	case <-cs.drainTriggeredCh:
	case <-cs.streamFailed.Done():
		return nil, cs.extStreamErr.Load().(error)
	case <-cs.ctx.Done():
		// Return an Internal status with error (rather than context.Canceled) if
		// the dataplane stream was dropped due to an external processor failure.
		if val := cs.extStreamErr.Load(); val != nil {
			return nil, val.(error)
		}
		return nil, cs.ctx.Err()
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
	// behavior (which is to return nil).
	if len(s.Trailer()) == 0 {
		return nil
	}
	cs.initiateResponseTrailerProcessing()
	select {
	case <-cs.responseTrailerModified.Done():
	case <-cs.drainTriggeredCh:
	case <-cs.streamFailed.Done():
		return nil
	case <-cs.ctx.Done():
		return nil
	}
	return cs.responseTrailers
}

func (cs *clientStream) CloseSend() error {
	extClosed := cs.extStreamBypass.Load()

	// If the stream is closed and we had started sending data from the processor
	// server to the dataplane server, wait for the buffer to finish before
	// calling CloseSend.
	if extClosed && cs.reqForwardingStarted {
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return err
		}
	}

	// If the stream is not started and the processor stream is closed or the mode
	// is skip, send directly on the dataplane stream.
	if extClosed || cs.config.processingModes.requestBodyMode == modeSkip {
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		return s.CloseSend()
	}

	// If external processor stream is active, send to the processor server as
	// request message with `EndOfStreamWithoutMessage` set.
	req := &v3procservicepb.ProcessingRequest{
		Request: &v3procservicepb.ProcessingRequest_RequestBody{
			RequestBody: &v3procservicepb.HttpBody{
				EndOfStreamWithoutMessage: true,
			},
		},
		Attributes:        cs.reqAttrs,
		ObservabilityMode: cs.config.observabilityMode,
	}

	if cs.initMsgSent.CompareAndSwap(false, true) {
		req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
			RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
			ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
		}
	}

	if cs.initClientMsgSent.CompareAndSwap(false, true) {
		req.Attributes = cs.reqAttrs
	}

	if !cs.initBodyMsgSent.Load() {
		cs.initBodyMsgSent.Store(true)
	}

	select {
	case cs.extSendCh <- req:
		return nil
	case <-cs.drainTriggeredCh:
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return err
		}
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		return s.CloseSend()
	case <-cs.streamFailed.Done():
		return cs.extStreamErr.Load().(error)
	case <-cs.ctx.Done():
		// Return an Internal status with error(rather than context.Canceled) if the
		// dataplane stream was dropped due to an external processor failure.
		if val := cs.extStreamErr.Load(); val != nil {
			return val.(error)
		}
		return cs.ctx.Err()
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
	// Initiate response header processing because external processor requires the
	// events to be sent in the correct order, i.e. response header before
	// response message. And if Header() has not already been called, send the
	// response headers to external processor server first.
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		cs.failStream(err)
		return err
	}

	// If all the responses from external processor server has been sent or if the
	// external processor is bypassed, or if the response body mode is skip, then
	// receive directly from the dataplane stream.
	if cs.responseDrained.Load() || (cs.extStreamBypass.Load() && !cs.responseRecvStarted) || cs.config.processingModes.responseBodyMode == modeSkip {
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		if err := s.RecvMsg(m); err != nil {
			// If RecvMsg returns error, fail the RPC incase external processor stream
			// has failed. Otherwise process the trailers.
			if val := cs.extStreamErr.Load(); val != nil {
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

	// Pull from mutatedRespBuffer (which strictly receives mutated
	// StreamedBodyResponses or a nil sentinel).
	select {
	case item := <-cs.mutatedRespBuffer.Get():
		cs.mutatedRespBuffer.Load()
		if item == nil {
			// Nil received from external processor server implies that all messages
			// from the external processor have been sent. Start receiving directly
			// from dataplane stream.
			cs.responseDrained.Store(true)
			if val := cs.extStreamErr.Load(); val != nil {
				return val.(error)
			}
			s, err := cs.waitForDataplaneStream(cs.ctx)
			if err != nil {
				return err
			}
			if err := s.RecvMsg(m); err != nil {
				cs.initiateResponseTrailerProcessing()
				cs.failStream(err)
				return err
			}
			return nil
		}
		// If not nil, unmarshal the message in the provided response.
		streamedResp, ok := item.(*v3procservicepb.StreamedBodyResponse)
		if !ok {
			return fmt.Errorf("extproc: unexpected response type in responseBuffer: %T", item)
		}
		if err := proto.Unmarshal(streamedResp.GetBody(), msg); err != nil {
			return err
		}
		return nil

	case <-cs.ctx.Done():
		// Return an Internal status and error(rather than context.Canceled) if the
		// dataplane stream was dropped due to an external processor failure.
		if cs.streamFailed.HasFired() {
			return cs.extStreamErr.Load().(error)
		}
		return cs.ctx.Err()
	case <-cs.streamFailed.Done():
		return cs.extStreamErr.Load().(error)
	}
}

func (cs *clientStream) SendMsg(m any) error {
	if cs.streamFailed.HasFired() {
		return cs.extStreamErr.Load().(error)
	}
	extClosed := cs.extStreamBypass.Load()

	// If the stream is closed and we started sending messages to the dataplane,
	// it means the drain has been triggered, and we need to wait for the forward
	// loop to finish before sending any more messages.
	if extClosed && cs.reqForwardingStarted {
		if err := cs.waitChannel(cs.requestForwardLoopDoneCh); err != nil {
			return err
		}
	}
	if extClosed || cs.config.processingModes.requestBodyMode == modeSkip {
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		return s.SendMsg(m)
	}

	msg, ok := m.(proto.Message)
	if !ok {
		return fmt.Errorf("extproc: message does not implement proto.Message")
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
		return err
	}

	req := &v3procservicepb.ProcessingRequest{
		Request: &v3procservicepb.ProcessingRequest_RequestBody{
			RequestBody: &v3procservicepb.HttpBody{
				Body: bodyBytes,
			},
		},
		ObservabilityMode: cs.config.observabilityMode,
	}

	if cs.initMsgSent.CompareAndSwap(false, true) {
		req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
			RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
			ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
		}
	}
	if cs.initClientMsgSent.CompareAndSwap(false, true) {
		req.Attributes = cs.reqAttrs
	}
	if !cs.initBodyMsgSent.Load() {
		cs.initBodyMsgSent.Store(true)
	}

	select {
	case cs.extSendCh <- req:
		return nil
	case <-cs.drainTriggeredCh:
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
	case <-cs.streamFailed.Done():
		return cs.extStreamErr.Load().(error)
	case <-cs.ctx.Done():
		return cs.ctx.Err()
	}
}

// responseForwardingToProcServerLoop continuously receives raw response
// messages from the underlying dataplane stream, marshals them, and forwards
// them to the external processor server via extSendCh.
func (cs *clientStream) responseForwardingToProcServerLoop(msgType protoreflect.MessageType) {
	defer func() {
		// Once the external processor server has draind, push the nil sentinel to
		// signal end of processed responses.
		cs.waitChannel(cs.drained.Done())
		cs.mutatedRespBuffer.Put(nil)
	}()

	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}

	for {
		// If the processor stream has closed or the server has drained, we can stop
		// receiving messages in the background and Recv should now directly be
		// called from the cs.RecvMsg() function.
		if cs.extStreamBypass.Load() || cs.drained.HasFired() {
			return
		}

		newMsg := msgType.New().Interface()
		if err := s.RecvMsg(newMsg); err != nil {
			cs.initiateResponseTrailerProcessing()
			cs.CloseSend()
			return
		}

		bodyBytes, err := proto.Marshal(newMsg)
		if err != nil {
			cs.failStream(err)
			return
		}

		req := &v3procservicepb.ProcessingRequest{
			Request: &v3procservicepb.ProcessingRequest_ResponseBody{
				ResponseBody: &v3procservicepb.HttpBody{
					Body: bodyBytes,
				},
			},
			ObservabilityMode: cs.config.observabilityMode,
		}

		if !cs.initBodyMsgSent.Load() {
			cs.initBodyMsgSent.Store(true)
		}
		if cs.initMsgSent.CompareAndSwap(false, true) {
			req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
				RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
				ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
			}
		}

		select {
		case cs.extSendCh <- req:
		case <-cs.drainTriggeredCh:
			// If drain is triggered, wait for external processor server to echo all
			// response messages before pushing this message on the mutatedRespBuffer
			// to ensure correct order of messages.
			<-cs.drained.Done()
			resp := &v3procservicepb.StreamedBodyResponse{
				Body: bodyBytes,
			}
			cs.mutatedRespBuffer.Put(resp)
			return
		case <-cs.streamFailed.Done():
			return
		}
	}
}

// requestForwardingToDataplaneLoop continuously consumes mutated request body
// chunks from mutatedReqBuffer (populated by the external processor server),
// unmarshals them, and forwards the final messages to the backend server.
func (cs *clientStream) requestForwardingToDataplaneLoop(msgType protoreflect.MessageType) {
	defer close(cs.requestForwardLoopDoneCh)
	_, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}
	for item := range cs.mutatedReqBuffer.Get() {
		cs.mutatedReqBuffer.Load()
		if item == nil {
			// If the failure mode allows it, close the dataplane stream when the proc
			// stream fails with no io.EOF error.
			if cs.streamFailed.HasFired() && !cs.config.failureModeAllow && cs.dataplaneStream != nil {
				cs.dataplaneStream.CloseSend()
			}
			return
		}
		streamedResp, ok := item.(*v3procservicepb.StreamedBodyResponse)
		if !ok {
			return
		}

		if streamedResp.GetEndOfStreamWithoutMessage() || streamedResp.GetEndOfStream() {
			cs.dataplaneStream.CloseSend()
			return
		}

		newMsg := msgType.New().Interface()
		if err := proto.Unmarshal(streamedResp.GetBody(), newMsg); err != nil {
			cs.failStream(err)
			return
		}

		if err := cs.dataplaneStream.SendMsg(newMsg); err != nil {
			cs.failStream(err)
			return
		}
	}
}

// recvFromProcServerLoop continuously receives messages from the external
// processor server and redirects them according to the response type.
func (cs *clientStream) recvFromProcServerLoop(ctx context.Context, done func(), newStream func(context.Context, func()) (resolver.ClientStream, error)) {
	defer func() {
		cs.drained.Fire()
		// Push nil sentinel to mutatedReqBuffer to indicate completion of receiving
		// the mutated requests. Do not push nil to mutatedResponseBuffer because we
		// might push the message that has been read when drain is triggered.
		cs.mutatedReqBuffer.Put(nil)
	}()

	// If request header mode is send, the first response should be the mutation
	// for request header. Create the dataplane stream using the mutated header.
	if cs.config.processingModes.requestHeaderMode == modeSend {
		if !cs.processInitialHeaders(ctx, done, newStream) {
			return
		}
	}

	// If header mode is not send or if we receive the header and create the
	// dataplane stream, start receiving from the external processor server.
	for {
		resp, err := cs.extStream.Recv()
		if err != nil {
			cs.failStream(err)
			return
		}
		if resp.GetRequestDrain() {
			// Trigger the drain but continue receiving the drained messages until we
			// get io.EOF.
			cs.triggerDrain()
		}

		if resp.GetImmediateResponse() != nil {
			cs.handleImmediateResponse(resp.GetImmediateResponse())
			return
		}

		switch {
		case resp.GetRequestBody() != nil:
			if cs.config.processingModes.requestBodyMode == modeSkip {
				cs.failStream(fmt.Errorf("extproc: external processor unexpectedly sent request body when request body processing is disabled"))
				return
			}

			bodyResp := resp.GetRequestBody()
			if bodyResp.GetResponse().GetStatus() != v3procservicepb.CommonResponse_CONTINUE {
				cs.failStream(fmt.Errorf("extproc: external processor returned invalid status instead of CONTINUE for request body"))
				return
			}
			streamedResp := bodyResp.GetResponse().GetBodyMutation().GetStreamedResponse()
			if streamedResp == nil {
				cs.failStream(fmt.Errorf("extproc: external processor returned invalid body mutation for request body"))
				return
			}
			if streamedResp.GetEndOfStream() || streamedResp.GetEndOfStreamWithoutMessage() {
				cs.discardRequests.Store(true)
			}
			if streamedResp.GetGrpcMessageCompressed() {
				cs.failStream(fmt.Errorf("extproc: external processor returned compressed grpc message which is not supported for request body"))
				return
			}
			cs.mutatedReqBuffer.Put(streamedResp)

		case resp.GetResponseBody() != nil:
			if cs.config.processingModes.responseBodyMode == modeSkip {
				cs.failStream(fmt.Errorf("extproc: external processor unexpectedly sent response body when response body processing is disabled"))
				return
			}

			// If response headers have been sent, mutated response headers have not
			// been received before receiving the response body message, fail the RPC.
			if cs.config.processingModes.responseHeaderMode == modeSend {
				select {
				case <-cs.responseHeaderModified.Done():
				default:
					cs.failStream(fmt.Errorf("extproc: external processor sent response body before sending response headers"))
					return

				}
			}

			// If mutated response trailers have been received before receiving the
			// response body message, fail the RPC.
			if cs.config.processingModes.responseTrailerMode == modeSend {
				select {
				case <-cs.responseTrailerModified.Done():
					cs.failStream(fmt.Errorf("extproc: external processor sent response body after response trailers were already processed"))
					return
				default:

				}
			}

			bodyResp := resp.GetResponseBody()
			if bodyResp.GetResponse().GetStatus() != v3procservicepb.CommonResponse_CONTINUE {
				cs.failStream(fmt.Errorf("extproc: external processor returned invalid status instead of CONTINUE for response body"))
				return
			}
			streamedResp := bodyResp.GetResponse().GetBodyMutation().GetStreamedResponse()
			if streamedResp == nil {
				cs.failStream(fmt.Errorf("extproc: external processor returned invalid body mutation for response body"))
				return
			}
			if streamedResp.GetGrpcMessageCompressed() {
				cs.failStream(fmt.Errorf("extproc: external processor returned compressed grpc message which is not supported for response body"))
				return
			}
			cs.mutatedRespBuffer.Put(streamedResp)

		case resp.GetResponseHeaders() != nil:
			if cs.config.processingModes.responseHeaderMode == modeSkip {
				cs.failStream(fmt.Errorf("extproc: external processor unexpectedly sent response headers when response header processing is disabled"))
				return
			}

			header := resp.GetResponseHeaders()
			// Check if the status in the header response is CONTINUE; if not, fail
			// the stream.
			if header.GetResponse().GetStatus() != v3procservicepb.CommonResponse_CONTINUE {
				cs.failStream(fmt.Errorf("extproc: external processor returned invalid status instead of CONTINUE for response headers"))
				return
			}
			if err = cs.config.mutationRules.ApplyAdditions(header.GetResponse().GetHeaderMutation().GetSetHeaders(), cs.responseHeader); err != nil {
				cs.failStream(err)
				return
			}
			if err = cs.config.mutationRules.ApplyRemovals(header.GetResponse().GetHeaderMutation().GetRemoveHeaders(), cs.responseHeader); err != nil {
				cs.failStream(err)
				return
			}
			// Signal that the response header is modified and ready to be sent to the
			// client, so that if there is any buffered response body, it can be sent
			// after the header.
			cs.responseHeaderModified.Fire()

		case resp.GetResponseTrailers() != nil:
			if cs.config.processingModes.responseTrailerMode == modeSkip {
				cs.failStream(fmt.Errorf("extproc: external processor unexpectedly sent response trailers when response trailer processing is disabled"))
				return
			}
			trailer := resp.GetResponseTrailers()
			if err = cs.config.mutationRules.ApplyAdditions(trailer.GetHeaderMutation().GetSetHeaders(), cs.responseTrailers); err != nil {
				cs.failStream(err)
				return
			}
			if err = cs.config.mutationRules.ApplyRemovals(trailer.GetHeaderMutation().GetRemoveHeaders(), cs.responseTrailers); err != nil {
				cs.failStream(err)
				return
			}
			// Signal that the response trailer is modified and ready to be sent to
			// the client.
			cs.responseTrailerModified.Fire()
			cs.extStream.CloseSend()
		}
	}
}

// sendToProcServer runs as a dedicated background goroutine that serializes all
// outbound messages to the external processor server. It listens on extSendCh
// for messages to forward. It actively monitors stream lifecycle events:
//   - Drain: If drainTriggeredCh is closed, it initiates a graceful shutdown
//     by sending a half-close (CloseSend) to the processing server.
//   - Cancellation/Failure: It aborts immediately if the context cancels or
//     the underlying stream fails.
//
// Any transmission errors immediately trigger failStream to safely abort the
// data plane RPC.
func (cs *clientStream) sendToProcServerLoop(ctx context.Context) {
	defer func() {
		cs.extStreamBypass.Store(true)
	}()
	for {
		select {
		case req := <-cs.extSendCh:
			if err := cs.extStream.Send(req); err != nil {
				// Send failed. Do not call failStream immediately; the Recv loop will
				// retrieve the actual status error from the server and propagate it.
				return
			}
		case <-cs.drainTriggeredCh:
			cs.extStream.CloseSend()
			return
		case <-cs.streamFailed.Done():
			return
		case <-ctx.Done():
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
		return cs.ctx.Err()
	case <-cs.streamFailed.Done():
		return cs.extStreamErr.Load().(error)
	}
}

// createDataplaneStream initializes the underlying gRPC dataplane stream using
// the provided newStream function.
func (cs *clientStream) createDataplaneStream(newStream func(context.Context, func()) (resolver.ClientStream, error), done func()) error {
	var err error
	if cs.dataplaneStream, err = newStream(cs.ctx, done); err != nil {
		return err
	}
	close(cs.dataplaneReady)
	return nil
}

// failStream handles stream failures, recording errors or bypassing external
// processor based on failureModeAllow configuration.
func (cs *clientStream) failStream(err error) {
	if !cs.extStreamClosed.CompareAndSwap(false, true) {
		return
	}
	if cs.streamFailed.HasFired() {
		return
	}
	if err != io.EOF && (cs.initBodyMsgSent.Load() || !cs.config.failureModeAllow) {
		cs.extStreamErr.Store(status.Errorf(codes.Internal, "extproc: external processor RPC failed: %v", err))
		cs.streamFailed.Fire()
		// Cancel the stream's context to immediately tear down the active dataplane
		// connection and unblock any pending client I/O.
		cs.cancel()
		return
	}
	cs.extStreamBypass.Store(true)
	cs.triggerDrain()
}

// cancelStream immediately terminates the stream with the specified error and
// fires the failure event.
func (cs *clientStream) cancelStream(err error) {
	if !cs.extStreamClosed.CompareAndSwap(false, true) {
		return
	}
	cs.extStreamErr.Store(err)
	if cs.dataplaneStream != nil {
		cs.dataplaneStream.CloseSend()
	}
	cs.streamFailed.Fire()
	// Cancel the stream's context to immediately tear down the active
	// dataplane connection and unblock any pending client I/O.
	cs.cancel()
	cs.extStreamBypass.Store(true)
}

// handleInitError handles failures during the initialization of the external
// processor stream in NewStream. It returns a new dataplane stream if failure
// mode allows, else returns the error.
func (cs *clientStream) handleInitError(err error, newStream func(context.Context, func()) (resolver.ClientStream, error), done func()) (resolver.ClientStream, error) {
	if cs.extCancel != nil {
		cs.extCancel()
	}
	if !cs.config.failureModeAllow {
		done()
		return nil, status.Errorf(codes.Internal, "extproc: %v", err)
	}
	if err := cs.createDataplaneStream(newStream, done); err != nil {
		return nil, err
	}
	cs.extStreamBypass.Store(true)
	return cs, nil
}

// handleHeaderError handles failures that occur during the initial request
// headers phase. If the failure mode allows it, the external processor is
// bypassed and the direct dataplane stream is created.
func (cs *clientStream) handleHeaderError(err error, done func(), newStream func(context.Context, func()) (resolver.ClientStream, error)) {
	if err != io.EOF && !cs.config.failureModeAllow {
		cs.extStreamErr.Store(status.Errorf(codes.Internal, "extproc: %v", err))
		cs.streamFailed.Fire()
		return
	}
	cs.triggerDrain()
	if err := cs.createDataplaneStream(newStream, done); err != nil {
		cs.extStreamErr.Store(status.Errorf(codes.Internal, "extproc: failed to create dataplane stream during bypass: %v", err))
		cs.streamFailed.Fire()
	}
}

// processInitialHeaders waits for the initial header response from the external
// processor server, applies any requested header mutations to the outgoing
// context, and initializes the data plane stream. It returns true on success,
// or false if the stream is aborted due to an error, immediate response, or
// invalid status.
func (cs *clientStream) processInitialHeaders(ctx context.Context, done func(), newStream func(context.Context, func()) (resolver.ClientStream, error)) bool {
	outgoingMD, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		outgoingMD = metadata.MD{}
	}
	resp, err := cs.extStream.Recv()
	if err != nil {
		cs.handleHeaderError(err, done, newStream)
		return false
	}
	if resp.GetRequestDrain() {
		cs.triggerDrain()
	}
	if resp.GetImmediateResponse() != nil {
		if cs.config.disableImmediateResponse {
			cs.handleHeaderError(status.Errorf(codes.Internal, "external processor sent an immediate response but immediate responses are disabled in configuration"), done, newStream)
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
	if resp.GetRequestHeaders() == nil {
		err := fmt.Errorf("external processor returned an unexpected message type, expected request headers response)")
		cs.handleHeaderError(err, done, newStream)
		return false
	}
	header := resp.GetRequestHeaders()
	// Check if status in header response is CONTINUE; if not, fail the stream.
	if header.GetResponse().GetStatus() != v3procservicepb.CommonResponse_CONTINUE {
		cs.handleHeaderError(fmt.Errorf("external processor returned invalid status instead of CONTINUE for request headers"), done, newStream)
		return false
	}
	// Mutate the outgoing headers with additions and removals received from the
	// external processor.
	if err = cs.config.mutationRules.ApplyAdditions(header.GetResponse().GetHeaderMutation().GetSetHeaders(), outgoingMD); err != nil {
		cs.handleHeaderError(err, done, newStream)
		return false
	}
	if err = cs.config.mutationRules.ApplyRemovals(header.GetResponse().GetHeaderMutation().GetRemoveHeaders(), outgoingMD); err != nil {
		cs.handleHeaderError(err, done, newStream)
		return false
	}
	dataplaneCtx := metadata.NewOutgoingContext(ctx, outgoingMD)
	dataplaneStream, err := newStream(dataplaneCtx, done)
	if err != nil {
		cs.extStreamErr.Store(status.Errorf(codes.Internal, "failed to create dataplane stream after header mutation: %v", err))
		cs.streamFailed.Fire()
		return false
	}
	cs.dataplaneStream = dataplaneStream
	close(cs.dataplaneReady)
	return true
}

func (cs *clientStream) handleImmediateResponse(imm *v3procservicepb.ImmediateResponse) {
	if cs.config.disableImmediateResponse {
		cs.failStream(fmt.Errorf("extproc: external processor sent an immediate response but immediate responses are disabled in configuration"))
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
		cs.extStreamErr.Store(err)
		cs.responseTrailerModified.Fire()
	} else {
		cs.cancelStream(err)
	}
}

func (cs *clientStream) triggerDrain() {
	if cs.drainTriggered.CompareAndSwap(false, true) {
		close(cs.drainTriggeredCh)
	}
}

// waitForDataplaneStream waits for the dataplane stream to be created or for
// the context to be done. It also checks if the processor stream has not ended
// abruptly with a non-io.EOF error.
func (cs *clientStream) waitForDataplaneStream(ctx context.Context) (resolver.ClientStream, error) {
	select {
	case <-cs.dataplaneReady:
		if cs.streamFailed.HasFired() {
			return nil, cs.extStreamErr.Load().(error)
		}
		if cs.dataplaneStream == nil {
			return nil, cs.extStreamErr.Load().(error)
		}
		return cs.dataplaneStream, nil
	case <-ctx.Done():
		// Return an Internal status and error (rather than context.Canceled) if the
		// dataplane stream was dropped due to an external processor failure.
		if cs.streamFailed.HasFired() {
			return nil, cs.extStreamErr.Load().(error)
		}
		return nil, ctx.Err()
	case <-cs.streamFailed.Done():
		return nil, cs.extStreamErr.Load().(error)
	}
}

func (cs *clientStream) initiateResponseHeaderProcessing() error {
	var err error
	cs.responseHeaderOnce.Do(func() {
		s, waitErr := cs.waitForDataplaneStream(cs.ctx)
		if waitErr != nil {
			err = waitErr
			return
		}
		header, headerErr := s.Header()
		if headerErr != nil {
			// Prefer the external processor error over the dataplane stream header
			// error.
			if cs.streamFailed.HasFired() {
				err = cs.extStreamErr.Load().(error)
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
		if cs.config.processingModes.responseHeaderMode == modeSend && !cs.extStreamBypass.Load() {
			req := &v3procservicepb.ProcessingRequest{
				Request: &v3procservicepb.ProcessingRequest_ResponseHeaders{ResponseHeaders: &v3procservicepb.HttpHeaders{
					Headers:     httpfilter.ConstructHeaderMap(header, cs.config.allowedHeaders, cs.config.disallowedHeaders),
					EndOfStream: cs.trailersOnly,
				}},
				ObservabilityMode: cs.config.observabilityMode,
			}
			if cs.initMsgSent.CompareAndSwap(false, true) {
				req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
					RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
					ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
				}
			}
			select {
			case cs.extSendCh <- req:
			case <-cs.ctx.Done():
				// Return an Internal status and error(rather than context.Canceled) if
				// the dataplane stream was dropped due to an external processor
				// failure.
				if cs.streamFailed.HasFired() {
					err = cs.extStreamErr.Load().(error)
				} else {
					err = cs.ctx.Err()
				}
			case <-cs.streamFailed.Done():
				err = cs.extStreamErr.Load().(error)
			case <-cs.drainTriggeredCh:
				cs.responseHeaderModified.Fire()
			}
		} else {
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
		if cs.config.processingModes.responseTrailerMode == modeSend && !cs.extStreamBypass.Load() && !cs.trailersOnly {
			req := &v3procservicepb.ProcessingRequest{
				Request: &v3procservicepb.ProcessingRequest_ResponseTrailers{ResponseTrailers: &v3procservicepb.HttpTrailers{
					Trailers: httpfilter.ConstructHeaderMap(cs.responseTrailers, cs.config.allowedHeaders, cs.config.disallowedHeaders),
				}},
				ObservabilityMode: cs.config.observabilityMode,
			}
			if cs.initMsgSent.CompareAndSwap(false, true) {
				req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
					RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
					ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
				}
			}
			select {
			case cs.extSendCh <- req:
				cs.trailerSent.Store(true)
			case <-cs.ctx.Done():
			case <-cs.streamFailed.Done():
			case <-cs.drainTriggeredCh:
				cs.responseTrailerModified.Fire()
			}
		} else {
			cs.responseTrailerModified.Fire()
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

func getHeader(md metadata.MD, key string) string {
	vs := md.Get(key)
	return strings.Join(vs, ",")
}

// constructRequestAttributes builds a map of request attributes specified in
// the interceptor config.
func constructRequestAttributes(rpcInfo resolver.RPCInfo, md metadata.MD, requestedAttributes []string) (map[string]*structpb.Struct, error) {
	if len(requestedAttributes) == 0 {
		return nil, nil
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
				headers[k] = strings.Join(values, ",")
			}
			reqFields[attr] = headers
		case "request.referer":
			if val := getHeader(md, "referer"); val != "" {
				reqFields[attr] = val
			}
		case "request.useragent":
			if val := getHeader(md, "user-agent"); val != "" {
				reqFields[attr] = val
			}
		case "request.id":
			if val := getHeader(md, "x-request-id"); val != "" {
				reqFields[attr] = val
			}
		case "request.query":
			reqFields[attr] = ""
		default:
			// Unknown/unsupported attributes are ignored.
		}
	}

	reqStruct, err := structpb.NewStruct(reqFields)
	if err != nil {
		return nil, err
	}

	return map[string]*structpb.Struct{
		"envoy.filters.http.ext_proc": reqStruct,
	}, nil
}
