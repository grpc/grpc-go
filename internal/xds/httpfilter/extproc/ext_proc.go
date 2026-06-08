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
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

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

var (
	// TODO: Remove this once we have code that actually uses messages from this
	// protobuf package.
	_ = v3procservicepb.ProcessingRequest{}

	parseGRPCServiceConfig = func(*v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("parseGRPCServiceConfig not implemented")
	}
	createExtProcChannel = func(xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		return nil, nil, fmt.Errorf("dialing external processing server not implemented")
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
	server, err := parseGRPCServiceConfig(msg.GetGrpcService())
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
		server, err := parseGRPCServiceConfig(override.GetGrpcService())
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

func (clientFilter) BuildClientInterceptor(base, override httpfilter.FilterConfig) (resolver.ClientInterceptor, error) {
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
	cc, cancel, err := createExtProcChannel(config.server)
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to create client: %v", err)
	}
	return &clientInterceptor{
		config:    config,
		extClient: v3procservicegrpc.NewExternalProcessorClient(cc),
		cancel:    cancel,
	}, nil
}

type clientInterceptor struct {
	resolver.ClientInterceptor
	config    baseConfig
	extClient v3procservicegrpc.ExternalProcessorClient
	cancel    func() error
}

func (i *clientInterceptor) Close() {
	i.cancel()
}
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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/resolver"
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
	v3procservicepb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

func init() {
	if envconfig.XDSClientExtProcEnabled {
		httpfilter.Register(builder{})
	}
}

// RegisterForTesting registers the RBAC HTTP Filter for testing purposes, regardless
// of the RBAC environment variable. This is needed because there is no way to set the RBAC
// environment variable to true in a test before init() in this package is run.
func RegisterForTesting() {
	httpfilter.Register(builder{})
}

// UnregisterForTesting unregisters the RBAC HTTP Filter for testing purposes. This is needed because
// there is no way to unregister the HTTP Filter after registering it solely for testing purposes using
// rbac.RegisterForTesting()
func UnregisterForTesting() {
	for _, typeURL := range builder.TypeURLs(builder{}) {
		httpfilter.UnregisterForTesting(typeURL)
	}
}

var (
	parseGRPCServiceConfig = func(gs *v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
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
	createExtProcChannel = func(cfg xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		cc, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return cc, cc.Close, nil
	}

	logger = grpclog.Component("extproc")
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
	server, err := parseGRPCServiceConfig(msg.GetGrpcService())
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
		processingModesOpt = optional.NewValue(processingModesFromProto(pm))
	}

	var serverOpt optional.Optional[xdsresource.GRPCServiceConfig]
	if override.GetGrpcService() != nil {
		server, err := parseGRPCServiceConfig(override.GetGrpcService())
		if err != nil {
			return nil, fmt.Errorf("extproc: failed to parse grpc_service: %v", err)
		}
		serverOpt = optional.NewValue(server)
	}

	var failureModeAllowOpt optional.Optional[bool]
	if override.GetFailureModeAllow() != nil {
		failureModeAllowOpt = optional.NewValue(override.GetFailureModeAllow().GetValue())
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

func (clientFilter) BuildClientInterceptor(base, override httpfilter.FilterConfig) (resolver.ClientInterceptor, error) {
	b, ok := base.(baseConfig)
	if !ok {
		return nil, fmt.Errorf("extproc: incorrect config type provided (%T): %v", base, base)
	}

	var ov overrideConfig
	if override != nil {
		var ok bool
		ov, ok = override.(overrideConfig)
		if !ok {
			return nil, fmt.Errorf("extproc: incorrect override config type provided (%T): %v", override, override)
		}
	}

	config := newInterceptorConfig(b, ov)

	// Create a channel to the external processing server.
	cc, cancel, err := createExtProcChannel(config.server)
	if err != nil {
		return nil, fmt.Errorf("extproc: failed to create client: %v", err)
	}
	extClient := v3procservicepb.NewExternalProcessorClient(cc)

	return &clientInterceptor{
		config:    config,
		extClient: extClient,
		cancel:    cancel,
	}, nil
}

type clientInterceptor struct {
	resolver.ClientInterceptor
	config    baseConfig
	extClient v3procservicepb.ExternalProcessorClient
	cancel    func() error
}

func (i *clientInterceptor) Close() {
	if i.cancel != nil {
		i.cancel()
	}
}

func (i *clientInterceptor) NewStream(ctx context.Context, ri resolver.RPCInfo, done func(), newStream func(ctx context.Context, done func()) (resolver.ClientStream, error)) (resolver.ClientStream, error) {
	cs := &clientStream{
		config:                  i.config,
		failed:                  grpcsync.NewEvent(),
		mutatedReqBuffer:        buffer.NewUnbounded(),
		mutatedRespBuffer:       buffer.NewUnbounded(),
		responseHeaderModified:  grpcsync.NewEvent(),
		responseTrailerModified: grpcsync.NewEvent(),
		dataplaneReady:          make(chan struct{}),
		ctx:                     ctx,
		extSendCh:               make(chan *v3procservicepb.ProcessingRequest),
		drainTriggeredCh:        make(chan struct{}),
		forwardLoopDoneCh:       make(chan struct{}),
		drained:                 grpcsync.NewEvent(),
	}
	// Create a new context for the RPC to the external processing server. This
	// context has a deadline of the timeout specified in the config, if present.
	// It also contains the incoming context's metadata, merged with the initial
	// metadata specified in the config.
	var extProcCtx context.Context
	if i.config.server.Timeout != 0 {
		extProcCtx, _ = context.WithTimeout(ctx, i.config.server.Timeout)
	} else {
		extProcCtx = ctx
	}

	outgoingMD, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		outgoingMD = metadata.MD{}
	}
	extProcCtx = metadata.NewOutgoingContext(extProcCtx, metadata.Join(i.config.server.InitialMetadata, outgoingMD))

	// Create new RPC to the Proc server
	extStream, err := i.extClient.Process(extProcCtx)
	if err != nil {
		if !i.config.failureModeAllow {
			done()
			return nil, status.Errorf(codes.Internal, "extproc: external processor failed to start: %v", err)
		}
		logger.Warning("External processor failed to start: %v", err)
		if err := cs.createDataplaneStream(ctx, done, newStream); err != nil {
			return nil, err
		}
		return cs, nil
	}

	cs.extStream = extStream
	cs.reqAttrs, err = constructAttributes(ri, outgoingMD, i.config.requestAttributes)
	if err != nil {
		if !i.config.failureModeAllow {
			done()
			return nil, status.Errorf(codes.Internal, "extproc: failed to construct attributes: %v", err)
		}
		logger.Warning("Failed to construct attributes: %v", err)
		if err := cs.createDataplaneStream(ctx, done, newStream); err != nil {
			return nil, err
		}
		return cs, nil
	}

	// If the request header processing mode is set to "send", forward the headers
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
			if !i.config.failureModeAllow {
				done()
				return nil, status.Errorf(codes.Internal, "extproc: failed to send header to ext proc server: %v", err)
			}
			logger.Warning("Failed to send header to external processor server: %v", err)
			if err := cs.createDataplaneStream(ctx, done, newStream); err != nil {
				return nil, err
			}
			return cs, nil
		}
		cs.initMsgSent = true
	} else {
		cs.dataplaneStream, err = newStream(ctx, done)
		if err != nil {
			return nil, err
		}
		close(cs.dataplaneReady)
	}

	// Start a background loop for sending to the proc server. Using a single
	// goroutine ensures we don't have concurrent sends.
	go cs.sendToProcServer()

	// Start a goroutine to receive messages from the external proc service and
	// send them to the dataplane stream.
	go cs.recvFromProcServer(ctx, done, newStream)

	return cs, nil
}

