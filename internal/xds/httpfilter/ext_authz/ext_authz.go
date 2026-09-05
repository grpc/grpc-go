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

// Package extauthz implements the xDS External Authorization HTTP filter.
package extauthz

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/status"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	iextauthz "google.golang.org/grpc/internal/xds/httpfilter/ext_authz/internal"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3extauthzpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	v3authgrpc "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	v3authpb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

func init() {
	if envconfig.XDSClientExtAuthzEnabled {
		httpfilter.Register(builder{})
	}
	iextauthz.RegisterForTesting = func() {
		httpfilter.Register(builder{})
	}
	iextauthz.UnregisterForTesting = func() {
		for _, typeURL := range builder.TypeURLs(builder{}) {
			httpfilter.UnregisterForTesting(typeURL)
		}
	}
}

type builder struct{}

func (builder) TypeURLs() []string {
	return []string{
		"type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz",
		"type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthzPerRoute",
	}
}

func parseFilterEnabled(fp *v3corepb.RuntimeFractionalPercent) (fraction, error) {
	if fp == nil {
		return fraction{numerator: 100, denominator: 100}, nil
	}
	fracPercent := fp.GetDefaultValue()
	if fracPercent == nil {
		return fraction{}, fmt.Errorf("extauthz: missing default_value in filter_enabled")
	}

	den := uint32(100)
	switch fracPercent.GetDenominator() {
	case v3typepb.FractionalPercent_TEN_THOUSAND:
		den = 10000
	case v3typepb.FractionalPercent_MILLION:
		den = 1000000
	}

	// If the numerator exceeds the denominator, cap the fractional value at 100%.
	num := min(fracPercent.GetNumerator(), den)
	return fraction{numerator: num, denominator: den}, nil
}

// ParseFilterConfig parses the provided filter configuration.
func (builder) ParseFilterConfig(cfg proto.Message, opts httpfilter.ParseOptions) (httpfilter.FilterConfig, error) {
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extauthz: error parsing config %v: unknown type %T, want *anypb.Any", cfg, cfg)
	}
	msg := new(v3extauthzpb.ExtAuthz)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extauthz: failed to unmarshal config: %v", err)
	}

	filterEnabled, err := parseFilterEnabled(msg.GetFilterEnabled())
	if err != nil {
		return nil, err
	}

	var denyAtDisable bool
	if denyAtDisableFlag := msg.GetDenyAtDisable(); denyAtDisableFlag != nil {
		if denyAtDisableFlag.GetDefaultValue() == nil {
			return nil, fmt.Errorf("extauthz: missing default_value in deny_at_disable")
		}
		denyAtDisable = denyAtDisableFlag.GetDefaultValue().GetValue()
	}

	httpStatus := int32(http.StatusForbidden)
	if st := msg.GetStatusOnError().GetCode(); st != 0 {
		httpStatus = int32(st)
	}
	statusOnError := grpcStatusCode(httpStatus)

	mutationRules, err := httpfilter.HeaderMutationRulesFromProto(msg.GetDecoderHeaderMutationRules())
	if err != nil {
		return nil, err
	}

	var allowedHeaders, disallowedHeaders []matcher.StringMatcher
	if allowed := msg.GetAllowedHeaders(); allowed != nil {
		allowedHeaders, err = httpfilter.ConvertStringMatchers(allowed.GetPatterns())
		if err != nil {
			return nil, err
		}
	}

	if disallowed := msg.GetDisallowedHeaders(); disallowed != nil {
		disallowedHeaders, err = httpfilter.ConvertStringMatchers(disallowed.GetPatterns())
		if err != nil {
			return nil, err
		}
	}

	// Parse the GrpcService last, so that no error path can drop the built
	// credentials: the caller owns them from here on.
	if msg.GetGrpcService() == nil {
		return nil, fmt.Errorf("extauthz: empty grpc_service provided in config %v", cfg)
	}
	server, err := grpcservice.Parse(msg.GetGrpcService(), opts.BootstrapConfig, opts.ServerConfig)
	if err != nil {
		return nil, fmt.Errorf("extauthz: failed to parse grpc_service: %v", err)
	}

	return config{
		grpcService:                server,
		filterEnabled:              filterEnabled,
		denyAtDisable:              denyAtDisable,
		failureModeAllow:           msg.GetFailureModeAllow(),
		failureModeAllowHeaderAdd:  msg.GetFailureModeAllowHeaderAdd(),
		statusOnError:              statusOnError,
		allowedHeaders:             allowedHeaders,
		disallowedHeaders:          disallowedHeaders,
		decoderHeaderMutationRules: mutationRules,
		includePeerCertificate:     msg.GetIncludePeerCertificate(),
	}, nil
}

