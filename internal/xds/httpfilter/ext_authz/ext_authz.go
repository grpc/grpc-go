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

func (builder) ParseFilterConfig(cfg proto.Message, _ httpfilter.ParseOptions) (httpfilter.FilterConfig, error) {
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extauthz: error parsing config %v: unknown type %T, want *anypb.Any", cfg, cfg)
	}
	msg := new(v3extauthzpb.ExtAuthz)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extauthz: failed to unmarshal config: %v", err)
	}

	if msg.GetGrpcService() == nil {
		return nil, fmt.Errorf("extauthz: empty grpc_service provided in config %v", cfg)
	}
	server, err := iextauthz.ParseGRPCServiceConfig(msg.GetGrpcService())
	if err != nil {
		return nil, fmt.Errorf("extauthz: failed to parse grpc_service: %v", err)
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
		channels:        make(map[authzClientKey]*grpcsync.RefCounted[v3authgrpc.AuthorizationClient]),
		metricsRecorder: opts.MetricsRecorder,
		target:          opts.Target,
	}
}

var _ httpfilter.ClientFilterBuilder = builder{}

// authzClientKey uniquely identifies an external authorization server
// configuration by its target URI, channel credentials, and call credentials.
// It is used as a map key in clientFilter to share and reuse the external
// authorization server channels.
type authzClientKey struct {
	targetURI          string
	channelCredentials string
	callCredentials    string
}

type clientFilter struct {
	// metricsRecorder is used to record client-side ext_authz metrics.
	metricsRecorder estats.MetricsRecorder
	// target is the target URI of the channel, used as a metric label.
	target string

	// mu protects channels.
	mu sync.Mutex
	// channels maps external authorization server configuration keys to their
	// ref-counted gRPC clients, enabling connection sharing across interceptors.
	channels map[authzClientKey]*grpcsync.RefCounted[v3authgrpc.AuthorizationClient]
}

func (cf *clientFilter) Close() {}

// getAuthzChannel returns an existing authz client from the map if present
// and its refcount is incremented.
func (cf *clientFilter) getAuthzChannel(key authzClientKey) *grpcsync.RefCounted[v3authgrpc.AuthorizationClient] {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	if rc, ok := cf.channels[key]; ok && rc.TryIncrement() {
		return rc
	}
	return nil
}

// storeAuthzChannel stores the created channel in the map if no valid channel
// exists for the key. If another goroutine already stored a channel while
// unlocked, it increments the existing channel's refcount and returns it.
func (cf *clientFilter) storeAuthzChannel(key authzClientKey, rc *grpcsync.RefCounted[v3authgrpc.AuthorizationClient]) *grpcsync.RefCounted[v3authgrpc.AuthorizationClient] {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	if existing, ok := cf.channels[key]; ok && existing.TryIncrement() {
		return existing
	}
	cf.channels[key] = rc
	return rc
}

// removeAuthzChannel removes rc from the map if it is still associated with
// key, avoiding deleting a newly created replacement channel.
func (cf *clientFilter) removeAuthzChannel(key authzClientKey, rc *grpcsync.RefCounted[v3authgrpc.AuthorizationClient]) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	if cf.channels[key] == rc {
		delete(cf.channels, key)
	}
}