type clientStream struct {
	config                   baseConfig
	dataplaneStream          resolver.ClientStream
	extStream                v3procservicepb.ExternalProcessor_ProcessClient
	failed                   *grpcsync.Event // marks that stream has closed and RPC should be failed
	extProcClosed            atomic.Bool // marks that stream is closed and should be bypassed
	streamFailed             atomic.Bool // marks if stream has failed / closed in case of any error
	extProcCloseErr          atomic.Value // stores error
	initMsgSent              bool
	discardRequests          atomic.Bool
	mutatedReqBuffer         *buffer.Unbounded
	mutatedRespBuffer        *buffer.Unbounded
	responseHeader           metadata.MD
	responseHeaderOnce       sync.Once
	responseHeaderModified   *grpcsync.Event
	responseTrailers         metadata.MD
	responseTrailerOnce      sync.Once
	responseTrailerModified  *grpcsync.Event
	sendToDataplaneStarted   bool
	recvFromDataplaneStarted bool
	reqAttrs                 map[string]*structpb.Struct
	dataplaneReady           chan struct{}
	ctx                      context.Context
	extSendCh                chan *v3procservicepb.ProcessingRequest
	drainTriggeredCh         chan struct{}
	forwardLoopDoneCh        chan struct{}
	drainTriggered           atomic.Bool
	drained                  *grpcsync.Event
	drainedConsumer          atomic.Bool
	trailerSent              atomic.Bool
	trailersOnly             bool
}

