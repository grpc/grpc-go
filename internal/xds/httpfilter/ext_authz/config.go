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

package extauthz

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
)

// config contains the configuration for the external authorization filter.
type config struct {
	httpfilter.FilterConfig
	// grpcService is the configuration for the external authorization server.
	grpcService xdsresource.GRPCServiceConfig
	// filterEnabled specifies the percentage of requests to be authorized by
	// the external authorization server.
	filterEnabled fraction
	// denyAtDisabled specifies whether to deny requests when external
	// authorization is disabled via the filterEnabled configuration. If true,
	// requests will be denied with a status based on statusOnError.
	denyAtDisable bool
	// failureModeAllow specifies the behavior when the call to the external
	// authorization server fails. If true, the request will be allowed in such
	// cases. If false, the request will be denied with a status based on
	// statusOnError.
	failureModeAllow bool
	// failureModeAllowHeaderAdd specifies whether to add the
	// `x-envoy-auth-failure-mode-allowed: true` header to the data plane RPC
	// when the call to the external authorization server fails.
	failureModeAllowHeaderAdd bool
	// statusOnError is the gRPC status code to use when the external
	// authorization is disabled or when the call to the external authorization
	// server fails.
	//
	// The default value is PERMISSION_DENIED.
	statusOnError codes.Code
	// allowedHeaders specifies the headers that are allowed to be sent to the
	// external authorization server. If unset, all headers are allowed.
	allowedHeaders []matcher.StringMatcher
	// disallowedHeaders specifies the headers that will not be sent to the
	// external authorization server. This takes precedence over the
	// allowedHeaders field.
	disallowedHeaders []matcher.StringMatcher
	// decoderHeaderMutationRules specifies the rules for what modifications an
	// external authorization server may make to headers sent on the data plane
	// RPC.
	decoderHeaderMutationRules httpfilter.HeaderMutationRules
	// includePeerCertificate specifies whether to include the peer certificate
	// in the request sent to the external authorization server.
	includePeerCertificate bool
}

// fraction uses a numerator and denominator to specify a fractional value. If
// the denominator specified is less than the numerator, the final fractional
// value is capped at 1.
type fraction struct {
	// numerator is the numerator of the fraction.
	numerator uint32
	// denominator is the denominator of the fraction.
	denominator uint32
}