// BuildClientInterceptor builds a client interceptor for the external
// authorization filter.
func (cf *clientFilter) BuildClientInterceptor(cfg, _ httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
	c, ok := cfg.(config)
	if !ok {
		return nil, fmt.Errorf("extauthz: incorrect config type provided (%T): %v", cfg, cfg)
	}

	key := authzClientKey{
		targetURI:          c.grpcService.TargetURI,
		channelCredentials: c.grpcService.ChannelCredentials,
		callCredentials:    c.grpcService.CallCredentials,
	}

	// If the channel for the key is present in the map and its refcount is
	// greater than 0, increment the refcount and return the interceptor.
	if rc := cf.getAuthzChannel(key); rc != nil {
		return &clientInterceptor{
			config:          c,
			authzClient:     rc,
			metricsRecorder: cf.metricsRecorder,
			target:          cf.target,
		}, nil
	}

	// Create the external authorization channel without holding the lock.
	cc, cancel, err := iextauthz.CreateExtAuthzChannel(c.grpcService)
	if err != nil {
		return nil, fmt.Errorf("extauthz: failed to create channel to the external authorization server %q: %v", c.grpcService.TargetURI, err)
	}

	client := v3authgrpc.NewAuthorizationClient(cc)
	// Create a new refcounted client. The onZero cleanup function will remove
	// the client from the map and close the underlying channel.
	var rc *grpcsync.RefCounted[v3authgrpc.AuthorizationClient]
	rc = grpcsync.NewRefCounted(client, func() {
		cf.removeAuthzChannel(key, rc)
		cancel()
	})

	// Double-check if another goroutine created and stored a channel for this
	// key while we were unlocked.
	if existingRC := cf.storeAuthzChannel(key, rc); existingRC != rc {
		rc.Decrement()
		return &clientInterceptor{
			config:          c,
			authzClient:     existingRC,
			metricsRecorder: cf.metricsRecorder,
			target:          cf.target,
		}, nil
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
	handle.Record(i.metricsRecorder, 1, i.target, "")
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

	// Increment authzClient's refcount so the Check RPC keeps the connection
	// open even if the interceptor is closed concurrently. Decrement is deferred
	// to release the reference when NewStream completes.
	i.authzClient.Increment()
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
		if i.config.failureModeAllowHeaderAdd {
			outgoingMD.Append("x-envoy-auth-failure-mode-allowed", "true")
			ctx = metadata.NewOutgoingContext(ctx, outgoingMD)
		}
		return newStream(ctx, opts...)
	}

	// When the external authorization server denies the RPC, we fail the RPC
	// immediately without creating a stream, and return a status error based
	// on the denied response.
	if resp.GetStatus().GetCode() != int32(codes.OK) {
		i.recordMetric(extAuthzClientDeniedRPCsMetric)
		deniedResp, ok := resp.GetHttpResponse().(*v3authpb.CheckResponse_DeniedResponse)
		if !ok {
			// If the status in the respose is not OK, and the response does not
			// contain a DeniedResponse message, we just fail the RPC with
			// PERMISSION_DENIED.
			return nil, status.Errorf(codes.PermissionDenied, "extauthz: RPC denied by external authorization server")
		}

		// Compute the status to return to the caller based on the status returned
		// by the external authorization server.
		code := codes.PermissionDenied
		if st := deniedResp.DeniedResponse.GetStatus(); st != nil {
			code = grpcStatusCode(int32(st.GetCode()))
		}
		return nil, status.Errorf(code, "extauthz: RPC denied by external authorization server")
	}

	// We get here only if the external authorization server allowed the RPC.
	i.recordMetric(extAuthzClientAllowedRPCsMetric)
	allowedResp, ok := resp.GetHttpResponse().(*v3authpb.CheckResponse_OkResponse)
	if !ok {
		// If the response does not contain an OkResponse message despite having
		// an OK status, we proceed to create the stream without making any header
		// mutations.
		return newStream(ctx, opts...)
	}

	// Update outgoing metadata with headers specified by the external
	// authorization server.
	if err := i.config.decoderHeaderMutationRules.ApplyAdditions(allowedResp.OkResponse.GetHeaders(), outgoingMD); err != nil {
		if !i.config.failureModeAllow {
			return nil, status.Errorf(i.config.statusOnError, "extauthz: error applying header mutation rules: %v", err)
		}
		if i.config.failureModeAllowHeaderAdd {
			outgoingMD.Append("x-envoy-auth-failure-mode-allowed", "true")
		}
	}
	// Update outgoing metadata with headers_to_remove specified by the external
	// authorization server.
	if err := i.config.decoderHeaderMutationRules.ApplyRemovals(allowedResp.OkResponse.GetHeadersToRemove(), outgoingMD); err != nil {
		if !i.config.failureModeAllow {
			return nil, status.Errorf(i.config.statusOnError, "extauthz: error applying header mutation rules: %v", err)
		}
		if i.config.failureModeAllowHeaderAdd && len(outgoingMD.Get("x-envoy-auth-failure-mode-allowed")) == 0 {
			outgoingMD.Append("x-envoy-auth-failure-mode-allowed", "true")
		}
	}
	// Create a new context with the mutated outgoing metadata. All subsequent
	// Filters in the chain and the final RPC call will see this new context.
	ctx = metadata.NewOutgoingContext(ctx, outgoingMD)

	// Create the underlying gRPC stream.
	stream, err := newStream(ctx, opts...)
	if err != nil {
		return nil, err
	}

	// If the external authorization server specified response headers to add,
	// wrap the stream to intercept and modify the response headers received
	// from the server.
	if len(allowedResp.OkResponse.GetResponseHeadersToAdd()) > 0 {
		return &clientStream{
			ClientStream:  stream,
			headersToAdd:  allowedResp.OkResponse.GetResponseHeadersToAdd(),
			mutationRules: i.config.decoderHeaderMutationRules,
			statusOnError: i.config.statusOnError,
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
	// statusOnError is the gRPC status code to return when mutation fails.
	statusOnError codes.Code
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
	if err := s.mutationRules.ApplyAdditions(s.headersToAdd, md); err != nil {
		return nil, status.Errorf(s.statusOnError, "extauthz: error applying header mutation rules to response headers: %v", err)
	}
	return md, nil
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
