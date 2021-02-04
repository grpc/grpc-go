/*
 *
 * Copyright 2020 gRPC authors.
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

package client

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"

	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/env"
	"google.golang.org/grpc/xds/internal/version"
)

// TransportSocket proto message has a `name` field which is expected to be set
// to this value by the management server.
const transportSocketName = "envoy.transport_sockets.tls"

// UnmarshalListener processes resources received in an LDS response, validates
// them, and transforms them into a native struct which contains only fields we
// are interested in.
func UnmarshalListener(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]ListenerUpdate, error) {
	update := make(map[string]ListenerUpdate)
	for _, r := range resources {
		if !IsListenerResource(r.GetTypeUrl()) {
			return nil, fmt.Errorf("xds: unexpected resource type: %q in LDS response", r.GetTypeUrl())
		}
		lis := &v3listenerpb.Listener{}
		if err := proto.Unmarshal(r.GetValue(), lis); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in LDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v", lis.GetName(), lis, lis)

		lu, err := processListener(lis)
		if err != nil {
			return nil, err
		}
		update[lis.GetName()] = *lu
	}
	return update, nil
}

func processListener(lis *v3listenerpb.Listener) (*ListenerUpdate, error) {
	if lis.GetApiListener() != nil {
		return processClientSideListener(lis)
	}
	return processServerSideListener(lis)
}

// processClientSideListener checks if the provided Listener proto meets
// the expected criteria. If so, it returns a non-empty routeConfigName.
func processClientSideListener(lis *v3listenerpb.Listener) (*ListenerUpdate, error) {
	update := &ListenerUpdate{}

	apiLisAny := lis.GetApiListener().GetApiListener()
	if !IsHTTPConnManagerResource(apiLisAny.GetTypeUrl()) {
		return nil, fmt.Errorf("xds: unexpected resource type: %q in LDS response", apiLisAny.GetTypeUrl())
	}
	apiLis := &v3httppb.HttpConnectionManager{}
	if err := proto.Unmarshal(apiLisAny.GetValue(), apiLis); err != nil {
		return nil, fmt.Errorf("xds: failed to unmarshal api_listner in LDS response: %v", err)
	}

	switch apiLis.RouteSpecifier.(type) {
	case *v3httppb.HttpConnectionManager_Rds:
		if apiLis.GetRds().GetConfigSource().GetAds() == nil {
			return nil, fmt.Errorf("xds: ConfigSource is not ADS in LDS response: %+v", lis)
		}
		name := apiLis.GetRds().GetRouteConfigName()
		if name == "" {
			return nil, fmt.Errorf("xds: empty route_config_name in LDS response: %+v", lis)
		}
		update.RouteConfigName = name
	case *v3httppb.HttpConnectionManager_RouteConfig:
		// TODO: Add support for specifying the RouteConfiguration inline
		// in the LDS response.
		return nil, fmt.Errorf("xds: LDS response contains RDS config inline. Not supported for now: %+v", apiLis)
	case nil:
		return nil, fmt.Errorf("xds: no RouteSpecifier in received LDS response: %+v", apiLis)
	default:
		return nil, fmt.Errorf("xds: unsupported type %T for RouteSpecifier in received LDS response", apiLis.RouteSpecifier)
	}

	update.MaxStreamDuration = apiLis.GetCommonHttpProtocolOptions().GetMaxStreamDuration().AsDuration()

	return update, nil
}

func processServerSideListener(lis *v3listenerpb.Listener) (*ListenerUpdate, error) {
	// Make sure that an address encoded in the received listener resource, and
	// that it matches the one specified in the name. Listener names on the
	// server-side as in the following format:
	// grpc/server?udpa.resource.listening_address=IP:Port.
	addr := lis.GetAddress()
	if addr == nil {
		return nil, fmt.Errorf("xds: no address field in LDS response: %+v", lis)
	}
	sockAddr := addr.GetSocketAddress()
	if sockAddr == nil {
		return nil, fmt.Errorf("xds: no socket_address field in LDS response: %+v", lis)
	}
	host, port, err := getAddressFromName(lis.GetName())
	if err != nil {
		return nil, fmt.Errorf("xds: no host:port in name field of LDS response: %+v, error: %v", lis, err)
	}
	if h := sockAddr.GetAddress(); host != h {
		return nil, fmt.Errorf("xds: socket_address host does not match the one in name. Got %q, want %q", h, host)
	}
	if p := strconv.Itoa(int(sockAddr.GetPortValue())); port != p {
		return nil, fmt.Errorf("xds: socket_address port does not match the one in name. Got %q, want %q", p, port)
	}

	// Make sure the listener resource contains a single filter chain. We do not
	// support multiple filter chains and picking the best match from the list.
	fcs := lis.GetFilterChains()
	if n := len(fcs); n != 1 {
		return nil, fmt.Errorf("xds: filter chains count in LDS response does not match expected. Got %d, want 1", n)
	}
	fc := fcs[0]

	// If the transport_socket field is not specified, it means that the control
	// plane has not sent us any security config. This is fine and the server
	// will use the fallback credentials configured as part of the
	// xdsCredentials.
	ts := fc.GetTransportSocket()
	if ts == nil {
		return &ListenerUpdate{}, nil
	}
	if name := ts.GetName(); name != transportSocketName {
		return nil, fmt.Errorf("xds: transport_socket field has unexpected name: %s", name)
	}
	any := ts.GetTypedConfig()
	if any == nil || any.TypeUrl != version.V3DownstreamTLSContextURL {
		return nil, fmt.Errorf("xds: transport_socket field has unexpected typeURL: %s", any.TypeUrl)
	}
	downstreamCtx := &v3tlspb.DownstreamTlsContext{}
	if err := proto.Unmarshal(any.GetValue(), downstreamCtx); err != nil {
		return nil, fmt.Errorf("xds: failed to unmarshal DownstreamTlsContext in LDS response: %v", err)
	}
	if downstreamCtx.GetCommonTlsContext() == nil {
		return nil, errors.New("xds: DownstreamTlsContext in LDS response does not contain a CommonTlsContext")
	}
	sc, err := securityConfigFromCommonTLSContext(downstreamCtx.GetCommonTlsContext())
	if err != nil {
		return nil, err
	}
	if sc.IdentityInstanceName == "" {
		return nil, errors.New("security configuration on the server-side does not contain identity certificate provider instance name")
	}
	sc.RequireClientCert = downstreamCtx.GetRequireClientCertificate().GetValue()
	if sc.RequireClientCert && sc.RootInstanceName == "" {
		return nil, errors.New("security configuration on the server-side does not contain root certificate provider instance name, but require_client_cert field is set")
	}
	return &ListenerUpdate{SecurityCfg: sc}, nil
}

func getAddressFromName(name string) (host string, port string, err error) {
	parts := strings.SplitN(name, "udpa.resource.listening_address=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("udpa.resource_listening_address not found in name: %v", name)
	}
	return net.SplitHostPort(parts[1])
}

// UnmarshalRouteConfig processes resources received in an RDS response,
// validates them, and transforms them into a native struct which contains only
// fields we are interested in. The provided hostname determines the route
// configuration resources of interest.
func UnmarshalRouteConfig(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]RouteConfigUpdate, error) {
	update := make(map[string]RouteConfigUpdate)
	for _, r := range resources {
		if !IsRouteConfigResource(r.GetTypeUrl()) {
			return nil, fmt.Errorf("xds: unexpected resource type: %q in RDS response", r.GetTypeUrl())
		}
		rc := &v3routepb.RouteConfiguration{}
		if err := proto.Unmarshal(r.GetValue(), rc); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in RDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v.", rc.GetName(), rc, rc)

		// Use the hostname (resourceName for LDS) to find the routes.
		u, err := generateRDSUpdateFromRouteConfiguration(rc, logger)
		if err != nil {
			return nil, fmt.Errorf("xds: received invalid RouteConfiguration in RDS response: %+v with err: %v", rc, err)
		}
		update[rc.GetName()] = u
	}
	return update, nil
}

// generateRDSUpdateFromRouteConfiguration checks if the provided
// RouteConfiguration meets the expected criteria. If so, it returns a
// RouteConfigUpdate with nil error.
//
// A RouteConfiguration resource is considered valid when only if it contains a
// VirtualHost whose domain field matches the server name from the URI passed
// to the gRPC channel, and it contains a clusterName or a weighted cluster.
//
// The RouteConfiguration includes a list of VirtualHosts, which may have zero
// or more elements. We are interested in the element whose domains field
// matches the server name specified in the "xds:" URI. The only field in the
// VirtualHost proto that the we are interested in is the list of routes. We
// only look at the last route in the list (the default route), whose match
// field must be empty and whose route field must be set.  Inside that route
// message, the cluster field will contain the clusterName or weighted clusters
// we are looking for.
func generateRDSUpdateFromRouteConfiguration(rc *v3routepb.RouteConfiguration, logger *grpclog.PrefixLogger) (RouteConfigUpdate, error) {
	var vhs []*VirtualHost
	for _, vh := range rc.GetVirtualHosts() {
		routes, err := routesProtoToSlice(vh.Routes, logger)
		if err != nil {
			return RouteConfigUpdate{}, fmt.Errorf("received route is invalid: %v", err)
		}
		vhs = append(vhs, &VirtualHost{
			Domains: vh.GetDomains(),
			Routes:  routes,
		})
	}
	return RouteConfigUpdate{VirtualHosts: vhs}, nil
}

func routesProtoToSlice(routes []*v3routepb.Route, logger *grpclog.PrefixLogger) ([]*Route, error) {
	var routesRet []*Route

	for _, r := range routes {
		match := r.GetMatch()
		if match == nil {
			return nil, fmt.Errorf("route %+v doesn't have a match", r)
		}

		if len(match.GetQueryParameters()) != 0 {
			// Ignore route with query parameters.
			logger.Warningf("route %+v has query parameter matchers, the route will be ignored", r)
			continue
		}

		pathSp := match.GetPathSpecifier()
		if pathSp == nil {
			return nil, fmt.Errorf("route %+v doesn't have a path specifier", r)
		}

		var route Route
		switch pt := pathSp.(type) {
		case *v3routepb.RouteMatch_Prefix:
			route.Prefix = &pt.Prefix
		case *v3routepb.RouteMatch_Path:
			route.Path = &pt.Path
		case *v3routepb.RouteMatch_SafeRegex:
			route.Regex = &pt.SafeRegex.Regex
		default:
			return nil, fmt.Errorf("route %+v has an unrecognized path specifier: %+v", r, pt)
		}

		if caseSensitive := match.GetCaseSensitive(); caseSensitive != nil {
			route.CaseInsensitive = !caseSensitive.Value
		}

		for _, h := range match.GetHeaders() {
			var header HeaderMatcher
			switch ht := h.GetHeaderMatchSpecifier().(type) {
			case *v3routepb.HeaderMatcher_ExactMatch:
				header.ExactMatch = &ht.ExactMatch
			case *v3routepb.HeaderMatcher_SafeRegexMatch:
				header.RegexMatch = &ht.SafeRegexMatch.Regex
			case *v3routepb.HeaderMatcher_RangeMatch:
				header.RangeMatch = &Int64Range{
					Start: ht.RangeMatch.Start,
					End:   ht.RangeMatch.End,
				}
			case *v3routepb.HeaderMatcher_PresentMatch:
				header.PresentMatch = &ht.PresentMatch
			case *v3routepb.HeaderMatcher_PrefixMatch:
				header.PrefixMatch = &ht.PrefixMatch
			case *v3routepb.HeaderMatcher_SuffixMatch:
				header.SuffixMatch = &ht.SuffixMatch
			default:
				return nil, fmt.Errorf("route %+v has an unrecognized header matcher: %+v", r, ht)
			}
			header.Name = h.GetName()
			invert := h.GetInvertMatch()
			header.InvertMatch = &invert
			route.Headers = append(route.Headers, &header)
		}

		if fr := match.GetRuntimeFraction(); fr != nil {
			d := fr.GetDefaultValue()
			n := d.GetNumerator()
			switch d.GetDenominator() {
			case v3typepb.FractionalPercent_HUNDRED:
				n *= 10000
			case v3typepb.FractionalPercent_TEN_THOUSAND:
				n *= 100
			case v3typepb.FractionalPercent_MILLION:
			}
			route.Fraction = &n
		}

		clusters := make(map[string]uint32)
		action := r.GetRoute()
		switch a := action.GetClusterSpecifier().(type) {
		case *v3routepb.RouteAction_Cluster:
			clusters[a.Cluster] = 1
		case *v3routepb.RouteAction_WeightedClusters:
			wcs := a.WeightedClusters
			var totalWeight uint32
			for _, c := range wcs.Clusters {
				w := c.GetWeight().GetValue()
				if w == 0 {
					continue
				}
				clusters[c.GetName()] = w
				totalWeight += w
			}
			if totalWeight != wcs.GetTotalWeight().GetValue() {
				return nil, fmt.Errorf("route %+v, action %+v, weights of clusters do not add up to total total weight, got: %v, want %v", r, a, wcs.GetTotalWeight().GetValue(), totalWeight)
			}
			if totalWeight == 0 {
				return nil, fmt.Errorf("route %+v, action %+v, has no valid cluster in WeightedCluster action", r, a)
			}
		case *v3routepb.RouteAction_ClusterHeader:
			continue
		}

		route.Action = clusters

		msd := action.GetMaxStreamDuration()
		// Prefer grpc_timeout_header_max, if set.
		dur := msd.GetGrpcTimeoutHeaderMax()
		if dur == nil {
			dur = msd.GetMaxStreamDuration()
		}
		if dur != nil {
			d := dur.AsDuration()
			route.MaxStreamDuration = &d
		}
		routesRet = append(routesRet, &route)
	}
	return routesRet, nil
}

// UnmarshalCluster processes resources received in an CDS response, validates
// them, and transforms them into a native struct which contains only fields we
// are interested in.
func UnmarshalCluster(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]ClusterUpdate, error) {
	update := make(map[string]ClusterUpdate)
	for _, r := range resources {
		if !IsClusterResource(r.GetTypeUrl()) {
			return nil, fmt.Errorf("xds: unexpected resource type: %q in CDS response", r.GetTypeUrl())
		}

		cluster := &v3clusterpb.Cluster{}
		if err := proto.Unmarshal(r.GetValue(), cluster); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in CDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v", cluster.GetName(), cluster, cluster)
		cu, err := validateCluster(cluster)
		if err != nil {
			return nil, err
		}

		// If the Cluster message in the CDS response did not contain a
		// serviceName, we will just use the clusterName for EDS.
		if cu.ServiceName == "" {
			cu.ServiceName = cluster.GetName()
		}
		logger.Debugf("Resource with name %v, value %+v added to cache", cluster.GetName(), cu)
		update[cluster.GetName()] = cu
	}
	return update, nil
}

func validateCluster(cluster *v3clusterpb.Cluster) (ClusterUpdate, error) {
	emptyUpdate := ClusterUpdate{ServiceName: "", EnableLRS: false}
	switch {
	case cluster.GetType() != v3clusterpb.Cluster_EDS:
		return emptyUpdate, fmt.Errorf("xds: unexpected cluster type %v in response: %+v", cluster.GetType(), cluster)
	case cluster.GetEdsClusterConfig().GetEdsConfig().GetAds() == nil:
		return emptyUpdate, fmt.Errorf("xds: unexpected edsConfig in response: %+v", cluster)
	case cluster.GetLbPolicy() != v3clusterpb.Cluster_ROUND_ROBIN:
		return emptyUpdate, fmt.Errorf("xds: unexpected lbPolicy %v in response: %+v", cluster.GetLbPolicy(), cluster)
	}

	sc, err := securityConfigFromCluster(cluster)
	if err != nil {
		return emptyUpdate, err
	}
	return ClusterUpdate{
		ServiceName: cluster.GetEdsClusterConfig().GetServiceName(),
		EnableLRS:   cluster.GetLrsServer().GetSelf() != nil,
		SecurityCfg: sc,
		MaxRequests: circuitBreakersFromCluster(cluster),
	}, nil
}

// securityConfigFromCluster extracts the relevant security configuration from
// the received Cluster resource.
func securityConfigFromCluster(cluster *v3clusterpb.Cluster) (*SecurityConfig, error) {
	// The Cluster resource contains a `transport_socket` field, which contains
	// a oneof `typed_config` field of type `protobuf.Any`. The any proto
	// contains a marshaled representation of an `UpstreamTlsContext` message.
	ts := cluster.GetTransportSocket()
	if ts == nil {
		return nil, nil
	}
	if name := ts.GetName(); name != transportSocketName {
		return nil, fmt.Errorf("xds: transport_socket field has unexpected name: %s", name)
	}
	any := ts.GetTypedConfig()
	if any == nil || any.TypeUrl != version.V3UpstreamTLSContextURL {
		return nil, fmt.Errorf("xds: transport_socket field has unexpected typeURL: %s", any.TypeUrl)
	}
	upstreamCtx := &v3tlspb.UpstreamTlsContext{}
	if err := proto.Unmarshal(any.GetValue(), upstreamCtx); err != nil {
		return nil, fmt.Errorf("xds: failed to unmarshal UpstreamTlsContext in CDS response: %v", err)
	}
	if upstreamCtx.GetCommonTlsContext() == nil {
		return nil, errors.New("xds: UpstreamTlsContext in CDS response does not contain a CommonTlsContext")
	}

	sc, err := securityConfigFromCommonTLSContext(upstreamCtx.GetCommonTlsContext())
	if err != nil {
		return nil, err
	}
	if sc.RootInstanceName == "" {
		return nil, errors.New("security configuration on the client-side does not contain root certificate provider instance name")
	}
	return sc, nil
}

// common is expected to be not nil.
func securityConfigFromCommonTLSContext(common *v3tlspb.CommonTlsContext) (*SecurityConfig, error) {
	// The `CommonTlsContext` contains a
	// `tls_certificate_certificate_provider_instance` field of type
	// `CertificateProviderInstance`, which contains the provider instance name
	// and the certificate name to fetch identity certs.
	sc := &SecurityConfig{}
	if identity := common.GetTlsCertificateCertificateProviderInstance(); identity != nil {
		sc.IdentityInstanceName = identity.GetInstanceName()
		sc.IdentityCertName = identity.GetCertificateName()
	}

	// The `CommonTlsContext` contains a `validation_context_type` field which
	// is a oneof. We can get the values that we are interested in from two of
	// those possible values:
	//  - combined validation context:
	//    - contains a default validation context which holds the list of
	//      accepted SANs.
	//    - contains certificate provider instance configuration
	//  - certificate provider instance configuration
	//    - in this case, we do not get a list of accepted SANs.
	switch t := common.GetValidationContextType().(type) {
	case *v3tlspb.CommonTlsContext_CombinedValidationContext:
		combined := common.GetCombinedValidationContext()
		if def := combined.GetDefaultValidationContext(); def != nil {
			for _, matcher := range def.GetMatchSubjectAltNames() {
				// We only support exact matches for now.
				if exact := matcher.GetExact(); exact != "" {
					sc.AcceptedSANs = append(sc.AcceptedSANs, exact)
				}
			}
		}
		if pi := combined.GetValidationContextCertificateProviderInstance(); pi != nil {
			sc.RootInstanceName = pi.GetInstanceName()
			sc.RootCertName = pi.GetCertificateName()
		}
	case *v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance:
		pi := common.GetValidationContextCertificateProviderInstance()
		sc.RootInstanceName = pi.GetInstanceName()
		sc.RootCertName = pi.GetCertificateName()
	case nil:
		// It is valid for the validation context to be nil on the server side.
	default:
		return nil, fmt.Errorf("xds: validation context contains unexpected type: %T", t)
	}
	return sc, nil
}

// circuitBreakersFromCluster extracts the circuit breakers configuration from
// the received cluster resource. Returns nil if no CircuitBreakers or no
// Thresholds in CircuitBreakers.
func circuitBreakersFromCluster(cluster *v3clusterpb.Cluster) *uint32 {
	if !env.CircuitBreakingSupport {
		return nil
	}
	for _, threshold := range cluster.GetCircuitBreakers().GetThresholds() {
		if threshold.GetPriority() != v3corepb.RoutingPriority_DEFAULT {
			continue
		}
		maxRequestsPb := threshold.GetMaxRequests()
		if maxRequestsPb == nil {
			return nil
		}
		maxRequests := maxRequestsPb.GetValue()
		return &maxRequests
	}
	return nil
}

// UnmarshalEndpoints processes resources received in an EDS response,
// validates them, and transforms them into a native struct which contains only
// fields we are interested in.
func UnmarshalEndpoints(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]EndpointsUpdate, error) {
	update := make(map[string]EndpointsUpdate)
	for _, r := range resources {
		if !IsEndpointsResource(r.GetTypeUrl()) {
			return nil, fmt.Errorf("xds: unexpected resource type: %q in EDS response", r.GetTypeUrl())
		}

		cla := &v3endpointpb.ClusterLoadAssignment{}
		if err := proto.Unmarshal(r.GetValue(), cla); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in EDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v", cla.GetClusterName(), cla, cla)

		u, err := parseEDSRespProto(cla)
		if err != nil {
			return nil, err
		}
		update[cla.GetClusterName()] = u
	}
	return update, nil
}

func parseAddress(socketAddress *v3corepb.SocketAddress) string {
	return net.JoinHostPort(socketAddress.GetAddress(), strconv.Itoa(int(socketAddress.GetPortValue())))
}

func parseDropPolicy(dropPolicy *v3endpointpb.ClusterLoadAssignment_Policy_DropOverload) OverloadDropConfig {
	percentage := dropPolicy.GetDropPercentage()
	var (
		numerator   = percentage.GetNumerator()
		denominator uint32
	)
	switch percentage.GetDenominator() {
	case v3typepb.FractionalPercent_HUNDRED:
		denominator = 100
	case v3typepb.FractionalPercent_TEN_THOUSAND:
		denominator = 10000
	case v3typepb.FractionalPercent_MILLION:
		denominator = 1000000
	}
	return OverloadDropConfig{
		Category:    dropPolicy.GetCategory(),
		Numerator:   numerator,
		Denominator: denominator,
	}
}

func parseEndpoints(lbEndpoints []*v3endpointpb.LbEndpoint) []Endpoint {
	endpoints := make([]Endpoint, 0, len(lbEndpoints))
	for _, lbEndpoint := range lbEndpoints {
		endpoints = append(endpoints, Endpoint{
			HealthStatus: EndpointHealthStatus(lbEndpoint.GetHealthStatus()),
			Address:      parseAddress(lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()),
			Weight:       lbEndpoint.GetLoadBalancingWeight().GetValue(),
		})
	}
	return endpoints
}

func parseEDSRespProto(m *v3endpointpb.ClusterLoadAssignment) (EndpointsUpdate, error) {
	ret := EndpointsUpdate{}
	for _, dropPolicy := range m.GetPolicy().GetDropOverloads() {
		ret.Drops = append(ret.Drops, parseDropPolicy(dropPolicy))
	}
	priorities := make(map[uint32]struct{})
	for _, locality := range m.Endpoints {
		l := locality.GetLocality()
		if l == nil {
			return EndpointsUpdate{}, fmt.Errorf("EDS response contains a locality without ID, locality: %+v", locality)
		}
		lid := internal.LocalityID{
			Region:  l.Region,
			Zone:    l.Zone,
			SubZone: l.SubZone,
		}
		priority := locality.GetPriority()
		priorities[priority] = struct{}{}
		ret.Localities = append(ret.Localities, Locality{
			ID:        lid,
			Endpoints: parseEndpoints(locality.GetLbEndpoints()),
			Weight:    locality.GetLoadBalancingWeight().GetValue(),
			Priority:  priority,
		})
	}
	for i := 0; i < len(priorities); i++ {
		if _, ok := priorities[uint32(i)]; !ok {
			return EndpointsUpdate{}, fmt.Errorf("priority %v missing (with different priorities %v received)", i, priorities)
		}
	}
	return ret, nil
}