func (cs *clientStream) Header() (metadata.MD, error) {
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		return nil, err
	}
	select {
	case <-cs.responseHeaderModified.Done():
	case <-cs.drainTriggeredCh:
	case <-cs.failed.Done():
		return nil, cs.extProcCloseErr.Load().(error)
	case <-cs.ctx.Done():
		return nil, cs.ctx.Err()
	}
	return cs.responseHeader, nil
}

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
	// called after CloseRecv or after Recv() returns an error, it should have
	// the dataplane stream created. If it does not, then Trailer has been called
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
	case <-cs.failed.Done():
		return nil
	case <-cs.ctx.Done():
		return nil
	}
	return cs.responseTrailers
}

func (cs *clientStream) CloseSend() error {
	extClosed := cs.extProcClosed.Load()

	// If the stream is closed and we had started sending data from the processor
	// server to the dataplane server, wait for the buffer to finish before
	// calling CloseSend.
	if extClosed && cs.sendToDataplaneStarted {
		if err := cs.waitChannel(cs.forwardLoopDoneCh); err != nil {
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

	// Send to the processor server as request message with EndOfStream
	// without message set.
	req := &v3procservicepb.ProcessingRequest{
		Request: &v3procservicepb.ProcessingRequest_RequestBody{
			RequestBody: &v3procservicepb.HttpBody{
				EndOfStreamWithoutMessage: true,
			},
		},
		Attributes:        cs.reqAttrs,
		ObservabilityMode: cs.config.observabilityMode,
	}

	if !cs.initMsgSent {
		req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
			RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
			ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
		}
		cs.initMsgSent = true
	}

	select {
	case cs.extSendCh <- req:
		return nil
	case <-cs.drainTriggeredCh:
		if err := cs.waitChannel(cs.forwardLoopDoneCh); err != nil {
			return err
		}
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		return s.CloseSend()
	case <-cs.failed.Done():
		return cs.extProcCloseErr.Load().(error)
	case <-cs.ctx.Done():
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
	if err := cs.initiateResponseHeaderProcessing(); err != nil {
		cs.failStream(err)
		return err
	}

	if cs.drainedConsumer.Load() || (cs.extProcClosed.Load() && !cs.recvFromDataplaneStarted) || cs.config.processingModes.responseBodyMode == modeSkip {
	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return err
	}
		if err := s.RecvMsg(m); err != nil {
			if cs.failed.HasFired() {
				return cs.extProcCloseErr.Load().(error)
			}
			cs.initiateResponseTrailerProcessing()
			return err
		}
		return nil
	}

	msg, ok := m.(proto.Message)
	if !ok {
		return fmt.Errorf("response message does not implement proto.Message")
	}

	// Start the background receiving loop on the first RecvMsg call.
	if !cs.recvFromDataplaneStarted {
		cs.recvFromDataplaneStarted = true
		go cs.responseReceivingLoop(msg.ProtoReflect().Type())
	}

	// Pull from mutatedRespBuffer (which strictly receives mutated
	// StreamedBodyResponses or a nil sentinel).
	select {
	case item := <-cs.mutatedRespBuffer.Get():
		cs.mutatedRespBuffer.Load()
		if item == nil {
			cs.drainedConsumer.Store(true)
			if cs.extProcCloseErr.Load() != nil {
				return cs.extProcCloseErr.Load().(error)
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

		streamedResp, ok := item.(*v3procservicepb.StreamedBodyResponse)
		if !ok {
			return fmt.Errorf("unexpected response type in responseBuffer: %T", item)
		}
		if err := proto.Unmarshal(streamedResp.GetBody(), msg); err != nil {
			return err
		}
		return nil

	case <-cs.ctx.Done():
		return cs.ctx.Err()
	case <-cs.failed.Done():
		return cs.extProcCloseErr.Load().(error)
	}
}

// Intercept the messages being sent by the client. The header is already taken
// care of in the newstream. This takes care of the request body.
func (cs *clientStream) SendMsg(m any) error {
	if cs.failed.HasFired() {
		return cs.extProcCloseErr.Load().(error)
	}
	extClosed := cs.extProcClosed.Load()

	// If the stream is closed and we started sending messages to the dataplane, it
	// means the drain has been triggered, and we need to wait for the forward loop
	// to finish before sending any more messages.
	if extClosed && cs.sendToDataplaneStarted {
		if err := cs.waitChannel(cs.forwardLoopDoneCh); err != nil {
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
		return fmt.Errorf("message does not implement proto.Message")
	}

	// 1. Guaranteed launch of request forwarding loop to send the data to the
	// dataplane server. We start it on the first send because we need the message
	// type to send the data to the dataplane server.
	if !cs.sendToDataplaneStarted {
		cs.sendToDataplaneStarted = true
		go cs.requestForwardingToDataplaneServerLoop(msg)
	}

	// 2. Discard Check: If the ExtProc server has already signaled end_of_stream,
	// discard any subsequent client messages.
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
		Attributes:        cs.reqAttrs,
		ObservabilityMode: cs.config.observabilityMode,
	}

	if !cs.initMsgSent {
		req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
			RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
			ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
		}
		cs.initMsgSent = true
	}

	// 3. Bounded Sequence-Safe Handoff
	select {
	case cs.extSendCh <- req:
		return nil

	case <-cs.drainTriggeredCh:
		// DRAIN RACE CAUGHT: The sender loop is shut down, preventing new handoffs.
		// Block the application stream thread until all echoes are flushed.
		if err := cs.waitChannel(cs.forwardLoopDoneCh); err != nil {
			return err
		}
		s, err := cs.waitForDataplaneStream(cs.ctx)
		if err != nil {
			return err
		}
		return s.SendMsg(m)

	case <-cs.failed.Done():
		return cs.extProcCloseErr.Load().(error)
	}
}

func (cs *clientStream) responseReceivingLoop(msgType protoreflect.MessageType) {
	defer func() {
		cs.waitChannel(cs.drained.Done())
		cs.mutatedRespBuffer.Put(nil)
	}()
	s, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}

	for {
		// If the processor stream has closed or the server has drained, we can
		// stop receiving messages in the background and Recv should now directly
		// be called from the cs.RecvMsg() function which will unblock on the
		// dataplane stream.
		if cs.extProcClosed.Load() || cs.drained.HasFired() {
			return
		}

		// Allocate a new message using reflection so we do not overwrite in-flight
		// buffered messages.
		newMsg := msgType.New().Interface()
		if err := s.RecvMsg(newMsg); err != nil {
			cs.initiateResponseTrailerProcessing()
			cs.CloseSend()
				cs.failStream(err)
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
			Attributes:        cs.reqAttrs,
			ObservabilityMode: cs.config.observabilityMode,
		}

		select {
		case cs.extSendCh <- req:
		case <-cs.drainTriggeredCh:
			<-cs.drained.Done()
			resp := &v3procservicepb.StreamedBodyResponse{
				Body: bodyBytes,
			}
			cs.mutatedRespBuffer.Put(resp)
			return
		case <-cs.failed.Done():
			return
		}
	}
}

// Loop to pull data out of request buffer and send to dataplane server.
func (cs *clientStream) requestForwardingToDataplaneServerLoop(reqPrototype proto.Message) {
	defer close(cs.forwardLoopDoneCh)
	_, err := cs.waitForDataplaneStream(cs.ctx)
	if err != nil {
		return
	}
	for item := range cs.mutatedReqBuffer.Get() {
		cs.mutatedReqBuffer.Load()
		if item == nil {
			// If the failure mode allows it, close the dataplane stream when the proc stream fails with no io.EOF error.
			if cs.failed.HasFired() && !cs.config.failureModeAllow && cs.dataplaneStream != nil {
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

		newMsg := reqPrototype.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(streamedResp.GetBody(), newMsg); err != nil {
			cs.extProcCloseErr.Store(err)
			cs.failed.Fire()
			return
		}

		if err := cs.dataplaneStream.SendMsg(newMsg); err != nil {
			cs.extProcCloseErr.Store(err)
			cs.failed.Fire()
			return
		}
	}
}

// waitChannel waits for the provided channel to be closed, while also respecting
// context cancellation and stream failures.
func (cs *clientStream) waitChannel(ch <-chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-cs.ctx.Done():
		return cs.ctx.Err()
	case <-cs.failed.Done():
		return cs.extProcCloseErr.Load().(error)
	}
}

// createDataplaneStream creates a direct dataplane stream and marks the
// external processor stream as closed. This is used to bypass the external
// processor.
func (cs *clientStream) createDataplaneStream(ctx context.Context, done func(), newStream func(context.Context, func()) (resolver.ClientStream, error)) error {
	dataplaneStream, err := newStream(cs.ctx, done)
	if err != nil {
		return err
	}
	cs.extProcClosed.Store(true)
	cs.dataplaneStream = dataplaneStream
	close(cs.dataplaneReady)
	return nil
}

func (cs *clientStream) failStream(err error) {
	if !cs.streamFailed.CompareAndSwap(false, true) {
		return
	}
	if cs.failed.HasFired() {
		return
	}
	if err != io.EOF && !cs.config.failureModeAllow {
		cs.extProcCloseErr.Store(status.Errorf(codes.Internal, "extproc: external processor RPC failed: %v", err))
		if cs.dataplaneStream != nil {
			cs.dataplaneStream.CloseSend()
		}
		cs.failed.Fire()
		return
	}
	logger.Warning("External processor failed: %v", err)
	cs.extProcClosed.Store(true)
	cs.triggerDrain()
}

func (cs *clientStream) cancelStream(err error) {
	if !cs.streamFailed.CompareAndSwap(false, true) {
		return
	}
	cs.extProcCloseErr.Store(err)
	if cs.dataplaneStream != nil {
		cs.dataplaneStream.CloseSend()
	}
	cs.failed.Fire()
	cs.extProcClosed.Store(true)
}

// handleHeaderError handles failures that occur during the initial request
// headers phase. If the failure mode allows it, the external processor is
// bypassed and the direct dataplane stream is created.
func (cs *clientStream) handleHeaderError(err error, ctx context.Context, done func(), newStream func(context.Context, func()) (resolver.ClientStream, error)) {
	if err != io.EOF && !cs.config.failureModeAllow {
		cs.extProcCloseErr.Store(status.Errorf(codes.Internal, "extproc: external processor failed: %v", err))
		cs.failed.Fire()
		return
	}
	logger.Warning("External processor failed: %v", err)
	cs.triggerDrain()
	if err := cs.createDataplaneStream(ctx, done, newStream); err != nil {
		cs.extProcCloseErr.Store(status.Errorf(codes.Internal, "extproc: failed to create dataplane stream during bypass: %v", err))
		cs.failed.Fire()
	}
}

func (cs *clientStream) recvFromProcServer(ctx context.Context, done func(), newStream func(context.Context, func()) (resolver.ClientStream, error)) {
	defer func() {
		cs.drained.Fire()
		// Push nil sentinel to mutatedReqBuffer to indicate completion of receiving
		// the mutated requests. Do not push nil to mutatedResponseBuffer because
		// we might push the message that has been read when drain is triggered.
		cs.mutatedReqBuffer.Put(nil)
	}()

	// If request header mode is send, the first response should be the mutation
	// for request header. Create the dataplane stream using the mutated header.
	if cs.config.processingModes.requestHeaderMode == modeSend {
		if !cs.handleInitialRequestHeaders(ctx, done, newStream) {
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
			// Trigger the drain but continue receiving the drained messages until
			// we get io.EOF.
			cs.triggerDrain()
		}

		if resp.GetImmediateResponse() != nil {
			cs.handleImmediateResponse(resp.GetImmediateResponse())
			return
		}

		switch {
		case resp.GetRequestBody() != nil:
			if cs.config.processingModes.requestBodyMode == modeSkip {
				cs.failStream(fmt.Errorf("unexpected request body response from processing server: mode is set to skip"))
				return
			}
			if resp.GetImmediateResponse() != nil {
				cs.handleImmediateResponse(resp.GetImmediateResponse())
				return
			}
			bodyResp := resp.GetRequestBody()
			if bodyResp.GetResponse().GetStatus() != v3procservicepb.CommonResponse_CONTINUE {
				cs.failStream(fmt.Errorf("proc server returned non-continue status in request body response"))
				return
			}
			streamedResp := bodyResp.GetResponse().GetBodyMutation().GetStreamedResponse()
			if streamedResp == nil {
				cs.failStream(fmt.Errorf("proc server returned invalid body mutation in request body response"))
				return
			}
			if streamedResp.GetEndOfStream() || streamedResp.GetEndOfStreamWithoutMessage() {
				cs.discardRequests.Store(true)
			}
			if streamedResp.GetGrpcMessageCompressed() {
				cs.failStream(fmt.Errorf("proc server returned grpc_message_compressed in request body response"))
				return
			}
			cs.mutatedReqBuffer.Put(streamedResp)

		case resp.GetResponseBody() != nil:
			if cs.config.processingModes.responseBodyMode == modeSkip {
				cs.failStream(fmt.Errorf("proc server sent unsolicited response body response when mode is skip"))
				return
			}
			if resp.GetImmediateResponse() != nil {
				cs.handleImmediateResponse(resp.GetImmediateResponse())
				return
			}
			bodyResp := resp.GetResponseBody()
			if bodyResp.GetResponse().GetStatus() != v3procservicepb.CommonResponse_CONTINUE {
				cs.failStream(fmt.Errorf("proc server returned non-continue status in response body response"))
				return
			}
			streamedResp := bodyResp.GetResponse().GetBodyMutation().GetStreamedResponse()
			if streamedResp == nil {
				cs.failStream(fmt.Errorf("proc server returned invalid body mutation in response body response"))
				return
			}
			if streamedResp.GetGrpcMessageCompressed() {
				cs.failStream(fmt.Errorf("proc server returned grpc_message_compressed in response body response"))
				return
			}
			cs.mutatedRespBuffer.Put(streamedResp)

		case resp.GetResponseHeaders() != nil:
			if cs.config.processingModes.responseHeaderMode == modeSkip {
				cs.failStream(fmt.Errorf("proc server sent unsolicited response headers response when mode is skip"))
				return
			}
			if resp.GetImmediateResponse() != nil {
				cs.handleImmediateResponse(resp.GetImmediateResponse())
				return
			}
			header := resp.GetResponseHeaders()
			// Check if the status in the header response is CONTINUE; if not, fail the stream.
			if header.GetResponse().GetStatus() != v3procservicepb.CommonResponse_CONTINUE {
				cs.failStream(fmt.Errorf("proc server returned non-continue status in response header response"))
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
			// Signal that the response header is modified and ready to be sent
			// to the client, so that if there is any buffered response body, it
			// can be sent after the header.
			cs.responseHeaderModified.Fire()

		case resp.GetResponseTrailers() != nil:
			if cs.config.processingModes.responseTrailerMode == modeSkip {
				cs.failStream(fmt.Errorf("proc server sent unsolicited response trailers response when mode is skip"))
				return
			}
			trailer := resp.GetResponseTrailers()
			if resp.GetImmediateResponse() != nil {
				if cs.config.disableImmediateResponse {
					cs.failStream(status.Error(codes.Internal, "ext_proc server sent immediate_response but disable_immediate_response is true"))
				}
				if err = cs.config.mutationRules.ApplyAdditions(trailer.GetHeaderMutation().GetSetHeaders(), cs.responseTrailers); err != nil {
					cs.failStream(err)
					return
				}
				if err = cs.config.mutationRules.ApplyRemovals(trailer.GetHeaderMutation().GetRemoveHeaders(), cs.responseTrailers); err != nil {
					cs.failStream(err)
					return
				}
				cs.cancelStream(status.Error(codes.Code(resp.GetImmediateResponse().GetGrpcStatus().GetStatus()), resp.GetImmediateResponse().GetDetails()))
				return
			}
			if err = cs.config.mutationRules.ApplyAdditions(trailer.GetHeaderMutation().GetSetHeaders(), cs.responseTrailers); err != nil {
				cs.failStream(err)
				return
			}
			if err = cs.config.mutationRules.ApplyRemovals(trailer.GetHeaderMutation().GetRemoveHeaders(), cs.responseTrailers); err != nil {
				cs.failStream(err)
				return
			}
			// Signal that the response trailer is modified and ready to be sent
			// to the client.
			cs.responseTrailerModified.Fire()
			cs.extStream.CloseSend()
		}
	}
}

func (cs *clientStream) handleInitialRequestHeaders(ctx context.Context, done func(), newStream func(context.Context, func()) (resolver.ClientStream, error)) bool {
	outgoingMD, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		outgoingMD = metadata.MD{}
	}
	resp, err := cs.extStream.Recv()
	if err != nil {
		cs.handleHeaderError(err, ctx, done, newStream)
		return false
	}
	if resp.GetRequestDrain() {
		cs.triggerDrain()
	}
	if resp.GetImmediateResponse() != nil {
		imm := resp.GetImmediateResponse()
		if cs.config.disableImmediateResponse {
			cs.handleHeaderError(status.Error(codes.Internal, "ext_proc server sent immediate_response but disable_immediate_response is true"), ctx, done, newStream)
		} else {
statusCode := codes.Internal
	if imm.GetGrpcStatus() != nil {
		statusCode = codes.Code(imm.GetGrpcStatus().GetStatus())
	}
	err := status.Error(statusCode, imm.GetDetails())
	cs.cancelStream(err)}
		return false
	}
	if resp.GetRequestHeaders() == nil {
		err := fmt.Errorf("the first response is not a header when headers were sent to the proc server")
		cs.handleHeaderError(err, ctx, done, newStream)
		return false
	}
	header := resp.GetRequestHeaders()
	// Check if status in header response is CONTINUE; if not, fail the stream.
	if header.GetResponse().GetStatus() != v3procservicepb.CommonResponse_CONTINUE {
		cs.handleHeaderError(fmt.Errorf("proc server returned non-continue status in request header response"), ctx, done, newStream)
		return false
	}
	// Mutate the outgoing headers with additions and removals received from the external processor.
	if err = cs.config.mutationRules.ApplyAdditions(header.GetResponse().GetHeaderMutation().GetSetHeaders(), outgoingMD); err != nil {
		cs.handleHeaderError(err, ctx, done, newStream)
		return false
	}
	if err = cs.config.mutationRules.ApplyRemovals(header.GetResponse().GetHeaderMutation().GetRemoveHeaders(), outgoingMD); err != nil {
		cs.handleHeaderError(err, ctx, done, newStream)
		return false
	}
	dataplaneCtx := metadata.NewOutgoingContext(ctx, outgoingMD)
	dataplaneStream, err := newStream(dataplaneCtx, done)
	if err != nil {
		cs.extProcCloseErr.Store(status.Errorf(codes.Internal, "extproc: failed to create dataplane stream after header mutation: %v", err))
		cs.failed.Fire()
		return false
	}
	cs.dataplaneStream = dataplaneStream
	close(cs.dataplaneReady)
	return true
}

func (cs *clientStream) handleImmediateResponse(imm *v3procservicepb.ImmediateResponse) {
	if cs.config.disableImmediateResponse {
		cs.failStream(fmt.Errorf("ext_proc server sent immediate_response but disable_immediate_response is true"))
		return
	}

	statusCode := codes.Internal
	if imm.GetGrpcStatus() != nil {
		statusCode = codes.Code(imm.GetGrpcStatus().GetStatus())
	}
	err := status.Error(statusCode, imm.GetDetails())

	if cs.trailerSent.Load() {
		if mutation := imm.GetHeaders(); mutation != nil {
			_ = cs.config.mutationRules.ApplyAdditions(mutation.GetSetHeaders(), cs.responseTrailers)
			_ = cs.config.mutationRules.ApplyRemovals(mutation.GetRemoveHeaders(), cs.responseTrailers)
		}
		cs.extProcCloseErr.Store(err)
		cs.responseTrailerModified.Fire()
	} else {
		cs.cancelStream(err)
	}
}

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

func constructAttributes(ri resolver.RPCInfo, md metadata.MD, requestedAttributes []string) (map[string]*structpb.Struct, error) {
	if len(requestedAttributes) == 0 {
		return nil, nil
	}

	reqFields := make(map[string]any)
	for _, attr := range requestedAttributes {
		switch attr {
		case "request.path", "request.url_path":
			reqFields[attr] = ri.Method
		case "request.host":
			reqFields[attr] = ri.Authority
		case "request.method":
			reqFields[attr] = "POST"
		case "request.headers":
			headers := make(map[string]any)
			for k, vs := range md {
				headers[k] = strings.Join(vs, ",")
			}
			reqFields[attr] = headers
		case "request.referer":
			if r := getHeader(md, "referer"); r != "" {
				reqFields[attr] = r
			}
		case "request.useragent":
			if r := getHeader(md, "user-agent"); r != "" {
				reqFields[attr] = r
			}
		case "request.id":
			if r := getHeader(md, "x-request-id"); r != "" {
				reqFields[attr] = r
			}
		case "request.query":
			reqFields[attr] = ""
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

func (cs *clientStream) triggerDrain() {
	if cs.drainTriggered.CompareAndSwap(false, true) {
		close(cs.drainTriggeredCh)
	}
}

// sendToProcServer is a loop that sends to the external processor.
func (cs *clientStream) sendToProcServer() {
	for {
		select {
		case req := <-cs.extSendCh:
			if err := cs.extStream.Send(req); err != nil {
				cs.failStream(fmt.Errorf("extproc: external processor Send failed: %v", err))
				return
			}
		case <-cs.drainTriggeredCh:
					cs.extStream.CloseSend()
					return
		case <-cs.failed.Done():
			return
		case <-cs.ctx.Done():
			return
		}
	}
}

// waitForDataplaneStream waits for the dataplane stream to be created or for the
// context to be done. It also checks if the processor stream has not ended
// abruptly with a non-io.EOF error.
func (cs *clientStream) waitForDataplaneStream(ctx context.Context) (resolver.ClientStream, error) {
	// if cs.failed.HasFired() {
	// 	return nil, cs.extProcCloseErr.Load().(error)
	// }
	select {
	case <-cs.dataplaneReady:
		if cs.failed.HasFired() {
			return nil, cs.extProcCloseErr.Load().(error)
		}
		if cs.dataplaneStream == nil {
			return nil, cs.extProcCloseErr.Load().(error)
		}
		return cs.dataplaneStream, nil
	case <-ctx.Done():
		// if cs.failed.HasFired() {
		// 	return nil, cs.extProcCloseErr.Load().(error)
		// }
		return nil, ctx.Err()
	case <-cs.failed.Done():
		return nil, cs.extProcCloseErr.Load().(error)
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
			err = headerErr
			return
		}
		cs.responseHeader = header
		// A trailers-only response will contain "grpc-status" in the headers.
		if header == nil || len(header.Get("grpc-status")) > 0 {
			cs.trailersOnly = true
		}
		if cs.config.processingModes.responseHeaderMode == modeSend && !cs.extProcClosed.Load() {
			req := &v3procservicepb.ProcessingRequest{
				Request: &v3procservicepb.ProcessingRequest_ResponseHeaders{ResponseHeaders: &v3procservicepb.HttpHeaders{
					Headers:     httpfilter.ConstructHeaderMap(header, cs.config.allowedHeaders, cs.config.disallowedHeaders),
					EndOfStream: cs.trailersOnly,
				}},
				Attributes:        cs.reqAttrs,
				ObservabilityMode: cs.config.observabilityMode,
			}
			if !cs.initMsgSent {
				req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
					RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
					ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
				}
				cs.initMsgSent = true
			}
			select {
			case cs.extSendCh <- req:
			case <-cs.ctx.Done():
				err = cs.ctx.Err()
			case <-cs.failed.Done():
				err = cs.extProcCloseErr.Load().(error)
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
		if cs.config.processingModes.responseTrailerMode == modeSend && !cs.extProcClosed.Load() && !cs.trailersOnly {
			req := &v3procservicepb.ProcessingRequest{
				Request: &v3procservicepb.ProcessingRequest_ResponseTrailers{ResponseTrailers: &v3procservicepb.HttpTrailers{
					Trailers: httpfilter.ConstructHeaderMap(cs.responseTrailers, cs.config.allowedHeaders, cs.config.disallowedHeaders),
				}},
				Attributes:        cs.reqAttrs,
				ObservabilityMode: cs.config.observabilityMode,
			}
			if !cs.initMsgSent {
				req.ProtocolConfig = &v3procservicepb.ProtocolConfiguration{
					RequestBodyMode:  convertBodyMode(cs.config.processingModes.requestBodyMode),
					ResponseBodyMode: convertBodyMode(cs.config.processingModes.responseBodyMode),
				}
				cs.initMsgSent = true
			}
			select {
			case cs.extSendCh <- req:
				cs.trailerSent.Store(true)
			case <-cs.ctx.Done():
			case <-cs.failed.Done():
			case <-cs.drainTriggeredCh:
				cs.responseTrailerModified.Fire()
			}
		} else {
			cs.responseTrailerModified.Fire()
		}
	})
}
