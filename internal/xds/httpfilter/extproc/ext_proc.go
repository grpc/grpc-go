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
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/resolver"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/internal/xds/httpfilter"
	iextproc "google.golang.org/grpc/internal/xds/httpfilter/extproc/internal"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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

func (builder) BuildClientFilter(opts httpfilter.ClientFilterOptions) httpfilter.ClientFilter {
	return &clientFilter{
		metricsRecorder: opts.MetricsRecorder,
		target:          opts.Target,
	}
}

var _ httpfilter.ClientFilterBuilder = builder{}

type clientFilter struct {
	metricsRecorder estats.MetricsRecorder
	target          string
}

func (clientFilter) Close() {}

func (cf *clientFilter) BuildClientInterceptor(base, override httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
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
	cc, cancel, err := iextproc.CreateExtProcChannel(config.server)
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to create channel to the external processor server %q: %v", config.server.TargetURI, err)

	}
	return &clientInterceptor{
		config:          config,
		extClient:       v3procservicegrpc.NewExternalProcessorClient(cc),
		closeClient:     cancel,
		metricsRecorder: cf.metricsRecorder,
		target:          cf.target,
	}, nil
}

type clientInterceptor struct {
	config          baseConfig
	extClient       v3procservicegrpc.ExternalProcessorClient
	closeClient     func() error
	metricsRecorder estats.MetricsRecorder
	target          string
}

func (i *clientInterceptor) Close() {
	i.closeClient()
}

func (i *clientInterceptor) NewStream(ctx context.Context, ri resolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	if i.config.observabilityMode {
		ocs := &observabilityClientStream{
			config:                 i.config,
			metricsRecorder:        i.metricsRecorder,
			target:                 i.target,
			clientHeadersStartTime: time.Now(),
			procRecvDone:           make(chan struct{}),
		}

		// Register telemetry label callback to capture backend service.
		ctx = istats.RegisterTelemetryLabelCallback(ctx, func(labels map[string]string) {
			if bs, ok := labels["grpc.lb.backend_service"]; ok {
				ocs.backendService.Store(bs)
			}
		})
		// Create a cancelable context to cancel the dataplane stream and close any
		// goroutines in case of error.
		ocs.ctx, ocs.cancel = context.WithCancel(ctx)

		// Create a new context for the RPC to the external processor server. This
		// context has a deadline of the timeout specified in the config, if
		// present. It also contains the initial metadata specified in the config.
		extProcCtx, cancel := context.WithCancel(ocs.ctx)
		if i.config.server.Timeout != 0 {
			extProcCtx, cancel = context.WithTimeout(ocs.ctx, i.config.server.Timeout)
		}
		ocs.procCancel = cancel
		extProcCtx = metadata.NewOutgoingContext(extProcCtx, i.config.server.InitialMetadata)

		var err error
		// In the ClientStream API, outgoing headers are sent immediately when the
		// stream is created. Because we need to send these headers to the external
		// processor server before they go over the wire, we must establish the
		// ext_proc stream now rather than deferring it.
		ocs.procStream, err = i.extClient.Process(extProcCtx)
		if err != nil {
			return ocs.handleProcStreamObsInitError(fmt.Errorf("external processor failed to start: %v", err), newStream, opts...)
		}

		// Build request attributes upfront to avoid adding protobuf allocation
		// overhead to the critical RPC data path.
		outgoingMD, added, _ := metadataFromOutgoingContextRaw(ctx)
		ocs.reqAttrs = constructRequestAttributes(ri, outgoingMD, added, i.config.requestAttributes)

		// If the request header processing mode is set to "Send", forward the
		// headers to the external processor server.
		if i.config.processingModes.requestHeaderMode == modeSend {
			headerReq := ocs.newProcessingRequest(true)
			headerReq.Request = &v3procservicepb.ProcessingRequest_RequestHeaders{
				RequestHeaders: &v3procservicepb.HttpHeaders{
					Headers: httpfilter.ConstructHeaderMap(outgoingMD, added, i.config.allowedHeaders, i.config.disallowedHeaders),
				},
			}
			if err = ocs.procStream.Send(headerReq); err != nil {
				return ocs.handleProcStreamObsInitError(fmt.Errorf("failed to send client headers to external processor server: %v", err), newStream, opts...)
			}
		}

		onFinishFunc := func(error) {
			go func() {
				time.Sleep(ocs.config.deferredCloseTimeout)
				ocs.procCancel()
			}()
		}
		newOpts := append(opts, grpc.OnFinish(onFinishFunc))

		if ocs.dataplaneStream, err = newStream(ocs.ctx, newOpts...); err != nil {
			ocs.cancel()
			return nil, err
		}
		ocs.recordMetric(clientHeadersDurationMetric, time.Since(ocs.clientHeadersStartTime).Seconds())

		// Start background goroutine to receive any messages from the external
		// processor server and discard them.
		go ocs.discardProcessorResponsesLoop()

		return ocs, nil
	}
	return newStream(ctx, opts...)
}