// ParseFilterConfigOverride parses the provided override configuration.
//
// Note that ExtAuthzPerRoute is unmarshaled to verify its syntax during xDS
// resource validation, no filter configuration object is returned. Per-route
// disabling is supported via the generic FilterConfig wrapper mechanism rather
// than the ExtAuthzPerRoute.disabled field directly.
func (builder) ParseFilterConfigOverride(overrideCfg proto.Message, _ httpfilter.ParseOptions) (httpfilter.FilterConfig, error) {
	m, ok := overrideCfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extauthz: error parsing override config %v: unknown type %T, want *anypb.Any", overrideCfg, overrideCfg)
	}
	msg := new(v3extauthzpb.ExtAuthzPerRoute)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extauthz: failed to unmarshal override config %v: %v", overrideCfg, err)
	}
	return nil, nil
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

// authzChannelEntry holds a refcounted client to an external authorization
// server along with the GrpcService config used to create it. The config is
// used to determine if the channel can be shared across interceptors, and to
// release credentials when the channel is closed.
type authzChannelEntry struct {
	server *grpcservice.Config
	rc     *grpcsync.RefCounted[v3authgrpc.AuthorizationClient]
}

type clientFilter struct {
	// metricsRecorder is used to record client-side ext_authz metrics.
	metricsRecorder estats.MetricsRecorder
	// target is the target URI of the channel, used as a metric label.
	target string

	// mu protects authzChannels.
	mu sync.Mutex
	// authzChannels holds external authorization server channels, enabling
	// connection sharing across interceptors.
	authzChannels []*authzChannelEntry
}

func (*clientFilter) Close() {}

// getAuthzChannel returns an existing refcounted client for grpcService config
// if present and its refcount is incremented successfully.
func (cf *clientFilter) getAuthzChannel(server *grpcservice.Config) *grpcsync.RefCounted[v3authgrpc.AuthorizationClient] {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	if i := slices.IndexFunc(cf.authzChannels, func(e *authzChannelEntry) bool {
		return httpfilter.SharesChannel(e.server, server) && e.rc.TryIncrement()
	}); i != -1 {
		return cf.authzChannels[i].rc
	}
	return nil
}

// storeAuthzChannel stores the created channel entry if no valid channel
// exists for the GrpcService config. If another goroutine already stored a
// channe while unlocked, it increments the existing channel's refcount and
// returns it.
func (cf *clientFilter) storeAuthzChannel(entry *authzChannelEntry) *grpcsync.RefCounted[v3authgrpc.AuthorizationClient] {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	if i := slices.IndexFunc(cf.authzChannels, func(e *authzChannelEntry) bool {
		return httpfilter.SharesChannel(e.server, entry.server) && e.rc.TryIncrement()
	}); i != -1 {
		return cf.authzChannels[i].rc
	}
	cf.authzChannels = append(cf.authzChannels, entry)
	return entry.rc
}

// removeAuthzChannel removes entry from authzChannels. Pointer comparison
// ensures only this specific expiring instance is removed if a replacement
// channel for the same config was created concurrently.
func (cf *clientFilter) removeAuthzChannel(entry *authzChannelEntry) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.authzChannels = slices.DeleteFunc(cf.authzChannels, func(e *authzChannelEntry) bool { return e == entry })
}

