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
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/status"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
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

	// Create a channel to the external processing server.
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
	if i.config.observabilityMode {
		ocs := &observabilityClientStream{
			ctx:          ctx,
			config:       i.config,
			streamFailed: grpcsync.NewEvent(),
		}

		// Create a new context for the RPC to the external processor server. This
		// context has a deadline of the timeout specified in the config, if
		// present. It also contains the outgoing context's metadata, merged with
		// the initial metadata specified in the config.
		extProcCtx := ctx
		if i.config.server.Timeout != 0 {
			extProcCtx, ocs.extCancel = context.WithTimeout(ctx, i.config.server.Timeout)
		}
		outgoingMD, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			outgoingMD = metadata.MD{}
		}
		extProcCtx = metadata.NewOutgoingContext(extProcCtx, metadata.Join(i.config.server.InitialMetadata, outgoingMD))

		// Create new stream to the external processor server.
		extStream, err := i.extClient.Process(extProcCtx)
		if err != nil {
			return ocs.handleInitError(fmt.Errorf("external processor failed to start: %v", err), newStream, done)
		}
		ocs.extStream = extStream

		// Construct request attributes to be sent to the external processor server.
		ocs.reqAttrs, err = constructRequestAttributes(ri, outgoingMD, i.config.requestAttributes)
		if err != nil {
			return ocs.handleInitError(fmt.Errorf("failed to construct attributes: %v", err), newStream, done)
		}
		// If the request header processing mode is set to "Send", forward the
		// headers to the external processor server.
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
				Attributes: ocs.reqAttrs,
			}
			if err = extStream.Send(&headerReq); err != nil {
				return ocs.handleInitError(fmt.Errorf("failed to send client headers to external processor server: %v", err), newStream, done)
			}
			// Mark that the initial message has been sent to prevent sending
			// ProtocolConfig on subsequent messages.
			ocs.initMsgSent.Store(true)
			// Mark that the initial client message has been sent to prevent adding
			// request attributes to any other message.
			ocs.initClientMsgSent.Store(true)
		}

		doneFunc := func() {
			// When the dataplane RPC is done, send end-of-stream to signal no more
			// client messages will be sent and also send trailers if needed.
			ocs.initiateRequestEOFProcessing()
			ocs.initiateResponseTrailerProcessing()
			// In observability mode, the dataplane stream may finish before the
			// external processor has finished reading all data off the stream. To
			// prevent early stream closure, we defer calling CloseSend on the
			// external processor stream by the configured deferredCloseTimeout. This
			// must run in a separate goroutine to avoid blocking the critical done()
			// callback thread which executes subchannel/balancer updates.
			go func() {
				time.Sleep(ocs.config.deferredCloseTimeout)
				ocs.mu.Lock()
				if !ocs.extStreamBypass.Load() {
					ocs.extStream.CloseSend()
				}
				ocs.mu.Unlock()
			}()
			done()
		}
		// If there is no error in creating external processor stream, create the
		// dataplane stream.
		if ocs.dataplaneStream, err = newStream(ctx, doneFunc); err != nil {
			return nil, err
		}

		// Start background goroutine to receive any messages from the external
		// processor server and discard them.
		go ocs.discardProcessorResponsesLoop()

		return ocs, nil
	}
	return nil, nil
}

// clientStream implements resolver.ClientStream to coordinate bidirectional
// message exchanges between the application client, the external processor, and
// the backend dataplane.
type observabilityClientStream struct {
	ctx                 context.Context
	extCancel           func()
	config              baseConfig                                      // parsed configuration for this interceptor
	dataplaneStream     resolver.ClientStream                           // underlying gRPC stream to the backend
	extStream           v3procservicepb.ExternalProcessor_ProcessClient // bidirectional stream to external processor
	streamFailed        *grpcsync.Event                                 // fired when the external processor stream has closed and its final status is processed
	extStreamBypass     atomic.Bool                                     // set to true when the external processor stream should be bypassed
	extStreamClosed     atomic.Bool                                     // ensures the stream closure logic is executed exactly once
	extStreamErr        atomic.Value                                    // holds the terminal error causing the external processor stream to fail
	initMsgSent         atomic.Bool                                     // tracks whether the first stream message has been sent to ensure ProtocolConfig is sent only once
	initClientMsgSent   atomic.Bool                                     // tracks whether the first client message has been sent to ensure request attributes are sent only once
	trailersOnly        bool                                            // tracks whether the backend response is trailers-only
	responseHeaderOnce  sync.Once                                       // ensures response headers are forwarded to the external processor only once
	responseTrailerOnce sync.Once                                       // ensures response trailers are forwarded to the external processor only once
	requestEOFOnce      sync.Once                                       // ensures client request end of stream is sent to the external processor only once
	responseRecvStarted bool                                            // tracks whether RecvMsg has started processing response messages
	mu                  sync.Mutex                                      // guards concurrent writes to the external processor stream
	reqAttrs            map[string]*structpb.Struct                     // holds the request attributes
}