// clientStream implements resolver.ClientStream to coordinate bidirectional
// message exchanges between the application client, the external processor, and
// the backend dataplane.
type observabilityClientStream struct {
	ctx        context.Context
	cancel     func()
	procCancel func()
	config     baseConfig // parsed configuration for this interceptor

	dataplaneStream  grpc.ClientStream                               // underlying gRPC stream to the backend
	procStream       v3procservicepb.ExternalProcessor_ProcessClient // bidirectional stream to external processor
	procStreamBypass atomic.Bool                                     // set to true when the external processor stream should be bypassed
	procStreamClosed atomic.Bool                                     // ensures the stream closure logic is executed exactly once
	procStreamErr    atomic.Value                                    // holds the terminal error causing the external processor stream to fail
	procRecvDone     chan struct{}                                   // closed when discardProcessorResponsesLoop detects stream closure/failure

	protocolConfigSent  atomic.Bool                 // tracks whether the first stream message has been sent to ensure ProtocolConfig is sent only once
	reqAttrsSent        atomic.Bool                 // tracks whether the first client message has been sent to ensure request attributes are sent only once
	reqAttrs            map[string]*structpb.Struct // holds the request attributes
	trailersOnly        bool                        // tracks whether the backend response is trailers-only
	responseHeaderOnce  atomic.Bool                 // ensures response headers are forwarded to the external processor only once
	responseTrailerOnce atomic.Bool                 // ensures response trailers are forwarded to the external processor only once
	responseRecvStarted bool                        // tracks whether RecvMsg has started processing response messages
	mu                  sync.Mutex                  // guards concurrent writes to the external processor stream

	metricsRecorder        estats.MetricsRecorder
	target                 string
	backendService         atomic.Value
	clientHeadersStartTime time.Time
}

// newProcessingRequest creates a new ProcessingRequest with ObservabilityMode,
// ProtocolConfig, Attributes fields initialized.
func (ocs *observabilityClientStream) newProcessingRequest(isClientMessage bool) *v3procservicepb.ProcessingRequest {
	req := &v3procservicepb.ProcessingRequest{
		ObservabilityMode: ocs.config.observabilityMode,
	}

	if ocs.protocolConfigSent.CompareAndSwap(false, true) {
		req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
			RequestBodyMode:  convertBodyMode(ocs.config.processingModes.requestBodyMode),
			ResponseBodyMode: convertBodyMode(ocs.config.processingModes.responseBodyMode),
		}
	}

	if isClientMessage && ocs.reqAttrsSent.CompareAndSwap(false, true) {
		req.Attributes = ocs.reqAttrs
	}
	return req
}

func (ocs *observabilityClientStream) recordMetric(handle *estats.Float64HistoHandle, duration float64) {
	if ocs.metricsRecorder == nil {
		return
	}
	bs := ""
	if v := ocs.backendService.Load(); v != nil {
		bs = v.(string)
	}
	handle.Record(ocs.metricsRecorder, duration, ocs.target, bs)
}

// Header returns the header metadata received from the server if there is any.
// It blocks if the metadata is not ready to read.
func (ocs *observabilityClientStream) Header() (metadata.MD, error) {
	// If the external processor has failed and RPC needs to be failed, fail the
	// RPC with the error received from the external processor stream.
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return nil, fatalErr
	}
	if err := ocs.initiateResponseHeaderProcessing(); err != nil {
		return nil, err
	}
	return ocs.dataplaneStream.Header()
}

func (ocs *observabilityClientStream) Trailer() metadata.MD {
	if _, ok := ocs.procStreamErr.Load().(error); ok {
		return nil
	}

	// If trailer is not yet ready, return nil. This may happen when Trailer() is
	// called before the server sends trailers, or before the entire response has
	// been received.
	trailer := ocs.dataplaneStream.Trailer()
	if trailer == nil {
		return nil
	}
	ocs.initiateResponseTrailerProcessing()
	return trailer
}