// getOrCreateAuthzChannel retrieves an existing refcounted external
// authorization client for an equal server config and increases its refcount,
// or creates a new one if there is none.
func (cf *clientFilter) getOrCreateAuthzChannel(server *grpcservice.Config) (*grpcsync.RefCounted[v3authgrpc.AuthorizationClient], error) {
	// If a channel for the GrpcService config is present and its refcount is
	// greater than 0, increment the refcount and return the channel.
	if rc := cf.getAuthzChannel(server); rc != nil {
		return rc, nil
	}

	// Create the external authorization channel without holding the lock. The
	// release function closes the channel.
	cc, release, err := iextauthz.CreateExtAuthzChannel(server)
	if err != nil {
		return nil, fmt.Errorf("extauthz: failed to create channel to the external authorization server %q: %v", server.TargetURI, err)
	}

	client := v3authgrpc.NewAuthorizationClient(cc)
	// Create a new refcounted client. The onZero cleanup function removes the
	// entry from the list, closes the underlying channel, and releases the
	// credentials of the config generation the channel was created from.
	entry := &authzChannelEntry{server: server}
	entry.rc = grpcsync.NewRefCounted(client, func() {
		cf.removeAuthzChannel(entry)
		release()
		entry.server.Close()
	})

	// Double-check if another goroutine created and stored a channel for an
	// equal config while we were unlocked.
	if existing := cf.storeAuthzChannel(entry); existing != entry.rc {
		entry.rc.Decrement()
		return existing, nil
	}
	return entry.rc, nil
}

// BuildClientInterceptor builds a client interceptor for the external
// authorization filter.
func (cf *clientFilter) BuildClientInterceptor(cfg, _ httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
	c, ok := cfg.(config)
	if !ok {
		return nil, fmt.Errorf("extauthz: incorrect config type provided (%T): %v", cfg, cfg)
	}

	// Create or reuse a refcounted channel to the external authorization server.
	rc, err := cf.getOrCreateAuthzChannel(c.grpcService)
	if err != nil {
		return nil, err
	}

	return &clientInterceptor{
		config:          c,
		authzClient:     rc,
		metricsRecorder: cf.metricsRecorder,
		target:          cf.target,
	}, nil
}

type clientInterceptor struct {
	config          config
	authzClient     *grpcsync.RefCounted[v3authgrpc.AuthorizationClient]
	metricsRecorder estats.MetricsRecorder
	target          string
	closed          atomic.Bool
}

func (i *clientInterceptor) Close() {
	if i.closed.CompareAndSwap(false, true) {
		i.authzClient.Decrement()
	}
}

func (i *clientInterceptor) recordMetric(handle *estats.Int64CountHandle) {
	if i.metricsRecorder == nil {
		return
	}
	handle.Record(i.metricsRecorder, 1, i.target)
}

// isExtAuthzEnabled checks if external authorization is enabled for this RPC.
func (i *clientInterceptor) isExtAuthzEnabled() bool {
	return rand.Uint32N(i.config.filterEnabled.denominator) < i.config.filterEnabled.numerator
}

// check sends a CheckRequest to the external authorization server and returns
// the CheckResponse.
func (i *clientInterceptor) check(ctx context.Context, ri resolver.RPCInfo, outgoingMD metadata.MD) (*v3authpb.CheckResponse, error) {
	// Construct the request header map for the CheckRequest, applying the
	// allowed_headers and disallowed_headers filtering rules if configured.
	headers := httpfilter.ConstructHeaderMap(outgoingMD, nil, i.config.allowedHeaders, i.config.disallowedHeaders).GetHeaders()

	req := &v3authpb.CheckRequest{
		Attributes: &v3authpb.AttributeContext{
			Request: &v3authpb.AttributeContext_Request{
				Time: timestamppb.New(time.Now()),
				Http: &v3authpb.AttributeContext_HttpRequest{
					Method:    "POST",
					HeaderMap: &v3corepb.HeaderMap{Headers: headers},
					Path:      ri.Method,
					Size:      -1,
					Protocol:  "HTTP/2",
				},
			},
		},
	}

	// Prepare the context for the Check RPC by applying the configured timeout
	// and attaching any configured initial metadata.
	var extAuthzCtx context.Context
	var cancel context.CancelFunc
	if i.config.grpcService.Timeout != 0 {
		extAuthzCtx, cancel = context.WithTimeout(ctx, i.config.grpcService.Timeout)
		defer cancel()
	} else {
		extAuthzCtx = ctx
	}
	// Append the initial metadata from the grpc_service configuration to the
	// context used for the Check RPC.
	extAuthzCtx = metadata.NewOutgoingContext(extAuthzCtx, i.config.grpcService.InitialMetadata)
	authClient := i.authzClient.Value()
	return authClient.Check(extAuthzCtx, req)
}