func (o *observabilityClientStream) Header() (metadata.MD, error) {
	if fatalErr, ok := o.extStreamErr.Load().(error); ok {
		return nil, fatalErr
	}
	if err := o.initiateResponseHeaderProcessing(); err != nil {
		o.failStream(err)
		return nil, err
	}
	return o.dataplaneStream.Header()
}

func (o *observabilityClientStream) Trailer() metadata.MD {
	if _, ok := o.extStreamErr.Load().(error); ok {
		return nil
	}

	// If trailer is not yet ready, return nil. This may happen when Trailer() is
	// called before the server sends trailers, or before the entire response has
	// been received.
	trailer := o.dataplaneStream.Trailer()
	if trailer == nil {
		return nil
	}
	if err := o.initiateResponseTrailerProcessing(); err != nil {
		return nil
	}
	return trailer
}

func (o *observabilityClientStream) CloseSend() error {
	if fatalErr, ok := o.extStreamErr.Load().(error); ok {
		return fatalErr
	}
	if err := o.initiateRequestEOFProcessing(); err != nil {
		return err
	}
	return o.dataplaneStream.CloseSend()
}

func (o *observabilityClientStream) Context() context.Context {
	return o.dataplaneStream.Context()
}

func (o *observabilityClientStream) SendMsg(m any) error {
	if fatalErr, ok := o.extStreamErr.Load().(error); ok {
		return fatalErr
	}
	if o.config.processingModes.requestBodyMode == modeSend && !o.extStreamBypass.Load() {
		msg, ok := m.(proto.Message)
		if !ok {
			return fmt.Errorf("extproc: request message does not implement proto.Message")
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
			ObservabilityMode: o.config.observabilityMode,
		}
		if o.initMsgSent.CompareAndSwap(false, true) {
			req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
				RequestBodyMode:  convertBodyMode(o.config.processingModes.requestBodyMode),
				ResponseBodyMode: convertBodyMode(o.config.processingModes.responseBodyMode),
			}
		}
		if o.initClientMsgSent.CompareAndSwap(false, true) {
			req.Attributes = o.reqAttrs
		}
		if err := o.sendToProcessor(req); err != nil {
			return err
		}
	}
	return o.dataplaneStream.SendMsg(m)
}

func (o *observabilityClientStream) RecvMsg(m any) error {
	if fatalErr, ok := o.extStreamErr.Load().(error); ok {
		return fatalErr
	}

	// If this is the first time RecvMsg is called, initiate response header
	// processing.
	if !o.responseRecvStarted {
		o.responseRecvStarted = true
		if err := o.initiateResponseHeaderProcessing(); err != nil {
			o.failStream(err)
			return err
		}
	}

	err := o.dataplaneStream.RecvMsg(m)
	if err != nil {
		if trailerErr := o.initiateResponseTrailerProcessing(); trailerErr != nil {
			return trailerErr
		}
		return err
	}

	if o.config.processingModes.responseBodyMode == modeSend && !o.extStreamBypass.Load() {
		msg, ok := m.(proto.Message)
		if !ok {
			return fmt.Errorf("extproc: response message does not implement proto.Message")
		}
		bodyBytes, err := proto.Marshal(msg)
		if err != nil {
			return err
		}

		req := &v3procservicepb.ProcessingRequest{
			Request: &v3procservicepb.ProcessingRequest_ResponseBody{
				ResponseBody: &v3procservicepb.HttpBody{
					Body: bodyBytes,
				},
			},
			ObservabilityMode: o.config.observabilityMode,
		}
		if o.initMsgSent.CompareAndSwap(false, true) {
			req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
				RequestBodyMode:  convertBodyMode(o.config.processingModes.requestBodyMode),
				ResponseBodyMode: convertBodyMode(o.config.processingModes.responseBodyMode),
			}
		}
		if err = o.sendToProcessor(req); err != nil {
			return err
		}
	}
	return nil
}

// initiateResponseTrailerProcessing fetches response trailers from the
// dataplane stream and forwards them to the external processor if configured.
// It is guarded by responseTrailerOnce to run at most once.
func (o *observabilityClientStream) initiateResponseTrailerProcessing() error {
	var err error
	o.responseTrailerOnce.Do(func() {
		trailers := o.dataplaneStream.Trailer()
		if o.config.processingModes.responseTrailerMode == modeSend && !o.extStreamBypass.Load() {
			req := &v3procservicepb.ProcessingRequest{
				Request: &v3procservicepb.ProcessingRequest_ResponseTrailers{ResponseTrailers: &v3procservicepb.HttpTrailers{
					Trailers: httpfilter.ConstructHeaderMap(trailers, o.config.allowedHeaders, o.config.disallowedHeaders),
				}},
				ObservabilityMode: o.config.observabilityMode,
			}
			err = o.sendToProcessor(req)
		}
	})
	return err
}