func (ocs *observabilityClientStream) CloseSend() error {
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return fatalErr
	}
	var err error
	startTime := time.Now()
	if !ocs.procStreamBypass.Load() && ocs.config.processingModes.requestBodyMode == modeSend {
		req := ocs.newProcessingRequest(true)
		req.Request = &v3procservicepb.ProcessingRequest_RequestBody{
			RequestBody: &v3procservicepb.HttpBody{
				EndOfStreamWithoutMessage: true,
				EndOfStream:               true,
			},
		}
		err = ocs.sendToProcessor(req)
	}
	ocs.recordMetric(clientHalfCloseDurationMetric, time.Since(startTime).Seconds())
	if err != nil {
		return err
	}
	return ocs.dataplaneStream.CloseSend()
}

func (ocs *observabilityClientStream) Context() context.Context {
	return ocs.dataplaneStream.Context()
}

func (ocs *observabilityClientStream) sendBodyToProcessor(m any, isClientMessage bool) error {
	msg, ok := m.(proto.Message)
	if !ok {
		return status.Errorf(codes.InvalidArgument, "extproc: message does not implement proto.Message (%T)", m)
	}
	bodyBytes, err := proto.Marshal(msg)
	if err != nil {
		return status.Errorf(codes.Internal, "extproc: failed to marshal message: %v", err)
	}

	req := ocs.newProcessingRequest(isClientMessage)
	if isClientMessage {
		req.Request = &v3procservicepb.ProcessingRequest_RequestBody{
			RequestBody: &v3procservicepb.HttpBody{Body: bodyBytes},
		}
	} else {
		req.Request = &v3procservicepb.ProcessingRequest_ResponseBody{
			ResponseBody: &v3procservicepb.HttpBody{Body: bodyBytes},
		}
	}
	return ocs.sendToProcessor(req)
}

func (ocs *observabilityClientStream) SendMsg(m any) error {
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return fatalErr
	}
	if ocs.config.processingModes.requestBodyMode == modeSend && !ocs.procStreamBypass.Load() {
		if err := ocs.sendBodyToProcessor(m, true); err != nil {
			return err
		}
	}
	return ocs.dataplaneStream.SendMsg(m)
}

func (ocs *observabilityClientStream) RecvMsg(m any) error {
	if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
		return fatalErr
	}

	// If this is the first time RecvMsg is called, initiate response header
	// processing.
	if !ocs.responseRecvStarted {
		ocs.responseRecvStarted = true
		if err := ocs.initiateResponseHeaderProcessing(); err != nil {
			return err
		}
	}

	err := ocs.dataplaneStream.RecvMsg(m)
	if err != nil {
		ocs.initiateResponseTrailerProcessing()
		if fatalErr, ok := ocs.procStreamErr.Load().(error); ok && err == io.EOF {
			return fatalErr
		}
		return err
	}

	if ocs.config.processingModes.responseBodyMode == modeSend && !ocs.procStreamBypass.Load() {
		if err = ocs.sendBodyToProcessor(m, false); err != nil {
			return err
		}
	}
	return nil
}

// initiateResponseTrailerProcessing fetches response trailers from the
// dataplane stream and forwards them to the external processor if configured.
// It is guarded by responseTrailerOnce to run at most once.
func (ocs *observabilityClientStream) initiateResponseTrailerProcessing() {
	if !ocs.responseTrailerOnce.CompareAndSwap(false, true) {
		return
	}
	trailers := ocs.dataplaneStream.Trailer()
	startTime := time.Now()
	if ocs.config.processingModes.responseTrailerMode == modeSend && !ocs.procStreamBypass.Load() && !ocs.trailersOnly {
		req := ocs.newProcessingRequest(false)
		req.Request = &v3procservicepb.ProcessingRequest_ResponseTrailers{ResponseTrailers: &v3procservicepb.HttpTrailers{
			Trailers: httpfilter.ConstructHeaderMap(trailers, nil, ocs.config.allowedHeaders, ocs.config.disallowedHeaders),
		}}
		ocs.sendToProcessor(req)
	}
	ocs.recordMetric(serverTrailersDurationMetric, time.Since(startTime).Seconds())

	// Half-close the external processor stream immediately so the server can
	// shut down gracefully.
	ocs.mu.Lock()
	if !ocs.procStreamBypass.Load() {
		ocs.procStream.CloseSend()
	}
	ocs.mu.Unlock()
}