// appendFailureModeHeader appends the x-envoy-auth-failure-mode-allowed: true
// header to md if failure_mode_allow_header_add is configured and the header
// is not already present.
func (i *clientInterceptor) appendFailureModeHeader(md metadata.MD) {
	if i.config.failureModeAllowHeaderAdd && len(md.Get("x-envoy-auth-failure-mode-allowed")) == 0 {
		md.Append("x-envoy-auth-failure-mode-allowed", "true")
	}
}

// applyHeaderMutations applies request header additions and removals from the
// OkHttpResponse to outgoingMD and validates response headers against the
// configured header mutation rules. It records the appropriate metrics and
// returns the validated response headers to add.
func (i *clientInterceptor) applyHeaderMutations(okResp *v3authpb.OkHttpResponse, outgoingMD metadata.MD) ([]*v3corepb.HeaderValueOption, error) {
	var mutationFailed bool
	handleErr := func(err error, desc string) error {
		if !mutationFailed {
			i.recordMetric(extAuthzClientFailedRPCsMetric)
			mutationFailed = true
		}
		if !i.config.failureModeAllow {
			return status.Errorf(i.config.statusOnError, "extauthz: %s: %v", desc, err)
		}
		i.appendFailureModeHeader(outgoingMD)
		return nil
	}

	// Update outgoing metadata with headers specified by the external
	// authorization server.
	if err := i.config.decoderHeaderMutationRules.ApplyAdditions(okResp.GetHeaders(), outgoingMD); err != nil {
		if err := handleErr(err, "error applying header mutation rules"); err != nil {
			return nil, err
		}
	}

	// Update outgoing metadata with headers_to_remove specified by the external
	// authorization server.
	if err := i.config.decoderHeaderMutationRules.ApplyRemovals(okResp.GetHeadersToRemove(), outgoingMD); err != nil {
		if err := handleErr(err, "error applying header mutation rules"); err != nil {
			return nil, err
		}
	}

	// Validate response headers specified by the external authorization server
	// against header mutation rules before creating the data plane stream.
	if err := i.config.decoderHeaderMutationRules.ApplyAdditions(okResp.GetResponseHeadersToAdd(), metadata.MD{}); err != nil {
		if err := handleErr(err, "header validation fails for response headers"); err != nil {
			return nil, err
		}
	}

	if !mutationFailed {
		i.recordMetric(extAuthzClientAllowedRPCsMetric)
		return okResp.GetResponseHeadersToAdd(), nil
	}
	// If any mutation failed and failure_mode_allow is true, do not apply
	// any response header mutations from the failed ext_authz response.
	return nil, nil
}

