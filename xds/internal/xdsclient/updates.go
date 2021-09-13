/*
 *
 * Copyright 2021 gRPC authors.
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

package xdsclient

import (
	"regexp"
	"time"

	"google.golang.org/protobuf/types/known/anypb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpcutil"
	"google.golang.org/grpc/xds/internal/httpfilter"
)

// ListenerUpdate contains information received in an LDS response, which is of
// interest to the registered LDS watcher.
type ListenerUpdate struct {
	// RouteConfigName is the route configuration name corresponding to the
	// target which is being watched through LDS.
	//
	// Only one of RouteConfigName and InlineRouteConfig is set.
	RouteConfigName string
	// InlineRouteConfig is the inline route configuration (RDS response)
	// returned inside LDS.
	//
	// Only one of RouteConfigName and InlineRouteConfig is set.
	InlineRouteConfig *RouteConfigUpdate

	// MaxStreamDuration contains the HTTP connection manager's
	// common_http_protocol_options.max_stream_duration field, or zero if
	// unset.
	MaxStreamDuration time.Duration
	// HTTPFilters is a list of HTTP filters (name, config) from the LDS
	// response.
	HTTPFilters []HTTPFilter
	// InboundListenerCfg contains inbound listener configuration.
	InboundListenerCfg *InboundListenerConfig

	// Raw is the resource from the xds response.
	Raw *anypb.Any
}

// Equal returns true if l and other are the same.
func (l ListenerUpdate) Equal(other ListenerUpdate) bool {
	switch {
	case l.RouteConfigName != other.RouteConfigName:
		return false
	case !l.InlineRouteConfig.Equal(other.InlineRouteConfig):
		return false
	case l.MaxStreamDuration != other.MaxStreamDuration:
		return false
	case !l.InboundListenerCfg.Equal(other.InboundListenerCfg):
		return false
	}
	if len(l.HTTPFilters) != len(other.HTTPFilters) {
		return false
	}
	for i := 0; i < len(l.HTTPFilters); i++ {
		if !l.HTTPFilters[i].Equal(other.HTTPFilters[i]) {
			return false
		}
	}
	return true
}

// HTTPFilter represents one HTTP filter from an LDS response's HTTP connection
// manager field.
type HTTPFilter struct {
	// Name is an arbitrary name of the filter.  Used for applying override
	// settings in virtual host / route / weighted cluster configuration (not
	// yet supported).
	Name string
	// Filter is the HTTP filter found in the registry for the config type.
	Filter httpfilter.Filter
	// Config contains the filter's configuration
	Config httpfilter.FilterConfig
}

// Equal returns true if h and other are the same.
func (h HTTPFilter) Equal(other HTTPFilter) bool {
	switch {
	case h.Name != other.Name:
		return false
	case !grpcutil.EqualAny(h.Filter, other.Filter, func(x, y interface{}) bool {
		return grpcutil.EqualStringSlice(x.(httpfilter.Filter).TypeURLs(), y.(httpfilter.Filter).TypeURLs())
	}):
		return false
	case !grpcutil.EqualAny(h.Config, other.Config, func(x, y interface{}) bool {
		return x.(httpfilter.FilterConfig).Equal(y.(httpfilter.FilterConfig))
	}):
		return false
	}
	return true
}

// InboundListenerConfig contains information about the inbound listener, i.e
// the server-side listener.
type InboundListenerConfig struct {
	// Address is the local address on which the inbound listener is expected to
	// accept incoming connections.
	Address string
	// Port is the local port on which the inbound listener is expected to
	// accept incoming connections.
	Port string
	// FilterChains is the list of filter chains associated with this listener.
	FilterChains *FilterChainManager
}

// Equal returns true if i and other are the same.
func (i *InboundListenerConfig) Equal(other *InboundListenerConfig) bool {
	if i == other {
		return true
	}
	if i == nil || other == nil {
		return false
	}
	switch {
	case i.Address != other.Address:
		return false
	case i.Port != other.Port:
		return false
	case !i.FilterChains.Equal(other.FilterChains):
		return false
	}
	return true
}

// RouteConfigUpdate contains information received in an RDS response, which is
// of interest to the registered RDS watcher.
type RouteConfigUpdate struct {
	VirtualHosts []*VirtualHost
	// Raw is the resource from the xds response.
	Raw *anypb.Any
}

// Equal returns true if r and other are the same.
func (r *RouteConfigUpdate) Equal(other *RouteConfigUpdate) bool {
	if r == other {
		return true
	}
	if r == nil || other == nil {
		return false
	}
	if len(r.VirtualHosts) != len(other.VirtualHosts) {
		return false
	}
	for i := 0; i < len(r.VirtualHosts); i++ {
		if !r.VirtualHosts[i].Equal(other.VirtualHosts[i]) {
			return false
		}
	}
	return true
}

// VirtualHost contains the routes for a list of Domains.
//
// Note that the domains in this slice can be a wildcard, not an exact string.
// The consumer of this struct needs to find the best match for its hostname.
type VirtualHost struct {
	Domains []string
	// Routes contains a list of routes, each containing matchers and
	// corresponding action.
	Routes []*Route
	// HTTPFilterConfigOverride contains any HTTP filter config overrides for
	// the virtual host which may be present.  An individual filter's override
	// may be unused if the matching Route contains an override for that
	// filter.
	HTTPFilterConfigOverride map[string]httpfilter.FilterConfig
	RetryConfig              *RetryConfig
}

// Equal returns true if v and other are the same.
func (v *VirtualHost) Equal(other *VirtualHost) bool {
	if v == other {
		return true
	}
	if v == nil || other == nil {
		return false
	}
	if !grpcutil.EqualStringSlice(v.Domains, other.Domains) {
		return false
	}
	if len(v.Routes) != len(other.Routes) {
		return false
	}
	for i := 0; i < len(v.Routes); i++ {
		if !v.Routes[i].Equal(other.Routes[i]) {
			return false
		}
	}
	if len(v.HTTPFilterConfigOverride) != len(other.HTTPFilterConfigOverride) {
		return false
	}
	for name, fc1 := range v.HTTPFilterConfigOverride {
		if fc2, ok := other.HTTPFilterConfigOverride[name]; !ok || !fc1.Equal(fc2) {
			return false
		}
	}
	return v.RetryConfig.Equal(other.RetryConfig)
}

// RetryConfig contains all retry-related configuration in either a VirtualHost
// or Route.
type RetryConfig struct {
	// RetryOn is a set of status codes on which to retry.  Only Canceled,
	// DeadlineExceeded, Internal, ResourceExhausted, and Unavailable are
	// supported; any other values will be omitted.
	RetryOn      map[codes.Code]bool
	NumRetries   uint32       // maximum number of retry attempts
	RetryBackoff RetryBackoff // retry backoff policy
}

// Equal returns true if r and other are the same.
func (r *RetryConfig) Equal(other *RetryConfig) bool {
	if r == other {
		return true
	}
	if r == nil || other == nil {
		return false
	}
	if len(r.RetryOn) != len(other.RetryOn) {
		return false
	}
	for code := range r.RetryOn {
		if !other.RetryOn[code] {
			return false
		}
	}
	return r.NumRetries == other.NumRetries && r.RetryBackoff == other.RetryBackoff
}

// RetryBackoff describes the backoff policy for retries.
type RetryBackoff struct {
	BaseInterval time.Duration // initial backoff duration between attempts
	MaxInterval  time.Duration // maximum backoff duration
}

// Equal returns true if r and other are the same.
func (r RetryBackoff) Equal(other RetryBackoff) bool {
	return r.BaseInterval == other.BaseInterval && r.MaxInterval == other.MaxInterval
}

// HashPolicyType specifies the type of HashPolicy from a received RDS Response.
type HashPolicyType int

const (
	// HashPolicyTypeHeader specifies to hash a Header in the incoming request.
	HashPolicyTypeHeader HashPolicyType = iota
	// HashPolicyTypeChannelID specifies to hash a unique Identifier of the
	// Channel. In grpc-go, this will be done using the ClientConn pointer.
	HashPolicyTypeChannelID
)

// HashPolicy specifies the HashPolicy if the upstream cluster uses a hashing
// load balancer.
type HashPolicy struct {
	HashPolicyType HashPolicyType
	Terminal       bool
	// Fields used for type HEADER.
	HeaderName        string
	Regex             *regexp.Regexp
	RegexSubstitution string
}

// Equal returns true if h and other are the same.
func (h *HashPolicy) Equal(other *HashPolicy) bool {
	if h == other {
		return true
	}
	if h == nil || other == nil {
		return false
	}
	switch {
	case h.HashPolicyType != other.HashPolicyType:
		return false
	case h.Terminal != other.Terminal:
		return false
	case h.HeaderName != other.HeaderName:
		return false
	case !grpcutil.EqualAny(h.Regex, other.Regex, func(x, y interface{}) bool {
		return x.(*regexp.Regexp).String() == y.(*regexp.Regexp).String()
	}):
		return false
	case h.RegexSubstitution != other.RegexSubstitution:
		return false
	}
	return true
}

// RouteAction is the action of the route from a received RDS response.
type RouteAction int

const (
	// RouteActionUnsupported are routing types currently unsupported by grpc.
	// According to A36, "A Route with an inappropriate action causes RPCs
	// matching that route to fail."
	RouteActionUnsupported RouteAction = iota
	// RouteActionRoute is the expected route type on the client side. Route
	// represents routing a request to some upstream cluster. On the client
	// side, if an RPC matches to a route that is not RouteActionRoute, the RPC
	// will fail according to A36.
	RouteActionRoute
	// RouteActionNonForwardingAction is the expected route type on the server
	// side. NonForwardingAction represents when a route will generate a
	// response directly, without forwarding to an upstream host.
	RouteActionNonForwardingAction
)

// WeightedCluster contains settings for an xds RouteAction.WeightedCluster.
type WeightedCluster struct {
	// Weight is the relative weight of the cluster.  It will never be zero.
	Weight uint32
	// HTTPFilterConfigOverride contains any HTTP filter config overrides for
	// the weighted cluster which may be present.
	HTTPFilterConfigOverride map[string]httpfilter.FilterConfig
}

// Equal returns true if w and other are the same.
func (w WeightedCluster) Equal(other WeightedCluster) bool {
	if w.Weight != other.Weight {
		return false
	}
	if len(w.HTTPFilterConfigOverride) != len(other.HTTPFilterConfigOverride) {
		return false
	}
	for name, fc1 := range w.HTTPFilterConfigOverride {
		if fc2, ok := other.HTTPFilterConfigOverride[name]; !ok || !fc1.Equal(fc2) {
			return false
		}
	}
	return true
}

// HeaderMatcher represents header matchers.
type HeaderMatcher struct {
	Name         string
	InvertMatch  *bool
	ExactMatch   *string
	RegexMatch   *regexp.Regexp
	PrefixMatch  *string
	SuffixMatch  *string
	RangeMatch   *Int64Range
	PresentMatch *bool
}

// Equal returns true if h and other are the same.
func (h *HeaderMatcher) Equal(other *HeaderMatcher) bool {
	if h == other {
		return true
	}
	if h == nil || other == nil {
		return false
	}
	switch {
	case h.Name != other.Name:
		return false
	case !grpcutil.EqualBoolP(h.InvertMatch, other.InvertMatch):
		return false
	case !grpcutil.EqualStringP(h.ExactMatch, other.ExactMatch):
		return false
	case !grpcutil.EqualAny(h.RegexMatch, other.RegexMatch, func(x, y interface{}) bool {
		return x.(*regexp.Regexp).String() == y.(*regexp.Regexp).String()
	}):
		return false
	case !grpcutil.EqualStringP(h.PrefixMatch, other.PrefixMatch):
		return false
	case !grpcutil.EqualStringP(h.SuffixMatch, other.SuffixMatch):
		return false
	case !h.RangeMatch.Equal(other.RangeMatch):
		return false
	case !grpcutil.EqualBoolP(h.PresentMatch, other.PresentMatch):
		return false
	}
	return true
}

// Int64Range is a range for header range match.
type Int64Range struct {
	Start int64
	End   int64
}

// Equal returns true if i and other are the same.
func (i *Int64Range) Equal(other *Int64Range) bool {
	if i == other {
		return true
	}
	if i == nil || other == nil {
		return false
	}
	return i.Start == other.Start && i.End == other.End
}

// Route is both a specification of how to match a request as well as an
// indication of the action to take upon match.
type Route struct {
	Path   *string
	Prefix *string
	Regex  *regexp.Regexp
	// Indicates if prefix/path matching should be case insensitive. The default
	// is false (case sensitive).
	CaseInsensitive bool
	Headers         []*HeaderMatcher
	Fraction        *uint32

	HashPolicies []*HashPolicy

	// If the matchers above indicate a match, the below configuration is used.
	WeightedClusters map[string]WeightedCluster
	// If MaxStreamDuration is nil, it indicates neither of the route action's
	// max_stream_duration fields (grpc_timeout_header_max nor
	// max_stream_duration) were set.  In this case, the ListenerUpdate's
	// MaxStreamDuration field should be used.  If MaxStreamDuration is set to
	// an explicit zero duration, the application's deadline should be used.
	MaxStreamDuration *time.Duration
	// HTTPFilterConfigOverride contains any HTTP filter config overrides for
	// the route which may be present.  An individual filter's override may be
	// unused if the matching WeightedCluster contains an override for that
	// filter.
	HTTPFilterConfigOverride map[string]httpfilter.FilterConfig
	RetryConfig              *RetryConfig

	RouteAction RouteAction
}

// Equal returns true if r and other are the same.
func (r *Route) Equal(other *Route) bool {
	if r == other {
		return true
	}
	if r == nil || other == nil {
		return false
	}
	switch {
	case !grpcutil.EqualStringP(r.Path, other.Path):
		return false
	case !grpcutil.EqualStringP(r.Prefix, other.Prefix):
		return false
	case !grpcutil.EqualAny(r.Regex, other.Regex, func(x, y interface{}) bool {
		return x.(*regexp.Regexp).String() == y.(*regexp.Regexp).String()
	}):
		return false
	case r.CaseInsensitive != other.CaseInsensitive:
		return false
	case !grpcutil.EqualUint32P(r.Fraction, other.Fraction):
		return false
	case !grpcutil.EqualDurationP(r.MaxStreamDuration, other.MaxStreamDuration):
		return false
	case !r.RetryConfig.Equal(other.RetryConfig):
		return false
	case r.RouteAction != other.RouteAction:
		return false
	}
	if len(r.Headers) != len(other.Headers) {
		return false
	}
	for i := 0; i < len(r.Headers); i++ {
		if !r.Headers[i].Equal(other.Headers[i]) {
			return false
		}
	}
	if len(r.HashPolicies) != len(other.HashPolicies) {
		return false
	}
	for i := 0; i < len(r.HashPolicies); i++ {
		if !r.HashPolicies[i].Equal(other.HashPolicies[i]) {
			return false
		}
	}
	if len(r.WeightedClusters) != len(other.WeightedClusters) {
		return false
	}
	for name, wc1 := range r.WeightedClusters {
		if wc2, ok := other.WeightedClusters[name]; !ok || !wc1.Equal(wc2) {
			return false
		}
	}
	if len(r.HTTPFilterConfigOverride) != len(other.HTTPFilterConfigOverride) {
		return false
	}
	for name, fc1 := range r.HTTPFilterConfigOverride {
		if fc2, ok := other.HTTPFilterConfigOverride[name]; !ok || !fc1.Equal(fc2) {
			return false
		}
	}
	return true
}