// initiateResponseHeaderProcessing fetches response headers from the dataplane
// stream, detects if the response is trailers-only, and forwards the response
// headers to the external processor if configured. It is guarded by
// responseHeaderOnce to run at most once.
func (ocs *observabilityClientStream) initiateResponseHeaderProcessing() error {
	if !ocs.responseHeaderOnce.CompareAndSwap(false, true) {
		return nil
	}
	header, err := ocs.dataplaneStream.Header()
	if err != nil {
		return err
	}
	startTime := time.Now()
	// A trailers-only response returns nil from dataplaneStream.Header().
	if header == nil {
		header = ocs.dataplaneStream.Trailer()
		if len(header) > 0 {
			ocs.trailersOnly = true
		}
	}

	if ocs.config.processingModes.responseHeaderMode == modeSend && !ocs.procStreamBypass.Load() {
		req := ocs.newProcessingRequest(false)
		req.Request = &v3procservicepb.ProcessingRequest_ResponseHeaders{ResponseHeaders: &v3procservicepb.HttpHeaders{
			Headers:     httpfilter.ConstructHeaderMap(header, nil, ocs.config.allowedHeaders, ocs.config.disallowedHeaders),
			EndOfStream: ocs.trailersOnly,
		}}
		err = ocs.sendToProcessor(req)
	}
	ocs.recordMetric(serverHeadersDurationMetric, time.Since(startTime).Seconds())
	return err
}

// sendToProcessor sends a ProcessingRequest to the external processor stream.
// If the write fails, it waits for the receive background loop to process the
// closure and returns the resulting error.
func (ocs *observabilityClientStream) sendToProcessor(req *v3procservicepb.ProcessingRequest) error {
	ocs.mu.Lock()
	err := ocs.procStream.Send(req)
	ocs.mu.Unlock()
	if err != nil {
		select {
		case <-ocs.procRecvDone:
		case <-ocs.ctx.Done():
		}
		if fatalErr, ok := ocs.procStreamErr.Load().(error); ok {
			return fatalErr
		}
		if ocs.procStreamBypass.Load() {
			return nil
		}
		return err
	}
	return nil
}

// discardProcessorResponsesLoop runs in a background to continuously read and
// discard messages from the external processor server, while also monitoring
// the stream for closure or failure.
func (ocs *observabilityClientStream) discardProcessorResponsesLoop() {
	for {
		if _, err := ocs.procStream.Recv(); err != nil {
			ocs.failObsProcStream(err)
			return
		}
	}
}

// failObsProcStream handles proc stream closure or failure by updating the
// stream state. If the failure is a non-EOF error and failure mode is deny, it
// stores the error to fail the dataplane RPC. Otherwise, it enables bypass
// mode.
func (ocs *observabilityClientStream) failObsProcStream(err error) {
	if !ocs.procStreamClosed.CompareAndSwap(false, true) {
		return
	}
	defer close(ocs.procRecvDone)
	ocs.procCancel()
	if err != io.EOF && !ocs.config.failureModeAllow {
		ocs.procStreamErr.Store(status.Errorf(codes.Internal, "extproc: external processor RPC failed: %v", err))
		ocs.cancel()
		return
	}
	ocs.procStreamBypass.Store(true)
}

// convertBodyMode converts the body mode from processingMode to
// v3procfilterpb.ProcessingMode_BodySendMode.
func convertBodyMode(mode processingMode) v3procfilterpb.ProcessingMode_BodySendMode {
	if mode == modeSend {
		return v3procfilterpb.ProcessingMode_GRPC
	}
	return v3procfilterpb.ProcessingMode_NONE
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

// handleProcStreamObsInitError handles failures during the initialization of
// the external processor stream in NewStream. If failure mode allows, it
// returns the dataplane stream directly as the proc stream needs to be
// bypassed, else returns the error.
func (ocs *observabilityClientStream) handleProcStreamObsInitError(err error, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ocs.procCancel()

	if !ocs.config.failureModeAllow {
		return nil, status.Errorf(codes.Internal, "extproc: %v", err)
	}
	var newStreamErr error
	if ocs.dataplaneStream, newStreamErr = newStream(ocs.ctx, opts...); newStreamErr != nil {
		ocs.cancel()
		return nil, newStreamErr
	}
	ocs.recordMetric(clientHeadersDurationMetric, time.Since(ocs.clientHeadersStartTime).Seconds())
	return ocs.dataplaneStream, nil
}