func (i *clientInterceptor) NewStream(ctx context.Context, ri resolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	// If the interceptor is already closed, no new streams should be created.
	if i.closed.Load() {
		return nil, status.Errorf(codes.Unavailable, "extauthz: interceptor is closed")
	}

	// When the filter is disabled for this RPC based on runtime fraction:
	// - If deny_at_disable is true, the RPC is denied with status_on_error.
	// - Otherwise, external authorization is bypassed and the RPC proceeds.
	if !i.isExtAuthzEnabled() {
		i.recordMetric(extAuthzClientFilterDisabledRPCsMetric)
		if i.config.denyAtDisable {
			return nil, status.Errorf(i.config.statusOnError, "extauthz: RPC denied due to filter disabled")
		}
		return newStream(ctx, opts...)
	}

	// Try to increment authzClient's refcount so the Check RPC keeps the
	// connection open even if the interceptor is closed concurrently. Decrement
	// is deferred to release the reference when NewStream completes. If
	// TryIncrement returns false, the underlying authz client is closed, so
	// we fail the RPC with an Unavailable status.
	if !i.authzClient.TryIncrement() {
		return nil, status.Errorf(codes.Unavailable, "extauthz: authz client is closed")
	}
	defer i.authzClient.Decrement()

	outgoingMD, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		outgoingMD = metadata.MD{}
	}

	resp, err := i.check(ctx, ri, outgoingMD)
	if err != nil {
		i.recordMetric(extAuthzClientFailedRPCsMetric)
		// If the RPC to the ext_authz service fails and the failure_mode_allow
		// config field is set to false, the data plane RPC will be failed with
		// the status derived from the StatusCodeOnError config field.
		if !i.config.failureModeAllow {
			return nil, status.Errorf(i.config.statusOnError, "extauthz: RPC denied due to error calling external authorization server: %v", err)
		}

		// Otherwise, the data plane RPC will be allowed. If the
		// failure_mode_allow_header_add config field is true, then the filter
		// will add a x-envoy-auth-failure-mode-allowed: true header to the data
		// plane RPC.
		i.appendFailureModeHeader(outgoingMD)
		ctx = metadata.NewOutgoingContext(ctx, outgoingMD)
		return newStream(ctx, opts...)
	}

	// When the external authorization server denies the RPC, we do not create
	// a dataplane stream to the backend. Instead, we return a denied client
	// stream wrapper that returns the denied error on stream operations.
	if resp.GetStatus().GetCode() != int32(codes.OK) {
		deniedResp, ok := resp.GetHttpResponse().(*v3authpb.CheckResponse_DeniedResponse)
		if !ok {
			i.recordMetric(extAuthzClientDeniedRPCsMetric)
			// If the status in the response is not OK, and the response does not
			// contain a DeniedResponse message, we fail the RPC with
			// PERMISSION_DENIED.
			return nil, status.Errorf(codes.PermissionDenied, "extauthz: RPC denied by external authorization server")
		}

		// Validate denied response headers.
		trailers := metadata.MD{}
		if headers := deniedResp.DeniedResponse.GetHeaders(); len(headers) > 0 {
			if err := i.config.decoderHeaderMutationRules.ApplyAdditions(headers, trailers); err != nil {
				i.recordMetric(extAuthzClientFailedRPCsMetric)
				if !i.config.failureModeAllow {
					return nil, status.Errorf(i.config.statusOnError, "extauthz: error applying header mutation rules on denied response: %v", err)
				}
				i.appendFailureModeHeader(outgoingMD)
				ctx = metadata.NewOutgoingContext(ctx, outgoingMD)
				return newStream(ctx, opts...)
			}
		}

		i.recordMetric(extAuthzClientDeniedRPCsMetric)

		// Compute the status to return to the caller based on the status returned
		// by the external authorization server.
		code := codes.PermissionDenied
		if st := deniedResp.DeniedResponse.GetStatus(); st != nil {
			code = grpcStatusCode(int32(st.GetCode()))
		}
		msg := "extauthz: RPC denied by external authorization server"
		if text := resp.GetStatus().GetMessage(); text != "" {
			msg = fmt.Sprintf("extauthz: RPC denied by external authorization server: %s", text)
		}
		return newDeniedClientStream(ctx, status.Errorf(code, "%s", msg), trailers, opts), nil
	}

	var responseHeadersToAdd []*v3corepb.HeaderValueOption
	okResp, _ := resp.GetHttpResponse().(*v3authpb.CheckResponse_OkResponse)
	if okResp != nil {
		var err error
		responseHeadersToAdd, err = i.applyHeaderMutations(okResp.OkResponse, outgoingMD)
		if err != nil {
			return nil, err
		}
	} else {
		// If the response does not contain an OkResponse message despite having
		// an OK status, we proceed to create the stream without making any header
		// mutations.
		i.recordMetric(extAuthzClientAllowedRPCsMetric)
	}

	// Create a new context with the mutated outgoing metadata. All subsequent
	// Filters in the chain and the final RPC call will see this new context.
	ctx = metadata.NewOutgoingContext(ctx, outgoingMD)

	// Create the underlying gRPC stream.
	stream, err := newStream(ctx, opts...)
	if err != nil {
		return nil, err
	}

	// If valid response headers to add were specified by the external
	// authorization server, wrap the stream to intercept and append them to the
	// response headers received from the server.
	if len(responseHeadersToAdd) > 0 {
		return &clientStream{
			ClientStream:  stream,
			headersToAdd:  responseHeadersToAdd,
			mutationRules: i.config.decoderHeaderMutationRules,
		}, nil
	}
	return stream, nil
}