// initiateResponseHeaderProcessing fetches response headers from the dataplane
// stream, detects if the response is trailers-only, and forwards the response
// headers to the external processor if configured. It is guarded by
// responseHeaderOnce to run at most once.
func (o *observabilityClientStream) initiateResponseHeaderProcessing() error {
	var err error
	o.responseHeaderOnce.Do(func() {
		var header metadata.MD
		header, err = o.dataplaneStream.Header()
		if err != nil {
			return
		}
		// A trailers-only response will contain "grpc-status" in the headers or
		// will return nil, nil on Header() call on dataplane stream.
		if header == nil || len(header.Get("grpc-status")) > 0 {
			o.trailersOnly = true
		}

		if o.config.processingModes.responseHeaderMode == modeSend && !o.extStreamBypass.Load() {
			req := &v3procservicepb.ProcessingRequest{
				Request: &v3procservicepb.ProcessingRequest_ResponseHeaders{ResponseHeaders: &v3procservicepb.HttpHeaders{
					Headers:     httpfilter.ConstructHeaderMap(header, o.config.allowedHeaders, o.config.disallowedHeaders),
					EndOfStream: o.trailersOnly,
				}},
				ObservabilityMode: o.config.observabilityMode,
			}
			if o.initMsgSent.CompareAndSwap(false, true) {
				req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
					RequestBodyMode:  convertBodyMode(o.config.processingModes.requestBodyMode),
					ResponseBodyMode: convertBodyMode(o.config.processingModes.responseBodyMode),
				}
			}
			err = o.sendToProcessor(req)
		}
	})
	return err
}

// initiateRequestEOFProcessing sends an end-of-stream indicator to the external
// processor if request body processing is configured to SEND. It is guarded by
// requestEOFOnce to run at most once.
func (o *observabilityClientStream) initiateRequestEOFProcessing() error {
	var err error
	o.requestEOFOnce.Do(func() {
		if !o.extStreamBypass.Load() && o.config.processingModes.requestBodyMode == modeSend {
			req := &v3procservicepb.ProcessingRequest{
				Request: &v3procservicepb.ProcessingRequest_RequestBody{
					RequestBody: &v3procservicepb.HttpBody{
						EndOfStreamWithoutMessage: true,
					},
				},
				ObservabilityMode: o.config.observabilityMode,
			}
			err = o.sendToProcessor(req)
		}
	})
	return err
}

// sendToProcessor sends a ProcessingRequest to the external processor stream.
// If the write fails, it waits for the receive background loop to process the
// closure and returns the resulting error.
func (o *observabilityClientStream) sendToProcessor(req *v3procservicepb.ProcessingRequest) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.extStreamBypass.Load() {
		return nil
	}
	err := o.extStream.Send(req)
	if err != nil {
		// Send failed (likely due to stream closure). Unlock and wait for the
		// background loop to retrieve the raw error via Recv() and process it.
		o.mu.Unlock()
		<-o.streamFailed.Done()
		o.mu.Lock()
		if fatalErr, ok := o.extStreamErr.Load().(error); ok {
			return fatalErr
		}
		if o.extStreamBypass.Load() {
			return nil
		}
		return fmt.Errorf("extproc: external processor Send failed: %v", err)
	}
	return nil
}

// discardProcessorResponsesLoop runs in a background to continuously read and
// discard messages from the external processor server, while also monitoring
// the stream for closure or failure.
func (o *observabilityClientStream) discardProcessorResponsesLoop() {
	for {
		if _, err := o.extStream.Recv(); err != nil {
			o.failStream(err)
			return
		}
	}
}

// failStream handles stream closure or failure by updating the stream state. If
// the failure is a non-EOF error and failure mode is deny, it stores the error
// to fail the dataplane RPC. Otherwise, it enables bypass mode.
func (o *observabilityClientStream) failStream(err error) {
	if !o.extStreamClosed.CompareAndSwap(false, true) {
		return
	}
	defer o.streamFailed.Fire()
	if err != io.EOF && !o.config.failureModeAllow {
		o.extStreamErr.Store(status.Errorf(codes.Internal, "extproc: external processor RPC failed: %v", err))
		return
	}
	o.extStreamBypass.Store(true)
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

func getHeader(md metadata.MD, key string) string {
	vs := md.Get(key)
	return strings.Join(vs, ",")
}

// handleInitError handles failures during the initialization of the external
// processor stream in NewStream. It returns a new dataplane stream if failure
// mode allows, else returns the error.
func (o *observabilityClientStream) handleInitError(err error, newStream func(context.Context, func()) (resolver.ClientStream, error), done func()) (resolver.ClientStream, error) {
	if o.extCancel != nil {
		o.extCancel()
	}
	if !o.config.failureModeAllow {
		done()
		return nil, status.Errorf(codes.Internal, "extproc: %v", err)
	}
	if o.dataplaneStream, err = newStream(o.ctx, done); err != nil {
		return nil, err
	}
	o.extStreamBypass.Store(true)
	return o, nil
}
