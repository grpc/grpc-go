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

package extproc

import (
	"fmt"
	"regexp"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"

	v3procfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

const defaultDeferredCloseTimeout = 5 * time.Second

type baseConfig struct {
	httpfilter.FilterConfig
	config *v3procfilterpb.ExternalProcessor
}

type overrideConfig struct {
	httpfilter.FilterConfig
	config *v3procfilterpb.ExtProcOverrides
}

// interceptorConfig contains the configuration for the external processing
// client interceptor.
type interceptorConfig struct {
	// The following fields can be set either in the filter config or the
	// override config. If both are set, the override config will be used.
	//
	// server is the configuration for the external processing server.
	server serverConfig
	// failureModeAllow specifies the behavior when the RPC to the external
	// processing server fails. If true, the dataplane PRC will be allowed to
	// continue. If false, the data plane RPC will be failed with a grpc status
	// code of UNAVAILABLE.
	failureModeAllow bool
	// processingMode specifies the processing mode for each dataplane event.
	processingMode processingModes
	// Attributes to be sent to the external processing server along with the
	// request and response dataplane events.
	requestAttributes  []string
	responseAttributes []string

	// The following fields can only be set in the base config.
	//
	// allowModeOverride specifies whether to allow the external processing
	// server to dynamically override the processing mode.
	allowModeOverride bool
	// allowedOverrideModes specifies the processing modes that the external
	// processing server is allowed to dynamically override to if
	// allowModeOverride is true. If allowModeOverride is false, this field is
	// ignored. If allowModeOverride is true and this field is empty, the
	// external processing server can override to any processing mode.
	allowedOverrideModes []*v3procfilterpb.ProcessingMode
	// mutationRules specifies the rules for what modifications an external
	// processing server may make to headers/trailers sent to it.
	mutationRules headerMutationRules
	// allowedHeaders specifies the headers that are allowed to be sent to the
	// external processing server. If unset, all headers are allowed.
	allowedHeaders []matcher.StringMatcher
	// disallowedHeaders specifies the headers that will not be sent to the
	// external processing server. This overrides the above AllowedHeaders if
	// a header matches both.
	disallowedHeaders []matcher.StringMatcher
	// disableImmediateResponse specifies whether to disable immediate response
	// from the external processing server. When true, if the response from
	// external processing server has the `immediate_response` field set, the
	// dataplane RPC will be failed with `UNAVAILABLE` status code. When false,
	// the `immediate_response` field in the response from external processing
	// server will be ignored.
	disableImmediateResponse bool
	// observabilityMode determines if the filter waits for the external
	// processing server. If true, events are sent to the server in
	// "observation-only" mode; the filter does not wait for a response. If
	// false, the filter waits for a response, allowing the server to modify
	// events before they reach the dataplane.
	observabilityMode bool
	// deferredCloseTimeout is the duration the filter waits before closing the
	// external processing stream after the dataplane RPC completes. This is
	// only applicable when observabilityMode is true; otherwise, it is ignored.
	// The default value is 5 seconds.
	deferredCloseTimeout time.Duration
}

// processingMode defines how headers, trailers, and bodies are handled
// in relation to the external processing server.
type processingMode int

const (
	// modeSkip indicates that the header/trailer/body should not be sent.
	modeSkip processingMode = iota
	// modeSend indicates that the header/trailer/body should be sent.
	modeSend
)

type processingModes struct {
	requestHeaderMode   processingMode
	responseHeaderMode  processingMode
	responseTrailerMode processingMode
	requestBodyMode     processingMode
	responseBodyMode    processingMode
}

// headerMutationRules specifies the rules for what modifications an external
// processing server may make to headers sent on the data plane RPC.
//
// Methods on this struct are safe to call on a nil pointer receiver, in which
// case all header mutations are permitted.
type headerMutationRules struct {
	// allowExpr specifies a regular expression that matches the headers that
	// can be mutated.
	allowExpr *regexp.Regexp
	// disallowExpr specifies a regular expression that matches the headers that
	// cannot be mutated. This overrides the above allowExpr if a header matches
	// both.
	disallowExpr *regexp.Regexp
	// disallowAll specifies that no header mutations are allowed. This
	// overrides all other settings.
	disallowAll bool
	// disallowIsError specifies whether to return an error if a header mutation
	// is disallowed. If true, the data plane RPC will be failed with a grpc
	// status code of Unknown.
	disallowIsError bool
}

// serverConfig contains the configuration for an external server.
type serverConfig struct {
	// targetURI is the name of the external server.
	targetURI string
	// channelCredentials specifies the transport credentials to use to connect
	// to the external server. Must not be nil.
	channelCredentials credentials.TransportCredentials
	// callCredentials specifies the per-RPC credentials to use when making
	// calls to the external server.
	callCredentials []credentials.PerRPCCredentials
	// timeout is the RPC timeout for the call to the external server. If unset,
	// the timeout depends on the usage of this external server. For example,
	// cases like ext_authz and ext_proc, where there is a 1:1 mapping between the
	// data plane RPC and the external server call, the timeout will be capped by
	// the timeout on the data plane RPC. For cases like RLQS where there is a
	// side channel to the external server, an unset timeout will result in no
	// timeout being applied to the external server call.
	timeout time.Duration
	// initialMetadata is the additional metadata to include in all RPCs sent to
	// the external server.
	initialMetadata metadata.MD
}

// newInterceptorConfig creates the interceptor config from the base and
// override filter configs. The base config is required and the override config
// is optional. If a field is set in both the base and override configs, the
// value from the override config will be used.
func newInterceptorConfig(base *v3procfilterpb.ExternalProcessor, override *v3procfilterpb.ExtProcOverrides) (*interceptorConfig, error) {
	iconfig := &interceptorConfig{
		failureModeAllow:         base.GetFailureModeAllow(),
		allowModeOverride:        base.GetAllowModeOverride(),
		requestAttributes:        base.GetRequestAttributes(),
		responseAttributes:       base.GetResponseAttributes(),
		allowedOverrideModes:     base.GetAllowedOverrideModes(),
		observabilityMode:        base.GetObservabilityMode(),
		disableImmediateResponse: base.GetDisableImmediateResponse(),
	}
	if base.GetDeferredCloseTimeout() != nil {
		iconfig.deferredCloseTimeout = base.GetDeferredCloseTimeout().AsDuration()
	} else {
		iconfig.deferredCloseTimeout = defaultDeferredCloseTimeout
	}

	var err error
	if fr := base.GetForwardRules(); fr != nil {
		if allowed := fr.GetAllowedHeaders(); allowed != nil {
			iconfig.allowedHeaders, err = convertStringMatchers(allowed.GetPatterns())
			if err != nil {
				return nil, fmt.Errorf("invalid allowed header matcher: %v", err)
			}
		}
		if disallowed := fr.GetDisallowedHeaders(); disallowed != nil {
			iconfig.disallowedHeaders, err = convertStringMatchers(disallowed.GetPatterns())
			if err != nil {
				return nil, fmt.Errorf("invalid disallowed header matcher: %v", err)
			}
		}
	}

	if mr := base.GetMutationRules(); mr != nil {
		if allowexp := mr.GetAllowExpression(); allowexp != nil {
			iconfig.mutationRules.allowExpr, err = regexp.Compile(allowexp.GetRegex())
			if err != nil {
				return nil, fmt.Errorf("invalid allow expression: %v", err)
			}
		}
		if disallowexp := mr.GetDisallowExpression(); disallowexp != nil {
			iconfig.mutationRules.disallowExpr, err = regexp.Compile(disallowexp.GetRegex())
			if err != nil {
				return nil, fmt.Errorf("invalid disallow expression: %v", err)
			}
		}
		iconfig.mutationRules.disallowAll = mr.GetDisallowAll().GetValue()
		iconfig.mutationRules.disallowIsError = mr.GetDisallowIsError().GetValue()
	}
	if iconfig.server, err = serverConfigFromGrpcService(base.GetGrpcService()); err != nil {
		return nil, fmt.Errorf("failed to parse gRPC service config: %v", err)
	}
	if pm := base.GetProcessingMode(); pm != nil {
		// The default processing mode is to send headers and skip body and
		// trailers.
		iconfig.processingMode.requestHeaderMode = resolveHeaderMode(pm.GetRequestHeaderMode(), modeSend)
		iconfig.processingMode.responseHeaderMode = resolveHeaderMode(pm.GetResponseHeaderMode(), modeSend)
		iconfig.processingMode.responseTrailerMode = resolveHeaderMode(pm.GetResponseTrailerMode(), modeSkip)
		iconfig.processingMode.requestBodyMode = resolveBodyMode(pm.GetRequestBodyMode())
		iconfig.processingMode.responseBodyMode = resolveBodyMode(pm.GetResponseBodyMode())
	}

	if override != nil {
		if gs := override.GetGrpcService(); gs != nil {
			serverCfg, err := serverConfigFromGrpcService(gs)
			if err != nil {
				return nil, err
			}
			iconfig.server = serverCfg
		}
		if fma := override.GetFailureModeAllow(); fma != nil {
			iconfig.failureModeAllow = fma.GetValue()
		}
		if override.GetRequestAttributes() != nil {
			iconfig.requestAttributes = override.GetRequestAttributes()
		}
		if override.GetResponseAttributes() != nil {
			iconfig.responseAttributes = override.GetResponseAttributes()
		}
		if pm := override.GetProcessingMode(); pm != nil {
			iconfig.processingMode.requestHeaderMode = resolveHeaderMode(pm.GetRequestHeaderMode(), modeSend)
			iconfig.processingMode.responseHeaderMode = resolveHeaderMode(pm.GetResponseHeaderMode(), modeSend)
			iconfig.processingMode.responseTrailerMode = resolveHeaderMode(pm.GetResponseTrailerMode(), modeSkip)
			iconfig.processingMode.requestBodyMode = resolveBodyMode(pm.GetRequestBodyMode())
			iconfig.processingMode.responseBodyMode = resolveBodyMode(pm.GetResponseBodyMode())
		}
	}
	return iconfig, nil
}

// convertStringMatchers converts a slice of protobuf StringMatcher messages to
// a slice of matcher.StringMatcher.
func convertStringMatchers(patterns []*v3matcherpb.StringMatcher) ([]matcher.StringMatcher, error) {
	var matchers []matcher.StringMatcher
	for _, m := range patterns {
		sm, err := matcher.StringMatcherFromProto(m)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, sm)
	}
	return matchers, nil
}

// resolveHeaderMode resolves the processing mode for headers based on the
// protobuf enum value. If the mode is not set or set to Default, it returns the
// provided defaultMode.
func resolveHeaderMode(mode v3procfilterpb.ProcessingMode_HeaderSendMode, defaultMode processingMode) processingMode {
	switch mode {
	case v3procfilterpb.ProcessingMode_SEND:
		return modeSend
	case v3procfilterpb.ProcessingMode_SKIP:
		return modeSkip
	default:
		return defaultMode
	}
}

// resolveBodyMode resolves the processing mode for body based on the protobuf
// enum value. If the mode is not set (i.e., default), it returns modeSkip, as
// the default for body is to skip.
func resolveBodyMode(mode v3procfilterpb.ProcessingMode_BodySendMode) processingMode {
	switch mode {
	case v3procfilterpb.ProcessingMode_GRPC:
		return modeSend
	case v3procfilterpb.ProcessingMode_NONE:
		return modeSkip
	default:
		return modeSkip
	}
}