// clientStream is a wrapper around grpc.ClientStream that intercepts the
// Header() method to append additional response headers specified by the
// external authorization server.
type clientStream struct {
	grpc.ClientStream
	// headersToAdd holds the response headers specified by the external
	// authorization server to be added to the server's response headers.
	headersToAdd []*v3corepb.HeaderValueOption
	// mutationRules defines the rules governing which headers may be added or
	// modified.
	mutationRules httpfilter.HeaderMutationRules
}

// Header returns the header metadata received from the server if there
// is any. It blocks if the metadata is not ready to read.
func (s *clientStream) Header() (metadata.MD, error) {
	md, err := s.ClientStream.Header()
	if err != nil {
		return nil, err
	}
	if md == nil {
		return nil, nil
	}
	// Since response headers to add were validated in NewStream before
	// wrapping the stream, ApplyAdditions will not fail here.
	s.mutationRules.ApplyAdditions(s.headersToAdd, md)
	return md, nil
}

// newDeniedClientStream returns a synthetic ClientStream that immediately fails
// stream operations with err and returns the specified trailers on Trailer(). It
// extracts and executes OnFinishCallOption callbacks when the stream completes
// or when ctx is canceled.
func newDeniedClientStream(ctx context.Context, err error, trailers metadata.MD, opts []grpc.CallOption) *deniedClientStream {
	// Collect OnFinishCallOption functions from the call options.
	var onFinish []func(error)
	for _, o := range opts {
		if onFinishOpt, ok := o.(grpc.OnFinishCallOption); ok && onFinishOpt.OnFinish != nil {
			onFinish = append(onFinish, onFinishOpt.OnFinish)
		}
	}

	s := &deniedClientStream{
		ctx:             ctx,
		err:             err,
		mutatedTrailers: trailers,
		onFinish:        onFinish,
	}

	// Ensure onFinish callbacks are executed when the context is canceled or
	// expires, even if the caller abandons the stream without invoking any
	// stream methods.
	go func() {
		<-ctx.Done()
		s.finish()
	}()
	return s
}

// deniedClientStream is a synthetic ClientStream returned when the external
// authorization server denies the RPC. It intercepts the stream operations
// to return error without creating a dataplane stream to the backend.
type deniedClientStream struct {
	// ctx is the context of the RPC call.
	ctx context.Context
	// err is the status error to return on stream operations.
	err error
	// mutatedTrailers holds response trailers specified by the external
	// authorization server in the denied response.
	mutatedTrailers metadata.MD
	// onFinish stores OnFinishCallOption callbacks to execute when the stream
	// finishes.
	onFinish []func(error)
	// once ensures onFinish callbacks are executed at most once.
	once sync.Once
}

func (s *deniedClientStream) finish() {
	s.once.Do(func() {
		for _, f := range s.onFinish {
			f(s.err)
		}
	})
}

func (s *deniedClientStream) Header() (metadata.MD, error) {
	s.finish()
	return nil, s.err
}

func (s *deniedClientStream) Trailer() metadata.MD {
	s.finish()
	return s.mutatedTrailers
}

func (s *deniedClientStream) CloseSend() error {
	s.finish()
	return nil
}

func (s *deniedClientStream) Context() context.Context {
	return s.ctx
}

func (s *deniedClientStream) SendMsg(any) error {
	s.finish()
	return s.err
}

func (s *deniedClientStream) RecvMsg(any) error {
	s.finish()
	return s.err
}

// grpcStatusCode converts an HTTP status code to a gRPC status code using the
// mapping defined in
// https://github.com/grpc/grpc/blob/master/doc/http-grpc-status-mapping.md
func grpcStatusCode(httpStatus int32) codes.Code {
	if code, ok := transport.HTTPStatusConvTab[int(httpStatus)]; ok {
		return code
	}
	return codes.Unknown
}
